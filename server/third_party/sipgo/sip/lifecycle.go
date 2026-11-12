package sip

import (
	"context"
	"sync"
)

// lifecycleGate admits callbacks until close is called and exposes a stable
// completion signal for all callbacks admitted before close. It deliberately
// does not cancel admitted callbacks: the caller decides whether cancellation
// is safe for the protocol operation being drained.
type lifecycleGate struct {
	mu     sync.Mutex
	active int
	closed bool
	done   chan struct{}
}

// Lifecycle is the small public adapter used by the parent sipgo package for
// tracking listener and callback goroutines without importing application
// lifecycle code into the protocol package.
type Lifecycle struct {
	gate lifecycleGate
}

func (l *Lifecycle) Go(fn func()) bool {
	if l == nil {
		return false
	}
	return l.gate.goRun(fn)
}

func (l *Lifecycle) Close() {
	if l != nil {
		l.gate.close()
	}
}

func (l *Lifecycle) Wait(ctx context.Context) error {
	if l == nil {
		return nil
	}
	return l.gate.wait(ctx)
}

func (g *lifecycleGate) enter() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	if g.done == nil {
		g.done = make(chan struct{})
	}
	g.active++
	return true
}

func (g *lifecycleGate) leave() {
	g.mu.Lock()
	g.active--
	if g.closed && g.active == 0 {
		close(g.done)
	}
	g.mu.Unlock()
}

func (g *lifecycleGate) close() {
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
	g.mu.Unlock()
}

func (g *lifecycleGate) isClosed() bool {
	g.mu.Lock()
	closed := g.closed
	g.mu.Unlock()
	return closed
}

func (g *lifecycleGate) run(fn func()) bool {
	if fn == nil || !g.enter() {
		return false
	}
	defer g.leave()
	fn()
	return true
}

func (g *lifecycleGate) goRun(fn func()) bool {
	if fn == nil || !g.enter() {
		return false
	}
	go func() {
		defer g.leave()
		fn()
	}()
	return true
}

func (g *lifecycleGate) wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	g.mu.Lock()
	done := g.done
	if done == nil {
		// Keep the completion channel stable. Before Close the caller may still
		// admit work, so Wait must remain pending until Close seals the gate.
		done = make(chan struct{})
		g.done = done
		if g.closed && g.active == 0 {
			close(done)
		}
	}
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
		// Completion wins when it became visible at the same time as the
		// deadline, matching the shutdown contract used by the app owners.
		select {
		case <-done:
			return nil
		default:
			return ctx.Err()
		}
	}
}
