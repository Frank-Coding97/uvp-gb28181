package playauth

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
)

// RTPRecoveryWork owns only conditional cleanup of one original RTP identity.
// The shared barrier retains it before SQL. Copies share call/join state; a
// snapshot, a new store, or a repeated reservation grants no fresh permission.
type RTPRecoveryWork struct{ work *rtpRecoveryWork }

type rtpRecoveryWork struct {
	barrier                       *DeviceOperationBarrier
	store                         *DeviceOperationIntentStore
	id                            DeviceOperationIntentIdentity
	stepID, key, processID, runID string
	ctx                           context.Context
	gate                          chan struct{}
	sealed                        atomic.Bool
	attempted, confirmed, dirty   bool
	identity                      DeviceRTPResourceIdentity
	before, durable, recovery     *DeviceRTPRecovery
	beforeCall                    *DeviceRTPRecovery
	lease                         DeviceOperationLease
	cancel                        context.CancelFunc
	runGate                       chan struct{}
	runtime                       RTPCleanupRuntime
	resolveAttempted              bool
	resolveErr                    error
	runtimeReleased               bool
	resourceAttempted             bool
	ingressAttempted              bool
}

// ReserveRTPCleanup publishes a bounded, process-local strong owner, without
// touching persistent state or granting HTTP permission. A repeated caller gets
// no handle to another owner's Run/Quiesce authority. The original creator must
// retain its handle for retries. Use the worker lifetime context, not a page's
// timeout, and the application's one shared barrier.
func (b *DeviceOperationBarrier) ReserveRTPCleanup(ctx context.Context, store *DeviceOperationIntentStore, id DeviceOperationIntentIdentity, stepID string) (*RTPRecoveryWork, error) {
	if b == nil || b.store == nil || b.store.db == nil || !store.available(ctx) || !store.hasAuthority() {
		return nil, ErrDeviceIntentUnavailable
	}
	if !validIntentIdentity(id) || !validIntentID(stepID) || id.DeviceEpoch == math.MaxInt64 {
		return nil, ErrDeviceIntentInvalid
	}
	a, err := b.store.db.DB()
	if err != nil {
		return nil, ErrDeviceIntentUnavailable
	}
	c, err := store.db.DB()
	if err != nil || a == nil || a != c {
		return nil, ErrDeviceIntentUnavailable
	}
	processID, err := sipCleanupProcessID()
	if err != nil {
		return nil, err
	}
	runID, err := NewDeviceOperationIntentID()
	if err != nil {
		return nil, err
	}
	key := id.OperationID + ":" + stepID
	b.rtpCleanupMu.Lock()
	defer b.rtpCleanupMu.Unlock()
	if old := b.rtpCleanupOwners[key]; old != nil {
		return nil, ErrDeviceIntentConflict
	}
	if len(b.rtpCleanupOwners) >= 64 {
		return nil, ErrDeviceIntentUnavailable
	}
	if b.rtpCleanupOwners == nil {
		b.rtpCleanupOwners = make(map[string]*RTPRecoveryWork)
	}
	ownerCtx, cancel := context.WithCancel(ctx)
	h := &RTPRecoveryWork{work: &rtpRecoveryWork{barrier: b, store: store, id: id, stepID: stepID, key: key, processID: processID, runID: runID, ctx: ownerCtx, cancel: cancel, gate: make(chan struct{}, 1), runGate: make(chan struct{}, 1)}}
	b.rtpCleanupOwners[key] = h
	return h, nil
}

func (h *RTPRecoveryWork) enter(ctx context.Context) (*rtpRecoveryWork, error) {
	if h == nil || h.work == nil || ctx == nil {
		return nil, ErrDeviceIntentUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case h.work.gate <- struct{}{}:
		return h.work, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Prepare joins the original epoch first. Only its confirmed owner CAS permits
// registration in the same barrier. A lost reply is retained for Quiesce, never
// retried as authorization. Foreign owners require the root authority's
// same-transaction retired-generation proof; their missing facts stay unknown.
func (h *RTPRecoveryWork) Prepare(ctx context.Context) error {
	w, err := h.enter(ctx)
	if err != nil {
		return err
	}
	defer func() { <-w.gate }()
	if w.sealed.Load() || w.attempted || w.ctx.Err() != nil {
		return ErrDeviceIntentConflict
	}
	if err = w.barrier.WaitBefore(ctx, uint(w.id.DevicePK), w.id.DeviceEpoch+1); err != nil {
		return err
	}
	loaded, err := w.store.LoadRTPResourceSteps(ctx, w.id)
	if err != nil {
		return err
	}
	w.attempted = true
	_, err = w.store.mutateRTPFactsTx(ctx, w.id, loaded.Intent.RowVersion, w.store.effectDeviceCheck(authorizeRTPCleanupDevice), func(tx *gorm.DB, out *DeviceRTPResourceSteps, now time.Time) (bool, error) {
		for i := range out.Steps {
			s := &out.Steps[i]
			if s.Identity.StepID != w.stepID {
				continue
			}
			if err := w.store.requireCleanupGenerationTx(tx, s.OwnerProcessID); err != nil {
				return false, err
			}
			if s.State != RTPStepMayHaveDispatched || (s.OwnerProcessID == w.processID && s.LocalQuiescedAt == nil) {
				return false, ErrDeviceIntentConflict
			}
			old := s.Recovery
			if old != nil {
				if err := w.store.requireCleanupGenerationTx(tx, old.OwnerProcessID); err != nil {
					return false, err
				}
				if (old.OwnerProcessID == w.processID && old.LocalQuiescedAt == nil) || old.Generation == math.MaxInt64 {
					return false, ErrDeviceIntentConflict
				}
			}
			r := &DeviceRTPRecovery{Version: 1, Generation: 1, OwnerProcessID: w.processID, OwnerRunID: w.runID, ReservedAt: now, PriorObservationIncomplete: s.LocalQuiescedAt == nil}
			if old != nil {
				r.Generation = old.Generation + 1
				r.CallSequence = old.CallSequence
				r.ResourceEvidence = old.ResourceEvidence
				r.IngressEvidence = old.IngressEvidence
				r.PriorObservationIncomplete = r.PriorObservationIncomplete || old.PriorObservationIncomplete || old.LocalQuiescedAt == nil
				if old.CurrentCall != nil && old.CurrentCall.Outcome == rtpCallUnknown {
					r.PriorObservationIncomplete = true
				}
			}
			w.identity = s.Identity
			w.before = cloneRTPRecovery(old)
			w.recovery = cloneRTPRecovery(r)
			w.durable = cloneRTPRecovery(r)
			s.Recovery = r
			if !validRTPRecovery(*s, now) {
				return false, ErrDeviceIntentUnavailable
			}
			return true, nil
		}
		return false, ErrDeviceIntentConflict
	})
	if err != nil {
		return err
	}
	// The once-only ticket is internal and tied to this exact retained object.
	ticket := &rtpCleanupTicket{owner: w}
	lease, err := w.barrier.beginRTPCleanup(ctx, ticket)
	if err != nil {
		return err
	}
	w.lease = lease
	w.confirmed = true
	return nil
}

func authorizeRTPCleanupDevice(tx *gorm.DB, ctx context.Context, id DeviceOperationIntentIdentity) error {
	rows, err := queryDeviceCleanupRows(tx, ctx, id.DeviceCode, true)
	if err != nil || len(rows) != 1 || rows[0].ID != id.DevicePK {
		return ErrDeviceIntentUnavailable
	}
	state, err := validateDeviceCleanupRow(rows[0])
	if err != nil {
		return ErrDeviceIntentUnavailable
	}
	if state.AccessEpoch <= id.DeviceEpoch || state.CleanupCompletedEpoch >= state.AccessEpoch || state.CleanupCompletedEpoch > id.DeviceEpoch {
		return ErrDeviceIntentRevoked
	}
	return nil
}

type rtpCleanupTicket struct {
	owner    *rtpRecoveryWork
	consumed atomic.Bool
}

func (b *DeviceOperationBarrier) beginRTPCleanup(ctx context.Context, ticket *rtpCleanupTicket) (DeviceOperationLease, error) {
	if ticket == nil || ticket.owner == nil || ticket.owner.barrier != b || !ticket.consumed.CompareAndSwap(false, true) {
		return nil, ErrDeviceIntentConflict
	}
	w := ticket.owner
	waitCtx, cancel := context.WithTimeout(ctx, deviceOperationAdmissionTimeout)
	defer cancel()
	lane, ref := b.acquireLane(uint(w.id.DevicePK))
	lane.mu.Lock()
	if err := waitLaneAvailableLocked(lane, waitCtx); err != nil {
		lane.mu.Unlock()
		ref.release()
		return nil, err
	}
	lane.guardHeld = true
	lane.mu.Unlock()
	err := b.store.db.WithContext(waitCtx).Transaction(func(tx *gorm.DB) error {
		if err := w.store.checkAuthorityTx(tx); err != nil {
			return err
		}
		if err := authorizeRTPCleanupDevice(tx, waitCtx, w.id); err != nil {
			return err
		}
		out, err := readRTPSteps(tx, w.id)
		if err != nil {
			return err
		}
		for _, s := range out.Steps {
			if s.Identity == w.identity && reflect.DeepEqual(s.Recovery, w.durable) {
				return nil
			}
		}
		return ErrDeviceIntentConflict
	})
	if err == nil {
		err = waitCtx.Err()
	}
	if err != nil {
		b.releaseAdmissionGate(lane)
		ref.release()
		return nil, err
	}
	lease := newDeviceOperationLease(w.ctx, lane, ref, w.id.DeviceEpoch)
	lease.deviceCode = w.id.DeviceCode
	lane.mu.Lock()
	lane.active[lease] = struct{}{}
	lane.guardHeld = false
	signalDeviceOperationLaneLocked(lane)
	lane.mu.Unlock()
	return lease, nil
}

func cloneRTPRecovery(r *DeviceRTPRecovery) *DeviceRTPRecovery { return recoveryToWire(r).recovery() }

func (h *RTPRecoveryWork) CloseResource(ctx context.Context, call func(context.Context, DeviceRTPResourceIdentity) (string, error)) (string, error) {
	return h.closeCall(ctx, rtpCleanupResource, call)
}
func (h *RTPRecoveryWork) CloseIngress(ctx context.Context, call func(context.Context, DeviceRTPResourceIdentity) (string, error)) (string, error) {
	return h.closeCall(ctx, rtpCleanupIngress, call)
}

func (h *RTPRecoveryWork) closeCall(ctx context.Context, action string, call func(context.Context, DeviceRTPResourceIdentity) (string, error)) (string, error) {
	w, err := h.enter(ctx)
	if err != nil {
		return "", err
	}
	defer func() { <-w.gate }()
	if w.sealed.Load() || !w.confirmed || w.dirty || w.ctx.Err() != nil || w.lease.Context().Err() != nil || call == nil || w.recovery.CallSequence == math.MaxInt64 {
		return "", ErrDeviceIntentConflict
	}
	loaded, err := w.store.LoadRTPResourceSteps(ctx, w.id)
	if err != nil {
		return "", err
	}
	w.beforeCall = cloneRTPRecovery(w.durable)
	_, err = w.store.mutateRTPFacts(ctx, w.id, loaded.Intent.RowVersion, w.store.effectDeviceCheck(authorizeRTPCleanupDevice), func(out *DeviceRTPResourceSteps, now time.Time) (bool, error) {
		for i := range out.Steps {
			s := &out.Steps[i]
			if s.Identity != w.identity {
				continue
			}
			if !reflect.DeepEqual(s.Recovery, w.durable) || s.Recovery.LocalQuiescedAt != nil {
				return false, ErrDeviceIntentConflict
			}
			r := cloneRTPRecovery(s.Recovery)
			if r.CurrentCall != nil && r.CurrentCall.Outcome == rtpCallUnknown {
				r.PriorObservationIncomplete = true
			}
			r.CallSequence++
			r.CurrentCall = &DeviceRTPCleanupCall{Sequence: r.CallSequence, Action: action, DispatchStartedAt: now}
			s.Recovery = r
			if !validRTPRecovery(*s, now) {
				return false, ErrDeviceIntentUnavailable
			}
			w.durable = cloneRTPRecovery(r)
			w.recovery = cloneRTPRecovery(r)
			w.dirty = true
			return true, nil
		}
		return false, ErrDeviceIntentConflict
	})
	if err != nil {
		if w.dirty {
			now := time.Now().UTC().Truncate(time.Microsecond)
			w.recovery.CurrentCall.Outcome = rtpCallNotInvoked
			w.recovery.CurrentCall.LocalQuiescedAt = &now
		}
		return "", err
	}
	// Only the confirmed CAS above reaches this synchronous, once-only call.
	// Joining is the gate, not ctx cancellation or an HTTP timeout assumption.
	admissionErr := ctx.Err()
	if admissionErr == nil {
		admissionErr = w.lease.Context().Err()
	}
	if admissionErr == nil && w.sealed.Load() {
		admissionErr = ErrDeviceIntentConflict
	}
	if admissionErr != nil {
		now := time.Now().UTC().Truncate(time.Microsecond)
		w.recovery.CurrentCall.Outcome = rtpCallNotInvoked
		w.recovery.CurrentCall.LocalQuiescedAt = &now
		return "", errors.Join(admissionErr, w.flush(ctx))
	}
	callCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(w.lease.Context(), cancel)
	result, callErr := call(callCtx, w.identity)
	stop()
	cancel()
	now := time.Now().UTC().Truncate(time.Microsecond)
	c := w.recovery.CurrentCall
	c.LocalQuiescedAt = &now
	if callErr == nil && validRTPCloseResult(result, action == rtpCleanupIngress) && !(result == "rtp_ingress_drained" && w.identity.TCPMode != 0) {
		c.Outcome = rtpCallObserved
		c.Result = result
		c.ResultObservedAt = &now
		e := &DeviceRTPCleanupEvidence{Sequence: c.Sequence, Result: result, ObservedAt: now}
		slot := &w.recovery.ResourceEvidence
		if action == rtpCleanupIngress {
			slot = &w.recovery.IngressEvidence
		}
		if *slot == nil || rtpCleanupEvidenceRank((*slot).Result) <= rtpCleanupEvidenceRank(result) {
			*slot = e
		}
	} else {
		c.Outcome = rtpCallUnknown
		if callErr == nil {
			callErr = ErrDeviceIntentUnavailable
		}
	}
	if err = w.flush(ctx); err != nil {
		return "", errors.Join(callErr, err)
	}
	if callErr != nil {
		return "", callErr
	}
	return result, nil
}

// Flush retries only this owner's pending SQL facts, never the HTTP call.
func (h *RTPRecoveryWork) Flush(ctx context.Context) error {
	w, err := h.enter(ctx)
	if err != nil {
		return err
	}
	defer func() { <-w.gate }()
	return w.flush(ctx)
}

func (w *rtpRecoveryWork) flush(ctx context.Context) error {
	if w.recovery == nil {
		return nil
	}
	loaded, err := w.store.LoadRTPResourceSteps(ctx, w.id)
	if err != nil {
		return err
	}
	_, err = w.store.mutateRTPFacts(ctx, w.id, loaded.Intent.RowVersion, authorizeRTPCleanupDevice, func(out *DeviceRTPResourceSteps, now time.Time) (bool, error) {
		for i := range out.Steps {
			s := &out.Steps[i]
			if s.Identity != w.identity {
				continue
			}
			if !w.confirmed && reflect.DeepEqual(s.Recovery, w.before) {
				return false, nil
			} // owner prepare did not commit
			if w.dirty && w.recovery.CurrentCall != nil && w.recovery.CurrentCall.Outcome == rtpCallNotInvoked && reflect.DeepEqual(s.Recovery, w.beforeCall) {
				q := w.recovery.LocalQuiescedAt
				w.recovery = cloneRTPRecovery(w.beforeCall)
				w.recovery.LocalQuiescedAt = q
				w.durable = cloneRTPRecovery(w.beforeCall)
			}
			if reflect.DeepEqual(s.Recovery, w.recovery) {
				return false, nil
			}
			if !reflect.DeepEqual(s.Recovery, w.durable) {
				// A previous outcome write may have committed with its reply lost.
				// Quiesce can append this same owner's actual later exit, but cannot
				// replace any call, result, identity or earlier observation.
				prefix := cloneRTPRecovery(w.recovery)
				if s.Recovery != nil && s.Recovery.LocalQuiescedAt == nil {
					prefix.LocalQuiescedAt = nil
				}
				if !reflect.DeepEqual(s.Recovery, prefix) {
					return false, ErrDeviceIntentConflict
				}
			}
			s.Recovery = cloneRTPRecovery(w.recovery)
			if !validRTPRecovery(*s, now) {
				return false, ErrDeviceIntentUnavailable
			}
			return true, nil
		}
		return false, ErrDeviceIntentConflict
	})
	if err != nil {
		return err
	}
	w.durable = cloneRTPRecovery(w.recovery)
	w.dirty = false
	w.beforeCall = nil
	return nil
}

// Quiesce seals new calls, joins the actual in-flight call, persists final local
// facts and only then releases its lease and strong registry entry. It does not
// change remote completion, the intent state, or the device cleanup watermark.
func (h *RTPRecoveryWork) Quiesce(ctx context.Context) error {
	if h == nil || h.work == nil || ctx == nil {
		return ErrDeviceIntentUnavailable
	}
	h.work.sealed.Store(true)
	h.work.cancel()
	// Resolve and the entire typed run are part of this owner's actual join
	// domain. Cancellation alone cannot release a late-returned runtime.
	select {
	case h.work.runGate <- struct{}{}:
		defer func() { <-h.work.runGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	w, err := h.enter(ctx)
	if err != nil {
		return err
	}
	defer func() { <-w.gate }()
	if !isNilInterface(w.runtime) && !w.runtimeReleased {
		w.runtime.Release()
		w.runtimeReleased = true
	}
	if w.recovery != nil && w.recovery.LocalQuiescedAt == nil {
		now := time.Now().UTC().Truncate(time.Microsecond)
		w.recovery.LocalQuiescedAt = &now
	}
	if err = w.flush(ctx); err != nil {
		return err
	}
	if w.lease != nil {
		w.lease.Release()
	}
	w.barrier.rtpCleanupMu.Lock()
	defer w.barrier.rtpCleanupMu.Unlock()
	if registered := w.barrier.rtpCleanupOwners[w.key]; registered != nil && registered.work == w {
		delete(w.barrier.rtpCleanupOwners, w.key)
	}
	return nil
}
