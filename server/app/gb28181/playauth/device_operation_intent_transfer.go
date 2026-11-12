package playauth

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// CancelReservedDeviceOperationIntents participates in the caller's transfer
// transaction after the device epoch update. It never commits, dispatches or
// closes resources. A failure requires rollback of the entire transfer.
func CancelReservedDeviceOperationIntents(ctx context.Context, tx *gorm.DB, devicePK int64, deviceCode string, newEpoch int64) error {
	if ctx == nil || ctx.Err() != nil || tx == nil || tx.Error != nil || tx.Statement == nil {
		return ErrDeviceIntentUnavailable
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return ErrDeviceIntentUnavailable // No implicit/autocommit transaction.
	}
	if devicePK <= 0 || !validGBID(deviceCode) || newEpoch <= 1 {
		return ErrDeviceIntentInvalid
	}
	tx = tx.WithContext(ctx)
	rows, err := queryDeviceCleanupRows(tx, ctx, deviceCode, true)
	if err != nil || len(rows) != 1 || rows[0].ID != devicePK {
		return ErrDeviceIntentUnavailable
	}
	state, err := validateDeviceCleanupRow(rows[0])
	if err != nil {
		return ErrDeviceIntentUnavailable
	}
	if state.AccessEpoch != newEpoch {
		return ErrDeviceIntentRevoked
	}
	now := time.Now().UTC()
	base := func() *gorm.DB {
		return tx.Model(&DeviceOperationIntent{}).
			Where("device_pk = ? AND device_code = ? AND device_epoch < ? AND state = ?", devicePK, deviceCode, newEpoch, IntentReserved)
	}
	result := base().Where("contract_version = 1 AND row_version = 1 AND dispatch_started_at IS NULL AND cancelled_at IS NULL AND created_at <= ?", now).
		Updates(map[string]any{"state": IntentCancelled, "row_version": 2, "cancelled_at": now, "updated_at": now})
	if result.Error != nil {
		return ErrDeviceIntentUnavailable
	}
	// Unsupported/corrupt reserved records cannot silently escape cancellation.
	// The parent device lock prevents a concurrent old-epoch reservation here.
	var remaining int64
	if err := base().Count(&remaining).Error; err != nil || remaining != 0 {
		return ErrDeviceIntentUnavailable
	}
	return nil
}
