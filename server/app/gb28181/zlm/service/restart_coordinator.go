package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// RestartStatus is the observable lifecycle of an accepted ZLM restart.
type RestartStatus string

const (
	RestartStatusUnknown          RestartStatus = "unknown"
	RestartStatusAccepted         RestartStatus = "accepted"
	RestartStatusWaitingOffline   RestartStatus = "waiting_offline"
	RestartStatusWaitingHeartbeat RestartStatus = "waiting_heartbeat"
	RestartStatusConverging       RestartStatus = "converging"
	RestartStatusReady            RestartStatus = "ready"
	RestartStatusFailed           RestartStatus = "failed"
)

// RestartOperation is a safe, secret-free snapshot returned to API callers.
type RestartOperation struct {
	OperationID string        `json:"operationId"`
	NodeID      int64         `json:"nodeId"`
	Generation  uint64        `json:"generation"`
	Status      RestartStatus `json:"status"`
	Error       string        `json:"error,omitempty"`
	AcceptedAt  time.Time     `json:"acceptedAt"`
	UpdatedAt   time.Time     `json:"updatedAt"`
}

// RestartAcceptedResponse is the restart API result. The command being
// accepted by ZLM is deliberately not reported as ready.
type RestartAcceptedResponse struct {
	Accepted    bool          `json:"accepted"`
	OperationID string        `json:"operationId"`
	Status      RestartStatus `json:"status"`
}

// RestartEventNotifier is the optional bridge used by heartbeat Watcher and
// Collector. Keeping it small lets old constructors remain source-compatible
// and leaves final bootstrap wiring to T14.
type RestartEventNotifier interface {
	OnNodeOffline(nodeID int64)
	OnNodeHeartbeat(nodeID int64)
}

// RestartCoordinator owns restart state independently of node.State. A node
// is never put into maintenance merely to wait for a callback; admission is
// blocked by the explicit Registry gate until the operation reaches ready or
// failed.
type RestartCoordinator struct {
	registry *node.Registry
	timeout  time.Duration
	now      func() time.Time

	mu          sync.Mutex
	operations  map[int64]*RestartOperation
	generations map[int64]uint64
	timers      map[int64]*time.Timer
	converge    func(context.Context, int64) error
}

// NewRestartCoordinator constructs an in-memory coordinator. timeout is
// optional for compatibility; the production default is two minutes.
func NewRestartCoordinator(reg *node.Registry, timeout ...time.Duration) *RestartCoordinator {
	t := 2 * time.Minute
	if len(timeout) > 0 && timeout[0] > 0 {
		t = timeout[0]
	}
	return &RestartCoordinator{
		registry:    reg,
		timeout:     t,
		now:         time.Now,
		operations:  make(map[int64]*RestartOperation),
		generations: make(map[int64]uint64),
		timers:      make(map[int64]*time.Timer),
	}
}

// SetConverger injects the service's verified Apply+readback path.
func (c *RestartCoordinator) SetConverger(fn func(context.Context, int64) error) {
	c.mu.Lock()
	c.converge = fn
	c.mu.Unlock()
}

// SetClock is primarily useful for deterministic timeout tests. Production
// uses time.Now unless an integration deliberately supplies another clock.
func (c *RestartCoordinator) SetClock(now func() time.Time) {
	if now == nil {
		return
	}
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

// Begin reserves a generation and marks the operation accepted. It does not
// call ZLM; the caller must invoke the restart command and then call
// AdvanceToWaitingOffline only after ZLM acknowledges that command.
func (c *RestartCoordinator) Begin(nodeID int64) (RestartOperation, error) {
	if c.registry == nil {
		return RestartOperation{}, ErrNodeNotFound
	}
	if _, ok := c.registry.Get(nodeID); !ok {
		return RestartOperation{}, ErrNodeNotFound
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if current := c.operations[nodeID]; current != nil && isRestartPending(current.Status) {
		return RestartOperation{}, ErrRestartPending
	}
	generation := c.generations[nodeID] + 1
	c.generations[nodeID] = generation
	now := c.now()
	op := &RestartOperation{
		OperationID: uuid.NewString(),
		NodeID:      nodeID,
		Generation:  generation,
		Status:      RestartStatusAccepted,
		AcceptedAt:  now,
		UpdatedAt:   now,
	}
	c.operations[nodeID] = op
	c.registry.SetAdmissionBlocked(nodeID, true)
	if old := c.timers[nodeID]; old != nil {
		old.Stop()
	}
	gen := generation
	c.timers[nodeID] = time.AfterFunc(c.timeout, func() {
		c.FailGeneration(nodeID, gen, errors.New("restart timeout"))
	})
	return cloneRestartOperation(*op), nil
}

// AdvanceToWaitingOffline transitions an accepted operation after the ZLM
// restart command has been acknowledged.
func (c *RestartCoordinator) AdvanceToWaitingOffline(nodeID int64, generation uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := c.operations[nodeID]
	if op == nil || op.Generation != generation || op.Status != RestartStatusAccepted {
		return false
	}
	c.setStatusLocked(op, RestartStatusWaitingOffline, "")
	return true
}

// OnNodeOffline is called only after Registry.MarkOffline succeeds.
func (c *RestartCoordinator) OnNodeOffline(nodeID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := c.operations[nodeID]
	if op == nil || !isRestartPending(op.Status) {
		return
	}
	if op.Status == RestartStatusAccepted || op.Status == RestartStatusWaitingOffline {
		c.setStatusLocked(op, RestartStatusWaitingHeartbeat, "")
	}
}

func (c *RestartCoordinator) MarkOffline(nodeID int64) { c.OnNodeOffline(nodeID) }

// OnNodeHeartbeat is called after an offline node is promoted by a received
// keepalive. Convergence runs asynchronously so the Hook response remains
// bounded and generation-guarded.
func (c *RestartCoordinator) OnNodeHeartbeat(nodeID int64) {
	c.mu.Lock()
	op := c.operations[nodeID]
	if op == nil || op.Status != RestartStatusWaitingHeartbeat {
		c.mu.Unlock()
		return
	}
	c.setStatusLocked(op, RestartStatusConverging, "")
	generation := op.Generation
	converge := c.converge
	c.mu.Unlock()

	if converge == nil {
		c.FailGeneration(nodeID, generation, errors.New("restart convergence unavailable"))
		return
	}
	go func() {
		c.runConvergence(nodeID, generation, converge)
	}()
}

func (c *RestartCoordinator) MarkHeartbeat(nodeID int64) { c.OnNodeHeartbeat(nodeID) }

// MarkOfflineForGeneration and MarkHeartbeatForGeneration are explicit
// generation-guarded hooks useful to tests and integrations that carry an
// operation generation alongside an event. The ordinary event methods above
// use the current generation and are safe for the normal one-operation path.
func (c *RestartCoordinator) MarkOfflineForGeneration(nodeID int64, generation uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := c.operations[nodeID]
	if op == nil || op.Generation != generation || !isRestartPending(op.Status) {
		return false
	}
	if op.Status == RestartStatusAccepted || op.Status == RestartStatusWaitingOffline {
		c.setStatusLocked(op, RestartStatusWaitingHeartbeat, "")
		return true
	}
	return false
}

func (c *RestartCoordinator) MarkHeartbeatForGeneration(nodeID int64, generation uint64) bool {
	c.mu.Lock()
	op := c.operations[nodeID]
	if op == nil || op.Generation != generation || op.Status != RestartStatusWaitingHeartbeat {
		c.mu.Unlock()
		return false
	}
	c.setStatusLocked(op, RestartStatusConverging, "")
	converge := c.converge
	c.mu.Unlock()
	if converge == nil {
		c.FailGeneration(nodeID, generation, errors.New("restart convergence unavailable"))
		return true
	}
	go func() {
		c.runConvergence(nodeID, generation, converge)
	}()
	return true
}

// runConvergence lets a restart join an equivalent convergence already
// claimed by the generic heartbeat scheduler. ErrConfigConvergenceInProgress
// is not itself a restart failure: wait for the existing owner to establish
// verified readiness, bounded by the operation timeout.
func (c *RestartCoordinator) runConvergence(nodeID int64, generation uint64, converge func(context.Context, int64) error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	if err := converge(ctx, nodeID); err != nil {
		if !errors.Is(err, ErrConfigConvergenceInProgress) || !c.waitForReady(ctx, nodeID) {
			c.FailGeneration(nodeID, generation, err)
			return
		}
	}
	c.CompleteGeneration(nodeID, generation)
}

func (c *RestartCoordinator) waitForReady(ctx context.Context, nodeID int64) bool {
	if c.registry == nil {
		return false
	}
	if c.registry.IsAutoOnDemandReady(nodeID) {
		return true
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if c.registry.IsAutoOnDemandReady(nodeID) {
				return true
			}
		}
	}
}

// CompleteGeneration moves a converged operation to ready only when the
// service's readback path has already marked the Registry ready.
func (c *RestartCoordinator) CompleteGeneration(nodeID int64, generation uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := c.operations[nodeID]
	if op == nil || op.Generation != generation || op.Status != RestartStatusConverging {
		return false
	}
	if c.registry == nil || !c.registry.IsAutoOnDemandReady(nodeID) {
		c.setStatusLocked(op, RestartStatusFailed, "restart convergence did not become ready")
		if c.registry != nil {
			c.registry.SetAdmissionBlocked(nodeID, true)
		}
		return false
	}
	c.setStatusLocked(op, RestartStatusReady, "")
	c.registry.SetAdmissionBlocked(nodeID, false)
	return true
}

// FailGeneration terminates only the operation that owns generation.
func (c *RestartCoordinator) FailGeneration(nodeID int64, generation uint64, err error) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := c.operations[nodeID]
	if op == nil || op.Generation != generation || !isRestartPending(op.Status) {
		return false
	}
	message := "restart failed"
	if err != nil {
		message = err.Error()
		if c.registry != nil {
			if n, ok := c.registry.Get(nodeID); ok {
				message = redactNodeError(err, n).Error()
			}
		}
	}
	c.setStatusLocked(op, RestartStatusFailed, message)
	if c.registry != nil {
		c.registry.SetAdmissionBlocked(nodeID, true)
		c.registry.SetAutoOnDemandReady(nodeID, false)
	}
	return true
}

func (c *RestartCoordinator) setStatusLocked(op *RestartOperation, status RestartStatus, message string) {
	op.Status = status
	op.Error = message
	op.UpdatedAt = c.now()
	if !isRestartPending(status) {
		if timer := c.timers[op.NodeID]; timer != nil {
			timer.Stop()
			delete(c.timers, op.NodeID)
		}
	}
}

func isRestartPending(status RestartStatus) bool {
	switch status {
	case RestartStatusAccepted, RestartStatusWaitingOffline, RestartStatusWaitingHeartbeat, RestartStatusConverging:
		return true
	default:
		return false
	}
}

func cloneRestartOperation(op RestartOperation) RestartOperation { return op }

// Get returns the current operation for a node. Terminal operations remain
// queryable until the next restart replaces them.
func (c *RestartCoordinator) Get(nodeID int64) (RestartOperation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	op, ok := c.operations[nodeID]
	if !ok {
		return RestartOperation{}, false
	}
	return cloneRestartOperation(*op), true
}

func (c *RestartCoordinator) GetByID(operationID string) (RestartOperation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, op := range c.operations {
		if op.OperationID == operationID {
			return cloneRestartOperation(*op), true
		}
	}
	return RestartOperation{}, false
}

func (c *RestartCoordinator) IsPending(nodeID int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := c.operations[nodeID]
	return op != nil && isRestartPending(op.Status)
}

func (c *RestartCoordinator) RestartPending(nodeID int64) bool { return c.IsPending(nodeID) }

func (c *RestartCoordinator) GetOperation(nodeID int64) (RestartOperation, bool) {
	return c.Get(nodeID)
}

// UnknownOperation is the explicit post-process-restart state. The in-memory
// coordinator intentionally has no persisted desired operation to turn into a
// false ready result.
func UnknownOperation(nodeID int64) RestartOperation {
	return RestartOperation{NodeID: nodeID, Status: RestartStatusUnknown}
}
