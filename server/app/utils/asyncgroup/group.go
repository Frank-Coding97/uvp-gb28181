// Package asyncgroup provides a small admission-controlled group for
// application-owned asynchronous work.
package asyncgroup

import (
	"context"
	"sync"
)

// Group accepts work until StopContext closes admission, then waits for every
// accepted function to finish. Its zero value is ready for use.
type Group struct {
	mu     sync.Mutex
	active int
	closed bool
	done   chan struct{}
}

// Go admits fn and runs it asynchronously. It returns false after the group
// has started stopping or when fn is nil. The accepted count is recorded
// before the goroutine is started so StopContext cannot race its admission.
func (g *Group) Go(fn func()) bool {
	if fn == nil {
		return false
	}

	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return false
	}
	if g.done == nil {
		g.done = make(chan struct{})
	}
	g.active++
	g.mu.Unlock()

	go func() {
		defer g.finish()
		fn()
	}()
	return true
}

func (g *Group) finish() {
	g.mu.Lock()
	g.active--
	if g.closed && g.active == 0 {
		close(g.done)
	}
	g.mu.Unlock()
}

// StopContext closes admission once and waits for all accepted work. A
// timeout only ends this wait; it never cancels the accepted functions. If
// work has already completed, that completion takes precedence over an
// already-expired context.
func (g *Group) StopContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	g.mu.Lock()
	if !g.closed {
		g.closed = true
		if g.done == nil {
			g.done = make(chan struct{})
		}
		if g.active == 0 {
			close(g.done)
		}
	}
	done := g.done
	g.mu.Unlock()

	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		// Prefer a completion that became visible before returning the
		// context error when both signals are ready.
		select {
		case <-done:
			return nil
		default:
			return ctx.Err()
		}
	}
}
