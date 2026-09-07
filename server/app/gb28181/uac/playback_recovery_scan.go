package uac

import (
	"context"
	"errors"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// PlaybackRecoveryPage reports work inspected, not proven terminal intents.
// NextAfter is a keyset cursor for this sweep only. Start the next sweep at
// the beginning so late facts and unfinished attempts are revisited.
type PlaybackRecoveryPage struct {
	Scanned, Cancelled, Pending int
	NextAfter                   string
}

type playbackRecoveryScan struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// RecoverPlaybackIntents executes one bounded page outside SQL transactions.
// The composition root must share this UAC/barrier; this is not a multi-API
// lease. Every dispatched intent stays pending until all adapters and the
// independent historical coverage authority prove completion.
func (u *UAC) RecoverPlaybackIntents(ctx context.Context, store *playauth.DeviceOperationIntentStore, barrier *playauth.DeviceOperationBarrier, devicePK int64, deviceCode string, beforeEpoch int64, afterID string, limit int) (PlaybackRecoveryPage, error) {
	var page PlaybackRecoveryPage
	if ctx == nil || u == nil || store == nil || devicePK <= 0 {
		return page, ErrPlaybackUnavailable
	}
	if err := ctx.Err(); err != nil {
		return page, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	scan := &playbackRecoveryScan{cancel: cancel, done: make(chan struct{})}
	u.playbackIntentMu.Lock()
	if !u.reservePlaybackBarrierLocked(barrier) || u.playbackRecoveryScans[devicePK] != nil || len(u.playbackRecoveryScans) >= maxPlaybackIntentOperations {
		u.playbackIntentMu.Unlock()
		return page, ErrPlaybackUnavailable
	}
	if u.playbackRecoveryScans == nil {
		u.playbackRecoveryScans = make(map[int64]*playbackRecoveryScan)
	}
	u.playbackRecoveryScans[devicePK] = scan
	u.playbackIntentMu.Unlock()
	defer func() {
		u.playbackIntentMu.Lock()
		delete(u.playbackRecoveryScans, devicePK)
		close(scan.done)
		u.playbackIntentMu.Unlock()
	}()
	rows, err := store.ListRecoveryPage(ctx, devicePK, deviceCode, beforeEpoch, afterID, limit)
	if err != nil {
		return page, err
	}
	var firstErr error
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return page, err
		}
		page.Scanned++
		page.NextAfter = row.OperationID
		if row.State == playauth.IntentReserved {
			err = store.CancelReserved(ctx, row.OperationID, row.RowVersion)
			if err == nil {
				page.Cancelled++
				continue
			}
		} else {
			err = u.recoverPlaybackIntent(ctx, store, barrier, row.DeviceOperationIntentIdentity)
		}
		page.Pending++
		if firstErr == nil {
			firstErr = err
		}
	}
	if page.Pending != 0 {
		return page, errors.Join(ErrPlaybackCleanupUnknown, firstErr)
	}
	return page, nil
}

func (u *UAC) recoverPlaybackIntent(ctx context.Context, store *playauth.DeviceOperationIntentStore, barrier *playauth.DeviceOperationBarrier, id playauth.DeviceOperationIntentIdentity) error {
	if !playbackIntentKind(id.Kind) || id.TargetScope != "channel" {
		return ErrPlaybackCleanupUnknown
	}
	u.playbackIntentMu.Lock()
	live, active := u.playbackIntents[id.OperationID], u.playbackRecoveries[id.OperationID]
	u.playbackIntentMu.Unlock()
	if live != nil {
		return ErrPlaybackUnavailable // The original supervisor owns this operation.
	}
	var resumedStep, resumedTag string
	if active != nil {
		if active.op.id != id {
			return ErrPlaybackCleanupUnknown
		}
		if err := active.waitReady(ctx); err != nil {
			return err
		}
		resumedStep, resumedTag = active.op.invite.StepID, active.branch.Identity.RemoteTag
		if err := u.runPlaybackRecovery(ctx, active); err != nil {
			return err
		}
	}
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	if err != nil {
		return err
	}
	// Freeze this page's bounded branch set. Late observations belong to the
	// next sweep; they must not grow an in-flight cleanup batch indefinitely.
	var observationErr error
	for _, step := range loaded.Steps {
		if step.State != playauth.SIPStepMayHaveDispatched {
			continue
		}
		u.playbackIntentMu.Lock()
		observing := u.playbackObservations[id.OperationID+":"+step.Identity.StepID] != nil
		u.playbackIntentMu.Unlock()
		if !observing {
			// Failed observation cannot forbid reliable known-branch cleanup.
			// No observer fields are read here: reservation may still be loading.
			_, err := u.beginRecoveredPlaybackObservation(ctx, store, barrier, id, step.Identity.StepID)
			if observationErr == nil {
				observationErr = err
			}
		}
		for _, branch := range playbackObservedBranchRecords(step) {
			if step.Identity.StepID == resumedStep && branch.Identity.RemoteTag == resumedTag {
				continue // A resumed attempt never becomes a second send this pass.
			}
			observed := false
			for _, attempt := range branch.CleanupAttempts {
				observed = observed || attempt.Response != nil
			}
			if observed {
				continue // Never recreate a branch already carrying a BYE response.
			}
			r, err := u.beginRecoveredPlaybackCleanup(ctx, store, barrier, id, step.Identity.StepID, branch.Identity.RemoteTag)
			if err != nil {
				return err
			}
			if err := u.runPlaybackRecovery(ctx, r); err != nil {
				return err
			}
		}
	}
	return errors.Join(ErrPlaybackCleanupUnknown, observationErr)
}

func (u *UAC) runPlaybackRecovery(ctx context.Context, r *playbackIntentRecovery) error {
	callCtx, cancel := context.WithTimeout(ctx, playbackTeardownTimeout)
	defer cancel()
	err := r.Run(callCtx)
	u.playbackIntentMu.Lock()
	retained := u.playbackRecoveries[r.op.id.OperationID] == r
	u.playbackIntentMu.Unlock()
	if retained {
		return errors.Join(ErrPlaybackCleanupUnknown, err)
	}
	// Unknown fork coverage does not prevent cleaning the next known branch.
	// Other errors stop this intent until the next bounded sweep.
	if err != nil && !errors.Is(err, ErrPlaybackCleanupUnknown) {
		return err
	}
	return nil
}
