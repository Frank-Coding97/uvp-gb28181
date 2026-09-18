package uac

import (
	"context"
	"errors"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// This reader is retained with the bounded operation registry. Local transaction
// shutdown is not observation shutdown: only the UA's observation owner ends it.
func (o *playbackIntentOperation) startQuarantineReader() {
	o.quarantineReaderDone = make(chan struct{})
	go func() {
		defer close(o.quarantineReaderDone)
		<-o.owned.Quiesced()
		var retry <-chan time.Time
		var timer *time.Timer
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()
		for {
			closing := false
			select {
			case <-o.owned.QuarantineChanges():
			case <-retry:
			case <-o.owned.ObservationDone():
				closing = true
			}
			retry = nil
			ctx, cancel := context.WithTimeout(context.Background(), playbackTeardownTimeout)
			err := o.enter(ctx)
			if err == nil {
				_, err = o.persistQuarantineFacts(ctx)
				o.leave()
			}
			cancel()
			if closing {
				return // Final best effort; durable may-have never becomes Complete.
			}
			if err != nil {
				if timer == nil {
					timer = time.NewTimer(time.Second)
				} else {
					timer.Reset(time.Second)
				}
				retry = timer.C
			}
		}
	}()
}

// Caller owns work. Only actual detached responses may enter this observation
// path. It never grows cleanupPlan, creates an attempt, or writes to the network.
func (o *playbackIntentOperation) persistQuarantineFacts(ctx context.Context) (bool, error) {
	snapshot := o.owned.QuarantinedBranches()
	if len(snapshot.Responses) == 0 && !snapshot.Incomplete {
		return false, nil
	}
	o.factMu.Lock()
	o.unsupported = true
	o.factMu.Unlock()
	loaded, err := o.store.LoadSIPInviteSteps(ctx, o.id)
	if err != nil {
		return true, err
	}
	selected := false
	for _, step := range loaded.Steps {
		if step.Identity == o.invite {
			selected = step.KnownBranch != nil
		}
	}
	if !selected {
		// Preserve the original actually observed first branch before recording
		// a late first valid response. An invalid original still leaves its fault.
		loaded, err = o.persistOriginalFacts(ctx)
		if err != nil && loaded.Intent.DeviceOperationIntentIdentity != o.id {
			return true, err
		}
	}
	// Post-freeze facts establish pending quarantine, not complete observation.
	// Persist the negative boundary before any late branch; it also prevents a
	// late first branch from being used as fresh business ACK/INFO authority.
	loaded, err = o.store.ObserveSIPBranchInventoryFault(ctx, o.id, loaded.Intent.RowVersion, o.invite.StepID, playauth.SIPBranchObserverIncomplete)
	if err != nil {
		return true, err
	}
	for _, response := range snapshot.Responses {
		selected = false
		for _, step := range loaded.Steps {
			if step.Identity == o.invite {
				selected = step.KnownBranch != nil
			}
		}
		loaded, err = observeStoredPlaybackBranchWith(ctx, o.store, o.id, loaded.Intent.RowVersion, o.invite, o.request, response, selected)
		if err != nil {
			return true, errors.Join(ErrPlaybackCleanupUnknown, err)
		}
	}
	o.version = loaded.Intent.RowVersion
	return true, nil
}
