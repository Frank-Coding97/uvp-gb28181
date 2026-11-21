package config

import "context"

// Process-wide gates also serialize callers using separate coordinator values.
// A bounded stripe table avoids retaining a lock for every submitted node ID.
// This implements the adopted single-API-process contract, not HA coordination.
var nodeProbeGates = func() [64]chan struct{} {
	var gates [64]chan struct{}
	for i := range gates {
		gates[i] = make(chan struct{}, 1)
	}
	return gates
}()

type NodeProbeCoordinator struct {
	store *NodeRuntimeStore
}

func NewNodeProbeCoordinator(store *NodeRuntimeStore) *NodeProbeCoordinator {
	return &NodeProbeCoordinator{store: store}
}

// Probe serializes the entire trusted network exchange and its durable commit.
// The callback must perform fresh, read-only checks using a verified control
// endpoint bound to ref. Hook payloads cannot be used as observations. No SQL
// transaction is held over network I/O; the final commit rechecks node revision.
// Successful control readback is NOT protocol/topology qualification, must-auth
// activation, or evidence that an old media process exited. Callers must still
// enforce those separate gates before issuing playback grants.
func (c *NodeProbeCoordinator) Probe(ctx context.Context, ref NodeRuntimeRef, probe func(context.Context) (NodeRuntimeObservation, error)) (NodeRuntimeSnapshot, error) {
	if err := validateNodeRuntimeRef(ctx, ref); err != nil {
		return NodeRuntimeSnapshot{}, err
	}
	if c == nil || c.store == nil || probe == nil || ctx.Err() != nil {
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
	}
	gate := nodeProbeGates[uint64(ref.NodeID)%uint64(len(nodeProbeGates))]
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
	}
	if ctx.Err() != nil {
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
	}
	// Withdraw the previous confirmation BEFORE I/O. Failure, cancellation, or
	// a crash cannot leave a previous active status claiming a successful probe.
	if _, err := c.store.MarkUnknown(ctx, ref); err != nil {
		return NodeRuntimeSnapshot{}, err
	}
	observation, err := probe(ctx)
	if err != nil || ctx.Err() != nil || observation.NodeRuntimeRef != ref {
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
	}
	return c.store.ConfirmProbe(ctx, observation)
}
