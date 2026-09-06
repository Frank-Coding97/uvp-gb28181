package limit

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

const (
	PendingGrantTTL    = 30 * time.Second
	QuotaReleaseBatch  = 500
	PlayLiveApplyScope = "play:live:apply"
)

var (
	ErrQuotaUnavailable   = errors.New("openapi quota unavailable")
	ErrQuotaExceeded      = errors.New("openapi viewer quota exceeded")
	ErrInvalidReservation = errors.New("invalid openapi quota reservation")
)

// ReservationRequest contains only already-authorized target metadata. It is
// deliberately not an HTTP DTO and must be built after HMAC, scope, owner and
// nonce checks. A pending grant is always tied to the requested device/channel
// with client, scope and root-device rows locked in admission order. Channel
// authorization remains the admission caller's responsibility; this is not
// an unscoped placeholder.
type ReservationRequest struct {
	ClientID  int64
	Scope     string
	DeviceID  string
	ChannelID string
}

type Reservation struct {
	GrantID   string
	ExpiresAt time.Time
}

// Quota owns durable viewer reservations. The database is the source of truth;
// this service intentionally has no in-memory occupancy cache.
type Quota struct {
	db  *gorm.DB
	now func() time.Time
}

func NewQuota(db *gorm.DB, now func() time.Time) *Quota {
	if now == nil {
		now = time.Now
	}
	return &Quota{db: db, now: now}
}

// ReservePending reserves one durable grant in a short transaction. The
// transaction locks client, then scope, then target device before it counts
// and inserts. No caller callback or network operation is accepted while the
// locks are held.
func (q *Quota) ReservePending(ctx context.Context, request ReservationRequest) (Reservation, error) {
	if err := validateReservation(ctx, request); err != nil {
		return Reservation{}, err
	}
	if q == nil || q.db == nil {
		return Reservation{}, ErrQuotaUnavailable
	}
	var reservation Reservation
	err := q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		reservation, err = q.reservePendingTx(ctx, tx, request)
		return err
	})
	if err != nil {
		return Reservation{}, normalizeQuotaError(err)
	}
	return reservation, nil
}

// ReservePendingTx is the transaction-aware form used by the admission
// service. The caller must pass its already-open transaction; this helper never
// starts a nested transaction and performs no I/O outside SQL statements.
func (q *Quota) ReservePendingTx(ctx context.Context, tx *gorm.DB, request ReservationRequest) (Reservation, error) {
	if err := validateReservation(ctx, request); err != nil {
		return Reservation{}, err
	}
	if q == nil || tx == nil {
		return Reservation{}, ErrQuotaUnavailable
	}
	reservation, err := q.reservePendingTx(ctx, tx, request)
	if err != nil {
		return Reservation{}, normalizeQuotaError(err)
	}
	return reservation, nil
}

func (q *Quota) reservePendingTx(ctx context.Context, tx *gorm.DB, request ReservationRequest) (Reservation, error) {
	if q.now == nil {
		return Reservation{}, ErrQuotaUnavailable
	}
	now := q.now().UTC().Round(0)
	if now.IsZero() {
		return Reservation{}, ErrQuotaUnavailable
	}

	// Keep this order stable. Management mutations use the same client-first
	// discipline, and no lock is held across a callback or network operation.
	client, err := lockClientRow(tx.WithContext(ctx), request.ClientID)
	if err != nil {
		return Reservation{}, err
	}
	if client.Status != models.StatusActive || client.AuthEpoch <= 0 || client.ViewerQuota <= 0 {
		return Reservation{}, ErrQuotaUnavailable
	}
	scope, err := lockScopeRow(tx.WithContext(ctx), request.ClientID, request.Scope)
	if err != nil {
		return Reservation{}, err
	}
	if !scope.Enabled || scope.ScopeEpoch <= 0 {
		return Reservation{}, ErrQuotaUnavailable
	}
	deviceEpoch, err := lockDeviceRow(tx.WithContext(ctx), request.DeviceID)
	if err != nil {
		return Reservation{}, err
	}

	// Expired pending/issued rows become releasable only when no real viewer is
	// attached. A bound grant, or any pending/active/revoke_pending viewer, keeps
	// the grant occupied even if its grant state/TTL is already terminal.
	if _, err := expireReleasable(tx.WithContext(ctx), request.ClientID, now); err != nil {
		return Reservation{}, err
	}
	occupied, err := countOccupied(tx.WithContext(ctx), request.ClientID, now)
	if err != nil {
		return Reservation{}, err
	}
	if occupied >= int64(client.ViewerQuota) {
		return Reservation{}, ErrQuotaExceeded
	}

	grantID, err := uuid.NewRandom()
	if err != nil {
		return Reservation{}, ErrQuotaUnavailable
	}
	grant := models.PlayGrant{
		GrantID:     grantID.String(),
		ClientID:    request.ClientID,
		Scope:       request.Scope,
		ClientEpoch: client.AuthEpoch,
		ScopeEpoch:  scope.ScopeEpoch,
		DeviceEpoch: deviceEpoch,
		IssuedAt:    now,
		ExpiresAt:   now.Add(PendingGrantTTL),
		State:       models.GrantStatePending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	grant.DeviceID = stringPtr(request.DeviceID)
	grant.ChannelID = stringPtr(request.ChannelID)
	if err := tx.WithContext(ctx).Create(&grant).Error; err != nil {
		return Reservation{}, err
	}
	return Reservation{GrantID: grant.GrantID, ExpiresAt: grant.ExpiresAt}, nil
}

// Occupied reports durable occupancy at the supplied service clock. It is a
// read-only observation; ReservePending is the only method that makes the
// count-and-insert decision.
func (q *Quota) Occupied(ctx context.Context, clientID int64) (int64, error) {
	if ctx == nil || clientID <= 0 {
		return 0, ErrInvalidReservation
	}
	if q == nil || q.db == nil || q.now == nil {
		return 0, ErrQuotaUnavailable
	}
	now := q.now().UTC().Round(0)
	if now.IsZero() {
		return 0, ErrQuotaUnavailable
	}
	occupied, err := countOccupied(q.db.WithContext(ctx), clientID, now)
	if err != nil {
		return 0, normalizeQuotaError(err)
	}
	return occupied, nil
}

// ReleaseExpired marks expired pending grants failed and expired issued grants
// expired when no live viewer references them. It is safe to call from a
// bounded maintenance job; active/bound/revoke_pending viewers are never
// released or kicked here.
func (q *Quota) ReleaseExpired(ctx context.Context, clientID int64) (int64, error) {
	if ctx == nil || clientID <= 0 {
		return 0, ErrInvalidReservation
	}
	if q == nil || q.db == nil || q.now == nil {
		return 0, ErrQuotaUnavailable
	}
	now := q.now().UTC().Round(0)
	if now.IsZero() {
		return 0, ErrQuotaUnavailable
	}
	var released int64
	err := q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockClientRow(tx.WithContext(ctx), clientID); err != nil {
			return err
		}
		var err error
		released, err = expireReleasable(tx.WithContext(ctx), clientID, now)
		return err
	})
	if err != nil {
		return 0, normalizeQuotaError(err)
	}
	return released, nil
}

func validateReservation(ctx context.Context, request ReservationRequest) error {
	if ctx == nil || request.ClientID <= 0 || request.Scope != PlayLiveApplyScope || request.Scope != strings.TrimSpace(request.Scope) || len(request.Scope) > 64 {
		return ErrInvalidReservation
	}
	if request.DeviceID == "" || request.ChannelID == "" || len(request.DeviceID) > 20 || request.DeviceID != strings.TrimSpace(request.DeviceID) || len(request.ChannelID) > 20 || request.ChannelID != strings.TrimSpace(request.ChannelID) {
		return ErrInvalidReservation
	}
	return nil
}

func normalizeQuotaError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrQuotaExceeded), errors.Is(err, ErrInvalidReservation), errors.Is(err, ErrQuotaUnavailable):
		return err
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return ErrQuotaUnavailable
	}
}

func lockClientRow(tx *gorm.DB, clientID int64) (models.Client, error) {
	var client models.Client
	query := lockedModel(tx, &models.Client{}, "sys_openapi_client")
	result := query.Where("id = ?", clientID).Take(&client)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return models.Client{}, ErrQuotaUnavailable
		}
		return models.Client{}, result.Error
	}
	if result.RowsAffected != 1 || client.ID != clientID || client.ID <= 0 || client.AK == "" {
		return models.Client{}, ErrQuotaUnavailable
	}
	return client, nil
}

func lockScopeRow(tx *gorm.DB, clientID int64, scopeName string) (models.ClientScope, error) {
	var scope models.ClientScope
	query := lockedModel(tx, &models.ClientScope{}, "sys_openapi_client_scope")
	result := query.Where("client_id = ? AND scope = ?", clientID, scopeName).Take(&scope)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return models.ClientScope{}, ErrQuotaUnavailable
		}
		return models.ClientScope{}, result.Error
	}
	if result.RowsAffected != 1 || scope.ClientID != clientID || scope.Scope != scopeName || scope.Scope == "" {
		return models.ClientScope{}, ErrQuotaUnavailable
	}
	return scope, nil
}

func lockDeviceRow(tx *gorm.DB, deviceID string) (int64, error) {
	query := lockedTable(tx, "gb_device")
	var row struct {
		ID          uint   `gorm:"column:id"`
		DeviceID    string `gorm:"column:device_id"`
		AccessEpoch int64  `gorm:"column:access_epoch"`
	}
	result := query.Select("id, device_id, access_epoch").Where("device_id = ? AND deleted_at IS NULL", deviceID).Take(&row)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return 0, ErrQuotaUnavailable
		}
		return 0, result.Error
	}
	if result.RowsAffected != 1 || row.ID == 0 || row.DeviceID != deviceID || row.AccessEpoch <= 0 {
		return 0, ErrQuotaUnavailable
	}
	return row.AccessEpoch, nil
}

func lockedModel(tx *gorm.DB, model any, table string) *gorm.DB {
	if tx.Dialector.Name() == "sqlserver" {
		return tx.Table(table + " WITH (UPDLOCK, HOLDLOCK)")
	}
	return tx.Model(model).Clauses(clause.Locking{Strength: "UPDATE"})
}

func lockedTable(tx *gorm.DB, table string) *gorm.DB {
	if tx.Dialector.Name() == "sqlserver" {
		return tx.Table(table + " WITH (UPDLOCK, HOLDLOCK)")
	}
	return tx.Table(table).Clauses(clause.Locking{Strength: "UPDATE"})
}

func expireReleasable(tx *gorm.DB, clientID int64, now time.Time) (int64, error) {
	live := liveViewerSubquery(tx)
	var candidates []struct {
		GrantID string            `gorm:"column:grant_id"`
		State   models.GrantState `gorm:"column:state"`
	}
	if err := tx.Model(&models.PlayGrant{}).
		Select("grant_id, state").
		Where("client_id = ? AND state IN ? AND expires_at <= ?", clientID, []models.GrantState{models.GrantStatePending, models.GrantStateIssued}, now).
		Where("NOT EXISTS (?)", live).
		Order("grant_id ASC").
		Limit(QuotaReleaseBatch).
		Find(&candidates).Error; err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	pendingIDs := make([]string, 0, len(candidates))
	issuedIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		switch candidate.State {
		case models.GrantStatePending:
			pendingIDs = append(pendingIDs, candidate.GrantID)
		case models.GrantStateIssued:
			issuedIDs = append(issuedIDs, candidate.GrantID)
		}
	}
	var released int64
	if len(pendingIDs) > 0 {
		result := tx.Model(&models.PlayGrant{}).
			Where("client_id = ? AND grant_id IN ? AND state = ? AND expires_at <= ?", clientID, pendingIDs, models.GrantStatePending, now).
			Where("NOT EXISTS (?)", liveViewerSubquery(tx)).
			Updates(map[string]any{"state": models.GrantStateFailed, "reason": "pending_timeout", "updated_at": now})
		if result.Error != nil {
			return released, result.Error
		}
		released += result.RowsAffected
	}
	if len(issuedIDs) > 0 {
		result := tx.Model(&models.PlayGrant{}).
			Where("client_id = ? AND grant_id IN ? AND state = ? AND expires_at <= ?", clientID, issuedIDs, models.GrantStateIssued, now).
			Where("NOT EXISTS (?)", liveViewerSubquery(tx)).
			Updates(map[string]any{"state": models.GrantStateExpired, "reason": "issued_timeout", "updated_at": now})
		if result.Error != nil {
			return released, result.Error
		}
		released += result.RowsAffected
	}
	return released, nil
}

func liveViewerSubquery(tx *gorm.DB) *gorm.DB {
	return tx.Model(&models.Viewer{}).
		Select("1").
		Where("gb_openapi_viewer.grant_id = gb_openapi_play_grant.grant_id").
		Where("state IN ?", []models.ViewerState{models.ViewerStatePending, models.ViewerStateActive, models.ViewerStateRevokePending})
}

func countOccupied(tx *gorm.DB, clientID int64, now time.Time) (int64, error) {
	live := liveViewerSubquery(tx)
	var occupied int64
	err := tx.Model(&models.PlayGrant{}).
		Where("client_id = ?", clientID).
		Where(tx.Where("state IN ? AND expires_at > ?", []models.GrantState{models.GrantStatePending, models.GrantStateIssued}, now).
			Or("state = ?", models.GrantStateBound).
			Or("EXISTS (?)", live)).
		Count(&occupied).Error
	return occupied, err
}

func stringPtr(value string) *string { return &value }
