package playauth

import (
	"context"
	"time"

	"gorm.io/gorm"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// ExpirePTZReservation can only retire a never-claimed, expired query. It grants
// no effect permission and is serialized with Claim by the same device lock.
func (s *DeviceOperationIntentStore) ExpirePTZReservation(ctx context.Context, id DeviceOperationIntentIdentity, operationPK uint, now time.Time) error {
	if !s.available(ctx) || !validIntentIdentity(id) || id.Kind != "ptz" || operationPK == 0 || now.IsZero() {
		return ErrDeviceIntentInvalid
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		devices, err := queryDeviceCleanupRows(tx, ctx, id.DeviceCode, true)
		if err != nil {
			return err
		}
		if len(devices) != 1 || devices[0].ID != id.DevicePK {
			return ErrDeviceIntentConflict
		}
		var parent DeviceOperationIntent
		if err := tx.First(&parent, "operation_id=?", id.OperationID).Error; err != nil {
			return err
		}
		if !validIntentRow(parent) || parent.DeviceOperationIntentIdentity != id {
			return ErrDeviceIntentConflict
		}
		var op gbmodels.GbPTZOperation
		if err := tx.First(&op, operationPK).Error; err != nil {
			return err
		}
		if op.DeviceIntentID == nil || *op.DeviceIntentID != id.OperationID || op.DeviceEpoch == nil || *op.DeviceEpoch != id.DeviceEpoch ||
			op.DeviceID != uint(id.DevicePK) || op.DeviceCode != id.DeviceCode || !ptzIntentTargetMatches(op, id) {
			return ErrDeviceIntentConflict
		}
		if !op.ResponseRequired || op.Status != gbmodels.PTZOperationQueued || op.Attempt != 0 || op.QueueDeadlineAt == nil || op.QueueDeadlineAt.After(now) {
			return nil
		}
		if parent.State != IntentReserved && parent.State != IntentCancelled {
			return ErrDeviceIntentConflict
		}
		var attempts int64
		if err := tx.Model(&gbmodels.GbPTZOperationAttempt{}).Where("operation_id=?", op.ID).Count(&attempts).Error; err != nil {
			return err
		}
		if attempts != 0 || op.DispatchStartedAt != nil {
			return ErrDeviceIntentConflict
		}
		if parent.State == IntentReserved {
			at := time.Now().UTC()
			r := tx.Model(&DeviceOperationIntent{}).Where("operation_id=? AND state=? AND row_version=?", id.OperationID, IntentReserved, parent.RowVersion).
				Updates(map[string]any{"state": IntentCancelled, "row_version": parent.RowVersion + 1, "updated_at": at, "cancelled_at": at})
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return ErrDeviceIntentConflict
			}
		}
		r := tx.Model(&gbmodels.GbPTZOperation{}).Where("id=? AND status=? AND attempt=0", op.ID, gbmodels.PTZOperationQueued).
			Updates(map[string]any{"status": gbmodels.PTZOperationRejected, "error_code": "HOME_POSITION_UNAVAILABLE", "error_message": "PTZ operation 排队超时", "completed_at": now, "next_attempt_at": nil})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return ErrDeviceIntentConflict
		}
		return nil
	})
}
