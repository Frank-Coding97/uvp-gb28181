package playback

import (
	"context"
	"sync"
	"sync/atomic"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type zlmRTPCleanupResolver struct {
	nodes    PlaybackNodeLookup
	resolver IntentRTPResolver
}

// NewZLMRTPCleanupResolver adapts the root's trusted immutable TLS bindings. It
// never builds a legacy node client or discovers a replacement resource boot.
func NewZLMRTPCleanupResolver(nodes PlaybackNodeLookup, resolver IntentRTPResolver) playauth.RTPCleanupResolver {
	return &zlmRTPCleanupResolver{nodes: nodes, resolver: resolver}
}

func (f *zlmRTPCleanupResolver) ResolveRTPCleanup(ctx context.Context, i playauth.DeviceRTPResourceIdentity) (playauth.RTPCleanupRuntime, error) {
	if ctx == nil || ctx.Err() != nil || f == nil || f.nodes == nil || f.resolver == nil || i.NodePK <= 0 || i.NodeRevision <= 0 || i.NodeUUID == "" {
		return nil, ErrRTPUnavailable
	}
	n, ok := f.nodes.Get(i.NodePK)
	if !ok || !rtpCleanupNodeMatches(n, i) {
		return nil, ErrRTPUnavailable
	}
	start := *n
	runtime, err := f.resolver.ResolveRTP(ctx, i.NodeUUID)
	if err != nil || ctx.Err() != nil || runtime.Control == nil || runtime.Release == nil || runtime.BootNonce != i.BootNonce {
		if runtime.Release != nil {
			runtime.Release()
		} else if runtime.Control != nil {
			runtime.Control.Close()
		}
		return nil, ErrRTPUnavailable
	}
	current, ok := f.nodes.Get(i.NodePK)
	if !ok || !rtpCleanupNodeMatches(current, i) || current.APISecret != start.APISecret {
		runtime.Release()
		return nil, ErrRTPUnavailable
	}
	return &zlmRTPCleanupRuntime{control: runtime.Control, selector: rtpSelector(i), identity: i, start: start, nodes: f.nodes, release: runtime.Release}, nil
}

func rtpCleanupNodeMatches(n *node.Node, i playauth.DeviceRTPResourceIdentity) bool {
	return n != nil && n.ID == i.NodePK && n.MediaServerUUID == i.NodeUUID && n.Revision == uint64(i.NodeRevision) &&
		n.State == node.StateActive && !n.RecoveryRequired && n.EffectiveReceiveHost() == i.LocalIP
}

type zlmRTPCleanupRuntime struct {
	control     *zlm.OpenAPIRuntimeControl
	selector    zlm.RtpResourceSelector
	identity    playauth.DeviceRTPResourceIdentity
	start       node.Node
	nodes       PlaybackNodeLookup
	release     func()
	released    atomic.Bool
	releaseOnce sync.Once
}

func (r *zlmRTPCleanupRuntime) available(ctx context.Context) bool {
	if r == nil || ctx == nil || ctx.Err() != nil || r.released.Load() {
		return false
	}
	n, ok := r.nodes.Get(r.identity.NodePK)
	return ok && rtpCleanupNodeMatches(n, r.identity) && n.APISecret == r.start.APISecret
}

func (r *zlmRTPCleanupRuntime) CloseResource(ctx context.Context) (string, error) {
	if !r.available(ctx) {
		return "", ErrRTPUnavailable
	}
	result, err := r.control.CloseRtpServerIfMatch(ctx, r.selector)
	return string(result), err
}
func (r *zlmRTPCleanupRuntime) CloseIngress(ctx context.Context) (string, error) {
	if !r.available(ctx) {
		return "", ErrRTPUnavailable
	}
	result, err := r.control.CloseRtpIngressIfMatchV2(ctx, r.selector)
	return string(result), err
}
func (r *zlmRTPCleanupRuntime) Release() {
	if r == nil {
		return
	}
	r.releaseOnce.Do(func() { r.released.Store(true); r.release() })
}
