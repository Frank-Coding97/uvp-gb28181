package playauth

import (
	"database/sql"
	"errors"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// PTZResponseObservation prepares bookkeeping only. It cannot issue an attempt
// or refresh the original epoch, and remains tied to its caller's transaction.
type PTZResponseObservation struct {
	tx       *gorm.DB
	original gbmodels.GbPTZOperation
	identity DeviceOperationIntentIdentity
	revoked  bool
	consumed atomic.Bool
}

// BeginPTZResponseObservation must precede response/business writes. Device and
// original intent are locked/read before the business CAS. Late responses do
// not require the current epoch: only a later Claim can authorize a new effect.
func (s *DeviceOperationIntentStore) BeginPTZResponseObservation(tx *gorm.DB, original gbmodels.GbPTZOperation) (*PTZResponseObservation, error) {
	if s == nil || tx == nil || tx.Statement == nil || original.DeviceIntentID == nil || original.DeviceEpoch == nil {
		return nil, ErrDeviceIntentUnavailable
	}
	if _, ok := tx.Statement.ConnPool.(*sql.Tx); !ok {
		return nil, ErrDeviceIntentInvalid
	}
	scope, pk, code, err := PTZIntentTarget(original)
	if err != nil {
		return nil, err
	}
	id := DeviceOperationIntentIdentity{OperationID: *original.DeviceIntentID, DevicePK: int64(original.DeviceID), DeviceCode: original.DeviceCode,
		DeviceEpoch: *original.DeviceEpoch, TargetScope: scope, TargetPK: pk, TargetCode: code, Kind: "ptz"}
	if !validIntentIdentity(id) || original.ID == 0 || original.Action != "home_position" || original.CmdType != "DeviceControl" {
		return nil, ErrDeviceIntentInvalid
	}
	devices, err := queryDeviceCleanupRows(tx, tx.Statement.Context, id.DeviceCode, true)
	if err != nil {
		return nil, err
	}
	if len(devices) != 1 || devices[0].ID != id.DevicePK {
		return nil, ErrDeviceIntentConflict
	}
	var parent DeviceOperationIntent
	if err := tx.First(&parent, "operation_id=?", id.OperationID).Error; err != nil {
		return nil, err
	}
	if !validIntentRow(parent) || parent.DeviceOperationIntentIdentity != id {
		return nil, ErrDeviceIntentConflict
	}
	var stored gbmodels.GbPTZOperation
	if err := tx.First(&stored, original.ID).Error; err != nil {
		return nil, err
	}
	if !samePTZCommand(stored, original) || stored.DeviceIntentID == nil || *stored.DeviceIntentID != id.OperationID || stored.DeviceEpoch == nil || *stored.DeviceEpoch != id.DeviceEpoch {
		return nil, ErrDeviceIntentConflict
	}
	revoked := devices[0].AccessEpoch == nil || *devices[0].AccessEpoch != id.DeviceEpoch ||
		devices[0].CleanupCompletedEpoch == nil || *devices[0].CleanupCompletedEpoch != id.DeviceEpoch
	return &PTZResponseObservation{tx: tx, original: original, identity: id, revoked: revoked}, nil
}

// ReserveHomePositionQuery atomically links one independently reserved child.
// Fields affecting authority always come from the original operation, not the
// current device row or the caller's proposed child.
func (p *PTZResponseObservation) ReserveHomePositionQuery(child gbmodels.GbPTZOperation) error {
	if p == nil || p.tx == nil || child.ID != 0 || child.OperationID == "" || child.CmdType != "HomePositionQuery" || child.Action != "refresh_home_position" ||
		child.Attempt != 0 || child.Status != gbmodels.PTZOperationQueued || !child.ResponseRequired || child.MaxAttempts < 1 || child.QueueDeadlineAt == nil {
		return ErrDeviceIntentInvalid
	}
	if !p.consumed.CompareAndSwap(false, true) {
		return ErrDeviceIntentConflict
	}
	// A late ACK may only reopen the observation path for an attempt that the
	// platform retired with a complete cross-process certificate. An attempt
	// row that never existed is the regular ACK path, not a retirement.
	var attempt gbmodels.GbPTZOperationAttempt
	if err := p.tx.First(&attempt, "operation_id=? AND attempt_no=?", p.original.ID, p.original.Attempt).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	} else if attempt.Status == gbmodels.PTZOperationAttemptUnknown && attempt.ErrorCode == PTZOwnerProcessRetired {
		if attempt.RetiredByProcessID == nil || attempt.RetiredAt == nil || attempt.RetiredAt.IsZero() ||
			!validIntentID(*attempt.RetiredByProcessID) || attempt.OwnerProcessID == nil ||
			*attempt.RetiredByProcessID == *attempt.OwnerProcessID || attempt.LocalQuiescedAt != nil {
			return ErrDeviceIntentConflict
		}
	}
	parentID, err := NewDeviceOperationIntentID()
	if err != nil {
		return err
	}
	id := p.identity
	id.OperationID = parentID
	child.DeviceID, child.DeviceCode = p.original.DeviceID, p.original.DeviceCode
	child.ChannelID, child.ChannelCode = p.original.ChannelID, p.original.ChannelCode
	child.DeviceEpoch, child.DeviceIntentID = &id.DeviceEpoch, &id.OperationID
	child.ProfileVersion, child.ProfileCharset = p.original.ProfileVersion, p.original.ProfileCharset
	child.TargetScope, child.TargetCode, child.ScopeKey = p.original.TargetScope, p.original.TargetCode, p.original.ScopeKey
	child.ActorID, child.ActorDeptID = p.original.ActorID, p.original.ActorDeptID
	child.TriggerOperationID = &p.original.OperationID
	child.IdempotencyKey = "home-reconcile:" + p.original.OperationID
	now := time.Now().UTC().Truncate(time.Millisecond)
	parent := DeviceOperationIntent{DeviceOperationIntentIdentity: id, ContractVersion: 1, State: IntentReserved, RowVersion: 1, CreatedAt: now, UpdatedAt: now}
	if p.revoked {
		parent.State, parent.RowVersion, parent.CancelledAt = IntentCancelled, 2, &now
		child.Status, child.ErrorCode = gbmodels.PTZOperationRejected, "DEVICE_EPOCH_REVOKED"
		completedAt := now.In(time.Local)
		child.CompletedAt = &completedAt
	}
	if err := p.tx.Create(&parent).Error; err != nil {
		return err
	}
	if err := p.tx.Create(&child).Error; err != nil {
		return err
	}
	if err := p.tx.Model(&child).UpdateColumn("attempt", 0).Error; err != nil {
		return err
	}
	r := p.tx.Model(&gbmodels.GbPTZOperation{}).Where("id=? AND status=? AND reconcile_operation_id IS NULL", p.original.ID, gbmodels.PTZOperationAccepted).
		Where("operation_id=? AND device_intent_id=? AND device_epoch=? AND cmd_type=? AND action=?", p.original.OperationID, p.identity.OperationID, p.identity.DeviceEpoch, p.original.CmdType, p.original.Action).
		Update("reconcile_operation_id", child.OperationID)
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return ErrDeviceIntentConflict
	}
	return nil
}
