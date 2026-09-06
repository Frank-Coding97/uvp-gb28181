package playauth

import (
	"context"
	"time"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

// RecordDeviceTransfer is the OpenAPI transaction participant for assignment.
// The caller must first lock the root device and update its owner and security
// epoch in this same transaction, and must roll back on any returned error.
// This does not implement assignment, background token revocation or resource
// cleanup. It only records durable intent for old machine grants/viewers.
//
// Unlike client revocation, this boundary never acquires client/scope locks:
// device -> ordered grants -> viewer preserves the shared admission lock order.
// No network I/O or nested transaction is performed here.
func (s *OpenAPIRevocationStore) RecordDeviceTransfer(ctx context.Context, tx *gorm.DB, deviceID string, newEpoch int64) error {
	if ctx == nil || tx == nil || !validGBID(deviceID) || newEpoch <= 1 {
		return ErrOpenAPIRevocationInvalid
	}
	if s == nil || s.now == nil || ctx.Err() != nil {
		return ErrOpenAPIRevocationUnavailable
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	if now.IsZero() {
		return ErrOpenAPIRevocationUnavailable
	}
	tx = tx.WithContext(ctx)
	epoch, err := lockOpenAPIRootDevice(tx, deviceID)
	if err != nil {
		return ErrOpenAPIRevocationUnavailable
	}
	if epoch != newEpoch {
		return ErrOpenAPIRevocationStale
	}
	var grantIDs []string
	query := lockedOpenAPIModel(tx, &models.PlayGrant{}, (models.PlayGrant{}).TableName()).
		Select("grant_id").
		Where("device_id = ? AND device_epoch < ? AND scope = ? AND state IN ?", deviceID, newEpoch, openAPIPlayScope, openAPINonTerminalGrantStates).
		Order("grant_id ASC")
	if err := query.Find(&grantIDs).Error; err != nil {
		return ErrOpenAPIRevocationUnavailable
	}
	for _, grantID := range grantIDs {
		grant, err := lockOpenAPIGrant(tx, grantID)
		if err != nil {
			return ErrOpenAPIRevocationUnavailable
		}
		if grant.DeviceID == nil || *grant.DeviceID != deviceID || grant.DeviceEpoch >= newEpoch || grant.Scope != openAPIPlayScope || !isOpenAPINonTerminalGrant(grant.State) {
			return ErrOpenAPIRevocationStale
		}
		updated := tx.Model(&models.PlayGrant{}).
			Where("grant_id = ? AND device_id = ? AND device_epoch = ? AND state = ?", grantID, deviceID, grant.DeviceEpoch, grant.State).
			Updates(map[string]any{"state": models.GrantStateRevoked, "reason": "device.transferred", "updated_at": now})
		if updated.Error != nil || updated.RowsAffected != 1 {
			return ErrOpenAPIRevocationUnavailable
		}
		if err := markOpenAPIViewerForRevocation(tx, grantID, now); err != nil {
			return err
		}
	}
	return nil
}
