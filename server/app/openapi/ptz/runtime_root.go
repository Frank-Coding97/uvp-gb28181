package ptz

import (
	"context"
	"sync"

	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
)

// RuntimeRoot is the process-lifetime PTZ boundary. SIP reloads replace the
// dispatcher atomically; an empty root is deliberately not ready, so the
// external gateway fails closed while a new SIP generation is assembling.
type RuntimeRoot struct {
	mu         sync.RWMutex
	dispatcher auth.PTZDispatcher
}

var _ auth.PTZDispatcher = (*RuntimeRoot)(nil)

func NewRuntimeRoot() *RuntimeRoot { return &RuntimeRoot{} }

func (r *RuntimeRoot) Replace(dispatcher auth.PTZDispatcher) error {
	if r == nil || dispatcher == nil || !safeReady(dispatcher) {
		return auth.ErrPTZUnavailable
	}
	r.mu.Lock()
	r.dispatcher = dispatcher
	r.mu.Unlock()
	return nil
}

func (r *RuntimeRoot) Publish(dispatcher auth.PTZDispatcher) error { return r.Replace(dispatcher) }

func (r *RuntimeRoot) Clear() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.dispatcher = nil
	r.mu.Unlock()
}

func (r *RuntimeRoot) Ready() bool {
	dispatcher := r.current()
	return dispatcher != nil && safeReady(dispatcher)
}

func (r *RuntimeRoot) Handle(ctx context.Context, request auth.PTZRequest) (any, error) {
	dispatcher := r.current()
	if dispatcher == nil || !safeReady(dispatcher) {
		return nil, auth.ErrPTZUnavailable
	}
	return dispatcher.Handle(ctx, request)
}

func (r *RuntimeRoot) current() auth.PTZDispatcher {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dispatcher
}

func safeReady(dispatcher auth.PTZDispatcher) (ready bool) {
	defer func() {
		if recover() != nil {
			ready = false
		}
	}()
	return dispatcher != nil && dispatcher.Ready()
}
