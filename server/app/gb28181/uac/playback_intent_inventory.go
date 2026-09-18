package uac

import (
	"context"
	"errors"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// Only NextResponse selects the business response. A retransmission observer
// can close admission and wake the workflow, never race to replace first.
func (o *playbackIntentOperation) refreshBranchInventory() {
	snapshot := o.owned.ObservedBranches()
	if snapshot.Incomplete || len(snapshot.Responses) > 1 {
		o.factMu.Lock()
		o.unsupported = true
		o.factMu.Unlock()
		o.lost.Store(true)
		select {
		case o.events <- struct{}{}:
		default:
		}
	}
}

// Called with work held after the original transaction and readers quiesce.
// This retains every bounded observed branch and the sticky loss marker. It
// does not prove post-transaction quarantine or authorize multi-branch replay.
func (o *playbackIntentOperation) persistBranchInventory(ctx context.Context, loaded playauth.DeviceSIPInviteSteps) (playauth.DeviceSIPInviteSteps, error) {
	snapshot := o.owned.ObservedBranches()
	o.factMu.Lock()
	first := o.first
	o.factMu.Unlock()
	for _, response := range snapshot.Responses {
		selected := false
		for _, step := range loaded.Steps {
			if step.Identity == o.invite {
				selected = step.KnownBranch != nil
			}
		}
		if selected && first != nil && samePlaybackIntentResponse(first, response) {
			continue // persistOriginalFacts already confirmed this exact branch
		}
		out, err := observeStoredPlaybackBranchWith(ctx, o.store, o.id, loaded.Intent.RowVersion, o.invite, o.request, response, selected)
		if err != nil {
			return playauth.DeviceSIPInviteSteps{}, errors.Join(ErrPlaybackCleanupUnknown, err)
		}
		loaded = out
	}
	if snapshot.Incomplete {
		out, err := o.store.ObserveSIPBranchInventoryFault(ctx, o.id, loaded.Intent.RowVersion, o.invite.StepID, playauth.SIPBranchObserverIncomplete)
		if err != nil {
			return playauth.DeviceSIPInviteSteps{}, errors.Join(ErrPlaybackCleanupUnknown, err)
		}
		loaded = out
	}
	return loaded, nil
}
