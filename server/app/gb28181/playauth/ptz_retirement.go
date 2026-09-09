package playauth

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const PTZOwnerProcessRetired = "OWNER_PROCESS_RETIRED"

// RecoverRetiredPTZ is a pre-publication barrier. It scans in bounded pages,
// including one-way commands and unexpired leases. Infrastructure failure must
// prevent this PTZ runtime from publishing producers; corrupt historical rows
// are skipped without fabricating a certificate, reported through the returned
// attempt IDs, and rescanned on the next startup. It never advances device
// coverage.
func (s *DeviceOperationIntentStore) RecoverRetiredPTZ(ctx context.Context) ([]uint, error) {
	if !s.available(ctx) || !s.hasAuthority() {
		return nil, ErrDeviceIntentUnavailable
	}
	var skipped []uint
	var cursor uint
	for {
		var rows []gbmodels.GbPTZOperationAttempt
		if err := s.db.WithContext(ctx).
			Where("id>? AND owner_process_id IS NOT NULL AND owner_process_id<>? AND local_quiesced_at IS NULL", cursor, s.authority.GenerationID()).
			Where("retired_by_process_id IS NULL OR retired_at IS NULL").Order("id").Limit(64).Find(&rows).Error; err != nil {
			return skipped, err
		}
		if len(rows) == 0 {
			return skipped, nil
		}
		for _, row := range rows {
			cursor = row.ID
			if err := s.RetirePTZAttempt(ctx, row); err != nil {
				if errors.Is(err, ErrDeviceIntentConflict) || errors.Is(err, ErrDeviceIntentInvalid) || errors.Is(err, gorm.ErrRecordNotFound) {
					skipped = append(skipped, row.ID)
					continue
				}
				return skipped, err
			}
		}
	}
}

// RetirePTZAttempt records proof of permanent OS-generation retirement. It
// never returns a dispatch permit, recreates an attempt, or claims that the
// device did not execute the command. The caller retains the exact snapshot
// across a failed/unknown commit and retries this fact-only operation.
func (s *DeviceOperationIntentStore) RetirePTZAttempt(ctx context.Context, expected gbmodels.GbPTZOperationAttempt) error {
	if !s.available(ctx) || !s.hasAuthority() || expected.ID == 0 || expected.OperationID == 0 ||
		expected.OwnerProcessID == nil || expected.OwnerRunID == nil || !validIntentID(*expected.OwnerProcessID) || !validIntentID(*expected.OwnerRunID) ||
		*expected.OwnerProcessID == s.authority.GenerationID() || expected.LocalQuiescedAt != nil || expected.AttemptNo <= 0 ||
		expected.StartedAt.IsZero() || !expected.LeaseUntil.After(expected.StartedAt) {
		return ErrDeviceIntentUnavailable
	}
	var hint gbmodels.GbPTZOperation
	if err := s.db.WithContext(ctx).First(&hint, expected.OperationID).Error; err != nil {
		return err
	}
	if hint.DeviceIntentID == nil || hint.DeviceEpoch == nil || *hint.DeviceEpoch <= 0 {
		return ErrDeviceIntentConflict
	}
	scope, pk, code, err := PTZIntentTarget(hint)
	if err != nil {
		return err
	}
	id := DeviceOperationIntentIdentity{OperationID: *hint.DeviceIntentID, DevicePK: int64(hint.DeviceID), DeviceCode: hint.DeviceCode,
		DeviceEpoch: *hint.DeviceEpoch, TargetScope: scope, TargetPK: pk, TargetCode: code, Kind: "ptz"}
	if !validIntentIdentity(id) {
		return ErrDeviceIntentInvalid
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// RequireRetiredTx already holds the current generation fence. A
		// lease deadline or a foreign string alone is not retirement proof.
		if err := s.authority.RequireRetiredTx(tx, *expected.OwnerProcessID); err != nil {
			return err
		}
		devices, err := queryDeviceCleanupRows(tx, ctx, id.DeviceCode, true)
		if err != nil {
			return err
		}
		if len(devices) != 1 || devices[0].ID != id.DevicePK {
			return ErrDeviceIntentConflict
		}
		var parent DeviceOperationIntent
		if err := lockedOpenAPIModel(tx, &parent, parent.TableName()).First(&parent, "operation_id=?", id.OperationID).Error; err != nil {
			return err
		}
		if !validIntentRow(parent) || parent.DeviceOperationIntentIdentity != id || parent.State != IntentDispatched {
			return ErrDeviceIntentConflict
		}
		var op gbmodels.GbPTZOperation
		if err := lockedOpenAPIModel(tx, &op, op.TableName()).First(&op, expected.OperationID).Error; err != nil {
			return err
		}
		if !samePTZCommand(op, hint) || op.DeviceIntentID == nil || *op.DeviceIntentID != id.OperationID || op.DeviceEpoch == nil || *op.DeviceEpoch != id.DeviceEpoch ||
			op.Attempt < expected.AttemptNo || op.SN != expected.SN {
			return ErrDeviceIntentConflict
		}
		var actual gbmodels.GbPTZOperationAttempt
		if err := lockedOpenAPIModel(tx, &actual, actual.TableName()).First(&actual, expected.ID).Error; err != nil {
			return err
		}
		if actual.OperationID != expected.OperationID || actual.AttemptNo != expected.AttemptNo || actual.SN != expected.SN ||
			actual.OwnerProcessID == nil || actual.OwnerRunID == nil || *actual.OwnerProcessID != *expected.OwnerProcessID || *actual.OwnerRunID != *expected.OwnerRunID ||
			!actual.StartedAt.Equal(expected.StartedAt) || !actual.LeaseUntil.Equal(expected.LeaseUntil) || actual.LocalQuiescedAt != nil {
			return ErrDeviceIntentConflict
		}
		if actual.RetiredByProcessID != nil || actual.RetiredAt != nil {
			if actual.RetiredByProcessID == nil || actual.RetiredAt == nil || actual.RetiredAt.IsZero() || !validIntentID(*actual.RetiredByProcessID) ||
				*actual.RetiredByProcessID == *actual.OwnerProcessID || actual.Status != gbmodels.PTZOperationAttemptUnknown || actual.ErrorCode != PTZOwnerProcessRetired {
				return ErrDeviceIntentConflict
			}
			if *actual.RetiredByProcessID != s.authority.GenerationID() {
				if err := s.authority.RequireRetiredTx(tx, *actual.RetiredByProcessID); err != nil {
					return err
				}
			}
			if op.Attempt == actual.AttemptNo && (op.Status == gbmodels.PTZOperationQueued || op.Status == gbmodels.PTZOperationSent || op.NextAttemptAt != nil) {
				return ErrDeviceIntentConflict
			}
			return nil
		}
		if actual.Status != gbmodels.PTZOperationAttemptDispatching && actual.Status != gbmodels.PTZOperationAttemptUnknown {
			return ErrDeviceIntentConflict
		}
		now := time.Now().In(time.Local).Truncate(time.Millisecond)
		result := tx.Model(&gbmodels.GbPTZOperationAttempt{}).
			Where("id=? AND operation_id=? AND attempt_no=? AND owner_process_id=? AND owner_run_id=? AND status=? AND local_quiesced_at IS NULL AND retired_by_process_id IS NULL AND retired_at IS NULL",
				actual.ID, op.ID, actual.AttemptNo, *actual.OwnerProcessID, *actual.OwnerRunID, actual.Status).
			Updates(map[string]any{"status": gbmodels.PTZOperationAttemptUnknown, "error_code": PTZOwnerProcessRetired, "error_message": "original process retired; remote command outcome unknown",
				"completed_at": now, "retired_by_process_id": s.authority.GenerationID(), "retired_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDeviceIntentConflict
		}
		if op.Attempt == actual.AttemptNo {
			return tx.Model(&gbmodels.GbPTZOperation{}).Where("id=? AND attempt=? AND status IN ?", op.ID, actual.AttemptNo,
				[]gbmodels.PTZOperationStatus{gbmodels.PTZOperationQueued, gbmodels.PTZOperationSent, gbmodels.PTZOperationUnknown}).
				Updates(map[string]any{"status": gbmodels.PTZOperationUnknown, "error_code": PTZOwnerProcessRetired, "error_message": "original process retired; remote command outcome unknown", "completed_at": now, "next_attempt_at": nil}).Error
		}
		return nil
	})
}
