package uac

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// Observation has the intent's lifetime, not one cleanup attempt's lifetime.
// It may coexist with fixed compensation but never with the original owner.
type playbackRecoveredObservation struct {
	store           *playauth.DeviceOperationIntentStore
	id              playauth.DeviceOperationIntentIdentity
	invite          playauth.DeviceSIPInviteIdentity
	observation     *sip.ClientResponseObservation
	mu              sync.Mutex // Only bounded detached material, no SQL/network/waits.
	known, branches []playauth.DeviceSIPKnownBranchIdentity
	changes         chan struct{}
	work            chan struct{}
	done            chan struct{}
	ready           chan struct{}
	prepareErr      error // Immutable after ready.
	closing         chan struct{}
	closeOnce       sync.Once
	initCancel      context.CancelFunc
}

func (u *UAC) hasPlaybackObservationLocked(operationID string) bool {
	for key := range u.playbackObservations {
		if strings.HasPrefix(key, operationID+":") {
			return true
		}
	}
	return false
}

func (u *UAC) beginRecoveredPlaybackObservation(ctx context.Context, store *playauth.DeviceOperationIntentStore, barrier *playauth.DeviceOperationBarrier, id playauth.DeviceOperationIntentIdentity, stepID string) (_ *playbackRecoveredObservation, err error) {
	if ctx == nil || u == nil || u.client == nil || store == nil || barrier == nil || !playbackIntentKind(id.Kind) || id.TargetScope != "channel" {
		return nil, ErrPlaybackUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := id.OperationID + ":" + stepID
	o := &playbackRecoveredObservation{store: store, id: id, changes: make(chan struct{}, 1), work: make(chan struct{}, 1), done: make(chan struct{}), ready: make(chan struct{}), closing: make(chan struct{})}
	ctx, o.initCancel = context.WithCancel(ctx)
	defer o.initCancel()
	u.playbackIntentMu.Lock()
	if !u.reservePlaybackBarrierLocked(barrier) || u.playbackIntents[id.OperationID] != nil || u.playbackObservations[key] != nil || len(u.playbackIntents)+len(u.playbackObservations) >= maxPlaybackIntentOperations {
		u.playbackIntentMu.Unlock()
		return nil, ErrPlaybackUnavailable
	}
	if u.playbackObservations == nil {
		u.playbackObservations = make(map[string]*playbackRecoveredObservation)
	}
	u.playbackObservations[key] = o // Reserve before Load; ordinary begin now refuses.
	u.playbackIntentMu.Unlock()
	defer func() { o.prepareErr = err; close(o.ready) }()
	removeUnused := func() {
		u.playbackIntentMu.Lock()
		if u.playbackObservations[key] == o {
			delete(u.playbackObservations, key)
		}
		u.playbackIntentMu.Unlock()
	}
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	if err != nil {
		removeUnused()
		return nil, err
	}
	for _, step := range loaded.Steps {
		if step.Identity.StepID == stepID && step.State == playauth.SIPStepMayHaveDispatched {
			o.invite = step.Identity
			for _, branch := range playbackObservedBranchRecords(step) {
				o.known = append(o.known, branch.Identity)
			}
		}
	}
	if o.invite.StepID == "" {
		removeUnused()
		return nil, ErrPlaybackCleanupUnknown
	}
	// The restart/registration gap is real even if no later packets arrive.
	// Persist it before installing a receive-path observer; never clear it.
	_, err = store.ObserveSIPBranchInventoryFault(ctx, id, loaded.Intent.RowVersion, stepID, playauth.SIPBranchObserverIncomplete)
	if err != nil {
		removeUnused()
		return nil, err
	}
	i := o.invite
	selector := sip.DetachedInviteResponseSelector{Branch: i.Branch, CallID: i.CallID, CSeq: i.CSeq, FromURI: i.FromURI, LocalTag: i.LocalTag, ToURI: i.ToURI, ViaHost: i.ViaHost, ViaPort: i.ViaPort, Transport: i.Transport, Destination: i.Destination}
	o.observation, err = u.client.TransactionLayer().ObserveDetachedClientResponses(selector, o)
	if err != nil {
		removeUnused()
		return nil, err
	}
	go o.run()
	return o, nil
}

func (o *playbackRecoveredObservation) ObservationLost() {
	// Incomplete was durably set before registration and is never reset. A
	// rejected packet adds no new fact: do not turn a packet flood into SQL.
}

func (o *playbackRecoveredObservation) signalFacts() {
	select {
	case o.changes <- struct{}{}:
	default:
	}
}

func (o *playbackRecoveredObservation) CaptureResponse(response *sip.Response) {
	if response == nil || !response.IsSuccess() {
		return
	}
	if len(response.Body()) > 65536 || len(response.String()) > 65536 {
		o.ObservationLost()
		return
	}
	branch, err := snapshotRecoveredPlaybackBranch(o.invite, response)
	if err != nil {
		o.ObservationLost()
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, group := range [][]playauth.DeviceSIPKnownBranchIdentity{o.known, o.branches} {
		for _, old := range group {
			if old.RemoteTag == branch.RemoteTag {
				if !samePlaybackINFOBranch(old, branch) {
					o.ObservationLost()
				}
				return
			}
		}
	}
	if len(o.known)+len(o.branches) >= 8 {
		o.ObservationLost()
		return
	}
	branch.RouteSet = slices.Clone(branch.RouteSet)
	o.branches = append(o.branches, branch)
	o.signalFacts()
}

func (o *playbackRecoveredObservation) flush(ctx context.Context) error {
	if ctx == nil {
		return ErrPlaybackUnavailable
	}
	select {
	case o.work <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-o.work }()
	o.mu.Lock()
	branches := slices.Clone(o.branches) // Inner routes are immutable, never exposed.
	o.mu.Unlock()
	loaded, err := o.store.LoadSIPInviteSteps(ctx, o.id)
	if err != nil {
		return err
	}
	found := false
	for _, step := range loaded.Steps {
		if step.Identity == o.invite {
			found = true
		}
	}
	if !found {
		return ErrPlaybackCleanupUnknown
	}
	loaded, err = o.store.ObserveSIPBranchInventoryFault(ctx, o.id, loaded.Intent.RowVersion, o.invite.StepID, playauth.SIPBranchObserverIncomplete)
	if err != nil {
		return err
	}
	for _, branch := range branches {
		additional := false
		for _, step := range loaded.Steps {
			if step.Identity == o.invite {
				additional = step.KnownBranch != nil && step.KnownBranch.Identity.RemoteTag != branch.RemoteTag
			}
		}
		if additional {
			loaded, err = o.store.ObserveSIPAdditionalBranch(ctx, o.id, loaded.Intent.RowVersion, branch)
		} else {
			loaded, err = o.store.ObserveSIPKnownBranch(ctx, o.id, loaded.Intent.RowVersion, branch)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (o *playbackRecoveredObservation) run() {
	defer close(o.done)
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
		case <-o.changes:
		case <-retry:
		case <-o.closing:
			o.observation.Close()
			closing = true
		case <-o.observation.Done():
			closing = true
		}
		retry = nil
		ctx, cancel := context.WithTimeout(context.Background(), playbackTeardownTimeout)
		err := o.flush(ctx)
		cancel()
		if closing {
			return
		} // Final best effort; strong snapshots remain for Close retry.
		if err != nil {
			if timer == nil {
				timer = time.NewTimer(time.Second)
			} else {
				timer.Reset(time.Second)
			}
			retry = timer.C
		}
	}
}

// Shutdown only; do not call this when a single cleanup attempt completes.
// The UAC registry retains identity and snapshots even after local close.
func (o *playbackRecoveredObservation) CloseLocal(ctx context.Context) error {
	if ctx == nil {
		return ErrPlaybackUnavailable
	}
	o.closeOnce.Do(func() { close(o.closing) })
	select {
	case <-o.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	if o.prepareErr != nil {
		return o.prepareErr
	}
	o.observation.Close()
	if err := o.flush(ctx); err != nil {
		return err
	}
	select {
	case <-o.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
