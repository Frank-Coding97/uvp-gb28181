package uac

import (
	"context"
	"errors"
	"slices"

	"github.com/emiago/sipgo/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// Frozen from this actual owner's responses after its transactions quiesce.
// The response is never rebuilt from a persisted record or a recovered grant.
type playbackCleanupBranchPlan struct {
	identity playauth.DeviceSIPKnownBranchIdentity
	response *sip.Response
	cleanup  *playbackIntentCleanup
}

func playbackObservedBranchRecords(step playauth.DeviceSIPInviteStep) []*playauth.DeviceSIPKnownBranch {
	var branches []*playauth.DeviceSIPKnownBranch
	if step.KnownBranch != nil {
		branches = append(branches, step.KnownBranch)
	}
	for index := range step.AdditionalBranches {
		branches = append(branches, &step.AdditionalBranches[index])
	}
	return branches
}

func playbackCleanupStoredBranch(step playauth.DeviceSIPInviteStep, identity playauth.DeviceSIPKnownBranchIdentity) *playauth.DeviceSIPKnownBranch {
	for _, b := range playbackObservedBranchRecords(step) {
		if samePlaybackINFOBranch(b.Identity, identity) {
			return b
		}
	}
	return nil
}

func playbackHasMultipleBranches(stored playauth.DeviceSIPInviteSteps, invite playauth.DeviceSIPInviteIdentity) bool {
	for _, step := range stored.Steps {
		if step.Identity == invite {
			return len(step.AdditionalBranches) != 0 || step.BranchInventoryFault != ""
		}
	}
	return false
}

func (o *playbackIntentOperation) cleanupObservedBranches(ctx context.Context) error {
	if o.cleanup != nil && !o.cleanup.finished {
		if err := o.finishCleanup(ctx); err != nil {
			return err
		}
	}
	stored, err := o.store.LoadSIPInviteSteps(ctx, o.id)
	if err != nil {
		return err
	}
	return o.cleanupObservedBranchesFrom(ctx, stored)
}

func (o *playbackIntentOperation) cleanupObservedBranchesFrom(ctx context.Context, stored playauth.DeviceSIPInviteSteps) error {
	if o.cleanupPlan == nil {
		if o.originalReleased {
			return ErrPlaybackCleanupUnknown
		}
		snapshot := o.owned.ObservedBranches()
		var plans []playbackCleanupBranchPlan
		for _, step := range stored.Steps {
			if step.Identity != o.invite {
				continue
			}
			for _, b := range playbackObservedBranchRecords(step) {
				var response *sip.Response
				for _, r := range snapshot.Responses {
					if singlePlaybackBranchTag(r.To().Params) == b.Identity.RemoteTag {
						response = r
						break
					}
				}
				if response == nil {
					return ErrPlaybackCleanupUnknown
				}
				identity := b.Identity
				identity.RouteSet = slices.Clone(identity.RouteSet)
				plans = append(plans, playbackCleanupBranchPlan{identity: identity, response: response})
			}
		}
		o.cleanupPlan = plans // Publish only the complete, frozen actual inventory.
	}
	if len(o.cleanupPlan) == 0 {
		return ErrPlaybackCleanupUnknown
	}
	var result error
	for index := range o.cleanupPlan {
		plan := &o.cleanupPlan[index]
		var branch *playauth.DeviceSIPKnownBranch
		for _, step := range stored.Steps {
			if step.Identity == o.invite {
				branch = playbackCleanupStoredBranch(step, plan.identity)
			}
		}
		if branch == nil {
			return errors.Join(result, ErrPlaybackCleanupUnknown)
		}
		if plan.cleanup == nil {
			if o.originalReleased || ctx.Err() != nil || (o.cleanupClose != nil && o.cleanupClose.Err() != nil) || len(branch.CleanupAttempts) != 0 {
				return errors.Join(result, ErrPlaybackCleanupUnknown, ctx.Err())
			}
			result = errors.Join(result, o.runCleanupBranch(ctx, branch, plan.response, plan))
			if plan.cleanup == nil || !plan.cleanup.finished {
				return errors.Join(result, ErrPlaybackCleanupUnknown)
			}
			if !plan.cleanup.networkOutcome {
				return errors.Join(result, ErrPlaybackCleanupUnknown) // Stop this batch on uncertain permission.
			}
			var err error
			stored, err = o.store.LoadSIPInviteSteps(ctx, o.id)
			if err != nil {
				return errors.Join(result, err)
			}
		} else if !plan.cleanup.finished {
			return errors.Join(result, ErrPlaybackCleanupUnknown)
		}
		if plan.cleanup.response == nil {
			result = errors.Join(result, ErrPlaybackCleanupUnknown)
		}
	}
	// Every actual owner has quiesced and persisted. This can release local
	// work, but is NOT full fork coverage, remote success or device completion.
	if err := o.validateCleanupBatch(stored, false); err != nil {
		return errors.Join(result, err)
	}
	o.releaseOriginal()
	return result
}

func (o *playbackIntentOperation) validateCleanupBatch(stored playauth.DeviceSIPInviteSteps, closing bool) error {
	if stored.Intent.DeviceOperationIntentIdentity != o.id {
		return ErrPlaybackCleanupUnknown
	}
	for _, step := range stored.Steps {
		if step.Identity != o.invite {
			continue
		}
		if step.BranchInventoryFault != "" || len(playbackObservedBranchRecords(step)) != len(o.cleanupPlan) {
			return ErrPlaybackCleanupUnknown
		}
		for _, plan := range o.cleanupPlan {
			b, c := playbackCleanupStoredBranch(step, plan.identity), plan.cleanup
			if closing && b != nil && c == nil && len(b.CleanupAttempts) == 0 {
				continue // Never started here; CloseLocal cannot prepare it later.
			}
			if b == nil || c == nil || !c.finished {
				return ErrPlaybackCleanupUnknown
			}
			var attempt *playauth.DeviceSIPCleanupAttempt
			for index := range b.CleanupAttempts {
				a := &b.CleanupAttempts[index]
				if a.Identity.AttemptID == c.identity.AttemptID {
					attempt = a
				}
			}
			if attempt == nil {
				if c.lease != nil || c.response != nil {
					return ErrPlaybackCleanupUnknown
				}
				continue // failed before any network owner was registered
			}
			if !samePlaybackCleanupRequest(attempt.Identity.ACK, c.identity.ACK) || !samePlaybackCleanupRequest(attempt.Identity.BYE, c.identity.BYE) ||
				attempt.LocalQuiescedAt == nil || (c.response != nil && (attempt.Response == nil || *attempt.Response != *c.response)) {
				return ErrPlaybackCleanupUnknown
			}
		}
		return nil
	}
	return ErrPlaybackCleanupUnknown
}
