package playauth

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

var ErrDeviceSecurityUnavailable = errors.New("device media security state unavailable")

// DeviceSecurityStore is the durable device-only authority for background
// media credentials. It does not replace HMAC verification or personnel/shared
// resource authorization, and deliberately imposes no API-client owner rule.
// Every call reads the current DB state; no process-local revocation cache is
// allowed to survive a transfer or to act as a dependency-failure fallback.
type DeviceSecurityStore struct{ db *gorm.DB }

type DeviceSecurityState struct {
	AccessEpoch         int64
	LegacyRevokedBefore *time.Time
}

func NewDeviceSecurityStore(db *gorm.DB) *DeviceSecurityStore {
	return &DeviceSecurityStore{db: db}
}

func (s *DeviceSecurityStore) Load(ctx context.Context, deviceID string) (DeviceSecurityState, error) {
	if s == nil || s.db == nil || ctx == nil || ctx.Err() != nil || !validGBID(deviceID) {
		return DeviceSecurityState{}, ErrDeviceSecurityUnavailable
	}
	var rows []struct {
		AccessEpoch         *int64     `gorm:"column:access_epoch"`
		LegacyRevokedBefore *time.Time `gorm:"column:legacy_revoked_before"`
	}
	result := s.db.WithContext(ctx).Table("gb_device").
		Select("access_epoch, legacy_revoked_before").
		Where("device_id = ? AND deleted_at IS NULL", deviceID).Limit(2).Find(&rows)
	if result.Error != nil || result.RowsAffected != 1 || len(rows) != 1 || rows[0].AccessEpoch == nil || *rows[0].AccessEpoch <= 0 {
		return DeviceSecurityState{}, ErrDeviceSecurityUnavailable
	}
	state := DeviceSecurityState{AccessEpoch: *rows[0].AccessEpoch}
	if rows[0].LegacyRevokedBefore != nil {
		cutoff := rows[0].LegacyRevokedBefore.UTC()
		// Assignment writes Unix-second-aligned cutoffs because legacy iat has
		// second precision. Malformed authority is not rounded down into access.
		if cutoff.Unix() <= 0 || cutoff.Nanosecond() != 0 {
			return DeviceSecurityState{}, ErrDeviceSecurityUnavailable
		}
		state.LegacyRevokedBefore = &cutoff
	}
	return state, nil
}

// AuthorizeLegacy is only for already HMAC-verified pre-upgrade v2 tokens.
// A NULL cutoff means no recorded transfer, not a missing security schema.
func (s *DeviceSecurityStore) AuthorizeLegacy(ctx context.Context, deviceID string, issuedAt int64) error {
	if issuedAt <= 0 {
		return ErrTokenInvalid
	}
	state, err := s.Load(ctx, deviceID)
	if err != nil {
		return err
	}
	if state.LegacyRevokedBefore != nil && issuedAt < state.LegacyRevokedBefore.Unix() {
		return ErrTokenRevoked
	}
	return nil
}

func (s *DeviceSecurityStore) AuthorizeEpoch(ctx context.Context, deviceID string, epoch int64) error {
	if epoch <= 0 {
		return ErrTokenInvalid
	}
	state, err := s.Load(ctx, deviceID)
	if err != nil {
		return err
	}
	if state.AccessEpoch != epoch {
		return ErrTokenRevoked
	}
	return nil
}
