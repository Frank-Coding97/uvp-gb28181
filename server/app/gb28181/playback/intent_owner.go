package playback

import (
	"context"
	"sync"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// Installed in Registry.Create before Reserve or any resource preparation.
// The ready boundary publishes ALL children, including those returned on error.
// Stop/sweeper/callback cleanup cannot observe a half-initialized empty owner.
type playbackIntentOwner struct {
	store              *playauth.DeviceOperationIntentStore
	barrier            *playauth.DeviceOperationBarrier
	id                 playauth.DeviceOperationIntentIdentity
	version            int64
	ctx                context.Context
	cancel             context.CancelFunc
	stopLease          func() bool
	lease              playauth.DeviceOperationLease
	rtp                IntentRTPChild
	sip                IntentSIPChild
	ready              chan struct{}
	readyOnce          sync.Once
	gate               chan struct{}
	sipLocal, rtpLocal bool
	released           bool
}

func newPlaybackIntentOwner(ctx context.Context, store *playauth.DeviceOperationIntentStore, barrier *playauth.DeviceOperationBarrier, request CreateRequest) (*playbackIntentOwner, error) {
	if ctx == nil || store == nil || barrier == nil {
		return nil, playauth.ErrDeviceOperationUnavailable
	}
	operationID, err := playauth.NewDeviceOperationIntentID()
	if err != nil {
		return nil, err
	}
	kind := string(request.Mode)
	if kind == "" {
		kind = string(ModePlayback)
	}
	if kind != string(ModePlayback) && kind != string(ModeDownload) {
		return nil, ErrInvalidSession
	}
	a := request.Authorization
	ownerCtx, cancel := context.WithCancel(ctx)
	return &playbackIntentOwner{store: store, barrier: barrier, ctx: ownerCtx, cancel: cancel, ready: make(chan struct{}), gate: make(chan struct{}, 1),
		id: playauth.DeviceOperationIntentIdentity{OperationID: operationID, DevicePK: a.DevicePK, DeviceCode: a.DeviceCode, DeviceEpoch: a.DeviceEpoch,
			TargetScope: "channel", TargetPK: a.ChannelPK, TargetCode: a.ChannelCode, Kind: kind}}, nil
}

func (o *playbackIntentOwner) begin(ctx context.Context) error {
	reserved, err := o.store.Reserve(ctx, o.id)
	if err != nil {
		return err
	}
	o.version = reserved.RowVersion
	o.lease, err = o.barrier.BeginEpoch(o.ctx, o.id.DeviceCode, o.id.DeviceEpoch)
	if err != nil {
		return err
	}
	o.stopLease = context.AfterFunc(o.lease.Context(), o.cancel)
	if err := o.ctx.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dispatched, err := o.store.Dispatch(ctx, o.id, reserved.RowVersion)
	if err != nil {
		return err
	}
	o.version = dispatched.RowVersion
	return nil
}

func (o *playbackIntentOwner) finishInitialization() { o.readyOnce.Do(func() { close(o.ready) }) }

func (o *playbackIntentOwner) initializing() bool {
	select {
	case <-o.ready:
		return false
	default:
		return true
	}
}

func (o *playbackIntentOwner) enter(ctx context.Context) error {
	if ctx == nil {
		return playauth.ErrDeviceOperationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-o.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case o.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (o *playbackIntentOwner) Teardown(ctx context.Context) error {
	o.cancel()
	if err := o.enter(ctx); err != nil {
		return err
	}
	defer func() { <-o.gate }()
	if o.sip == nil {
		o.sipLocal = true
		return nil
	}
	result, err := o.sip.Close(ctx)
	err = localIntentCloseError(result, err)
	o.sipLocal = err == nil
	return err
}

func localIntentCloseError(result IntentCloseResult, err error) error {
	if err != nil {
		return err
	}
	if !result.LocalQuiesced {
		return playauth.ErrDeviceOperationUnavailable
	}
	return nil
}

func (o *playbackIntentOwner) CloseRTP(ctx context.Context) error {
	o.cancel()
	if err := o.enter(ctx); err != nil {
		return err
	}
	defer func() { <-o.gate }()
	var err error
	if o.rtp == nil {
		o.rtpLocal = true
	} else {
		var result IntentCloseResult
		result, err = o.rtp.Close(ctx)
		err = localIntentCloseError(result, err)
		o.rtpLocal = err == nil
	}
	if o.rtpLocal && o.sipLocal && !o.released {
		if o.stopLease != nil {
			o.stopLease()
		}
		if o.lease != nil {
			o.lease.Release()
		}
		o.released = true
	}
	return err
}

func (o *playbackIntentOwner) Unbind(ctx context.Context) error {
	if err := o.enter(ctx); err != nil {
		return err
	}
	defer func() { <-o.gate }()
	if !o.released {
		return playauth.ErrDeviceOperationUnavailable
	}
	if o.rtp != nil {
		return o.rtp.Unbind(ctx)
	}
	return nil
}

func (o *playbackIntentOwner) action(ctx context.Context, command playauth.DeviceSIPINFOCommand) error {
	if err := o.enter(ctx); err != nil {
		return err
	}
	defer func() { <-o.gate }()
	if o.ctx.Err() != nil || o.sip == nil || o.released || o.id.Kind != string(ModePlayback) {
		return playauth.ErrDeviceOperationUnavailable
	}
	return o.sip.SendINFO(ctx, command)
}
