package media

import (
	"context"
	"sync"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
)

// LivePlayerRuntime is the stable OpenAPI-only facade across SIP reloads.
// Retire closes admission and joins calls before root destroys dependencies.
// It does not claim to drain backend callers or the resulting media sessions.
type LivePlayerRuntime struct {
	mu      sync.Mutex
	current *livePlayerGeneration
}

type livePlayerGeneration struct {
	player  LivePlayer
	ctx     context.Context
	cancel  context.CancelFunc
	retired bool
	active  int
	done    chan struct{}
}

var _ LivePlayer = (*LivePlayerRuntime)(nil)

func NewLivePlayerRuntime() *LivePlayerRuntime { return &LivePlayerRuntime{} }

func (r *LivePlayerRuntime) Ready() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current != nil && !r.current.retired
}

func (r *LivePlayerRuntime) Publish(player LivePlayer) error {
	if r == nil || interfaceIsNil(player) {
		return ErrLiveApplicationUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current != nil {
		return ErrLiveApplicationUnavailable
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.current = &livePlayerGeneration{player: player, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	return nil
}

func (r *LivePlayerRuntime) Retire(ctx context.Context) error {
	if r == nil || ctx == nil {
		return ErrLiveApplicationUnavailable
	}
	r.mu.Lock()
	generation := r.current
	if generation == nil {
		r.mu.Unlock()
		return nil
	}
	if !generation.retired {
		generation.retired = true
		if generation.active == 0 {
			close(generation.done)
		}
	}
	r.mu.Unlock()
	generation.cancel()
	select {
	case <-generation.done:
		r.mu.Lock()
		if r.current == generation {
			r.current = nil
		}
		r.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *LivePlayerRuntime) EnsureLive(ctx context.Context, request play.Request) (*play.Result, error) {
	if r == nil || ctx == nil || ctx.Err() != nil {
		return nil, ErrLiveApplicationUnavailable
	}
	r.mu.Lock()
	generation := r.current
	if generation == nil || generation.retired {
		r.mu.Unlock()
		return nil, ErrLiveApplicationUnavailable
	}
	generation.active++
	r.mu.Unlock()
	callCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(generation.ctx, cancel)
	defer func() {
		stop()
		cancel()
		r.mu.Lock()
		generation.active--
		if generation.retired && generation.active == 0 {
			close(generation.done)
		}
		r.mu.Unlock()
	}()
	if generation.ctx.Err() != nil {
		return nil, ErrLiveApplicationUnavailable
	}
	result, err := generation.player.EnsureLive(callCtx, request)
	// A non-cooperative player may return success after cancellation. Do not
	// let that become a successful handoff from an already-retired generation.
	r.mu.Lock()
	retired := generation.retired
	r.mu.Unlock()
	if retired || generation.ctx.Err() != nil || callCtx.Err() != nil {
		return nil, ErrLiveApplicationUnavailable
	}
	return result, err
}
