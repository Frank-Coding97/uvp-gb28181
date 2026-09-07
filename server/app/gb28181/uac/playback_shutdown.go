package uac

import (
	"context"
	"errors"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// ShutdownPlaybackIntents permanently stops admission to persistent playback
// owners and their recovery loop. Success means only local work is drained and
// final facts persisted, never remote/device completion. Root must retain this
// UAC and its UA/DB on error and retry; this does not shut down legacy services.
func (u *UAC) ShutdownPlaybackIntents(ctx context.Context) error {
	if ctx == nil || u == nil {
		return ErrPlaybackUnavailable
	}
	u.playbackIntentMu.Lock()
	u.playbackShuttingDown = true
	if u.playbackShutdownWork == nil {
		u.playbackShutdownWork = make(chan struct{}, 1)
	}
	work, worker := u.playbackShutdownWork, u.playbackRecoveryWorker
	var originals []*playbackIntentOperation
	var recoveries []*playbackIntentRecovery
	var observations []*playbackRecoveredObservation
	var scans []*playbackRecoveryScan
	for _, o := range u.playbackIntents {
		originals = append(originals, o)
	}
	for _, r := range u.playbackRecoveries {
		recoveries = append(recoveries, r)
	}
	for _, o := range u.playbackObservations {
		observations = append(observations, o)
	}
	for _, s := range u.playbackRecoveryScans {
		scans = append(scans, s)
	}
	u.playbackIntentMu.Unlock()
	// Immutable cancellation handles exist before every registry publication.
	// Signal all owners before waiting for any slow query or network operation.
	if worker != nil {
		worker.cancel()
	}
	for _, s := range scans {
		s.cancel()
	}
	for _, o := range originals {
		o.lost.Store(true)
		o.cleanupCancel()
		o.initCancel()
	}
	for _, r := range recoveries {
		r.op.cleanupCancel()
		r.op.initCancel()
	}
	for _, o := range observations {
		o.initCancel()
		o.closeOnce.Do(func() { close(o.closing) })
	}
	select {
	case work <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-work }()
	if worker != nil {
		if err := waitPlaybackShutdown(ctx, worker.done); err != nil {
			return err
		}
	}
	for _, s := range scans {
		if err := waitPlaybackShutdown(ctx, s.done); err != nil {
			return err
		}
	}
	var result error
	for _, o := range originals {
		result = errors.Join(result, o.shutdownLocal(ctx))
	}
	for _, r := range recoveries {
		if err := waitPlaybackShutdown(ctx, r.ready); err != nil {
			result = errors.Join(result, err)
			continue
		}
		if r.prepareErr != nil {
			continue // Failed pure preparation already removed its owner.
		}
		err := r.CloseLocal(ctx)
		if err == ErrPlaybackCleanupUnknown {
			// Never suppress an unknown sentinel joined with a SQL error.
			if lockErr := r.op.enter(ctx); lockErr != nil {
				err = lockErr
			} else {
				if r.op.cleanup != nil && r.op.cleanup.finished {
					err = nil
				}
				r.op.leave()
			}
		}
		result = errors.Join(result, err)
	}
	for _, o := range observations {
		if err := waitPlaybackShutdown(ctx, o.ready); err != nil {
			result = errors.Join(result, err)
			continue
		}
		if o.prepareErr == nil {
			result = errors.Join(result, o.CloseLocal(ctx))
		}
	}
	return result
}

func waitPlaybackShutdown(ctx context.Context, done <-chan struct{}) error {
	if done == nil {
		return nil // Optional reader was never created.
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (o *playbackIntentOperation) shutdownLocal(ctx context.Context) error {
	if err := waitPlaybackShutdown(ctx, o.ready); err != nil {
		return err
	}
	if o.owned == nil {
		return nil // Failed preparation released its lease before ready.
	}
	if err := o.stopOriginal(ctx); err != nil {
		return err
	}
	o.observationCloseOnce.Do(func() {
		go func() { o.owned.CloseObservation(); close(o.observationCloseDone) }()
	})
	if err := waitPlaybackShutdown(ctx, o.observationCloseDone); err != nil {
		return err
	}
	if err := waitPlaybackShutdown(ctx, o.quarantineReaderDone); err != nil {
		return err
	}
	if err := waitPlaybackShutdown(ctx, o.supervisorDone); err != nil {
		return err
	}
	if err := o.enter(ctx); err != nil {
		return err
	}
	defer o.leave()
	// An actual never-dispatched preparation has no remote observation gap.
	// Preserve prepared storage unchanged; unsolicited captured facts would
	// instead keep shutdown unknown rather than silently discarding them.
	loaded, err := o.store.LoadSIPInviteSteps(ctx, o.id)
	if err != nil {
		return err
	}
	for _, step := range loaded.Steps {
		if step.Identity == o.invite && step.State == playauth.SIPStepPrepared {
			if len(o.owned.ObservedBranches().Responses)+len(o.owned.QuarantinedBranches().Responses) != 0 || o.info != nil || o.cleanup != nil {
				return ErrPlaybackCleanupUnknown
			}
			o.releaseOriginal()
			return nil
		}
	}
	// A prior failed final flush may have set in-memory unsupported without
	// committing its loss marker yet. Persist that marker before interpreting
	// the original reader's unknown as a durable remote-coverage result.
	if _, err := o.persistQuarantineFacts(ctx); err != nil {
		return err
	}
	loaded, err = o.persistOriginalFacts(ctx)
	if err != nil && !(err == ErrPlaybackCleanupUnknown && loaded.Intent.DeviceOperationIntentIdentity == o.id && playbackHasMultipleBranches(loaded, o.invite)) {
		return err
	}
	if err := o.finishINFO(ctx); err != nil {
		return err
	}
	if o.cleanup != nil {
		if err := o.finishCleanup(ctx); err != nil {
			return err
		}
	}
	o.releaseOriginal()
	return nil
}
