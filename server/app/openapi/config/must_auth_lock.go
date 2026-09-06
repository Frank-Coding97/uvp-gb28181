// Package config owns the durable OpenAPI activation boundary. It deliberately
// does not read YAML, create schema, or seed the singleton row; those actions
// belong to the composition root and migration/initialization paths.
package config

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

const mustAuthSecurityStateID int64 = 1

// ErrUnavailable is the only error crossing this security boundary. It hides
// missing tables/rows, malformed state, driver errors, and commit-unknown
// outcomes alike so callers cannot mistake an unavailable store for unlocked.
var ErrUnavailable = errors.New("openapi must-auth security state unavailable")

// State is the validated public snapshot returned by Load and Latch. LockedAt
// is nil only for the valid, initial unlocked state; once latched it is a UTC
// timestamp and LockVersion is positive for this one-way latch.
type State struct {
	ID             int64
	MustAuthLocked bool
	LockedAt       *time.Time
	LockVersion    int64
}

// MustAuthStore reads and latches the singleton security state. It has no
// unlock, save, migration, or seed operation by design.
type MustAuthStore struct {
	db  *gorm.DB
	now func() time.Time
}

// NewMustAuthStore constructs a store over an already migrated and seeded
// database. It has no database side effects.
func NewMustAuthStore(db *gorm.DB, now func() time.Time) *MustAuthStore {
	if now == nil {
		now = time.Now
	}
	return &MustAuthStore{db: db, now: now}
}

// Load returns the singleton state only when exactly one healthy id=1 row can
// be read. Every failure is generalized to ErrUnavailable for fail-closed
// composition by the HTTP/media root.
func (s *MustAuthStore) Load(ctx context.Context) (State, error) {
	if s == nil || s.db == nil || ctx == nil {
		return State{}, ErrUnavailable
	}
	row, err := loadRow(s.db.WithContext(ctx))
	if err != nil {
		return State{}, ErrUnavailable
	}
	return stateFromModel(row), nil
}

// Latch atomically changes the valid initial state from false to true. The
// conditional update is the linearization point: concurrent instances can
// make at most one successful transition and lock_version never increments a
// second time. A pre-existing locked row is returned idempotently. No network
// or other external I/O occurs while the short transaction is open.
func (s *MustAuthStore) Latch(ctx context.Context) (State, error) {
	if s == nil || s.db == nil || ctx == nil {
		return State{}, ErrUnavailable
	}

	lockedAt := s.now().UTC().Round(0)
	var state State
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&models.SecurityState{}).
			Where("id = ? AND must_auth_locked = ? AND lock_version = ? AND locked_at IS NULL", mustAuthSecurityStateID, false, 0).
			Updates(map[string]any{
				"must_auth_locked": true,
				"locked_at":        lockedAt,
				"lock_version":     1,
			})
		if updated.Error != nil {
			return ErrUnavailable
		}
		if updated.RowsAffected != 0 && updated.RowsAffected != 1 {
			return ErrUnavailable
		}

		row, err := loadRow(tx)
		if err != nil {
			return ErrUnavailable
		}
		if !row.MustAuthLocked {
			// A zero-row conditional update must never be reported as a
			// successful latch while the transaction still observes unlocked
			// state (for example after a dialect-specific write conflict).
			return ErrUnavailable
		}
		state = stateFromModel(row)
		return nil
	})
	if err != nil {
		return State{}, ErrUnavailable
	}
	return state, nil
}

func loadRow(db *gorm.DB) (models.SecurityState, error) {
	if db == nil {
		return models.SecurityState{}, ErrUnavailable
	}
	// Read exactly the security columns once. Pointer fields preserve NULL so a
	// weak or damaged table cannot turn a missing value into Go's zero value.
	var rows []securityStateProjection
	result := db.Model(&models.SecurityState{}).
		Select("id, must_auth_locked, locked_at, lock_version").
		Order("id ASC").
		Limit(2).
		Find(&rows)
	if result.Error != nil || result.RowsAffected != 1 || len(rows) != 1 {
		return models.SecurityState{}, ErrUnavailable
	}
	projection := rows[0]
	if projection.ID == nil || *projection.ID != mustAuthSecurityStateID || projection.MustAuthLocked == nil || projection.LockVersion == nil {
		return models.SecurityState{}, ErrUnavailable
	}
	row := models.SecurityState{
		ID:             *projection.ID,
		MustAuthLocked: *projection.MustAuthLocked,
		LockVersion:    *projection.LockVersion,
	}
	if projection.LockedAt != nil {
		lockedAt := projection.LockedAt.UTC().Round(0)
		row.LockedAt = &lockedAt
	}
	if !validModelState(row) {
		return models.SecurityState{}, ErrUnavailable
	}
	return row, nil
}

type securityStateProjection struct {
	ID             *int64     `gorm:"column:id"`
	MustAuthLocked *bool      `gorm:"column:must_auth_locked"`
	LockedAt       *time.Time `gorm:"column:locked_at"`
	LockVersion    *int64     `gorm:"column:lock_version"`
}

func validModelState(row models.SecurityState) bool {
	if row.ID != mustAuthSecurityStateID {
		return false
	}
	if row.MustAuthLocked {
		return row.LockVersion > 0 && row.LockedAt != nil && !row.LockedAt.IsZero()
	}
	return row.LockVersion == 0 && row.LockedAt == nil
}

func stateFromModel(row models.SecurityState) State {
	var lockedAt *time.Time
	if row.LockedAt != nil {
		value := row.LockedAt.UTC().Round(0)
		lockedAt = &value
	}
	return State{ID: row.ID, MustAuthLocked: row.MustAuthLocked, LockedAt: lockedAt, LockVersion: row.LockVersion}
}
