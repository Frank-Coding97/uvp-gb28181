package playauth

import (
	"context"
	"database/sql"
	"sync/atomic"

	"gorm.io/gorm"
)

// SIPCleanupWorkTicket is an opaque, process-local, single-use registration
// ticket, not permission to send SIP. Copying it shares the consumption bit.
// Load and idempotent Prepare observations never reconstruct a ticket.
type SIPCleanupWorkTicket struct{ work *sipCleanupWork }

type sipCleanupWork struct {
	authority *sql.DB
	intent    DeviceOperationIntentIdentity
	identity  DeviceSIPCleanupAttemptIdentity
	runID     string
	consumed  atomic.Bool
}

// PrepareSIPBranchCleanupWork mints a ticket only after the new attempt's CAS
// commit is confirmed. Keep the original operation lease until BeginSIPCleanup
// succeeds and the concrete cleanup owner is installed in the strong registry.
func (s *DeviceOperationIntentStore) PrepareSIPBranchCleanupWork(ctx context.Context, id DeviceOperationIntentIdentity, version int64, identity DeviceSIPCleanupAttemptIdentity) (DeviceSIPInviteSteps, *SIPCleanupWorkTicket, error) {
	out, err := s.PrepareSIPBranchCleanup(ctx, id, version, identity)
	if err != nil {
		return DeviceSIPInviteSteps{}, nil, err
	}
	if out.Intent.RowVersion == version {
		return out, nil, nil // existing immutable attempt, observation only
	}
	authority, err := s.db.DB()
	if err != nil || authority == nil {
		return DeviceSIPInviteSteps{}, nil, ErrDeviceIntentUnavailable
	}
	runID, err := sipCleanupProcessID()
	if err != nil {
		return DeviceSIPInviteSteps{}, nil, ErrDeviceIntentUnavailable
	}
	work := &sipCleanupWork{authority: authority, intent: id, identity: cloneSIPCleanupIdentity(identity), runID: runID}
	if out.Intent.RowVersion != version+1 || !work.matches(out) {
		return DeviceSIPInviteSteps{}, nil, ErrDeviceIntentConflict
	}
	return out, &SIPCleanupWorkTicket{work: work}, nil
}

func (work *sipCleanupWork) matches(out DeviceSIPInviteSteps) bool {
	if out.Intent.DeviceOperationIntentIdentity != work.intent {
		return false
	}
	for _, step := range out.Steps {
		if step.Identity.StepID != work.identity.ACK.Request.StepID || step.KnownBranch == nil {
			continue
		}
		attempts := step.KnownBranch.CleanupAttempts
		if len(attempts) == 0 {
			return false
		}
		last := attempts[len(attempts)-1]
		return equalSIPCleanupIdentity(last.Identity, work.identity) && last.OwnerRunID == work.runID &&
			last.State == SIPCleanupPrepared && last.RowVersion == 1 && last.LocalQuiescedAt == nil
	}
	return false
}

// BeginSIPCleanup registers exact old-epoch compensation in the SAME shared
// barrier as BeginEpoch and LockTransfer. It does not relax business admission
// or authorize ACK/BYE: their durable dispatch CAS remains mandatory.
// Register before PrepareBYE (even connection creation), overlap the original
// lease, and release only after concrete Quiesced plus durable observed facts.
// A canceled/failed registration consumes its ticket; no retry gains an owner.
func (b *DeviceOperationBarrier) BeginSIPCleanup(ctx context.Context, ticket *SIPCleanupWorkTicket) (DeviceOperationLease, error) {
	if isNilInterface(ctx) || b == nil || b.store == nil || b.store.db == nil {
		return nil, ErrDeviceOperationUnavailable
	}
	if ticket == nil || ticket.work == nil {
		return nil, ErrDeviceIntentInvalid
	}
	work := ticket.work
	if !work.consumed.CompareAndSwap(false, true) {
		return nil, ErrDeviceIntentConflict
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runID, err := sipCleanupProcessID()
	if err != nil || runID != work.runID || !validIntentIdentity(work.intent) {
		return nil, ErrDeviceIntentConflict
	}
	authority, err := b.store.db.DB()
	if err != nil || authority == nil || authority != work.authority {
		return nil, ErrDeviceIntentUnavailable
	}
	waitCtx, cancel := context.WithTimeout(ctx, deviceOperationAdmissionTimeout)
	defer cancel()
	lane, ref := b.acquireLane(uint(work.intent.DevicePK))
	lane.mu.Lock()
	if err := waitLaneAvailableLocked(lane, waitCtx); err != nil {
		lane.mu.Unlock()
		ref.release()
		return nil, err
	}
	lane.guardHeld = true
	lane.mu.Unlock()
	// The admission gate stays reserved, but no lane mutex is held over SQL.
	// This serializes registration with transfer Commit/Release notification.
	err = b.store.db.WithContext(waitCtx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeSIPCancelCleanupDevice(tx, waitCtx, work.intent); err != nil {
			return err
		}
		out, err := readSIPInviteSteps(tx, work.intent)
		if err != nil {
			return err
		}
		if !work.matches(out) {
			return ErrDeviceIntentConflict
		}
		return nil
	})
	if err == nil {
		err = waitCtx.Err()
	}
	if err != nil {
		b.releaseAdmissionGate(lane)
		ref.release()
		return nil, err
	}
	lease := newDeviceOperationLease(ctx, lane, ref, work.intent.DeviceEpoch)
	lane.mu.Lock()
	lane.active[lease] = struct{}{}
	lane.guardHeld = false
	signalDeviceOperationLaneLocked(lane)
	lane.mu.Unlock()
	return lease, nil
}
