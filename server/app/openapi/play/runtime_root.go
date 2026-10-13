package play

import (
	"context"
	"sync"

	"uvplatform.com/uvp-gb28181/app/openapi/auth"
)

type RuntimeRoot struct {
	mu         sync.RWMutex
	dispatcher auth.PlayDispatcher
}

var _ auth.PlayDispatcher = (*RuntimeRoot)(nil)
var _ auth.PlayViewerMaintainer = (*RuntimeRoot)(nil)

func NewRuntimeRoot() *RuntimeRoot { return &RuntimeRoot{} }

func (r *RuntimeRoot) Replace(dispatcher auth.PlayDispatcher) error {
	if r == nil || dispatcher == nil || !safeReady(dispatcher) {
		return auth.ErrPlayUnavailable
	}
	r.mu.Lock()
	r.dispatcher = dispatcher
	r.mu.Unlock()
	return nil
}

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

func (r *RuntimeRoot) Start(ctx context.Context, request auth.PlayRequest) (any, error) {
	dispatcher := r.current()
	if dispatcher == nil || !safeReady(dispatcher) {
		return nil, auth.ErrPlayUnavailable
	}
	return dispatcher.Start(ctx, request)
}

func (r *RuntimeRoot) ReconcileViewers(ctx context.Context) error {
	dispatcher := r.current()
	if dispatcher == nil {
		return nil
	}
	maintainer, ok := dispatcher.(auth.PlayViewerMaintainer)
	if !ok {
		return nil
	}
	return maintainer.ReconcileViewers(ctx)
}

func (r *RuntimeRoot) current() auth.PlayDispatcher {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dispatcher
}

func safeReady(dispatcher auth.PlayDispatcher) (ready bool) {
	defer func() {
		if recover() != nil {
			ready = false
		}
	}()
	return dispatcher != nil && dispatcher.Ready()
}
