package play

import (
	"context"

	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
)

// Restore installs a recovered live generation as ready without invoking the
// start function. A channel already owned by an in-flight or ready generation
// is left untouched.
func (c *Coordinator) Restore(req Request, result *Result) bool {
	if result == nil || result.StreamID == "" || result.SSRC == "" || result.Generation == 0 {
		return false
	}
	if result.Node != nil && req.RequiredNode != 0 && req.RequiredNode != result.Node.ID {
		return false
	}

	key := coordinatorKey{deviceID: req.DeviceID, channelID: req.ChannelID}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; exists {
		return false
	}

	ownerNode := req.RequiredNode
	if result.Node != nil && result.Node.ID != 0 {
		ownerNode = result.Node.ID
	}
	done := make(chan struct{})
	close(done)
	c.entries[key] = &coordinatorEntry{
		state:     LiveStateReady,
		result:    result,
		ownerNode: ownerNode,
		done:      done,
	}
	return true
}

// BeginRecovery closes the shared EnsureLive entry point while persisted live
// ownership is reconciled. It is only valid before live entries exist.
func (c *Coordinator) BeginRecovery() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) != 0 {
		return false
	}
	c.recoveryPending = true
	return true
}

func (c *Coordinator) FinishRecovery() {
	c.mu.Lock()
	c.recoveryPending = false
	c.mu.Unlock()
}

// CurrentResult returns the ready generation currently owning streamID.
func (c *Coordinator) CurrentResult(streamID string) (*Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, entry := range c.entries {
		if entry.state == LiveStateReady && entry.result != nil && entry.result.StreamID == streamID {
			return cloneResult(entry.result), true
		}
	}
	return nil, false
}

func (c *Coordinator) cleanupPendingResult(streamID string) (*Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, entry := range c.entries {
		if entry.state == LiveStateCleanupPending && entry.result != nil && entry.result.StreamID == streamID {
			return cloneResult(entry.result), true
		}
	}
	return nil, false
}

// StopIfCurrent applies stop only when ref is the current live generation.
// A known stream with an older ref is considered handled but must not affect
// the newer generation.
func (c *Coordinator) StopIfCurrent(ctx context.Context, ref stream.LiveRef) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	var entry *coordinatorEntry
	var key coordinatorKey
	for candidateKey, candidate := range c.entries {
		if candidate.result != nil && candidate.result.StreamID == ref.StreamID {
			entry = candidate
			key = candidateKey
			break
		}
	}
	if entry == nil {
		c.mu.Unlock()
		return false, nil
	}
	if !resultMatchesRef(entry.result, ref) {
		c.mu.Unlock()
		return true, nil
	}
	if entry.state == LiveStateStopping {
		c.mu.Unlock()
		return true, nil
	}
	if entry.state != LiveStateReady && entry.state != LiveStateCleanupPending {
		c.mu.Unlock()
		return true, nil
	}
	if entry.pins > 0 {
		c.mu.Unlock()
		return true, ErrLivePinned
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
	return true, err
}

func resultMatchesRef(result *Result, ref stream.LiveRef) bool {
	if result == nil || result.StreamID != ref.StreamID || result.SSRC != ref.SSRC || result.Generation != ref.Generation {
		return false
	}
	var nodeID int64
	if result.Node != nil {
		nodeID = result.Node.ID
	}
	return nodeID == ref.NodeID
}
