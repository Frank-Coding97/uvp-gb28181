package media

import (
	"context"
	"time"
)

// RunMaintenance is a blocking, caller-owned single runner. Root must start it
// after the node registry, then cancel and wait before stopping media services.
// It is independent of gateway/playback flags and never enables admission.
func (w *RevocationWorker) RunMaintenance(ctx context.Context, report func(RevocationTickResult, error)) {
	if w == nil || ctx == nil || ctx.Err() != nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	w.runMaintenance(ctx, ticker.C, report)
}

func (w *RevocationWorker) runMaintenance(ctx context.Context, ticks <-chan time.Time, report func(RevocationTickResult, error)) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ticks:
			if !ok || ctx.Err() != nil {
				return
			}
			// Synchronous execution deliberately keeps a non-cooperative call
			// owned by this runner; cancellation cannot detach it or overlap it.
			result, err := w.Tick(ctx)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				err = ErrRevocationWorkerUnavailable
			}
			if report != nil {
				report(result, err)
			}
		}
	}
}
