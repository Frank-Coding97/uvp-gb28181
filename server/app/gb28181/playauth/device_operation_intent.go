package playauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math"
	"time"

	"gorm.io/gorm"
)

const (
	IntentReserved = "reserved"
	// Dispatched means the operation MAY have produced external side effects.
	// Neither restart nor a missing response makes this state safe to cancel.
	IntentDispatched = "dispatched"
	IntentCancelled  = "cancelled"
)

var (
	ErrDeviceIntentInvalid     = errors.New("invalid device operation intent")
	ErrDeviceIntentUnavailable = errors.New("device operation intent unavailable")
	ErrDeviceIntentConflict    = errors.New("device operation intent conflict")
	ErrDeviceIntentRevoked     = errors.New("device operation intent revoked")
)

// DeviceOperationIntentIdentity is immutable, internal authorization metadata.
// The operation owner generates a fresh CSPRNG 128-bit lowercase hex ID and
// passes its ORIGINAL authorization snapshot, never a refreshed device epoch.
type DeviceOperationIntentIdentity struct {
	OperationID string `gorm:"primaryKey;column:operation_id" json:"-"`
	DevicePK    int64  `json:"-"`
	DeviceCode  string `json:"-"`
	DeviceEpoch int64  `json:"-"`
	TargetScope string `json:"-"`
	TargetPK    int64  `json:"-"`
	TargetCode  string `json:"-"`
	Kind        string `json:"-"`
}

type DeviceOperationIntent struct {
	DeviceOperationIntentIdentity `gorm:"embedded" json:"-"`
	ContractVersion               int64      `json:"-"`
	State                         string     `json:"-"`
	RowVersion                    int64      `json:"-"`
	CreatedAt                     time.Time  `json:"-"`
	UpdatedAt                     time.Time  `json:"-"`
	DispatchStartedAt             *time.Time `json:"-"`
	CancelledAt                   *time.Time `json:"-"`
}

func (DeviceOperationIntent) TableName() string { return "gb_device_operation_intent" }

func NewDeviceOperationIntentID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", ErrDeviceIntentUnavailable
	}
	return hex.EncodeToString(raw[:]), nil
}

// DeviceOperationIntentStore only provides durable reservation/dispatch CAS.
// It performs no I/O, recovery dispatch, resource closure or coverage inference.
// Production wiring additionally needs the shared operation lease and durable
// resource-step identities; this store alone is NOT safe media recovery.
type DeviceOperationIntentStore struct {
	db        *gorm.DB
	authority deviceIntentAuthority
}

// NewDeviceOperationIntentStore permits reads, observations and preparation
// without dispatch permission. Only the root-authorized constructor can send.
func NewDeviceOperationIntentStore(db *gorm.DB) *DeviceOperationIntentStore {
	return newDeviceOperationIntentStore(db, nil)
}

func (s *DeviceOperationIntentStore) available(ctx context.Context) bool {
	return s != nil && s.db != nil && ctx != nil && ctx.Err() == nil
}

// Reserve commits before the caller may try Dispatch. Returning an existing
// row is idempotent observation, never permission to repeat external effects.
func (s *DeviceOperationIntentStore) Reserve(ctx context.Context, id DeviceOperationIntentIdentity) (DeviceOperationIntent, error) {
	if !s.available(ctx) {
		return DeviceOperationIntent{}, ErrDeviceIntentUnavailable
	}
	if !validIntentIdentity(id) {
		return DeviceOperationIntent{}, ErrDeviceIntentInvalid
	}
	var out DeviceOperationIntent
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeIntentDevice(tx, ctx, id); err != nil {
			return err
		}
		var rows []DeviceOperationIntent
		if err := tx.Where("operation_id = ?", id.OperationID).Limit(1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 1 {
			if !validIntentRow(rows[0]) {
				return ErrDeviceIntentUnavailable
			}
			if rows[0].DeviceOperationIntentIdentity != id {
				return ErrDeviceIntentConflict
			}
			out = rows[0]
			return nil
		}
		now := time.Now().UTC()
		out = DeviceOperationIntent{DeviceOperationIntentIdentity: id, ContractVersion: 1,
			State: IntentReserved, RowVersion: 1, CreatedAt: now, UpdatedAt: now}
		return tx.Create(&out).Error
	})
	if err != nil {
		return DeviceOperationIntent{}, normalizeIntentError(err)
	}
	return out, nil
}

// Dispatch returns success only to the one CAS winner after confirmed commit.
// Commit failure/unknown and repeat calls grant NO dispatch permission. Callers
// must retain the device operation lease through all effects and compensation.
func (s *DeviceOperationIntentStore) Dispatch(ctx context.Context, id DeviceOperationIntentIdentity, version int64) (DeviceOperationIntent, error) {
	if !s.available(ctx) {
		return DeviceOperationIntent{}, ErrDeviceIntentUnavailable
	}
	if !validIntentIdentity(id) || version <= 0 || version == math.MaxInt64 {
		return DeviceOperationIntent{}, ErrDeviceIntentInvalid
	}
	var out DeviceOperationIntent
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.checkAuthorityTx(tx); err != nil {
			return err
		}
		if err := authorizeIntentDevice(tx, ctx, id); err != nil {
			return err
		}
		var rows []DeviceOperationIntent
		if err := tx.Where("operation_id = ?", id.OperationID).Limit(1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 1 {
			return ErrDeviceIntentConflict
		}
		out = rows[0]
		if !validIntentRow(out) {
			return ErrDeviceIntentUnavailable
		}
		if out.DeviceOperationIntentIdentity != id || out.RowVersion != version || out.State != IntentReserved {
			return ErrDeviceIntentConflict
		}
		now := time.Now().UTC()
		result := tx.Model(&DeviceOperationIntent{}).
			Where("operation_id = ? AND state = ? AND row_version = ? AND contract_version = 1", id.OperationID, IntentReserved, version).
			Updates(map[string]any{"state": IntentDispatched, "row_version": version + 1, "updated_at": now, "dispatch_started_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDeviceIntentConflict
		}
		out.State, out.RowVersion, out.UpdatedAt = IntentDispatched, version+1, now
		out.DispatchStartedAt = &now
		return nil
	})
	if err != nil {
		return DeviceOperationIntent{}, normalizeIntentError(err)
	}
	return out, nil
}

// CancelReserved is a one-way CAS for an intent with NO dispatch permission.
// It intentionally refuses dispatched intents, including after API restart.
func (s *DeviceOperationIntentStore) CancelReserved(ctx context.Context, operationID string, version int64) error {
	if !s.available(ctx) {
		return ErrDeviceIntentUnavailable
	}
	if !validIntentID(operationID) || version <= 0 || version == math.MaxInt64 {
		return ErrDeviceIntentInvalid
	}
	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&DeviceOperationIntent{}).
		Where("operation_id = ? AND state = ? AND row_version = ? AND contract_version = 1", operationID, IntentReserved, version).
		Updates(map[string]any{"state": IntentCancelled, "row_version": version + 1, "updated_at": now, "cancelled_at": now})
	if result.Error != nil {
		return ErrDeviceIntentUnavailable
	}
	if result.RowsAffected != 1 {
		return ErrDeviceIntentConflict
	}
	return nil
}

// ListUnsettled uses keyset pagination. An empty result carries NO historical
// coverage or device-completion evidence. Recovery must never dispatch rows.
func (s *DeviceOperationIntentStore) ListUnsettled(ctx context.Context, pk int64, code string, beforeEpoch int64, afterID string, limit int) ([]DeviceOperationIntent, error) {
	if !s.available(ctx) {
		return nil, ErrDeviceIntentUnavailable
	}
	if pk <= 0 || !validGBID(code) || beforeEpoch <= 0 || (afterID != "" && !validIntentID(afterID)) {
		return nil, ErrDeviceIntentInvalid
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	var rows []DeviceOperationIntent
	query := s.db.WithContext(ctx).Where("device_pk = ? AND device_code = ? AND device_epoch < ? AND state <> ?", pk, code, beforeEpoch, IntentCancelled)
	if afterID != "" {
		query = query.Where("operation_id > ?", afterID)
	}
	if err := query.Order("operation_id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, ErrDeviceIntentUnavailable
	}
	for _, row := range rows {
		if !validIntentRow(row) {
			return nil, ErrDeviceIntentUnavailable
		}
	}
	return rows, nil
}

func authorizeIntentDevice(tx *gorm.DB, ctx context.Context, id DeviceOperationIntentIdentity) error {
	// Same device lock order as assignment: device first, intent second. No
	// external I/O while locked; fresh SQL remains the authority after restart.
	rows, err := queryDeviceCleanupRows(tx, ctx, id.DeviceCode, true)
	if err != nil || len(rows) != 1 || rows[0].ID != id.DevicePK {
		return ErrDeviceIntentUnavailable
	}
	state, err := validateDeviceCleanupRow(rows[0])
	if err != nil {
		return ErrDeviceIntentUnavailable
	}
	if state.AccessEpoch != id.DeviceEpoch {
		return ErrDeviceIntentRevoked
	}
	if state.CleanupCompletedEpoch != state.AccessEpoch {
		return ErrDeviceCleanupPending
	}
	if id.TargetScope == "channel" {
		var count int64
		err := tx.Table("gb_channel").Where("id = ? AND device_id = ? AND channel_id = ? AND deleted_at IS NULL", id.TargetPK, id.DeviceCode, id.TargetCode).Count(&count).Error
		if err != nil || count != 1 {
			return ErrDeviceIntentUnavailable
		}
	}
	return nil
}

func validIntentIdentity(id DeviceOperationIntentIdentity) bool {
	if !validIntentID(id.OperationID) || id.DevicePK <= 0 || id.DeviceEpoch <= 0 || !validGBID(id.DeviceCode) || id.TargetPK <= 0 || !validGBID(id.TargetCode) {
		return false
	}
	if id.TargetScope != "channel" && id.TargetScope != "device" {
		return false
	}
	if id.TargetScope == "device" && (id.TargetPK != id.DevicePK || id.TargetCode != id.DeviceCode) {
		return false
	}
	switch id.Kind {
	case "live", "playback", "download", "talk", "ptz":
		return true
	}
	return false
}

func validIntentID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func validIntentRow(row DeviceOperationIntent) bool {
	if !validIntentIdentity(row.DeviceOperationIntentIdentity) || row.ContractVersion != 1 || row.RowVersion <= 0 ||
		row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() || row.UpdatedAt.Before(row.CreatedAt) {
		return false
	}
	validTime := func(value *time.Time) bool {
		return value != nil && !value.Before(row.CreatedAt) && !value.After(row.UpdatedAt)
	}
	switch row.State {
	case IntentReserved:
		return row.RowVersion == 1 && row.DispatchStartedAt == nil && row.CancelledAt == nil
	case IntentDispatched:
		return row.RowVersion >= 2 && validTime(row.DispatchStartedAt) && row.CancelledAt == nil
	case IntentCancelled:
		return row.RowVersion >= 2 && row.DispatchStartedAt == nil && validTime(row.CancelledAt)
	}
	return false
}

func normalizeIntentError(err error) error {
	for _, known := range []error{ErrDeviceIntentInvalid, ErrDeviceIntentUnavailable, ErrDeviceIntentConflict, ErrDeviceIntentRevoked, ErrDeviceCleanupPending} {
		if errors.Is(err, known) {
			return known
		}
	}
	return ErrDeviceIntentUnavailable
}
