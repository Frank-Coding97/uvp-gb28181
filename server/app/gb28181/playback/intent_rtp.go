package playback

import (
	"context"
	"errors"
	"math"
	"strconv"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

var ErrIntentRTPRemoteUnknown = errors.New("persistent RTP remote coverage is incomplete")

// The composition root adapts its existing operator TLS binding resolver.
// Never construct this control from node tags, a request DTO or private IP trust.
type IntentRTPRuntime struct {
	Control   *zlm.OpenAPIRuntimeControl
	BootNonce string
	Release   func()
}
type IntentRTPResolver interface {
	ResolveRTP(context.Context, string) (IntentRTPRuntime, error)
}

type zlmIntentRTPOpener struct {
	nodes     PlaybackNodeLookup
	locations PlaybackLocationStore
	resolver  IntentRTPResolver
}

func NewZLMIntentRTPOpener(nodes PlaybackNodeLookup, locations PlaybackLocationStore, resolver IntentRTPResolver) RTPOpener {
	return &zlmIntentRTPOpener{nodes: nodes, locations: locations, resolver: resolver}
}

func (*zlmIntentRTPOpener) Open(context.Context, RTPRequest) (RTPAllocation, error) {
	return RTPAllocation{}, ErrRTPUnavailable // no legacy fallback on this adapter
}

func (f *zlmIntentRTPOpener) PrepareIntent(ctx context.Context, store *playauth.DeviceOperationIntentStore, id playauth.DeviceOperationIntentIdentity, version int64, picked NodeInfo, request RTPRequest) (IntentRTPChild, error) {
	if ctx == nil || f == nil || f.nodes == nil || f.locations == nil || f.resolver == nil || store == nil {
		return nil, ErrRTPUnavailable
	}
	pk, err := strconv.ParseInt(request.NodeID, 10, 64)
	if err != nil || picked.ID != request.NodeID || picked.NodeUUID == "" || picked.NodeRevision == 0 || picked.NodeRevision > math.MaxInt64 {
		return nil, ErrRTPUnavailable
	}
	selected, ok := f.nodes.Get(pk)
	if !ok || selected == nil || selected.State != node.StateActive || selected.RecoveryRequired || selected.MediaServerUUID != picked.NodeUUID || selected.Revision != picked.NodeRevision || selected.EffectiveReceiveHost() != picked.RecvIP {
		return nil, ErrRTPUnavailable
	}
	runtime, err := f.resolver.ResolveRTP(ctx, picked.NodeUUID)
	if err != nil {
		if runtime.Release != nil {
			runtime.Release()
		}
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred && runtime.Release != nil {
			runtime.Release()
		}
	}()
	if runtime.Control == nil || runtime.Release == nil {
		return nil, ErrRTPUnavailable
	}
	current, ok := f.nodes.Get(pk)
	if !ok || current == nil || current.ID != selected.ID || current.MediaServerUUID != selected.MediaServerUUID || current.Revision != selected.Revision || current.APISecret != selected.APISecret || current.State != node.StateActive || current.RecoveryRequired {
		return nil, ErrRTPUnavailable
	}
	ssrc, err := strconv.ParseUint(request.SSRC, 10, 32)
	if err != nil {
		return nil, ErrRTPUnavailable
	}
	stepID, err := playauth.NewDeviceOperationIntentID()
	if err != nil {
		return nil, err
	}
	resourceID, err := playauth.NewDeviceRTPResourceID(id.OperationID, stepID, time.Now())
	if err != nil {
		return nil, err
	}
	tcpMode := 0
	if request.TCPMode {
		tcpMode = 1
	}
	identity := playauth.DeviceRTPResourceIdentity{StepID: stepID, NodePK: pk, NodeUUID: picked.NodeUUID, NodeRevision: int64(picked.NodeRevision),
		BootNonce: runtime.BootNonce, ResourceID: resourceID, VHost: "__defaultVhost__", App: playbackZLMApp, Stream: request.StreamID,
		Port: request.Port, LocalIP: picked.RecvIP, TCPMode: tcpMode, SSRC: uint32(ssrc)}
	stored, err := store.AddRTPResourceStep(ctx, id, version, identity)
	if err != nil {
		return nil, err
	} // no resource network call was started
	work, err := store.PrepareRTPResourceWork(ctx, id, stepID)
	if err != nil {
		return nil, err
	}
	child := &intentRTPChild{work: work, version: stored.Intent.RowVersion, identity: identity, runtime: runtime,
		locations: f.locations, ssrc: request.SSRC, gate: make(chan struct{}, 1)}
	transferred = true
	return child, nil
}

type intentRTPChild struct {
	work                                         *playauth.RTPResourceWork
	version                                      int64
	identity                                     playauth.DeviceRTPResourceIdentity
	runtime                                      IntentRTPRuntime
	locations                                    PlaybackLocationStore
	ssrc                                         string
	gate                                         chan struct{}
	attempted, dispatched, cleanupStarted, local bool
	remoteErr                                    error
}

func (c *intentRTPChild) enter(ctx context.Context) error {
	if ctx == nil {
		return ErrRTPUnavailable
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

func rtpSelector(i playauth.DeviceRTPResourceIdentity) zlm.RtpResourceSelector {
	return zlm.RtpResourceSelector{BootNonce: i.BootNonce, ResourceID: i.ResourceID, VHost: i.VHost, App: i.App, Stream: i.Stream}
}

func (c *intentRTPChild) Open(ctx context.Context) (RTPAllocation, error) {
	if err := c.enter(ctx); err != nil {
		return RTPAllocation{}, err
	}
	defer func() { <-c.gate }()
	if c.attempted || c.cleanupStarted || c.local {
		return RTPAllocation{}, ErrRTPUnavailable
	}
	c.attempted = true
	if _, err := c.work.Dispatch(ctx, c.version); err != nil {
		return RTPAllocation{}, err
	}
	c.dispatched = true
	result, err := c.work.Open(ctx, func(ctx context.Context, i playauth.DeviceRTPResourceIdentity) (playauth.DeviceRTPOpenResult, error) {
		result, err := c.runtime.Control.OpenRtpServerIfMatch(ctx, zlm.RtpResourceOpenRequest{RtpResourceSelector: rtpSelector(i),
			Port: i.Port, LocalIP: i.LocalIP, TCPMode: i.TCPMode, SSRC: i.SSRC, OnlyTrack: i.OnlyTrack})
		return playauth.DeviceRTPOpenResult{Result: string(result.Result), Port: result.Port}, err
	})
	if err != nil {
		return RTPAllocation{}, err
	}
	if err := c.work.Flush(ctx); err != nil {
		return RTPAllocation{}, err
	}
	if result.Result != "created" && result.Result != "existing" {
		return RTPAllocation{}, ErrRTPUnavailable
	}
	return RTPAllocation{StreamID: c.identity.Stream, SSRC: c.ssrc, Port: result.Port,
		Bind: func() error { c.locations.Bind(c.identity.Stream, c.identity.NodePK); return nil }}, nil
}

func (c *intentRTPChild) Close(ctx context.Context) (bool, error) {
	if err := c.enter(ctx); err != nil {
		return false, err
	}
	defer func() { <-c.gate }()
	if c.local {
		return true, c.remoteErr
	}
	if !c.cleanupStarted {
		c.cleanupStarted = true
		if c.dispatched {
			_, resourceErr := c.work.CloseResource(ctx, func(ctx context.Context, i playauth.DeviceRTPResourceIdentity) (string, error) {
				result, err := c.runtime.Control.CloseRtpServerIfMatch(ctx, rtpSelector(i))
				return string(result), err
			})
			_, ingressErr := c.work.CloseIngress(ctx, func(ctx context.Context, i playauth.DeviceRTPResourceIdentity) (string, error) {
				result, err := c.runtime.Control.CloseRtpIngressIfMatchV2(ctx, rtpSelector(i))
				return string(result), err
			})
			c.remoteErr = errors.Join(resourceErr, ingressErr, ErrIntentRTPRemoteUnknown)
		}
	}
	if err := c.work.Quiesce(ctx); err != nil {
		return false, errors.Join(c.remoteErr, err)
	}
	c.runtime.Release() // all actual synchronous HTTP calls have returned
	c.local = true
	return true, c.remoteErr
}

func (c *intentRTPChild) Unbind(ctx context.Context) error {
	if err := c.enter(ctx); err != nil {
		return err
	}
	defer func() { <-c.gate }()
	if !c.local {
		return ErrRTPUnavailable
	}
	if pk, ok := c.locations.Lookup(c.identity.Stream); ok && pk != c.identity.NodePK {
		return ErrRTPUnavailable
	}
	c.locations.Unbind(c.identity.Stream)
	return nil
}
