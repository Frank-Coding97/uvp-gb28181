package uac

import (
	"context"

	"github.com/emiago/sipgo"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// A reservation is installed before Load and retains the concrete owner until
// actual quiescence, persisted facts and lease release. This is one branch's
// compensation, never proof of complete fork coverage or device completion.
type playbackIntentRecovery struct {
	u       *UAC
	op      *playbackIntentOperation
	owner   *sipgo.OwnedBranchCleanup
	branch  *playauth.DeviceSIPKnownBranch
	started bool // Protected by op.work.
	closed  bool
	unknown bool
}

// Caller holds playbackIntentMu. The composition root must share one UAC and
// barrier for original and recovery work within its application instance.
func (u *UAC) reservePlaybackBarrierLocked(barrier *playauth.DeviceOperationBarrier) bool {
	if barrier == nil || (u.playbackIntentBarrier != nil && u.playbackIntentBarrier != barrier) {
		return false
	}
	u.playbackIntentBarrier = barrier
	return true
}

func (u *UAC) beginRecoveredPlaybackCleanup(ctx context.Context, store *playauth.DeviceOperationIntentStore, barrier *playauth.DeviceOperationBarrier, id playauth.DeviceOperationIntentIdentity, stepID, remoteTag string) (*playbackIntentRecovery, error) {
	if ctx == nil || u == nil || u.client == nil || u.client.TxRequester != nil || store == nil || barrier == nil || id.Kind != "playback" || id.TargetScope != "channel" {
		return nil, ErrPlaybackUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o := &playbackIntentOperation{store: store, barrier: barrier, id: id, work: make(chan struct{}, 1), originalReleased: true}
	o.cleanupClose, o.cleanupCancel = context.WithCancel(context.Background())
	r := &playbackIntentRecovery{u: u, op: o}
	u.playbackIntentMu.Lock()
	if !u.reservePlaybackBarrierLocked(barrier) || len(u.playbackIntents)+len(u.playbackRecoveries) >= maxPlaybackIntentOperations || u.playbackIntents[id.OperationID] != nil || u.playbackRecoveries[id.OperationID] != nil {
		u.playbackIntentMu.Unlock()
		o.cleanupCancel()
		return nil, ErrPlaybackUnavailable
	}
	if u.playbackRecoveries == nil {
		u.playbackRecoveries = make(map[string]*playbackIntentRecovery)
	}
	u.playbackRecoveries[id.OperationID] = r
	u.playbackIntentMu.Unlock()
	owner, loaded, err := u.prepareRecoveredPlaybackCleanup(ctx, store, id, stepID, remoteTag)
	if err != nil {
		o.cleanupCancel()
		r.removeReservation()
		return nil, err
	}
	r.owner = owner
	o.version = loaded.Intent.RowVersion
	for _, step := range loaded.Steps {
		if step.Identity.StepID != stepID {
			continue
		}
		o.invite = step.Identity
		r.unknown = playbackHasMultipleBranches(loaded, o.invite)
		for _, branch := range playbackObservedBranchRecords(step) {
			if branch.Identity.RemoteTag == remoteTag {
				r.branch = branch
			}
		}
	}
	if r.branch == nil {
		_ = r.CloseLocal(context.Background())
		return nil, ErrPlaybackCleanupUnknown
	}
	return r, nil
}

func (r *playbackIntentRecovery) removeReservation() {
	r.u.playbackIntentMu.Lock()
	if r.u.playbackRecoveries[r.op.id.OperationID] == r {
		delete(r.u.playbackRecoveries, r.op.id.OperationID)
	}
	r.u.playbackIntentMu.Unlock()
}

func (r *playbackIntentRecovery) Run(ctx context.Context) error {
	o := r.op
	if err := o.enter(ctx); err != nil {
		return err
	}
	defer o.leave()
	if r.started {
		return r.finish(ctx)
	}
	if r.closed || o.cleanupClose.Err() != nil {
		return ErrPlaybackCleanupUnknown
	}
	r.started = true
	callCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(o.cleanupClose, cancel)
	defer func() { stop(); cancel() }()
	err := o.runFixedCleanupBranch(callCtx, r.branch, r.owner, nil)
	if o.cleanup.finished {
		r.removeReservation()
	}
	if err != nil {
		return err
	}
	if r.unknown {
		return ErrPlaybackCleanupUnknown
	}
	return nil
}

// Repeated calls only finish this attempt's local work and facts; they cannot
// mint a second attempt or resend ACK/BYE, including after a failed Run.
func (r *playbackIntentRecovery) finish(ctx context.Context) error {
	if err := r.op.finishCleanup(ctx); err != nil {
		return err
	}
	r.removeReservation()
	if r.op.cleanup.response == nil || r.unknown {
		return ErrPlaybackCleanupUnknown
	}
	return nil
}

func (r *playbackIntentRecovery) CloseLocal(ctx context.Context) error {
	o := r.op
	o.cleanupCancel() // Interrupt active network work before waiting for work.
	if err := o.enter(ctx); err != nil {
		return err
	}
	defer o.leave()
	r.closed = true
	if r.started {
		return r.finish(ctx)
	}
	r.owner.Terminate()
	select {
	case <-r.owner.Quiesced():
		r.removeReservation()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
