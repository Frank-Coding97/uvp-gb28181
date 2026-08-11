package play

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Request identifies one channel-level live ensure operation.
// Trigger is diagnostic metadata only; it never creates a separate
// coordination lane for the same device/channel pair.
type Request struct {
	DeviceID     string
	ChannelID    string
	Trigger      string
	RequiredNode int64
}

// EnsureRequest is kept as a descriptive alias for callers that prefer the
// operation-specific name; both names represent the same public contract.
type EnsureRequest = Request

// StartFunc owns the complete side-effecting live start transaction. It must
// return only after any failure compensation has completed.
type StartFunc func(context.Context, Request) (*Result, error)

// StopFunc owns the complete live stop/cleanup transaction.
type StopFunc func(context.Context, *Result) error

var (
	ErrOwnerNodeMismatch  = errors.New("owner-node-mismatch")
	ErrLiveStartNilResult = errors.New("live start returned nil result")
)

// OwnerNodeMismatchError is returned when a request requires a node different
// from the node that owns the current live generation.
type OwnerNodeMismatchError struct {
	DeviceID     string
	ChannelID    string
	RequiredNode int64
	OwnerNode    int64
}

func (e *OwnerNodeMismatchError) Error() string {
	return fmt.Sprintf("%s: device=%s channel=%s requiredNode=%d ownerNode=%d",
		ErrOwnerNodeMismatch, e.DeviceID, e.ChannelID, e.RequiredNode, e.OwnerNode)
}

func (e *OwnerNodeMismatchError) Unwrap() error { return ErrOwnerNodeMismatch }

type coordinatorKey struct {
	deviceID  string
	channelID string
}

type coordinatorEntry struct {
	state     LiveState
	result    *Result
	err       error
	ownerNode int64
	done      chan struct{}
}

// Coordinator serializes side effects per device/channel while allowing
// independent channels to progress concurrently.
type Coordinator struct {
	mu              sync.Mutex
	entries         map[coordinatorKey]*coordinatorEntry
	recoveryPending bool
	start           StartFunc
	stop            StopFunc
}

func NewCoordinator(start StartFunc) *Coordinator {
	return NewCoordinatorWithStop(start, nil)
}

func NewCoordinatorWithStop(start StartFunc, stop StopFunc) *Coordinator {
	if start == nil {
		panic("play: nil live start function")
	}
	return &Coordinator{entries: make(map[coordinatorKey]*coordinatorEntry), start: start, stop: stop}
}

// EnsureLive ensures that one live generation exists for the channel. An
// owner request starts the shared operation; other requests wait for that
// operation and receive the same result or error. A waiting request's context
// only controls its own wait.
func (c *Coordinator) EnsureLive(ctx context.Context, req Request) (*Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	key := coordinatorKey{deviceID: req.DeviceID, channelID: req.ChannelID}

	for {
		c.mu.Lock()
		if c.recoveryPending {
			c.mu.Unlock()
			return nil, ErrLiveRecoveryPending
		}
		entry := c.entries[key]
		if entry == nil {
			entry = &coordinatorEntry{
				state:     LiveStateStarting,
				ownerNode: req.RequiredNode,
				done:      make(chan struct{}),
			}
			c.entries[key] = entry
			c.mu.Unlock()
			return c.runStart(ctx, req, key, entry)
		}

		switch entry.state {
		case LiveStateReady:
			err := ownerNodeConflict(req, entry)
			result := entry.result
			c.mu.Unlock()
			if err != nil {
				return nil, err
			}
			return result, entry.err
		case LiveStateStarting:
			if err := ownerNodeConflict(req, entry); err != nil {
				c.mu.Unlock()
				return nil, err
			}
			done := entry.done
			c.mu.Unlock()
			if err := waitFor(ctx, done); err != nil {
				return nil, err
			}
			// The entry is immutable after done is closed. This preserves the
			// exact shared result/error for all waiters, including failed starts.
			if err := ownerNodeConflict(req, entry); err != nil {
				return nil, err
			}
			return entry.result, entry.err
		case LiveStateStopping:
			done := entry.done
			c.mu.Unlock()
			if err := waitFor(ctx, done); err != nil {
				return nil, err
			}
			// Stop completion removes the entry. Re-check the map before
			// reserving a new generation.
		default:
			c.mu.Unlock()
		}
	}
}

func (c *Coordinator) runStart(ctx context.Context, req Request, key coordinatorKey, entry *coordinatorEntry) (*Result, error) {
	// A caller disappearing must not cancel the public start shared by other
	// callers. Service.Start applies its own configured total deadline.
	startCtx := context.WithoutCancel(ctx)
	result, err := c.start(startCtx, req)

	c.mu.Lock()
	if err == nil && result == nil {
		err = ErrLiveStartNilResult
	}
	if err == nil {
		if result.Node != nil && result.Node.ID != 0 {
			if entry.ownerNode != 0 && entry.ownerNode != result.Node.ID {
				err = &OwnerNodeMismatchError{
					DeviceID: req.DeviceID, ChannelID: req.ChannelID,
					RequiredNode: entry.ownerNode, OwnerNode: result.Node.ID,
				}
				result = nil
			} else if entry.ownerNode == 0 {
				entry.ownerNode = result.Node.ID
			}
		}
	}
	entry.result = result
	entry.err = err
	if err == nil {
		entry.state = LiveStateReady
	} else {
		entry.state = LiveStateIdle
		if current := c.entries[key]; current == entry {
			delete(c.entries, key)
		}
	}
	close(entry.done)
	c.mu.Unlock()
	return result, err
}

// Stop transitions a ready generation through Stopping and blocks new starts
// until cleanup has returned. Concurrent stop callers share the same barrier.
func (c *Coordinator) Stop(ctx context.Context, req Request) error {
	if ctx == nil {
		ctx = context.Background()
	}
	key := coordinatorKey{deviceID: req.DeviceID, channelID: req.ChannelID}
	for {
		c.mu.Lock()
		entry := c.entries[key]
		if entry == nil {
			c.mu.Unlock()
			return nil
		}
		switch entry.state {
		case LiveStateStarting:
			done := entry.done
			c.mu.Unlock()
			if err := waitFor(ctx, done); err != nil {
				return err
			}
			if entry.err != nil {
				return entry.err
			}
		case LiveStateStopping:
			done := entry.done
			c.mu.Unlock()
			if err := waitFor(ctx, done); err != nil {
				return err
			}
		case LiveStateReady:
			if err := ownerNodeConflict(req, entry); err != nil {
				c.mu.Unlock()
				return err
			}
			entry.state = LiveStateStopping
			entry.done = make(chan struct{})
			result := entry.result
			c.mu.Unlock()

			var err error
			if c.stop != nil {
				err = c.stop(context.WithoutCancel(ctx), result)
			}
			c.mu.Lock()
			entry.state = LiveStateIdle
			if current := c.entries[key]; current == entry {
				delete(c.entries, key)
			}
			close(entry.done)
			c.mu.Unlock()
			return err
		default:
			c.mu.Unlock()
			return nil
		}
	}
}

// StopStream resolves the channel owner for a stream and applies the same
// Stopping barrier as a keyed Stop. It is used by the legacy stream-only Stop
// entry point while the public request contract remains channel based.
func (c *Coordinator) StopStream(ctx context.Context, streamID string) (bool, error) {
	c.mu.Lock()
	var req Request
	for key, entry := range c.entries {
		if entry.result == nil || entry.result.StreamID != streamID {
			continue
		}
		req = Request{DeviceID: key.deviceID, ChannelID: key.channelID, RequiredNode: entry.ownerNode}
		c.mu.Unlock()
		return true, c.Stop(ctx, req)
	}
	c.mu.Unlock()
	return false, nil
}

func ownerNodeConflict(req Request, entry *coordinatorEntry) error {
	if req.RequiredNode == 0 || entry.ownerNode == 0 || req.RequiredNode == entry.ownerNode {
		return nil
	}
	return &OwnerNodeMismatchError{
		DeviceID: req.DeviceID, ChannelID: req.ChannelID,
		RequiredNode: req.RequiredNode, OwnerNode: entry.ownerNode,
	}
}

func waitFor(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// EnsureLive exposes the channel coordinator through the existing Service.
// Existing Start callers remain compatible; new REST/Hook integrations can
// migrate to this method without changing the underlying Start transaction.
func (s *Service) EnsureLive(ctx context.Context, req Request) (*Result, error) {
	c := s.coordinator()
	result, err := c.EnsureLive(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.resultForCaller(req, result)
}

func (s *Service) coordinator() *Coordinator {
	s.liveCoordinatorMu.Lock()
	if s.liveCoordinator == nil {
		s.liveCoordinator = NewCoordinatorWithStop(
			func(ctx context.Context, req Request) (*Result, error) {
				return s.startDirect(ctx, req)
			},
			func(ctx context.Context, result *Result) error {
				return s.stopCurrentResult(ctx, result)
			},
		)
	}
	c := s.liveCoordinator
	s.liveCoordinatorMu.Unlock()
	return c
}
