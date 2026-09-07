package play

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// playStartAdmission tracks public start calls that crossed the admission
// boundary. It uses a close-on-zero channel instead of WaitGroup so closing
// admission and waiting can never race with a later Add.
type playStartAdmission struct {
	mu     sync.Mutex
	closed bool
	active int
	done   chan struct{}
}

func (a *playStartAdmission) begin() (func(), bool) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, false
	}
	if a.active == 0 {
		a.done = make(chan struct{})
	}
	a.active++
	a.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(a.release)
	}, true
}

func (a *playStartAdmission) release() {
	a.mu.Lock()
	if a.active > 0 {
		a.active--
		if a.active == 0 && a.done != nil {
			close(a.done)
		}
	}
	a.mu.Unlock()
}

func (a *playStartAdmission) close() {
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
}

func (a *playStartAdmission) wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.Lock()
	active := a.active
	done := a.done
	a.mu.Unlock()
	if active == 0 {
		return nil
	}
	return waitFor(ctx, done)
}

type playShutdownCall struct {
	done chan struct{}
	err  error
}

// BeginShutdown closes both the Service-level and Coordinator-level start
// admission gates. The operation is non-blocking and permanently idempotent.
func (s *Service) BeginShutdown() {
	if s == nil {
		return
	}
	s.startAdmission.close()
	s.coordinator().BeginShutdown()
}

// Shutdown waits for accepted starts, lists persisted playing channels, and
// routes each stream through the existing Stop cleanup. Concurrent or repeated
// calls share one bounded shutdown operation and therefore do not duplicate
// media cleanup.
func (s *Service) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.BeginShutdown()

	s.shutdownMu.Lock()
	call := s.shutdownCall
	owner := false
	if call == nil {
		call = &playShutdownCall{done: make(chan struct{})}
		s.shutdownCall = call
		owner = true
	}
	s.shutdownMu.Unlock()
	if !owner {
		return waitForShutdownCall(ctx, call)
	}

	err := s.shutdownAccepted(ctx)
	s.shutdownMu.Lock()
	call.err = err
	close(call.done)
	s.shutdownMu.Unlock()
	return err
}

func waitForShutdownCall(ctx context.Context, call *playShutdownCall) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := waitFor(ctx, call.done); err != nil {
		return err
	}
	return call.err
}

func (s *Service) shutdownAccepted(ctx context.Context) error {
	if err := s.startAdmission.wait(ctx); err != nil {
		return err
	}
	coordinator := s.coordinator()
	if err := coordinator.waitForAcceptedStarts(ctx); err != nil {
		return err
	}
	if s.channels == nil {
		return nil
	}

	channels, listErr := s.channels.ListPlayingChannels(ctx)
	var shutdownErr error
	if listErr != nil {
		shutdownErr = errors.Join(shutdownErr, listErr)
	}
	for _, channel := range channels {
		if channel == nil || channel.StreamID == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			shutdownErr = errors.Join(shutdownErr, err)
			break
		}
		if err := s.Stop(ctx, channel.StreamID); err != nil {
			shutdownErr = errors.Join(shutdownErr, fmt.Errorf("stop stream %s: %w", channel.StreamID, err))
		}
	}
	return shutdownErr
}
