package play

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Request identifies one channel-level live ensure operation.
// Trigger is diagnostic metadata only; it never creates a separate
// coordination lane for the same device/channel pair.
type Request struct {
	DeviceID        string
	ChannelID       string
	Trigger         string
	RequiredNode    int64
	AuthorizationID string
}

// EnsureRequest is kept as a descriptive alias for callers that prefer the
// operation-specific name; both names represent the same public contract.
type EnsureRequest = Request

// StartFunc owns the complete side-effecting live start transaction. It must
// return only after failure compensation completed or was quarantined for an
// exact retry by the coordinator.
type StartFunc func(context.Context, Request) (*Result, error)

// StopFunc owns the complete live stop/cleanup transaction.
type StopFunc func(context.Context, *Result) error

// stopCompletionError retains cleanup failures after media has definitely
// stopped. Callers still receive the original failure through errors.Is.
type stopCompletionError struct {
	err error
}

func (e *stopCompletionError) Error() string { return e.err.Error() }

func (e *stopCompletionError) Unwrap() error { return e.err }

func completedMediaStop(err error) error {
	if err == nil {
		return nil
	}
	return &stopCompletionError{err: err}
}

func stopReachedMediaTerminal(err error) bool {
	if err == nil {
		return true
	}
	var completion *stopCompletionError
	return errors.As(err, &completion)
}

var (
	ErrOwnerNodeMismatch  = errors.New("owner-node-mismatch")
	ErrLiveStartNilResult = errors.New("live start returned nil result")
	ErrLiveCleanupPending = errors.New("live start cleanup pending")
)

const cleanupPendingRetryTimeout = 5 * time.Second

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
	result, _, err := c.ensureLive(ctx, req)
	return result, err
}

func (c *Coordinator) ensureLive(ctx context.Context, req Request) (*Result, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	key := coordinatorKey{deviceID: req.DeviceID, channelID: req.ChannelID}

	for {
		c.mu.Lock()
		if c.recoveryPending {
			c.mu.Unlock()
			return nil, false, ErrLiveRecoveryPending
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
			result, err := c.runStart(ctx, req, key, entry)
			return result, false, err
		}

		switch entry.state {
		case LiveStateReady:
			err := ownerNodeConflict(req, entry)
			result := entry.result
			c.mu.Unlock()
			if err != nil {
				return nil, true, err
			}
			return result, true, entry.err
		case LiveStateStarting:
			if err := ownerNodeConflict(req, entry); err != nil {
				c.mu.Unlock()
				return nil, true, err
			}
			done := entry.done
			c.mu.Unlock()
			if err := waitFor(ctx, done); err != nil {
				return nil, true, err
			}
			// The entry is immutable after done is closed. This preserves the
			// exact shared result/error for all waiters, including failed starts.
			if err := ownerNodeConflict(req, entry); err != nil {
				return nil, true, err
			}
			return entry.result, true, entry.err
		case LiveStateCleanupPending:
			if err := ownerNodeConflict(req, entry); err != nil {
				c.mu.Unlock()
				return nil, true, err
			}
			entry.state = LiveStateStopping
			entry.done = make(chan struct{})
			result := entry.result
			c.mu.Unlock()

			var cleanupErr error
			if c.stop != nil {
				cleanupErr = c.stop(context.WithoutCancel(ctx), result)
			}
			c.finishStop(key, entry, cleanupErr, LiveStateCleanupPending)
			if cleanupErr != nil {
				return nil, true, errors.Join(ErrLiveCleanupPending, cleanupErr)
			}
			// A successful cleanup removes the entry. Re-check the map and let
			// this request reserve a new generation through the normal path.
		case LiveStateStopping:
			done := entry.done
			c.mu.Unlock()
			if err := waitFor(ctx, done); err != nil {
				return nil, true, err
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
	// callers. Keep the original deadline so a caller cannot turn the public
	// operation into an unbounded start.
	startCtx := context.WithoutCancel(ctx)
	var cancel context.CancelFunc
	if deadline, ok := ctx.Deadline(); ok {
		startCtx, cancel = context.WithDeadline(startCtx, deadline)
		defer cancel()
	}
	result, err := c.start(startCtx, req)

	var retryCleanup bool
	retryReq := Request{}
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
	} else if errors.Is(err, ErrLiveCleanupPending) && result != nil {
		entry.state = LiveStateCleanupPending
		retryCleanup = true
		retryReq = Request{DeviceID: key.deviceID, ChannelID: key.channelID, RequiredNode: entry.ownerNode}
	} else {
		entry.state = LiveStateIdle
		if current := c.entries[key]; current == entry {
			delete(c.entries, key)
		}
	}
	close(entry.done)
	c.mu.Unlock()
	if retryCleanup {
		go c.retryCleanupPending(retryReq)
	}
	return result, err
}

// retryCleanupPending gives a failed start one bounded retry after its result
// has been published as CleanupPending. This closes the gap where a ZLM timeout
// hook arrived while the start was still in-flight and therefore had no current
// generation to stop. Further retries remain serialized through Stop/EnsureLive.
func (c *Coordinator) retryCleanupPending(req Request) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupPendingRetryTimeout)
	defer cancel()
	_ = c.Stop(ctx, req)
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
			if errors.Is(entry.err, ErrLiveCleanupPending) {
				continue
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
		case LiveStateReady, LiveStateCleanupPending:
			if err := ownerNodeConflict(req, entry); err != nil {
				c.mu.Unlock()
				return err
			}
			failureState := entry.state
			entry.state = LiveStateStopping
			entry.done = make(chan struct{})
			result := entry.result
			c.mu.Unlock()

			var err error
			if c.stop != nil {
				err = c.stop(context.WithoutCancel(ctx), result)
			}
			c.finishStop(key, entry, err, failureState)
			return err
		default:
			c.mu.Unlock()
			return nil
		}
	}
}

func (c *Coordinator) finishStop(key coordinatorKey, entry *coordinatorEntry, err error, failureState LiveState) {
	c.mu.Lock()
	if stopReachedMediaTerminal(err) {
		entry.state = LiveStateIdle
		if current := c.entries[key]; current == entry {
			delete(c.entries, key)
		}
	} else {
		entry.state = failureState
	}
	close(entry.done)
	c.mu.Unlock()
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
	result, reused, err := c.ensureLive(ctx, req)
	if err != nil {
		return nil, err
	}
	if req.AuthorizationID != "" {
		if result == nil || result.Generation == 0 || s.bindAuthorization(req.AuthorizationID, result.Generation) != nil {
			return nil, ErrPlayAuthorizationUnavailable
		}
	}
	callerResult, err := s.resultForCaller(req, result)
	if callerResult != nil && reused {
		callerResult.Reused = true
	}
	if err == nil {
		s.fireSnapshot(result, req.DeviceID, req.ChannelID)
	}
	return callerResult, err
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
