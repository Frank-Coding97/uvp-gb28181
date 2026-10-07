package play

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/openapi/models"
)

const (
	playLiveScope         = "play:live"
	pendingReservationTTL = 30 * time.Second
)

var (
	ErrQuotaExceeded = errors.New("openapi viewer quota exceeded")
	ErrQuotaDenied   = errors.New("openapi viewer quota denied")
	ErrQuotaFailed   = errors.New("openapi viewer quota unavailable")
)

type ReservationRequest struct {
	ClientID  int64
	DeviceID  string
	ChannelID string
}

type Reservation struct {
	GrantID     string
	DeviceEpoch int64
}

// ViewerRuntimeTarget is the immutable media tuple persisted when ZLM admits
// an OpenAPI viewer. Reconciliation never infers identity from a stream alone.
type ViewerRuntimeTarget struct {
	NodeUUID string
	Schema   string
	VHost    string
	App      string
	Stream   string
}

type ViewerRuntimeSnapshot struct {
	BootNonce string
	// TargetAuthoritative is set only by a fresh query for the complete media
	// tuple when the node cannot supply a boot-bound snapshot.
	TargetAuthoritative bool
	Identifiers         map[string]struct{}
}

type ViewerRuntimeSnapshotter interface {
	SnapshotViewerRuntime(context.Context, ViewerRuntimeTarget) (ViewerRuntimeSnapshot, error)
}

// ViewerQuota is the internal durable lease manager for unified live playback.
// It deliberately exposes no HTTP route and does not issue bearer credentials.
type ViewerQuota struct {
	db  *gorm.DB
	now func() time.Time
}

func NewViewerQuota(db *gorm.DB, now func() time.Time) *ViewerQuota {
	return &ViewerQuota{db: db, now: now}
}

func (q *ViewerQuota) Reserve(ctx context.Context, req ReservationRequest) (Reservation, error) {
	if q == nil || q.db == nil || q.now == nil || ctx == nil || req.ClientID <= 0 || !validGBID(req.DeviceID) || !validGBID(req.ChannelID) {
		return Reservation{}, ErrQuotaDenied
	}
	now, err := q.currentTime()
	if err != nil {
		return Reservation{}, err
	}
	var reservation Reservation
	err = q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		client, err := lockClient(tx, req.ClientID)
		if err != nil {
			return err
		}
		if client.Status != models.StatusActive || client.AuthEpoch <= 0 || client.ViewerQuota <= 0 {
			return ErrQuotaDenied
		}
		scope, err := lockScope(tx, req.ClientID)
		if err != nil || !scope.Enabled || scope.ScopeEpoch <= 0 {
			return ErrQuotaDenied
		}
		deviceEpoch, err := lockDeviceEpoch(tx, req.DeviceID)
		if err != nil {
			return err
		}
		if err := expirePending(tx, req.ClientID, now); err != nil {
			return err
		}
		occupied, err := countOccupied(tx, req.ClientID, now)
		if err != nil {
			return err
		}
		if occupied >= int64(client.ViewerQuota) {
			return ErrQuotaExceeded
		}
		grantID, err := uuid.NewRandom()
		if err != nil {
			return ErrQuotaFailed
		}
		grant := models.PlayGrant{
			GrantID: grantID.String(), ClientID: req.ClientID, Scope: playLiveScope,
			DeviceID: stringPtr(req.DeviceID), ChannelID: stringPtr(req.ChannelID),
			ClientEpoch: client.AuthEpoch, ScopeEpoch: scope.ScopeEpoch, DeviceEpoch: deviceEpoch,
			IssuedAt: now, ExpiresAt: now.Add(pendingReservationTTL), State: models.GrantStatePending,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&grant).Error; err != nil {
			return ErrQuotaFailed
		}
		reservation = Reservation{GrantID: grant.GrantID, DeviceEpoch: grant.DeviceEpoch}
		return nil
	})
	if err != nil {
		return Reservation{}, normalizeQuotaError(err)
	}
	return reservation, nil
}

func (q *ViewerQuota) Fail(ctx context.Context, clientID int64, grantID, reason string) error {
	if q == nil || q.db == nil || q.now == nil || ctx == nil || clientID <= 0 || !validUUID(grantID) {
		return ErrQuotaDenied
	}
	now, err := q.currentTime()
	if err != nil {
		return err
	}
	err = q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockClient(tx, clientID); err != nil {
			return err
		}
		grant, err := lockGrant(tx, grantID)
		if err != nil || grant.ClientID != clientID {
			return ErrQuotaDenied
		}
		if grant.State == models.GrantStateFailed || grant.State == models.GrantStateExpired {
			return nil
		}
		if grant.State != models.GrantStatePending && grant.State != models.GrantStateIssued {
			return ErrQuotaDenied
		}
		result := tx.Model(&models.PlayGrant{}).
			Where("grant_id = ? AND client_id = ? AND state = ?", grantID, clientID, grant.State).
			Updates(map[string]any{"state": models.GrantStateFailed, "reason": boundedReason(reason), "updated_at": now})
		if result.Error != nil || result.RowsAffected != 1 {
			return ErrQuotaFailed
		}
		return nil
	})
	return normalizeQuotaError(err)
}

func (q *ViewerQuota) BindViewer(ctx context.Context, claims playauth.Claims, binding playauth.OpenAPIViewerBinding) error {
	if q == nil || q.db == nil || q.now == nil || ctx == nil || !validClaimsBinding(claims, binding) {
		return ErrQuotaDenied
	}
	now, err := q.currentTime()
	if err != nil {
		return err
	}
	err = q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		client, err := lockClient(tx, claims.OpenAPIClientID)
		if err != nil {
			return err
		}
		if client.Status != models.StatusActive || client.AuthEpoch <= 0 {
			return ErrQuotaDenied
		}
		scope, err := lockScope(tx, client.ID)
		if err != nil || !scope.Enabled || scope.ScopeEpoch <= 0 {
			return ErrQuotaDenied
		}
		deviceEpoch, err := lockDeviceEpoch(tx, claims.DeviceID)
		if err != nil {
			return err
		}
		grant, err := lockGrant(tx, claims.OpenAPIGrantID)
		if err != nil || !grantMatchesAuthority(grant, client, scope, claims, deviceEpoch) {
			return ErrQuotaDenied
		}
		existing, exists, err := lockViewerByGrant(tx, grant.GrantID)
		if err != nil {
			return err
		}
		switch grant.State {
		case models.GrantStatePending:
			if exists || !now.Before(grant.ExpiresAt) {
				return ErrQuotaDenied
			}
			viewer := models.Viewer{
				GrantID: grant.GrantID, NodeUUID: binding.NodeUUID, BootNonce: binding.BootNonce,
				Identifier: binding.Identifier, Schema: binding.Schema, VHost: binding.VHost,
				App: binding.App, Stream: binding.Stream, MediaGeneration: binding.MediaGeneration,
				State: models.ViewerStateActive, LastSeenAt: &now, CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&viewer).Error; err != nil {
				return ErrQuotaFailed
			}
			updates := map[string]any{
				"state": models.GrantStateBound, "node_uuid": binding.NodeUUID, "boot_nonce": binding.BootNonce,
				"schema": binding.Schema, "vhost": binding.VHost, "app": binding.App, "stream": binding.Stream,
				"media_generation": binding.MediaGeneration, "protocol": binding.Protocol, "updated_at": now,
			}
			result := tx.Model(&models.PlayGrant{}).Where("grant_id = ? AND state = ?", grant.GrantID, models.GrantStatePending).Updates(updates)
			if result.Error != nil || result.RowsAffected != 1 {
				return ErrQuotaFailed
			}
			return nil
		case models.GrantStateBound:
			if !exists || existing.State != models.ViewerStateActive || !sameViewer(existing, binding) || !grantMatchesViewer(grant, binding) {
				return ErrQuotaDenied
			}
			result := tx.Model(&models.Viewer{}).Where("id = ? AND state = ?", existing.ID, models.ViewerStateActive).Updates(map[string]any{"last_seen_at": now, "updated_at": now})
			if result.Error != nil || result.RowsAffected != 1 {
				return ErrQuotaFailed
			}
			return nil
		default:
			return ErrQuotaDenied
		}
	})
	return normalizeQuotaError(err)
}

func (q *ViewerQuota) CloseViewer(ctx context.Context, report playauth.OpenAPIFlowReport) error {
	if q == nil || q.db == nil || q.now == nil || ctx == nil || !report.Player || !validFlowReport(report) {
		if report.Player {
			return ErrQuotaDenied
		}
		return nil
	}
	now, err := q.currentTime()
	if err != nil {
		return err
	}
	var hint struct {
		GrantID  string `gorm:"column:grant_id"`
		ClientID int64  `gorm:"column:client_id"`
	}
	result := q.db.WithContext(ctx).Table("gb_openapi_viewer AS v").
		Select("v.grant_id, g.client_id").Joins("JOIN gb_openapi_play_grant AS g ON g.grant_id = v.grant_id").
		Where("v.node_uuid = ? AND v.boot_nonce = ? AND v.identifier = ?", report.NodeUUID, report.BootNonce, report.Identifier).
		Limit(2).Find(&hint)
	if result.Error != nil {
		return ErrQuotaFailed
	}
	if result.RowsAffected == 0 {
		return nil
	}
	if result.RowsAffected != 1 || hint.ClientID <= 0 || !validUUID(hint.GrantID) {
		return ErrQuotaDenied
	}
	err = q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockClient(tx, hint.ClientID); err != nil {
			return err
		}
		grant, err := lockGrant(tx, hint.GrantID)
		if err != nil || grant.ClientID != hint.ClientID {
			return ErrQuotaDenied
		}
		viewer, exists, err := lockViewerByGrant(tx, grant.GrantID)
		if err != nil || !exists {
			return err
		}
		if !flowMatches(viewer, grant, report) {
			return ErrQuotaDenied
		}
		if viewer.State == models.ViewerStateClosed {
			return nil
		}
		if viewer.State != models.ViewerStateActive {
			return ErrQuotaDenied
		}
		updated := tx.Model(&models.Viewer{}).Where("id = ? AND state = ?", viewer.ID, models.ViewerStateActive).
			Updates(map[string]any{"state": models.ViewerStateClosed, "last_seen_at": now, "updated_at": now})
		if updated.Error != nil || updated.RowsAffected != 1 {
			return ErrQuotaFailed
		}
		return nil
	})
	return normalizeQuotaError(err)
}

// ReconcileViewers repairs the only gap that on_flow_report cannot cover: the
// backend may be down while a player disconnects. Absence is authoritative
// only for the same media-server boot or a fresh query of the complete media
// tuple that admitted the persisted viewer.
func (q *ViewerQuota) ReconcileViewers(ctx context.Context, runtime ViewerRuntimeSnapshotter, limit int) (int, error) {
	if q == nil || q.db == nil || q.now == nil || ctx == nil || runtime == nil || limit < 1 || limit > 1000 {
		return 0, ErrQuotaDenied
	}
	now, err := q.currentTime()
	if err != nil {
		return 0, err
	}
	var viewers []models.Viewer
	if err := q.db.WithContext(ctx).Where("state = ?", models.ViewerStateActive).Order("id ASC").Limit(limit).Find(&viewers).Error; err != nil {
		return 0, ErrQuotaFailed
	}
	closed := 0
	for _, viewer := range viewers {
		snapshot, err := runtime.SnapshotViewerRuntime(ctx, ViewerRuntimeTarget{
			NodeUUID: viewer.NodeUUID, Schema: viewer.Schema, VHost: viewer.VHost, App: viewer.App, Stream: viewer.Stream,
		})
		if err != nil {
			return closed, ErrQuotaFailed
		}
		if snapshot.BootNonce != viewer.BootNonce && (snapshot.BootNonce != "" || !snapshot.TargetAuthoritative) {
			continue
		}
		_, present := snapshot.Identifiers[viewer.Identifier]
		updates := map[string]any{"last_seen_at": now, "updated_at": now}
		if !present {
			updates["state"] = models.ViewerStateClosed
		}
		result := q.db.WithContext(ctx).Model(&models.Viewer{}).
			Where("id = ? AND grant_id = ? AND state = ? AND updated_at = ?", viewer.ID, viewer.GrantID, models.ViewerStateActive, viewer.UpdatedAt).
			Updates(updates)
		if result.Error != nil {
			return closed, ErrQuotaFailed
		}
		if !present && result.RowsAffected == 1 {
			closed++
		}
	}
	return closed, nil
}

func (q *ViewerQuota) currentTime() (time.Time, error) {
	now := q.now().UTC().Truncate(time.Microsecond)
	if now.IsZero() {
		return time.Time{}, ErrQuotaFailed
	}
	return now, nil
}

func lockClient(tx *gorm.DB, clientID int64) (models.Client, error) {
	var client models.Client
	result := lockedModel(tx, &models.Client{}, "sys_openapi_client").Where("id = ?", clientID).Take(&client)
	if result.Error != nil || client.ID != clientID {
		return models.Client{}, ErrQuotaFailed
	}
	return client, nil
}

func lockScope(tx *gorm.DB, clientID int64) (models.ClientScope, error) {
	var scope models.ClientScope
	result := lockedModel(tx, &models.ClientScope{}, "sys_openapi_client_scope").Where("client_id = ? AND scope = ?", clientID, playLiveScope).Take(&scope)
	if result.Error != nil || scope.ClientID != clientID || scope.Scope != playLiveScope {
		return models.ClientScope{}, ErrQuotaFailed
	}
	return scope, nil
}

func lockDeviceEpoch(tx *gorm.DB, deviceID string) (int64, error) {
	var row struct {
		ID                    uint   `gorm:"column:id"`
		DeviceID              string `gorm:"column:device_id"`
		AccessEpoch           int64  `gorm:"column:access_epoch"`
		CleanupCompletedEpoch int64  `gorm:"column:cleanup_completed_epoch"`
	}
	result := lockedTable(tx, "gb_device").Select("id, device_id, access_epoch, cleanup_completed_epoch").Where("device_id = ? AND deleted_at IS NULL", deviceID).Take(&row)
	if result.Error != nil || row.ID == 0 || row.DeviceID != deviceID || row.AccessEpoch <= 0 || row.CleanupCompletedEpoch != row.AccessEpoch {
		return 0, ErrQuotaDenied
	}
	return row.AccessEpoch, nil
}

func lockGrant(tx *gorm.DB, grantID string) (models.PlayGrant, error) {
	var grant models.PlayGrant
	result := lockedModel(tx, &models.PlayGrant{}, "gb_openapi_play_grant").Where("grant_id = ?", grantID).Take(&grant)
	if result.Error != nil || grant.GrantID != grantID {
		return models.PlayGrant{}, ErrQuotaDenied
	}
	return grant, nil
}

func lockViewerByGrant(tx *gorm.DB, grantID string) (models.Viewer, bool, error) {
	var viewers []models.Viewer
	result := lockedModel(tx, &models.Viewer{}, "gb_openapi_viewer").Where("grant_id = ?", grantID).Limit(2).Find(&viewers)
	if result.Error != nil || len(viewers) > 1 {
		return models.Viewer{}, false, ErrQuotaFailed
	}
	if len(viewers) == 0 {
		return models.Viewer{}, false, nil
	}
	return viewers[0], true, nil
}

func lockedModel(tx *gorm.DB, model any, table string) *gorm.DB {
	if tx.Name() == "sqlserver" {
		return tx.Table(table + " WITH (UPDLOCK, HOLDLOCK)")
	}
	return tx.Model(model).Clauses(clause.Locking{Strength: "UPDATE"})
}

func lockedTable(tx *gorm.DB, table string) *gorm.DB {
	if tx.Name() == "sqlserver" {
		return tx.Table(table + " WITH (UPDLOCK, HOLDLOCK)")
	}
	return tx.Table(table).Clauses(clause.Locking{Strength: "UPDATE"})
}

func expirePending(tx *gorm.DB, clientID int64, now time.Time) error {
	result := tx.Model(&models.PlayGrant{}).
		Where("client_id = ? AND state IN ? AND expires_at <= ?", clientID, []models.GrantState{models.GrantStatePending, models.GrantStateIssued}, now).
		Where("NOT EXISTS (?)", tx.Model(&models.Viewer{}).Select("1").Where("gb_openapi_viewer.grant_id = gb_openapi_play_grant.grant_id AND state IN ?", []models.ViewerState{models.ViewerStatePending, models.ViewerStateActive, models.ViewerStateRevokePending})).
		Updates(map[string]any{"state": models.GrantStateExpired, "reason": "reservation_timeout", "updated_at": now})
	if result.Error != nil {
		return ErrQuotaFailed
	}
	return nil
}

func countOccupied(tx *gorm.DB, clientID int64, now time.Time) (int64, error) {
	live := tx.Model(&models.Viewer{}).Select("1").Where("gb_openapi_viewer.grant_id = gb_openapi_play_grant.grant_id AND state IN ?", []models.ViewerState{models.ViewerStatePending, models.ViewerStateActive, models.ViewerStateRevokePending})
	var occupied int64
	err := tx.Model(&models.PlayGrant{}).Where("client_id = ?", clientID).
		Where(tx.Where("state IN ? AND expires_at > ?", []models.GrantState{models.GrantStatePending, models.GrantStateIssued}, now).Or("EXISTS (?)", live)).
		Count(&occupied).Error
	if err != nil {
		return 0, ErrQuotaFailed
	}
	return occupied, nil
}

func validClaimsBinding(claims playauth.Claims, binding playauth.OpenAPIViewerBinding) bool {
	return claims.OpenAPIClientID > 0 && validUUID(claims.OpenAPIGrantID) && claims.DeviceEpoch > 0 && validGBID(claims.DeviceID) && validGBID(claims.ChannelID) &&
		claims.MediaServerID == binding.NodeUUID && claims.App == binding.App && claims.Stream == binding.Stream && claims.MediaGeneration == binding.MediaGeneration &&
		validViewerBinding(binding)
}

func validViewerBinding(binding playauth.OpenAPIViewerBinding) bool {
	return validField(binding.NodeUUID, 64) && validBootNonce(binding.BootNonce) && validField(binding.Identifier, 128) &&
		validField(binding.Protocol, 16) && validField(binding.Schema, 32) && validField(binding.VHost, 128) &&
		validField(binding.App, 64) && validField(binding.Stream, 255) && binding.MediaGeneration > 0
}

func validFlowReport(report playauth.OpenAPIFlowReport) bool {
	return validField(report.NodeUUID, 64) && validBootNonce(report.BootNonce) && validField(report.Identifier, 128) &&
		validField(report.Protocol, 16) && validField(report.Schema, 32) && validField(report.VHost, 128) &&
		validField(report.App, 64) && validField(report.Stream, 255)
}

func validField(value string, max int) bool {
	if value == "" || len(value) > max || strings.TrimSpace(value) != value || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func validBootNonce(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func grantMatchesAuthority(grant models.PlayGrant, client models.Client, scope models.ClientScope, claims playauth.Claims, deviceEpoch int64) bool {
	return grant.ClientID == client.ID && grant.Scope == playLiveScope && grant.ClientEpoch == client.AuthEpoch && grant.ScopeEpoch == scope.ScopeEpoch &&
		grant.DeviceEpoch == deviceEpoch && grant.DeviceEpoch == claims.DeviceEpoch && deref(grant.DeviceID) == claims.DeviceID && deref(grant.ChannelID) == claims.ChannelID
}

func sameViewer(viewer models.Viewer, binding playauth.OpenAPIViewerBinding) bool {
	return viewer.NodeUUID == binding.NodeUUID && viewer.BootNonce == binding.BootNonce && viewer.Identifier == binding.Identifier &&
		viewer.Schema == binding.Schema && viewer.VHost == binding.VHost && viewer.App == binding.App && viewer.Stream == binding.Stream && viewer.MediaGeneration == binding.MediaGeneration
}

func grantMatchesViewer(grant models.PlayGrant, binding playauth.OpenAPIViewerBinding) bool {
	return deref(grant.NodeUUID) == binding.NodeUUID && deref(grant.BootNonce) == binding.BootNonce && deref(grant.Schema) == binding.Schema &&
		deref(grant.VHost) == binding.VHost && deref(grant.App) == binding.App && deref(grant.Stream) == binding.Stream &&
		grant.MediaGeneration != nil && *grant.MediaGeneration == binding.MediaGeneration && deref(grant.Protocol) == binding.Protocol
}

func flowMatches(viewer models.Viewer, grant models.PlayGrant, report playauth.OpenAPIFlowReport) bool {
	return viewer.NodeUUID == report.NodeUUID && viewer.BootNonce == report.BootNonce && viewer.Identifier == report.Identifier &&
		viewer.Schema == report.Schema && viewer.VHost == report.VHost && viewer.App == report.App && viewer.Stream == report.Stream &&
		deref(grant.Protocol) == report.Protocol && grantMatchesViewer(grant, playauth.OpenAPIViewerBinding{
		NodeUUID: report.NodeUUID, BootNonce: report.BootNonce, Identifier: report.Identifier, Protocol: report.Protocol,
		Schema: report.Schema, VHost: report.VHost, App: report.App, Stream: report.Stream, MediaGeneration: viewer.MediaGeneration,
	})
}

func normalizeQuotaError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrQuotaExceeded), errors.Is(err, ErrQuotaDenied), errors.Is(err, ErrQuotaFailed):
		return err
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return ErrQuotaFailed
	default:
		return ErrQuotaFailed
	}
}

func validGBID(value string) bool {
	if len(value) != 20 {
		return false
	}
	for i := range value {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func validUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func boundedReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 64 {
		return "play_failed"
	}
	return reason
}

func stringPtr(value string) *string { return &value }
func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
