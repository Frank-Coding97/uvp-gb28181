package playauth

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.cn/uvp-gb28181/app/openapi/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/resource"
)

var (
	// ErrOpenAPIGrantUnavailable is deliberately shared by missing rows,
	// dependency failures and malformed security projections. The external
	// controller must not turn those details into an existence oracle.
	ErrOpenAPIGrantUnavailable = errors.New("openapi grant dependency unavailable")
	ErrOpenAPIGrantDenied      = errors.New("openapi grant denied")
	ErrOpenAPIGrantExpired     = errors.New("openapi grant expired")
)

// OpenAPINodeAuthorization is the narrow, internal proof required before a
// media grant can be issued or a viewer can be bound. Implementations must
// read the durable node/runtime row through the supplied transaction. They
// must not call ZLM, consult a cache, or accept an HTTP/Hook assertion.
//
// T09 has not yet established a qualified-node pool or topology proof. The
// service therefore requires this dependency but does not provide a product
// fallback that treats an active node as media-qualified.
type OpenAPINodeAuthorization struct {
	NodeUUID  string
	BootNonce string
	Protocol  string
}

type OpenAPINodeAuthority interface {
	AuthorizeOpenAPI(context.Context, *gorm.DB, OpenAPINodeAuthorization) error
}

// OpenAPIGrantIssueRequest is built only after the live media coordinator has
// selected a qualified node and complete media tuple. The grant row remains
// the source of client/scope/device IDs and all three epochs.
type OpenAPIGrantIssueRequest struct {
	GrantID         string
	DeviceID        string
	ChannelID       string
	NodeUUID        string
	BootNonce       string
	Schema          string
	VHost           string
	App             string
	Stream          string
	MediaGeneration uint64
	Protocol        string
}

// OpenAPIGrantService owns the short durable transaction that turns a quota
// pending row into an issued v3 grant. It contains no network client and does
// not dispatch SIP, ZLM, Hook, or viewer work.
type OpenAPIGrantService struct {
	db            *gorm.DB
	signer        *Signer
	nodeAuthority OpenAPINodeAuthority
	now           func() time.Time
}

func NewOpenAPIGrantService(db *gorm.DB, signer *Signer, nodeAuthority OpenAPINodeAuthority, now func() time.Time) (*OpenAPIGrantService, error) {
	if db == nil || signer == nil || nodeAuthority == nil || now == nil {
		return nil, ErrOpenAPIGrantUnavailable
	}
	return &OpenAPIGrantService{db: db, signer: signer, nodeAuthority: nodeAuthority, now: now}, nil
}

// Issue signs and persists one existing pending quota grant. The signed token
// is returned only after the surrounding SQL transaction commits; a commit or
// grant-write failure therefore returns a zero Grant and cannot leak a token
// whose durable authorization was rolled back.
func (s *OpenAPIGrantService) Issue(ctx context.Context, request OpenAPIGrantIssueRequest) (Grant, error) {
	if err := validateOpenAPIGrantIssue(ctx, request); err != nil {
		return Grant{}, err
	}
	if s == nil || s.db == nil || s.signer == nil || s.nodeAuthority == nil || s.now == nil {
		return Grant{}, ErrOpenAPIGrantUnavailable
	}

	// This first read only discovers the client/device rows needed to acquire
	// the mandated lock order. The row is re-read and locked after all parent
	// locks; none of its values are trusted without that second read.
	var hint struct {
		ClientID int64   `gorm:"column:client_id"`
		DeviceID *string `gorm:"column:device_id"`
	}
	result := s.db.WithContext(ctx).Table((models.PlayGrant{}).TableName()).
		Select("client_id, device_id").Where("grant_id = ?", request.GrantID).Limit(2).Find(&hint)
	if result.Error != nil {
		return Grant{}, normalizeOpenAPIGrantError(result.Error)
	}
	if result.RowsAffected != 1 || hint.ClientID <= 0 || hint.DeviceID == nil || *hint.DeviceID == "" {
		return Grant{}, ErrOpenAPIGrantDenied
	}

	var issued Grant
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := s.now().UTC().Truncate(time.Microsecond)
		if now.IsZero() {
			return ErrOpenAPIGrantUnavailable
		}
		client, err := lockOpenAPIClient(tx.WithContext(ctx), hint.ClientID)
		if err != nil {
			return err
		}
		if client.Status != models.StatusActive || client.AuthEpoch <= 0 || client.OwnerDeptID == 0 {
			return ErrOpenAPIGrantDenied
		}
		scope, err := lockOpenAPIScope(tx.WithContext(ctx), client.ID, openAPIPlayScope)
		if err != nil {
			return err
		}
		if !scope.Enabled || scope.ScopeEpoch <= 0 {
			return ErrOpenAPIGrantDenied
		}
		deviceEpoch, err := loadGrantDeviceEpoch(tx.WithContext(ctx), *hint.DeviceID)
		if err != nil {
			return err
		}
		grant, err := lockOpenAPIGrant(tx.WithContext(ctx), request.GrantID)
		if err != nil {
			return err
		}
		if err := validatePendingGrant(grant, client, scope, request, *hint.DeviceID, deviceEpoch, now); err != nil {
			return err
		}
		if _, err := resource.New(tx.WithContext(ctx)).GetChannel(ctx, client.OwnerDeptID, *grant.DeviceID, *grant.ChannelID); err != nil {
			if errors.Is(err, resource.ErrResourceNotFound) {
				return ErrOpenAPIGrantDenied
			}
			return ErrOpenAPIGrantUnavailable
		}
		if err := s.nodeAuthority.AuthorizeOpenAPI(ctx, tx.WithContext(ctx), OpenAPINodeAuthorization{
			NodeUUID: request.NodeUUID, BootNonce: request.BootNonce, Protocol: request.Protocol,
		}); err != nil {
			return normalizeOpenAPIGrantError(err)
		}

		binding := OpenAPIBinding{
			GrantID: grant.GrantID, ClientID: client.ID, ClientEpoch: client.AuthEpoch,
			Scope: grant.Scope, ScopeEpoch: scope.ScopeEpoch, DeviceEpoch: deviceEpoch,
			DeviceID: *grant.DeviceID, ChannelID: *grant.ChannelID, NodeUUID: request.NodeUUID,
			BootNonce: request.BootNonce, Schema: request.Schema, VHost: request.VHost,
			App: request.App, Stream: request.Stream, MediaGeneration: request.MediaGeneration,
			Protocol: request.Protocol,
		}
		signed, err := s.signer.IssueOpenAPI(binding)
		if err != nil {
			return ErrOpenAPIGrantUnavailable
		}
		claims, err := s.signer.AuthenticateOpenAPI(signed.Token)
		if err != nil || claims.OpenAPIBinding != binding {
			return ErrOpenAPIGrantUnavailable
		}
		issuedAt := time.Unix(claims.IssuedAt, 0).UTC()
		expiresAt := time.Unix(claims.ExpiresAt, 0).UTC()
		if issuedAt.IsZero() || !expiresAt.After(issuedAt) || !now.Before(expiresAt) {
			return ErrOpenAPIGrantExpired
		}
		updates := map[string]any{
			"device_id":        binding.DeviceID,
			"channel_id":       binding.ChannelID,
			"client_epoch":     binding.ClientEpoch,
			"scope_epoch":      binding.ScopeEpoch,
			"device_epoch":     binding.DeviceEpoch,
			"node_uuid":        binding.NodeUUID,
			"boot_nonce":       binding.BootNonce,
			"schema":           binding.Schema,
			"vhost":            binding.VHost,
			"app":              binding.App,
			"stream":           binding.Stream,
			"media_generation": binding.MediaGeneration,
			"protocol":         binding.Protocol,
			"issued_at":        issuedAt,
			"expires_at":       expiresAt,
			"state":            models.GrantStateIssued,
			"reason":           "",
			"updated_at":       now,
		}
		updated := tx.Model(&models.PlayGrant{}).
			Where("grant_id = ? AND state = ?", grant.GrantID, models.GrantStatePending).
			Updates(updates)
		if updated.Error != nil || updated.RowsAffected != 1 {
			return ErrOpenAPIGrantUnavailable
		}
		issued = signed
		return nil
	})
	if err != nil {
		return Grant{}, normalizeOpenAPIGrantError(err)
	}
	return issued, nil
}

const openAPIPlayScope = "play:live:apply"

type openAPIDeviceRow struct {
	ID                    uint   `gorm:"column:id"`
	DeviceID              string `gorm:"column:device_id"`
	AccessEpoch           int64  `gorm:"column:access_epoch"`
	CleanupCompletedEpoch *int64 `gorm:"column:cleanup_completed_epoch"`
}

func lockOpenAPIClient(tx *gorm.DB, clientID int64) (models.Client, error) {
	var rows []models.Client
	query := lockedOpenAPIModel(tx, &models.Client{}, (models.Client{}).TableName())
	result := query.Where("id = ?", clientID).Limit(2).Find(&rows)
	if result.Error != nil || result.RowsAffected != 1 || len(rows) != 1 || rows[0].ID != clientID || rows[0].AK == "" {
		return models.Client{}, normalizeOpenAPIGrantError(result.Error)
	}
	return rows[0], nil
}

func lockOpenAPIScope(tx *gorm.DB, clientID int64, name string) (models.ClientScope, error) {
	var rows []models.ClientScope
	query := lockedOpenAPIModel(tx, &models.ClientScope{}, (models.ClientScope{}).TableName())
	result := query.Where("client_id = ? AND scope = ?", clientID, name).Limit(2).Find(&rows)
	if result.Error != nil || result.RowsAffected != 1 || len(rows) != 1 || rows[0].ClientID != clientID || rows[0].Scope != name {
		return models.ClientScope{}, normalizeOpenAPIGrantError(result.Error)
	}
	return rows[0], nil
}

func lockOpenAPIRootDevice(tx *gorm.DB, deviceID string) (int64, error) {
	var rows []openAPIDeviceRow
	query := lockedOpenAPITable(tx, "gb_device")
	result := query.Select("id, device_id, access_epoch").
		Where("device_id = ? AND deleted_at IS NULL", deviceID).Limit(2).Find(&rows)
	if result.Error != nil || result.RowsAffected != 1 || len(rows) != 1 || rows[0].ID == 0 || rows[0].DeviceID != deviceID || rows[0].AccessEpoch <= 0 {
		return 0, normalizeOpenAPIGrantError(result.Error)
	}
	return rows[0].AccessEpoch, nil
}

// loadGrantDeviceEpoch is the media-admission projection of the device row.
// A transfer records a new access epoch before its asynchronous cleanup has
// completed; that device must not admit a new external grant or viewer until
// the durable cleanup watermark catches up. The transfer-revocation path uses
// lockOpenAPIRootDevice and intentionally does not apply this gate.
func loadGrantDeviceEpoch(tx *gorm.DB, deviceID string) (int64, error) {
	var rows []openAPIDeviceRow
	query := lockedOpenAPITable(tx, "gb_device")
	result := query.Select("id, device_id, access_epoch, cleanup_completed_epoch").
		Where("device_id = ? AND deleted_at IS NULL", deviceID).Limit(2).Find(&rows)
	if result.Error != nil || result.RowsAffected != 1 || len(rows) != 1 || rows[0].ID == 0 || rows[0].DeviceID != deviceID || rows[0].AccessEpoch <= 0 || rows[0].CleanupCompletedEpoch == nil || *rows[0].CleanupCompletedEpoch <= 0 || *rows[0].CleanupCompletedEpoch != rows[0].AccessEpoch {
		return 0, normalizeOpenAPIGrantError(result.Error)
	}
	return rows[0].AccessEpoch, nil
}

func lockOpenAPIGrant(tx *gorm.DB, grantID string) (models.PlayGrant, error) {
	var rows []models.PlayGrant
	query := lockedOpenAPIModel(tx, &models.PlayGrant{}, (models.PlayGrant{}).TableName())
	result := query.Where("grant_id = ?", grantID).Limit(2).Find(&rows)
	if result.Error != nil || result.RowsAffected != 1 || len(rows) != 1 || rows[0].GrantID != grantID {
		return models.PlayGrant{}, normalizeOpenAPIGrantError(result.Error)
	}
	return rows[0], nil
}

func lockedOpenAPIModel(tx *gorm.DB, model any, table string) *gorm.DB {
	if tx != nil && tx.Dialector.Name() == "sqlserver" {
		return tx.Table(table + " WITH (UPDLOCK, HOLDLOCK)")
	}
	return tx.Model(model).Clauses(clause.Locking{Strength: "UPDATE"})
}

func lockedOpenAPITable(tx *gorm.DB, table string) *gorm.DB {
	if tx != nil && tx.Dialector.Name() == "sqlserver" {
		return tx.Table(table + " WITH (UPDLOCK, HOLDLOCK)")
	}
	return tx.Table(table).Clauses(clause.Locking{Strength: "UPDATE"})
}

func validatePendingGrant(grant models.PlayGrant, client models.Client, scope models.ClientScope, request OpenAPIGrantIssueRequest, deviceID string, deviceEpoch int64, now time.Time) error {
	if grant.State != models.GrantStatePending || grant.ClientID != client.ID || grant.Scope != scope.Scope || grant.DeviceID == nil || grant.ChannelID == nil || *grant.DeviceID != deviceID || *grant.DeviceID != request.DeviceID || *grant.ChannelID != request.ChannelID || *grant.DeviceID == "" || *grant.ChannelID == "" {
		return ErrOpenAPIGrantDenied
	}
	if grant.ClientEpoch != client.AuthEpoch || grant.ScopeEpoch != scope.ScopeEpoch || grant.DeviceEpoch != deviceEpoch {
		return ErrOpenAPIGrantDenied
	}
	if grant.ExpiresAt.IsZero() || !now.Before(grant.ExpiresAt) {
		return ErrOpenAPIGrantExpired
	}
	return nil
}

func validateOpenAPIGrantIssue(ctx context.Context, request OpenAPIGrantIssueRequest) error {
	if ctx == nil || !validOpenAPIUUID(request.GrantID) || !validGBID(request.DeviceID) || !validGBID(request.ChannelID) || !validOpenAPINodeUUID(request.NodeUUID) || !validOpenAPIBootNonce(request.BootNonce) || !validOpenAPIField(request.Schema, 32) || !validOpenAPIField(request.VHost, 128) || !validOpenAPIField(request.App, 64) || !validOpenAPIField(request.Stream, 255) || request.MediaGeneration == 0 || (request.Protocol != "https-flv" && request.Protocol != "wss-flv") {
		return ErrOpenAPIGrantDenied
	}
	return nil
}

func validOpenAPIUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func validOpenAPINodeUUID(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 64 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func validOpenAPIBootNonce(value string) bool {
	if len(value) != 32 || value != strings.ToLower(value) {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func normalizeOpenAPIGrantError(err error) error {
	if err == nil {
		return ErrOpenAPIGrantUnavailable
	}
	switch {
	case errors.Is(err, ErrOpenAPIGrantUnavailable):
		return ErrOpenAPIGrantUnavailable
	case errors.Is(err, ErrOpenAPIGrantDenied):
		return ErrOpenAPIGrantDenied
	case errors.Is(err, ErrOpenAPIGrantExpired):
		return ErrOpenAPIGrantExpired
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return ErrOpenAPIGrantUnavailable
	default:
		return ErrOpenAPIGrantUnavailable
	}
}
