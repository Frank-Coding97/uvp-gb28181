package launcher

import (
	"context"
	"errors"
	"sync"
	"uvplatform.cn/uvp-gb28181/internal/standalone/control"
)

// controlOwner acknowledges stop only after component exits, Job cleanup, and
// run-marker removal. The command client's lifetime does not own cleanup.
type controlOwner struct {
	mu        sync.Mutex
	state     State
	requested bool
	cancel    context.CancelFunc
	done      chan struct{}
	result    error
}

func newControlOwner(cancel context.CancelFunc) *controlOwner {
	return &controlOwner{state: Stopped, cancel: cancel, done: make(chan struct{})}
}
func (o *controlOwner) publish(state State) { o.mu.Lock(); o.state = state; o.mu.Unlock() }
func (o *controlOwner) finish(err error)    { o.result = err; close(o.done) }
func (o *controlOwner) stopRequested() bool { o.mu.Lock(); defer o.mu.Unlock(); return o.requested }
func (o *controlOwner) handle(ctx context.Context, command control.Command) (control.Reply, error) {
	o.mu.Lock()
	if command == control.Probe {
		state := o.state
		o.mu.Unlock()
		if state == Ready {
			return control.Ready, nil
		}
		if state == Failed {
			return control.Failed, nil
		}
		return control.Stopping, nil
	}
	if command != control.Stop || (o.state != Ready && !o.requested) {
		o.mu.Unlock()
		return control.Failed, errors.New("launcher is not ready to stop")
	}
	o.requested = true
	o.mu.Unlock()
	o.cancel()
	if err := ctx.Err(); err != nil {
		return control.Failed, err
	}
	select {
	case <-ctx.Done():
		return control.Failed, ctx.Err()
	case <-o.done:
		if o.result != nil {
			return control.Failed, o.result
		}
		return control.Finalized, nil
	}
}
