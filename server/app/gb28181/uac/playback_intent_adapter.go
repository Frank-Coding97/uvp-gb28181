package uac

import (
	"context"
	"errors"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	gbplayback "uvplatform.cn/uvp-gb28181/app/gb28181/playback"
)

var _ gbplayback.IntentSIPFactory = (*PlaybackAdapter)(nil)

func (a *PlaybackAdapter) PrepareIntent(ctx context.Context, store *playauth.DeviceOperationIntentStore, barrier *playauth.DeviceOperationBarrier, parent playauth.DeviceOperationLease, id playauth.DeviceOperationIntentIdentity, version int64, stepID string, in gbplayback.UACInvite) (gbplayback.IntentSIPChild, error) {
	if a == nil || a.UAC == nil {
		return nil, ErrPlaybackUnavailable
	}
	op, err := a.UAC.beginPlaybackIntentChild(ctx, store, barrier, parent, id, version, stepID, PlaybackInviteRequest{
		DeviceID: in.DeviceID, ChannelID: in.ChannelID, Destination: in.Destination, Transport: in.Transport, SSRC: in.SSRC, SDP: in.SDP,
	})
	if op == nil {
		return nil, err
	}
	return &playbackIntentChild{u: a.UAC, op: op, gate: make(chan struct{}, 1)}, err
}

type playbackIntentChild struct {
	u             *UAC
	op            *playbackIntentOperation
	gate          chan struct{}
	closed        bool
	remotePending bool
	remoteErr     error
}

func (c *playbackIntentChild) enter(ctx context.Context) error {
	if c == nil || c.op == nil || ctx == nil {
		return ErrPlaybackUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case c.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *playbackIntentChild) Invite(ctx context.Context) (gbplayback.DialogInfo, error) {
	if err := c.enter(ctx); err != nil {
		return gbplayback.DialogInfo{}, err
	}
	defer func() { <-c.gate }()
	if c.closed {
		return gbplayback.DialogInfo{}, ErrPlaybackUnavailable
	}
	if err := c.op.Start(ctx); err != nil {
		return gbplayback.DialogInfo{CleanupRequired: true}, err
	}
	metadata, err := c.op.ReadAndAccept(ctx)
	return gbplayback.DialogInfo{CallID: metadata.CallID, CleanupRequired: true}, err
}

func (c *playbackIntentChild) SendINFO(ctx context.Context, command playauth.DeviceSIPINFOCommand) error {
	if err := c.enter(ctx); err != nil {
		return err
	}
	defer func() { <-c.gate }()
	if c.closed || c.op.id.Kind != "playback" {
		return ErrPlaybackUnavailable
	}
	return c.op.SendINFO(ctx, command)
}

func (c *playbackIntentChild) Close(ctx context.Context) (gbplayback.IntentCloseResult, error) {
	if err := c.enter(ctx); err != nil {
		return gbplayback.IntentCloseResult{}, err
	}
	defer func() { <-c.gate }()
	if c.closed {
		return gbplayback.IntentCloseResult{LocalQuiesced: true, RemotePending: c.remotePending}, nil
	}
	o := c.op
	if err := waitPlaybackShutdown(ctx, o.ready); err != nil {
		return gbplayback.IntentCloseResult{}, err
	}
	if err := o.enter(ctx); err != nil {
		return gbplayback.IntentCloseResult{}, err
	}
	started := o.started
	o.leave()
	if started {
		// CANCEL requires an actual provisional response. Do not wait for an
		// imaginary response before joining a silent original transaction.
		o.factMu.Lock()
		cancelEligible := o.provisional && o.first == nil
		o.factMu.Unlock()
		if cancelEligible {
			cancelCtx, cancel := context.WithTimeout(ctx, time.Second)
			_ = o.Cancel(cancelCtx)
			cancel()
		}
		c.remoteErr = o.CleanupKnownBranch(ctx)
	}
	o.initCancel()
	o.cleanupCancel()
	if err := o.shutdownLocal(ctx); err != nil {
		return gbplayback.IntentCloseResult{}, errors.Join(c.remoteErr, err)
	}
	loaded, err := o.store.LoadSIPInviteSteps(ctx, o.id)
	if err != nil {
		return gbplayback.IntentCloseResult{}, errors.Join(c.remoteErr, err)
	}
	valid := false
	for _, step := range loaded.Steps {
		if step.Identity != o.invite {
			continue
		}
		if step.State == playauth.SIPStepPrepared {
			valid = !started
		} else if step.State == playauth.SIPStepMayHaveDispatched {
			valid = step.BranchInventoryFault == playauth.SIPBranchObserverIncomplete
			// Observation loss stays unknown even after a known branch's BYE.
			c.remotePending = true
		}
	}
	if !valid {
		return gbplayback.IntentCloseResult{}, errors.Join(c.remoteErr, ErrPlaybackCleanupUnknown)
	}
	c.u.playbackIntentMu.Lock()
	if c.u.playbackIntents[o.id.OperationID] != o {
		c.u.playbackIntentMu.Unlock()
		return gbplayback.IntentCloseResult{}, errors.Join(c.remoteErr, ErrPlaybackUnavailable)
	}
	delete(c.u.playbackIntents, o.id.OperationID)
	c.u.playbackIntentMu.Unlock()
	c.closed = true
	return gbplayback.IntentCloseResult{LocalQuiesced: true, RemotePending: c.remotePending}, nil
}
