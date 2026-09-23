package playauth

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	defaultDeviceCleanupListLimit = 100
	maxDeviceCleanupListLimit     = 1000
)

var (
	ErrDeviceCleanupUnavailable = errors.New("device cleanup state unavailable")
	ErrDeviceCleanupPending     = errors.New("device cleanup pending")
	ErrDeviceCleanupRevoked     = errors.New("device cleanup epoch revoked")
	ErrDeviceCleanupMismatch    = errors.New("device cleanup epoch mismatch")
	ErrDeviceCleanupStaleTarget = errors.New("device cleanup target is stale")
	ErrDeviceCleanupInvalid     = errors.New("invalid device cleanup request")
)

// DeviceCleanupState is the durable admission barrier for a device. A media
// request may use AccessEpoch only after CleanupCompletedEpoch reaches the
// same value. The state is deliberately not an HTTP DTO.
type DeviceCleanupState struct {
	DeviceID              string
	AccessEpoch           int64
	CleanupCompletedEpoch int64
}

// DeviceCleanupStore reads and advances the durable device cleanup barrier.
// It never performs media discovery, network cleanup, or process-local
// completion inference.
type DeviceCleanupStore struct {
	db *gorm.DB
}

type deviceCleanupRow struct {
	ID                    int64  `gorm:"column:id"`
	DeviceID              string `gorm:"column:device_id"`
	AccessEpoch           *int64 `gorm:"column:access_epoch"`
	CleanupCompletedEpoch *int64 `gorm:"column:cleanup_completed_epoch"`
}

func NewDeviceCleanupStore(db *gorm.DB) *DeviceCleanupStore {
	return &DeviceCleanupStore{db: db}
}

// Load performs a fresh, fail-closed read of the one non-deleted device row.
// Missing security columns, ambiguous rows, malformed epochs, and canceled
// contexts are dependency failures rather than an empty/usable state.
func (s *DeviceCleanupStore) Load(ctx context.Context, deviceID string) (DeviceCleanupState, error) {
	if err := validateDeviceCleanupRead(s, ctx, deviceID); err != nil {
		return DeviceCleanupState{}, err
	}
	rows, err := queryDeviceCleanupRows(s.db, ctx, deviceID, false)
	if err != nil || len(rows) != 1 {
		return DeviceCleanupState{}, ErrDeviceCleanupUnavailable
	}
	return validateDeviceCleanupRow(rows[0])
}

// Authorize admits only the current device epoch after the durable cleanup
// barrier reaches it. An older epoch is revoked even while a newer transfer
// is still pending; the current epoch gets the stable pending error.
func (s *DeviceCleanupStore) Authorize(ctx context.Context, deviceID string, expectedEpoch int64) error {
	if expectedEpoch <= 0 {
		return ErrDeviceCleanupInvalid
	}
	state, err := s.Load(ctx, deviceID)
	if err != nil {
		return err
	}
	switch {
	case expectedEpoch < state.AccessEpoch:
		return errors.Join(ErrDeviceCleanupRevoked, ErrDeviceCleanupMismatch)
	case expectedEpoch > state.AccessEpoch:
		return ErrDeviceCleanupMismatch
	case state.CleanupCompletedEpoch < state.AccessEpoch:
		return ErrDeviceCleanupPending
	default:
		return nil
	}
}

// Complete is an internal-only operation for the device-level aggregation
// coordinator. The coordinator must call it only after every trusted media
// subsystem has reached its own terminal cleanup state. This method does not
// probe, guess, or complete work after a restart, boot change, or empty scan;
// it only advances the durable barrier for the requested device.
func (s *DeviceCleanupStore) Complete(ctx context.Context, deviceID string, targetEpoch int64) error {
	if targetEpoch <= 0 {
		return ErrDeviceCleanupInvalid
	}
	if err := validateDeviceCleanupRead(s, ctx, deviceID); err != nil {
		return err
	}
	transactionErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rows, err := queryDeviceCleanupRows(tx, ctx, deviceID, true)
		if err != nil || len(rows) != 1 {
			return ErrDeviceCleanupUnavailable
		}
		state, err := validateDeviceCleanupRow(rows[0])
		if err != nil {
			return err
		}
		if targetEpoch < state.AccessEpoch {
			return ErrDeviceCleanupStaleTarget
		}
		if targetEpoch > state.AccessEpoch {
			return ErrDeviceCleanupInvalid
		}
		if state.CleanupCompletedEpoch > targetEpoch {
			return ErrDeviceCleanupStaleTarget
		}

		updated := tx.WithContext(ctx).Table("gb_device").
			Where("device_id = ? AND deleted_at IS NULL AND access_epoch = ? AND cleanup_completed_epoch < ?", deviceID, targetEpoch, targetEpoch).
			Update("cleanup_completed_epoch", targetEpoch)
		if updated.Error != nil {
			return ErrDeviceCleanupUnavailable
		}

		// RowsAffected == 0 is not success by itself: it may be a duplicate
		// completion, a lost CAS, or a dialect-specific no-op. Read back the
		// locked row and accept only the exact requested state.
		freshRows, err := queryDeviceCleanupRows(tx, ctx, deviceID, true)
		if err != nil || len(freshRows) != 1 {
			return ErrDeviceCleanupUnavailable
		}
		fresh, err := validateDeviceCleanupRow(freshRows[0])
		if err != nil {
			return err
		}
		if fresh.AccessEpoch == targetEpoch && fresh.CleanupCompletedEpoch == targetEpoch {
			return nil
		}
		if fresh.AccessEpoch > targetEpoch {
			return ErrDeviceCleanupStaleTarget
		}
		return ErrDeviceCleanupUnavailable
	})
	if transactionErr != nil {
		return normalizeDeviceCleanupError(transactionErr)
	}
	return nil
}

// ListPending discovers a bounded page of non-deleted devices whose durable
// cleanup barrier trails access_epoch. It never marks a row complete.
func (s *DeviceCleanupStore) ListPending(ctx context.Context, limit int) ([]DeviceCleanupState, error) {
	if s == nil || s.db == nil || ctx == nil || ctx.Err() != nil {
		return nil, ErrDeviceCleanupUnavailable
	}
	limit = normalizeDeviceCleanupLimit(limit)
	var rows []deviceCleanupRow
	result := s.db.WithContext(ctx).Table("gb_device").
		Select("id, device_id, access_epoch, cleanup_completed_epoch").
		Where("deleted_at IS NULL AND cleanup_completed_epoch < access_epoch").
		Order("id ASC").Limit(limit).Find(&rows)
	if result.Error != nil {
		return nil, ErrDeviceCleanupUnavailable
	}
	out := make([]DeviceCleanupState, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		state, err := validateDeviceCleanupRow(row)
		if err != nil {
			return nil, err
		}
		if state.CleanupCompletedEpoch >= state.AccessEpoch {
			continue
		}
		if _, exists := seen[state.DeviceID]; exists {
			return nil, ErrDeviceCleanupUnavailable
		}
		seen[state.DeviceID] = struct{}{}
		out = append(out, state)
	}
	return out, nil
}

func validateDeviceCleanupRead(s *DeviceCleanupStore, ctx context.Context, deviceID string) error {
	if s == nil || s.db == nil || ctx == nil || ctx.Err() != nil || !validGBID(deviceID) {
		return ErrDeviceCleanupUnavailable
	}
	return nil
}

func queryDeviceCleanupRows(db *gorm.DB, ctx context.Context, deviceID string, lock bool) ([]deviceCleanupRow, error) {
	if db == nil || ctx == nil || ctx.Err() != nil {
		return nil, ErrDeviceCleanupUnavailable
	}
	query := db.WithContext(ctx)
	if db.Dialector.Name() == "sqlserver" && lock {
		query = query.Table("gb_device WITH (UPDLOCK, HOLDLOCK)")
	} else {
		query = query.Table("gb_device")
		if lock {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
	}
	var rows []deviceCleanupRow
	result := query.Select("id, device_id, access_epoch, cleanup_completed_epoch").
		Where("device_id = ? AND deleted_at IS NULL", deviceID).
		Order("id ASC").Limit(2).Find(&rows)
	if result.Error != nil {
		return nil, result.Error
	}
	return rows, nil
}

func validateDeviceCleanupRow(row deviceCleanupRow) (DeviceCleanupState, error) {
	if row.ID <= 0 || !validGBID(row.DeviceID) || row.AccessEpoch == nil || row.CleanupCompletedEpoch == nil {
		return DeviceCleanupState{}, ErrDeviceCleanupUnavailable
	}
	access, completed := *row.AccessEpoch, *row.CleanupCompletedEpoch
	if access <= 0 || completed <= 0 || completed > access {
		return DeviceCleanupState{}, ErrDeviceCleanupUnavailable
	}
	return DeviceCleanupState{
		DeviceID:              row.DeviceID,
		AccessEpoch:           access,
		CleanupCompletedEpoch: completed,
	}, nil
}

func normalizeDeviceCleanupLimit(limit int) int {
	if limit <= 0 {
		return defaultDeviceCleanupListLimit
	}
	if limit > maxDeviceCleanupListLimit {
		return maxDeviceCleanupListLimit
	}
	return limit
}

func normalizeDeviceCleanupError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrDeviceCleanupPending),
		errors.Is(err, ErrDeviceCleanupRevoked),
		errors.Is(err, ErrDeviceCleanupMismatch),
		errors.Is(err, ErrDeviceCleanupStaleTarget),
		errors.Is(err, ErrDeviceCleanupInvalid),
		errors.Is(err, ErrDeviceCleanupUnavailable):
		return err
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return ErrDeviceCleanupUnavailable
	default:
		return ErrDeviceCleanupUnavailable
	}
}
