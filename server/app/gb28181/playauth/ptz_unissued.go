package playauth

import (
	"context"
	"gorm.io/gorm"
	"time"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// PTZUnissuedAttempt proves that Claim never returned dispatch permission.
// Its fields cannot be supplied by callers; reconciliation yields no attempt.
type PTZUnissuedAttempt struct {
	operation gbmodels.GbPTZOperation
	store     *DeviceOperationIntentStore
	identity  DeviceOperationIntentIdentity
	attempt   gbmodels.GbPTZOperationAttempt
	cause     error
}

func (p *PTZUnissuedAttempt) Error() string {
	return "PTZ attempt was not issued: commit outcome unavailable"
}
func (p *PTZUnissuedAttempt) Unwrap() error {
	if p == nil {
		return ErrDeviceIntentUnavailable
	}
	return p.cause
}

func (p *PTZUnissuedAttempt) Reconcile(ctx context.Context) error {
	if p == nil || p.store == nil || !p.store.available(ctx) || !validIntentIdentity(p.identity) || p.attempt.OwnerProcessID == nil || p.attempt.OwnerRunID == nil {
		return ErrDeviceIntentUnavailable
	}
	return p.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize against the original transaction before treating absence
		// as rollback. A plain SELECT can race a lost commit acknowledgement.
		devices, err := queryDeviceCleanupRows(tx, ctx, p.identity.DeviceCode, true)
		if err != nil {
			return err
		}
		if len(devices) != 1 || devices[0].ID != p.identity.DevicePK {
			return ErrDeviceIntentConflict
		}
		var parent DeviceOperationIntent
		if err := tx.First(&parent, "operation_id=?", p.identity.OperationID).Error; err != nil {
			return err
		}
		if !validIntentRow(parent) || parent.DeviceOperationIntentIdentity != p.identity {
			return ErrDeviceIntentConflict
		}
		var op gbmodels.GbPTZOperation
		if err := tx.First(&op, p.attempt.OperationID).Error; err != nil {
			return err
		}
		if op.DeviceIntentID == nil || *op.DeviceIntentID != p.identity.OperationID || op.DeviceEpoch == nil || *op.DeviceEpoch != p.identity.DeviceEpoch {
			return ErrDeviceIntentConflict
		}
		if !ptzIntentTargetMatches(op, p.identity) || !samePTZCommand(op, p.operation) {
			return ErrDeviceIntentConflict
		}
		var rows []gbmodels.GbPTZOperationAttempt
		if err := tx.Where("operation_id=? AND attempt_no=?", p.attempt.OperationID, p.attempt.AttemptNo).Limit(2).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			// The device lock serialized us after Claim, and attempt rows are
			// retained. Absence proves rollback even if cancellation has since
			// advanced the parent. A progressed counter without its row is not
			// a rollback and must remain an integrity conflict.
			if op.Attempt != p.attempt.AttemptNo-1 {
				return ErrDeviceIntentConflict
			}
			return nil
		}
		if len(rows) != 1 {
			return ErrDeviceIntentConflict
		}
		a := rows[0]
		if a.OperationID != p.attempt.OperationID || a.AttemptNo != p.attempt.AttemptNo || a.SN != p.attempt.SN ||
			!a.StartedAt.Equal(p.attempt.StartedAt) || !a.LeaseUntil.Equal(p.attempt.LeaseUntil) {
			return ErrDeviceIntentConflict
		}
		if a.OwnerProcessID == nil || a.OwnerRunID == nil || *a.OwnerProcessID != *p.attempt.OwnerProcessID || *a.OwnerRunID != *p.attempt.OwnerRunID {
			return ErrDeviceIntentConflict
		}
		if a.Status == gbmodels.PTZOperationAttemptFailed && a.ErrorCode == "DISPATCH_NOT_INVOKED" && a.LocalQuiescedAt != nil {
			return nil
		}
		if a.Status != gbmodels.PTZOperationAttemptDispatching || a.LocalQuiescedAt != nil {
			return ErrDeviceIntentConflict
		}
		now := time.Now().In(time.Local).Truncate(time.Millisecond)
		r := tx.Model(&gbmodels.GbPTZOperationAttempt{}).Where("id=? AND status=? AND owner_process_id=? AND owner_run_id=? AND local_quiesced_at IS NULL", a.ID, gbmodels.PTZOperationAttemptDispatching, *a.OwnerProcessID, *a.OwnerRunID).
			Updates(map[string]any{"status": gbmodels.PTZOperationAttemptFailed, "error_code": "DISPATCH_NOT_INVOKED", "error_message": "dispatch permission was never issued", "completed_at": now, "local_quiesced_at": now})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return ErrDeviceIntentConflict
		}
		if !op.ResponseRequired || op.Attempt >= op.MaxAttempts {
			return tx.Model(&gbmodels.GbPTZOperation{}).Where("id=? AND attempt=? AND status=?", op.ID, a.AttemptNo, gbmodels.PTZOperationQueued).
				Updates(map[string]any{"status": gbmodels.PTZOperationUnknown, "error_code": "DISPATCH_NOT_INVOKED", "next_attempt_at": nil, "completed_at": now}).Error
		}
		return nil
	})
}
