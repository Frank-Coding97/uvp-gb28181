package playauth

import (
	"context"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// PTZIntentTarget separates the wire address from the resource which authorized
// it. Alarm addresses are resolved through a channel; they never grant device
// authority, including when the wire address falls back to the device code.
func PTZIntentTarget(op gbmodels.GbPTZOperation) (string, int64, string, error) {
	switch op.TargetScope {
	case "channel", "alarm":
		if op.ChannelID == 0 || op.ChannelCode == "" || strings.TrimSpace(op.TargetCode) == "" ||
			(op.TargetScope == "channel" && op.TargetCode != op.ChannelCode) {
			return "", 0, "", ErrDeviceIntentInvalid
		}
		return "channel", int64(op.ChannelID), op.ChannelCode, nil
	case "device":
		if op.DeviceID != 0 && op.DeviceCode != "" && op.TargetCode == op.DeviceCode {
			return "device", int64(op.DeviceID), op.DeviceCode, nil
		}
	}
	return "", 0, "", ErrDeviceIntentInvalid
}

func ptzIntentTargetMatches(op gbmodels.GbPTZOperation, id DeviceOperationIntentIdentity) bool {
	scope, pk, code, err := PTZIntentTarget(op)
	return err == nil && scope == id.TargetScope && pk == id.TargetPK && code == id.TargetCode
}

// ReservePTZOperation binds the original authorization and the business row in
// one transaction. It returns no network permission, including on replay.
// The PTZ-specific boundary deliberately exposes neither a transaction nor a
// callback that could perform an effect before the outer commit is confirmed.
func (s *DeviceOperationIntentStore) ReservePTZOperation(ctx context.Context, id DeviceOperationIntentIdentity, operation gbmodels.GbPTZOperation) (gbmodels.GbPTZOperation, error) {
	if !s.available(ctx) || !s.hasAuthority() {
		return gbmodels.GbPTZOperation{}, ErrDeviceIntentUnavailable
	}
	if !validIntentIdentity(id) || id.Kind != "ptz" ||
		operation.ID != 0 || operation.OperationID == "" || operation.IdempotencyKey == "" ||
		operation.DeviceID != uint(id.DevicePK) || operation.DeviceCode != id.DeviceCode ||
		!ptzIntentTargetMatches(operation, id) ||
		operation.DeviceEpoch != nil || operation.DeviceIntentID != nil ||
		operation.Status != gbmodels.PTZOperationQueued || operation.Attempt != 0 ||
		operation.DispatchStartedAt != nil || operation.SentAt != nil || operation.CompletedAt != nil ||
		operation.MaxAttempts < 1 || (operation.ResponseRequired && operation.QueueDeadlineAt == nil) {
		return gbmodels.GbPTZOperation{}, ErrDeviceIntentInvalid
	}
	if id.TargetScope == "channel" && (operation.ChannelID != uint(id.TargetPK) || operation.ChannelCode != id.TargetCode) {
		return gbmodels.GbPTZOperation{}, ErrDeviceIntentInvalid
	}
	out := operation
	out.DeviceEpoch, out.DeviceIntentID = &id.DeviceEpoch, &id.OperationID
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.checkAuthorityTx(tx); err != nil {
			return err
		}
		if err := authorizeIntentDevice(tx, ctx, id); err != nil {
			return err
		}
		now := time.Now().UTC().Truncate(time.Millisecond)
		parent := DeviceOperationIntent{DeviceOperationIntentIdentity: id, ContractVersion: 1, State: IntentReserved,
			RowVersion: 1, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&parent).Error; err != nil {
			return err
		}
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		// The legacy model has default attempt=1. No attempt has been admitted:
		// override that default inside this same, still-uncommitted transaction.
		if err := tx.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", out.ID).UpdateColumn("attempt", 0).Error; err != nil {
			return err
		}
		out.Attempt = 0
		return nil
	})
	if err != nil {
		if out.ID != 0 {
			ticket := &PTZReservationOutcome{store: s, identity: id, operation: operation, cause: normalizeIntentError(err)}
			resolved, found, resolveErr := ticket.resolve(ctx, false)
			if resolveErr != nil {
				return gbmodels.GbPTZOperation{}, ticket
			}
			if found {
				return resolved, nil
			}
		}
		return gbmodels.GbPTZOperation{}, normalizeIntentError(err)
	}
	return out, nil
}

// ClaimPTZAttempt grants one attempt only after the parent transition, business
// CAS and attempt insertion have all committed. Reading these rows grants no
// permission. The caller must additionally hold the original-epoch lease.
func (s *DeviceOperationIntentStore) ClaimPTZAttempt(ctx context.Context, id DeviceOperationIntentIdentity, operationPK uint, expectedAttempt int, now time.Time) (gbmodels.GbPTZOperationAttempt, error) {
	if !s.available(ctx) || !s.hasAuthority() {
		return gbmodels.GbPTZOperationAttempt{}, ErrDeviceIntentUnavailable
	}
	if !validIntentIdentity(id) || id.Kind != "ptz" || operationPK == 0 || expectedAttempt < 0 || now.IsZero() {
		return gbmodels.GbPTZOperationAttempt{}, ErrDeviceIntentInvalid
	}
	// All three native PTZ schemas store attempt times with millisecond
	// precision. Normalize before persistence, never loosen receipt equality.
	now = now.In(time.Local).Truncate(time.Millisecond)
	run, err := NewDeviceOperationIntentID()
	if err != nil {
		return gbmodels.GbPTZOperationAttempt{}, err
	}
	var out gbmodels.GbPTZOperationAttempt
	var snapshot gbmodels.GbPTZOperation
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.checkAuthorityTx(tx); err != nil {
			return err
		}
		if err := authorizeIntentDevice(tx, ctx, id); err != nil {
			return err
		}
		var parent DeviceOperationIntent
		if err := tx.First(&parent, "operation_id = ?", id.OperationID).Error; err != nil {
			return err
		}
		if !validIntentRow(parent) || parent.RowVersion == math.MaxInt64 || parent.DeviceOperationIntentIdentity != id {
			return ErrDeviceIntentConflict
		}
		var rows []gbmodels.GbPTZOperation
		if err := tx.Where("device_intent_id = ?", id.OperationID).Limit(2).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 1 {
			return ErrDeviceIntentConflict
		}
		op := rows[0]
		snapshot = op
		if op.ID != operationPK || op.DeviceEpoch == nil || *op.DeviceEpoch != id.DeviceEpoch ||
			op.DeviceID != uint(id.DevicePK) || op.DeviceCode != id.DeviceCode || !ptzIntentTargetMatches(op, id) ||
			(id.TargetScope == "channel" && (op.ChannelID != uint(id.TargetPK) || op.ChannelCode != id.TargetCode)) ||
			op.Attempt != expectedAttempt || op.Attempt >= op.MaxAttempts ||
			(op.Status != gbmodels.PTZOperationQueued && op.Status != gbmodels.PTZOperationSent) {
			return ErrDeviceIntentConflict
		}
		if op.Attempt == 0 {
			if parent.State != IntentReserved || op.DispatchStartedAt != nil || (op.ResponseRequired && (op.QueueDeadlineAt == nil || !op.QueueDeadlineAt.After(now))) {
				return ErrDeviceIntentConflict
			}
		} else {
			if parent.State != IntentDispatched || !op.ResponseRequired || op.NextAttemptAt == nil || op.NextAttemptAt.After(now) || op.TransportDeadlineAt == nil || !op.TransportDeadlineAt.After(now) || op.DispatchStartedAt == nil {
				return ErrDeviceIntentConflict
			}
		}
		var live int64
		if err := tx.Model(&gbmodels.GbPTZOperationAttempt{}).Where("operation_id = ? AND (status = ? OR local_quiesced_at IS NULL)", op.ID, gbmodels.PTZOperationAttemptDispatching).Count(&live).Error; err != nil {
			return err
		}
		if live != 0 {
			return ErrDeviceIntentConflict
		}
		start := now
		if op.DispatchStartedAt != nil {
			start = *op.DispatchStartedAt
		}
		deadline := start.Add(15 * time.Second)
		if op.TransportDeadlineAt != nil {
			deadline = *op.TransportDeadlineAt
		}
		lease := deadline
		var next *time.Time
		if op.MaxAttempts > 1 {
			for _, slot := range []time.Time{start.Add(time.Second), start.Add(6 * time.Second)} {
				if slot.After(now) && slot.Before(deadline) {
					lease = slot
					n := slot
					next = &n
					break
				}
			}
		}
		if !lease.After(now) {
			return ErrDeviceIntentConflict
		}
		if op.Attempt == 0 {
			res := tx.Model(&DeviceOperationIntent{}).Where("operation_id = ? AND row_version = ? AND state = ?", id.OperationID, parent.RowVersion, IntentReserved).
				Updates(map[string]any{"state": IntentDispatched, "row_version": parent.RowVersion + 1, "dispatch_started_at": now.UTC(), "updated_at": now.UTC()})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return ErrDeviceIntentConflict
			}
		}
		res := tx.Model(&gbmodels.GbPTZOperation{}).Where("id = ? AND attempt = ? AND device_intent_id = ? AND device_epoch = ? AND status = ?", op.ID, expectedAttempt, id.OperationID, id.DeviceEpoch, op.Status).
			Updates(map[string]any{"attempt": expectedAttempt + 1, "dispatch_started_at": start, "transport_deadline_at": deadline, "next_attempt_at": next})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrDeviceIntentConflict
		}
		process := s.authority.GenerationID()
		out = gbmodels.GbPTZOperationAttempt{OperationID: op.ID, AttemptNo: expectedAttempt + 1, SN: op.SN,
			Status: gbmodels.PTZOperationAttemptDispatching, StartedAt: now, LeaseUntil: lease, OwnerProcessID: &process, OwnerRunID: &run}
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		// Use the database's precision/location representation in both the
		// returned attempt and any private commit-unknown receipt. This read
		// is still inside the transaction and grants no permission by itself.
		var stored gbmodels.GbPTZOperationAttempt
		if err := tx.Where("operation_id=? AND attempt_no=? AND owner_process_id=? AND owner_run_id=?", op.ID, expectedAttempt+1, process, run).First(&stored).Error; err != nil {
			return err
		}
		if stored.ID != out.ID || stored.SN != out.SN || stored.Status != gbmodels.PTZOperationAttemptDispatching || stored.LocalQuiescedAt != nil {
			return ErrDeviceIntentConflict
		}
		out = stored
		return nil
	})
	if err != nil {
		if out.OwnerRunID != nil {
			return gbmodels.GbPTZOperationAttempt{}, &PTZUnissuedAttempt{store: s, identity: id, operation: snapshot, attempt: out, cause: normalizeIntentError(err)}
		}
		return gbmodels.GbPTZOperationAttempt{}, normalizeIntentError(err)
	}
	return out, nil
}
