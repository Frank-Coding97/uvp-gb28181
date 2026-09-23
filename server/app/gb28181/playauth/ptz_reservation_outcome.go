package playauth

import (
	"context"
	"gorm.io/gorm"
	"time"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// PTZReservationOutcome observes one reservation whose commit reply was lost.
// It grants no send permission; a confirmed row still requires a fresh claim.
type PTZReservationOutcome struct {
	store     *DeviceOperationIntentStore
	identity  DeviceOperationIntentIdentity
	operation gbmodels.GbPTZOperation
	cause     error
}

func (p *PTZReservationOutcome) Error() string { return "PTZ reservation commit outcome unavailable" }
func (p *PTZReservationOutcome) Unwrap() error {
	if p == nil {
		return ErrDeviceIntentUnavailable
	}
	return p.cause
}

func (p *PTZReservationOutcome) resolve(ctx context.Context, abandoned bool) (gbmodels.GbPTZOperation, bool, error) {
	if p == nil || p.store == nil || !p.store.available(ctx) {
		return gbmodels.GbPTZOperation{}, false, ErrDeviceIntentUnavailable
	}
	var out gbmodels.GbPTZOperation
	found := false
	err := p.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		devices, err := queryDeviceCleanupRows(tx, ctx, p.identity.DeviceCode, true)
		if err != nil {
			return err
		}
		if len(devices) != 1 || devices[0].ID != p.identity.DevicePK {
			return ErrDeviceIntentConflict
		}
		var parents []DeviceOperationIntent
		if err := tx.Where("operation_id=?", p.identity.OperationID).Limit(2).Find(&parents).Error; err != nil {
			return err
		}
		var ops []gbmodels.GbPTZOperation
		if err := tx.Where("operation_id=?", p.operation.OperationID).Limit(2).Find(&ops).Error; err != nil {
			return err
		}
		if len(parents) == 0 && len(ops) == 0 {
			return nil
		}
		if len(parents) != 1 || len(ops) != 1 || parents[0].DeviceOperationIntentIdentity != p.identity || !validIntentRow(parents[0]) {
			return ErrDeviceIntentConflict
		}
		out = ops[0]
		if out.DeviceIntentID == nil || *out.DeviceIntentID != p.identity.OperationID || out.DeviceEpoch == nil || *out.DeviceEpoch != p.identity.DeviceEpoch ||
			!ptzIntentTargetMatches(out, p.identity) || !samePTZCommand(out, p.operation) {
			return ErrDeviceIntentConflict
		}
		found = true
		if abandoned && !out.ResponseRequired && out.Attempt == 0 {
			// No claim was made. End the reservation in the same transaction as
			// the business outcome, so a failed write cannot strand either half.
			now := time.Now().UTC().Truncate(time.Millisecond)
			parent := parents[0]
			if parent.State != IntentReserved && parent.State != IntentCancelled {
				return ErrDeviceIntentConflict
			}
			if parent.State == IntentReserved {
				result := tx.Model(&DeviceOperationIntent{}).
					Where("operation_id=? AND state=? AND row_version=?", parent.OperationID, IntentReserved, parent.RowVersion).
					Updates(map[string]any{"state": IntentCancelled, "row_version": parent.RowVersion + 1, "updated_at": now, "cancelled_at": now})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return ErrDeviceIntentConflict
				}
			}
			return tx.Model(&gbmodels.GbPTZOperation{}).Where("id=? AND attempt=0 AND status=?", out.ID, gbmodels.PTZOperationQueued).
				Updates(map[string]any{"status": gbmodels.PTZOperationUnknown, "error_code": "RESERVATION_NOT_DISPATCHED", "next_attempt_at": nil, "completed_at": now.In(time.Local)}).Error
		}
		return nil
	})
	if err != nil {
		return gbmodels.GbPTZOperation{}, false, err
	}
	return out, found, nil
}

// Reconcile runs after the original caller has already received an error.
// It never revives a synchronous command or calls a sender.
func (p *PTZReservationOutcome) Reconcile(ctx context.Context) error {
	_, _, err := p.resolve(ctx, true)
	return err
}
