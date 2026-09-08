package playauth

import (
	"context"
	"reflect"
	"sync/atomic"
	"time"
)

// RTPResourceWork is a retained local cleanup witness. Only its own confirmed
// dispatch CAS can grant sending permission. Copying the handle shares its
// single-use calls and join domain; Load cannot recreate permission. The actual
// parent must retain this witness BEFORE Dispatch, including an unknown reply,
// and release its lease only after Quiesce and all other children have joined.
type RTPResourceWork struct{ work *rtpResourceWork }

type rtpResourceWork struct {
	store                                 *DeviceOperationIntentStore
	id                                    DeviceOperationIntentIdentity
	step                                  DeviceRTPResourceStep
	gate                                  chan struct{}
	sealed                                atomic.Bool
	runID, processID                      string
	attempted, confirmed                  bool
	opened, resourceClosed, ingressClosed bool
}

// DispatchRTPResourceWork is a convenience for isolated confirmed-dispatch
// callers. It is NOT the parent-facing API: a real parent must use Prepare,
// retain the witness, then Dispatch so unknown replies cannot discard it.
func (s *DeviceOperationIntentStore) DispatchRTPResourceWork(ctx context.Context, id DeviceOperationIntentIdentity, version int64, stepID string) (DeviceRTPResourceSteps, *RTPResourceWork, error) {
	work, err := s.PrepareRTPResourceWork(ctx, id, stepID)
	if err != nil {
		return DeviceRTPResourceSteps{}, nil, err
	}
	out, err := work.Dispatch(ctx, version)
	if err != nil {
		return DeviceRTPResourceSteps{}, nil, err
	}
	return out, work, nil
}

// PrepareRTPResourceWork creates a cleanup owner with NO sending permission.
// Publish it before Dispatch so a lost commit reply still has an actual local
// witness that can persist its own run's no-call/quiescence facts.
func (s *DeviceOperationIntentStore) PrepareRTPResourceWork(ctx context.Context, id DeviceOperationIntentIdentity, stepID string) (*RTPResourceWork, error) {
	if !validIntentID(stepID) {
		return nil, ErrDeviceIntentInvalid
	}
	processID, err := sipCleanupProcessID()
	if err != nil {
		return nil, err
	}
	runID, err := NewDeviceOperationIntentID()
	if err != nil {
		return nil, err
	}
	loaded, err := s.LoadRTPResourceSteps(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, step := range loaded.Steps {
		if step.Identity.StepID == stepID && step.State == RTPStepPrepared {
			return &RTPResourceWork{work: &rtpResourceWork{store: s, id: id, step: step, runID: runID, processID: processID, gate: make(chan struct{}, 1)}}, nil
		}
	}
	return nil, ErrDeviceIntentConflict
}

func (h *RTPResourceWork) Dispatch(ctx context.Context, version int64) (DeviceRTPResourceSteps, error) {
	w, err := h.enter(ctx)
	if err != nil {
		return DeviceRTPResourceSteps{}, err
	}
	defer func() { <-w.gate }()
	if w.sealed.Load() || w.attempted {
		return DeviceRTPResourceSteps{}, ErrDeviceIntentConflict
	}
	w.attempted = true
	out, err := w.store.mutateRTPStep(ctx, w.id, version, func(out *DeviceRTPResourceSteps, now time.Time) (bool, error) {
		for index := range out.Steps {
			step := &out.Steps[index]
			if step.Identity != w.step.Identity {
				continue
			}
			if step.State != RTPStepPrepared {
				return false, ErrDeviceIntentConflict
			}
			step.State, step.RowVersion, step.DispatchStartedAt, step.OwnerRunID = RTPStepMayHaveDispatched, 2, &now, w.runID
			step.OwnerProcessID = w.processID
			w.step = *step
			stamp := now
			w.step.DispatchStartedAt = &stamp
			return true, nil
		}
		return false, ErrDeviceIntentConflict
	})
	if err != nil {
		return DeviceRTPResourceSteps{}, err
	}
	w.confirmed = true
	return out, nil
}

func (h *RTPResourceWork) enter(ctx context.Context) (*rtpResourceWork, error) {
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

func (h *RTPResourceWork) Open(ctx context.Context, call func(context.Context, DeviceRTPResourceIdentity) (DeviceRTPOpenResult, error)) (DeviceRTPOpenResult, error) {
	w, err := h.enter(ctx)
	if err != nil {
		return DeviceRTPOpenResult{}, err
	}
	defer func() { <-w.gate }()
	if w.sealed.Load() || !w.confirmed || w.opened || call == nil {
		return DeviceRTPOpenResult{}, ErrDeviceIntentConflict
	}
	w.opened = true
	result, err := call(ctx, w.step.Identity) // synchronous; cancellation alone never joins it
	if err != nil {
		return DeviceRTPOpenResult{}, err
	}
	if !validRTPOpenResult(result) {
		return DeviceRTPOpenResult{}, ErrDeviceIntentUnavailable
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	w.step.OpenResult, w.step.OpenObservedAt = &result, &now
	return result, nil
}

func (h *RTPResourceWork) CloseResource(ctx context.Context, call func(context.Context, DeviceRTPResourceIdentity) (string, error)) (string, error) {
	return h.closeCall(ctx, false, call)
}

func (h *RTPResourceWork) CloseIngress(ctx context.Context, call func(context.Context, DeviceRTPResourceIdentity) (string, error)) (string, error) {
	return h.closeCall(ctx, true, call)
}

func (h *RTPResourceWork) closeCall(ctx context.Context, ingress bool, call func(context.Context, DeviceRTPResourceIdentity) (string, error)) (string, error) {
	w, err := h.enter(ctx)
	if err != nil {
		return "", err
	}
	defer func() { <-w.gate }()
	if w.sealed.Load() || !w.confirmed || call == nil || (!ingress && w.resourceClosed) || (ingress && w.ingressClosed) {
		return "", ErrDeviceIntentConflict
	}
	if ingress {
		w.ingressClosed = true
	} else {
		w.resourceClosed = true
	}
	result, err := call(ctx, w.step.Identity)
	if err != nil {
		return "", err
	}
	if !validRTPCloseResult(result, ingress) {
		return "", ErrDeviceIntentUnavailable
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if ingress {
		w.step.IngressCloseResult, w.step.IngressCloseObservedAt = result, &now
	} else {
		w.step.ResourceCloseResult, w.step.ResourceCloseObservedAt = result, &now
	}
	return result, nil
}

// Flush persists actual observations before another resource starts. SQL
// failure retains the exact in-memory facts for a later flush, not a resend.
func (h *RTPResourceWork) Flush(ctx context.Context) error {
	w, err := h.enter(ctx)
	if err != nil {
		return err
	}
	defer func() { <-w.gate }()
	return w.flush(ctx)
}

// Quiesce permanently denies further calls, joins any real call even if that
// call ignored cancellation, then durably records local exit. It says nothing
// about remote resource, source, viewer, SIP or device completion.
func (h *RTPResourceWork) Quiesce(ctx context.Context) error {
	if h == nil || h.work == nil || ctx == nil {
		return ErrDeviceIntentUnavailable
	}
	h.work.sealed.Store(true)
	w, err := h.enter(ctx)
	if err != nil {
		return err
	}
	defer func() { <-w.gate }()
	if w.step.LocalQuiescedAt == nil {
		now := time.Now().UTC().Truncate(time.Microsecond)
		w.step.LocalQuiescedAt = &now
	}
	return w.flush(ctx)
}

func (w *rtpResourceWork) flush(ctx context.Context) error {
	loaded, err := w.store.LoadRTPResourceSteps(ctx, w.id)
	if err != nil {
		return err
	}
	if !w.confirmed && !w.opened {
		for _, step := range loaded.Steps {
			if step.Identity == w.step.Identity && step.State == RTPStepPrepared {
				return nil // No call was admitted; original durable preparation remains unknown coverage.
			}
		}
	}
	_, err = w.store.mutateRTPFacts(ctx, w.id, loaded.Intent.RowVersion, authorizeSIPCancelCleanupDevice, func(out *DeviceRTPResourceSteps, now time.Time) (bool, error) {
		for index := range out.Steps {
			step := &out.Steps[index]
			if step.Identity != w.step.Identity {
				continue
			}
			candidate := w.step
			candidate.RowVersion = step.RowVersion
			candidate.Recovery = step.Recovery // independent cleanup domain, never owned by this execution
			if step.State != RTPStepMayHaveDispatched || step.OwnerRunID != w.step.OwnerRunID || step.OwnerProcessID != w.processID || !rtpFactsExtend(*step, candidate) {
				return false, ErrDeviceIntentConflict
			}
			if reflect.DeepEqual(*step, candidate) {
				return false, nil
			}
			if !validRTPExecution(w.step, now) {
				return false, ErrDeviceIntentUnavailable
			}
			*step = candidate
			step.RowVersion++
			return true, nil
		}
		return false, ErrDeviceIntentConflict
	})
	if err != nil {
		return err
	}
	// Readback is observation, never dispatch. Keep the current step version
	// after a successful CAS or exact acknowledgement-uncertain retry.
	loaded, err = w.store.LoadRTPResourceSteps(ctx, w.id)
	if err != nil {
		return err
	}
	for _, step := range loaded.Steps {
		if step.Identity == w.step.Identity && step.OwnerRunID == w.step.OwnerRunID && step.OwnerProcessID == w.processID {
			copy := w.step
			copy.RowVersion = step.RowVersion
			copy.Recovery = step.Recovery
			if reflect.DeepEqual(copy, step) {
				w.step.RowVersion = step.RowVersion
				return nil
			}
		}
	}
	return ErrDeviceIntentConflict
}

// A previous Flush may have committed even when its acknowledgement was lost.
// It may be followed by genuinely new cleanup observations before the retry.
// Accept only an immutable prefix of THIS retained owner's facts; never replace
// an earlier result/time, run identity or resource identity from storage.
func rtpFactsExtend(old, next DeviceRTPResourceStep) bool {
	if old.OpenResult == nil {
		old.OpenResult, old.OpenObservedAt = next.OpenResult, next.OpenObservedAt
	}
	if old.ResourceCloseResult == "" {
		old.ResourceCloseResult, old.ResourceCloseObservedAt = next.ResourceCloseResult, next.ResourceCloseObservedAt
	}
	if old.IngressCloseResult == "" {
		old.IngressCloseResult, old.IngressCloseObservedAt = next.IngressCloseResult, next.IngressCloseObservedAt
	}
	if old.LocalQuiescedAt == nil {
		old.LocalQuiescedAt = next.LocalQuiescedAt
	}
	return reflect.DeepEqual(old, next)
}
