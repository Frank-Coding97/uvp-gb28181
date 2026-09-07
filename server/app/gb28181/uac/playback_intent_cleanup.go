package uac

import (
	"context"
	"errors"
	"math"
	"slices"
	"sync"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// Owned by the operation's work domain and bounded strong registry. A repeat
// invocation only joins/persists this attempt, never rebuilds or sends again.
type playbackIntentCleanup struct {
	owned    *sipgo.OwnedBranchCleanup
	lease    playauth.DeviceOperationLease
	identity playauth.DeviceSIPCleanupAttemptIdentity
	branch   playauth.DeviceSIPKnownBranchIdentity
	response *playauth.DeviceSIPCleanupBYEResponse
	stopOnce sync.Once
	stopDone chan struct{}
	finished bool
	// Set only by the actual transport/response path, never by a failed CAS
	// followed by successful readback. Batch advancement also requires finish.
	networkOutcome bool
}

// CleanupKnownBranch performs one fresh, durably authorized compensation. It
// is not device completion, fork coverage, or a recovery dispatch from Load.
func (o *playbackIntentOperation) CleanupKnownBranch(ctx context.Context) (result error) {
	if err := o.stopOriginal(ctx); err != nil {
		return err
	}
	if err := o.enter(ctx); err != nil {
		return err
	}
	defer o.leave()
	if pending, err := o.persistQuarantineFacts(ctx); err != nil {
		return err
	} else if pending {
		if o.cleanup != nil {
			if err := o.finishCleanup(ctx); err != nil {
				return err
			}
		}
		return ErrPlaybackCleanupUnknown
	}
	if o.multiCleanup {
		return o.cleanupObservedBranches(ctx)
	}
	if o.cleanup != nil {
		if err := o.finishCleanup(ctx); err != nil {
			return err
		}
		if o.cleanup.response == nil {
			return ErrPlaybackCleanupUnknown
		}
		return nil
	}
	if o.originalReleased {
		return ErrPlaybackCleanupUnknown
	}
	if err := o.finishINFO(ctx); err != nil {
		return err
	}
	stored, err := o.persistOriginalFacts(ctx)
	if playbackHasMultipleBranches(stored, o.invite) {
		o.multiCleanup = true
		return o.cleanupObservedBranchesFrom(ctx, stored)
	}
	if err != nil {
		return err // Keep original owner until its observed facts are durable.
	}
	o.factMu.Lock()
	first := o.first
	o.factMu.Unlock()
	if first == nil {
		o.releaseOriginal() // No observed branch to authorize; still durable unknown.
		return ErrPlaybackCleanupUnknown
	}
	var branch *playauth.DeviceSIPKnownBranch
	for _, step := range stored.Steps {
		if step.Identity == o.invite {
			branch = step.KnownBranch
		}
	}
	if branch == nil || len(branch.CleanupAttempts) != 0 {
		o.releaseOriginal()
		return ErrPlaybackCleanupUnknown
	}
	return o.runCleanupBranch(ctx, branch, first, nil)
}

// The caller owns work and has confirmed this branch's durable identity.
func (o *playbackIntentOperation) runCleanupBranch(ctx context.Context, branch *playauth.DeviceSIPKnownBranch, response *sip.Response, plan *playbackCleanupBranchPlan) (result error) {
	if o.cleanupClose != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		stop := context.AfterFunc(o.cleanupClose, cancel)
		defer stop()
		defer cancel()
		if o.cleanupClose.Err() != nil {
			return ErrPlaybackCleanupUnknown
		}
	}
	lastCSeq := playbackLastINFOCSeq(o.invite.CSeq, branch)
	if lastCSeq == math.MaxUint32 {
		o.releaseSingleCleanupOriginal()
		return ErrPlaybackCleanupUnknown
	}
	owner, err := o.dua.NewBranchCleanup(o.request, response, lastCSeq+1)
	if err != nil {
		o.releaseSingleCleanupOriginal()
		return err
	}
	c := &playbackIntentCleanup{owned: owner, branch: branch.Identity, stopDone: make(chan struct{})}
	c.branch.RouteSet = slices.Clone(branch.Identity.RouteSet)
	o.cleanup = c // Strongly retained before persistence, connection or release.
	if plan != nil {
		plan.cleanup = c
	}
	defer func() {
		finishCtx, cancel := context.WithTimeout(context.Background(), playbackTeardownTimeout)
		defer cancel()
		result = errors.Join(result, o.finishCleanup(finishCtx))
	}()
	c.identity.AttemptID, err = playauth.NewDeviceOperationIntentID()
	if err != nil {
		return err
	}
	c.identity.ACK, err = snapshotPlaybackCleanupRequest(owner.ACKRequest(), sip.ACK, o.invite.StepID)
	if err != nil {
		return err
	}
	c.identity.BYE, err = snapshotPlaybackCleanupRequest(owner.BYERequest(), sip.BYE, o.invite.StepID)
	if err != nil {
		return err
	}
	stored, ticket, err := o.store.PrepareSIPBranchCleanupWork(ctx, o.id, o.version, c.identity)
	if err != nil {
		return err
	}
	o.version = stored.Intent.RowVersion
	if ticket == nil {
		return ErrPlaybackCleanupUnknown
	}
	c.lease, err = o.barrier.BeginSIPCleanup(ctx, ticket)
	if err != nil {
		return err
	}
	// No gap: both owners are installed until this release. Everything below,
	// including TCP connection creation, is protected by the cleanup lease.
	o.releaseSingleCleanupOriginal()
	stopOnCancel := context.AfterFunc(ctx, owner.Terminate)
	stopOnTransfer := context.AfterFunc(c.lease.Context(), owner.Terminate)
	defer stopOnCancel()
	defer stopOnTransfer()
	prepared, err := owner.PrepareBYE(ctx)
	if err != nil {
		c.networkOutcome = true
		return err
	}
	actual, err := snapshotPlaybackCleanupRequest(prepared, sip.BYE, o.invite.StepID)
	if err != nil || !samePlaybackCleanupRequest(actual, c.identity.BYE) {
		return errPlaybackIntentSnapshot
	}
	stored, err = o.store.DispatchSIPCleanupACK(ctx, o.id, o.version, c.identity.AttemptID)
	if err != nil {
		return err
	}
	o.version = stored.Intent.RowVersion
	if ctx.Err() != nil || c.lease.Context().Err() != nil {
		return ErrPlaybackCleanupUnknown
	}
	if err := owner.WriteACK(); err != nil {
		c.networkOutcome = true
		return err
	}
	stored, err = o.store.DispatchSIPCleanupBYE(ctx, o.id, o.version, c.identity.AttemptID)
	if err != nil {
		return err
	}
	o.version = stored.Intent.RowVersion
	if ctx.Err() != nil || c.lease.Context().Err() != nil {
		return ErrPlaybackCleanupUnknown
	}
	if err := owner.StartBYE(); err != nil {
		c.networkOutcome = true
		return err
	}
	for {
		response, err := owner.NextResponse(ctx)
		if err != nil {
			c.networkOutcome = true
			return err
		}
		if response.StatusCode < 200 {
			continue
		}
		c.networkOutcome = true
		if response.StatusCode >= 300 {
			return ErrPlaybackCleanupUnknown
		}
		// Owner already validated the exact wire response against its fixed BYE.
		c.response = &playauth.DeviceSIPCleanupBYEResponse{AttemptID: c.identity.AttemptID,
			CallID: string(*response.CallID()), CSeq: response.CSeq().SeqNo,
			LocalTag: singlePlaybackBranchTag(response.From().Params), RemoteTag: singlePlaybackBranchTag(response.To().Params),
			StatusCode: response.StatusCode}
		return nil
	}
}

func samePlaybackCleanupRequest(a, b playauth.DeviceSIPCleanupRequestIdentity) bool {
	return a.Request == b.Request && a.RemoteTag == b.RemoteTag && slices.Equal(a.Routes, b.Routes)
}

// Caller owns work. A deadline or uncertain persistence retains both the
// concrete owner and any lease; a retry only confirms facts, never sends SIP.
func (o *playbackIntentOperation) finishCleanup(ctx context.Context) error {
	c := o.cleanup
	if c.finished {
		return nil
	}
	c.stopOnce.Do(func() {
		go func() { c.owned.Terminate(); <-c.owned.Quiesced(); close(c.stopDone) }()
	})
	select {
	case <-c.stopDone:
	case <-ctx.Done():
		return ctx.Err()
	}
	stored, err := o.store.LoadSIPInviteSteps(ctx, o.id)
	if err != nil {
		return err
	}
	var attempt *playauth.DeviceSIPCleanupAttempt
	originalMatched := false
	for _, step := range stored.Steps {
		if step.Identity != o.invite {
			continue
		}
		b := playbackCleanupStoredBranch(step, c.branch)
		if b == nil {
			continue
		}
		originalMatched = true
		for _, a := range b.CleanupAttempts {
			if a.Identity.AttemptID == c.identity.AttemptID {
				if !samePlaybackCleanupRequest(a.Identity.ACK, c.identity.ACK) || !samePlaybackCleanupRequest(a.Identity.BYE, c.identity.BYE) {
					return ErrPlaybackCleanupUnknown
				}
				attempt = &a
			}
		}
	}
	if !originalMatched || (attempt == nil && c.lease != nil) {
		return ErrPlaybackCleanupUnknown
	}
	if c.response != nil {
		stored, err = o.store.ObserveSIPCleanupBYE(ctx, o.id, stored.Intent.RowVersion, *c.response)
		if err != nil {
			return err
		}
	}
	if attempt != nil {
		stored, err = o.store.ObserveSIPCleanupQuiesced(ctx, o.id, stored.Intent.RowVersion, c.identity.AttemptID)
		if err != nil {
			return err
		}
	}
	o.version = stored.Intent.RowVersion
	if c.lease != nil {
		c.lease.Release()
	}
	o.releaseSingleCleanupOriginal()
	c.finished = true
	return nil
}

func (o *playbackIntentOperation) releaseSingleCleanupOriginal() {
	if !o.multiCleanup {
		o.releaseOriginal()
	}
}
