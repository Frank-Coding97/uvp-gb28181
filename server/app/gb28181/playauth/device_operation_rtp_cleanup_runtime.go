package playauth

import "context"

// RTPCleanupResolver resolves a trusted control bound to exactly this persisted
// identity. It grants no sending permission. On error it must release partial
// construction resources and return a nil runtime.
type RTPCleanupResolver interface {
	ResolveRTPCleanup(context.Context, DeviceRTPResourceIdentity) (RTPCleanupRuntime, error)
}

// RTPCleanupRuntime has no Open or selector-changing method. Release is
// synchronous local resource release, not evidence of remote termination.
type RTPCleanupRuntime interface {
	CloseResource(context.Context) (string, error)
	CloseIngress(context.Context) (string, error)
	Release()
}

// Run processes this owner's fixed two-call batch, never a remote completion
// decision. Each action is attempted at most once. After SQL failure, retry on
// the SAME handle flushes its facts before attempting the remaining action.
// A new batch requires Quiesce and a new owner with fresh dispatch CASes.
func (h *RTPRecoveryWork) Run(ctx context.Context, resolver RTPCleanupResolver) error {
	if h == nil || h.work == nil || ctx == nil || isNilInterface(resolver) {
		return ErrDeviceIntentUnavailable
	}
	w := h.work
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case w.runGate <- struct{}{}:
		defer func() { <-w.runGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if w.sealed.Load() || w.ctx.Err() != nil {
		return ErrDeviceIntentConflict
	}
	runCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(w.ctx, cancel)
	defer func() { stop(); cancel() }()
	if err := h.resolveRuntime(runCtx, resolver); err != nil {
		return err
	}
	if err := h.Flush(runCtx); err != nil {
		return err
	}
	if !w.resourceAttempted {
		w.resourceAttempted = true
		if _, err := h.CloseResource(runCtx, func(ctx context.Context, _ DeviceRTPResourceIdentity) (string, error) {
			return w.runtime.CloseResource(ctx)
		}); err != nil {
			return err
		}
	}
	if !w.ingressAttempted {
		w.ingressAttempted = true
		if _, err := h.CloseIngress(runCtx, func(ctx context.Context, _ DeviceRTPResourceIdentity) (string, error) {
			return w.runtime.CloseIngress(ctx)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (h *RTPRecoveryWork) resolveRuntime(ctx context.Context, resolver RTPCleanupResolver) error {
	w, err := h.enter(ctx)
	if err != nil {
		return err
	}
	defer func() { <-w.gate }()
	if w.sealed.Load() || !w.confirmed || w.runtimeReleased || w.lease.Context().Err() != nil {
		return ErrDeviceIntentConflict
	}
	if w.resolveAttempted {
		return w.resolveErr
	}
	w.resolveAttempted = true
	// Publish even a nonconforming resolver's runtime+error before inspecting
	// cancellation. Quiesce owns its eventual release after this call returns.
	w.runtime, w.resolveErr = resolver.ResolveRTPCleanup(ctx, w.identity)
	if w.resolveErr == nil && isNilInterface(w.runtime) {
		w.resolveErr = ErrDeviceIntentUnavailable
	}
	if w.resolveErr != nil {
		return w.resolveErr
	}
	if err = ctx.Err(); err != nil {
		w.resolveErr = err
		return err
	}
	if w.sealed.Load() {
		w.resolveErr = ErrDeviceIntentConflict
		return w.resolveErr
	}
	return nil
}
