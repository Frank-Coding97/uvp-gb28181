package catalogprogress

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

type contextKey struct{}

func WithOperationID(ctx context.Context, operationID string) context.Context {
	return context.WithValue(ctx, contextKey{}, operationID)
}

func OperationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(contextKey{}).(string)
	return value
}

type Status string

const (
	StatusWaiting    Status = "waiting"
	StatusReceiving  Status = "receiving"
	StatusPersisting Status = "persisting"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusTimeout    Status = "timeout"
)

type Snapshot struct {
	OperationID   string     `json:"operationId"`
	DeviceID      string     `json:"deviceId"`
	Status        Status     `json:"status"`
	ReceivedCount int        `json:"receivedCount"`
	TotalCount    *int       `json:"totalCount"`
	ErrorMessage  string     `json:"errorMessage,omitempty"`
	StartedAt     time.Time  `json:"startedAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
}

type StartResult struct {
	Snapshot     Snapshot
	Deduplicated bool
}

type operation struct {
	snapshot Snapshot
	sn       int
	timer    *time.Timer
}

type Tracker struct {
	mu             sync.Mutex
	operations     map[string]*operation
	activeByDevice map[string]string
	timeout        time.Duration
	retention      time.Duration
}

func NewTracker(timeout, retention time.Duration) *Tracker {
	return &Tracker{
		operations:     make(map[string]*operation),
		activeByDevice: make(map[string]string),
		timeout:        timeout,
		retention:      retention,
	}
}

var Default = NewTracker(2*time.Minute, 30*time.Second)

func (t *Tracker) Start(deviceID string) StartResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	if operationID := t.activeByDevice[deviceID]; operationID != "" {
		if current := t.operations[operationID]; current != nil {
			return StartResult{Snapshot: current.snapshot, Deduplicated: true}
		}
		delete(t.activeByDevice, deviceID)
	}
	now := time.Now()
	item := &operation{snapshot: Snapshot{
		OperationID: uuid.NewString(),
		DeviceID:    deviceID,
		Status:      StatusWaiting,
		StartedAt:   now,
		UpdatedAt:   now,
	}}
	t.operations[item.snapshot.OperationID] = item
	t.activeByDevice[deviceID] = item.snapshot.OperationID
	if t.timeout > 0 {
		operationID := item.snapshot.OperationID
		item.timer = time.AfterFunc(t.timeout, func() { t.timeoutOperation(operationID) })
	}
	return StartResult{Snapshot: item.snapshot}
}

func (t *Tracker) Bind(operationID string, sn int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if current := t.operations[operationID]; current != nil && current.snapshot.Status == StatusWaiting {
		current.sn = sn
		current.snapshot.UpdatedAt = time.Now()
	}
}

func (t *Tracker) Fail(operationID string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	current := t.operations[operationID]
	if current == nil || isTerminal(current.snapshot.Status) {
		return
	}
	now := time.Now()
	current.snapshot.Status = StatusFailed
	current.snapshot.ErrorMessage = errString(err)
	current.snapshot.UpdatedAt = now
	current.snapshot.CompletedAt = &now
	if current.timer != nil {
		current.timer.Stop()
	}
	delete(t.activeByDevice, current.snapshot.DeviceID)
	t.scheduleRemovalLocked(operationID)
}

func (t *Tracker) Observe(deviceID string, sn, received, total int, complete bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	current := t.activeOperationLocked(deviceID)
	if current == nil || (current.sn != 0 && current.sn != sn) {
		return
	}
	if current.sn == 0 {
		current.sn = sn
	}
	current.snapshot.ReceivedCount = max(received, 0)
	if total >= 0 {
		value := total
		current.snapshot.TotalCount = &value
	}
	current.snapshot.UpdatedAt = time.Now()
	if complete {
		current.snapshot.Status = StatusPersisting
	} else {
		current.snapshot.Status = StatusReceiving
	}
}

func (t *Tracker) Finish(deviceID string, sn int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	current := t.activeOperationLocked(deviceID)
	if current == nil || (current.sn != 0 && current.sn != sn) {
		return
	}
	now := time.Now()
	current.snapshot.UpdatedAt = now
	current.snapshot.CompletedAt = &now
	if err != nil {
		current.snapshot.Status = StatusFailed
		current.snapshot.ErrorMessage = err.Error()
	} else {
		current.snapshot.Status = StatusCompleted
	}
	if current.timer != nil {
		current.timer.Stop()
	}
	delete(t.activeByDevice, deviceID)
	t.scheduleRemovalLocked(current.snapshot.OperationID)
}

func (t *Tracker) Get(operationID string) (Snapshot, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	current := t.operations[operationID]
	if current == nil {
		return Snapshot{}, false
	}
	return current.snapshot, true
}

func (t *Tracker) activeOperationLocked(deviceID string) *operation {
	operationID := t.activeByDevice[deviceID]
	if operationID == "" {
		return nil
	}
	return t.operations[operationID]
}

func (t *Tracker) timeoutOperation(operationID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	current := t.operations[operationID]
	if current == nil || current.snapshot.Status == StatusCompleted || current.snapshot.Status == StatusFailed || current.snapshot.Status == StatusTimeout {
		return
	}
	now := time.Now()
	current.snapshot.Status = StatusTimeout
	current.snapshot.UpdatedAt = now
	current.snapshot.CompletedAt = &now
	delete(t.activeByDevice, current.snapshot.DeviceID)
	t.scheduleRemovalLocked(operationID)
}

func (t *Tracker) scheduleRemovalLocked(operationID string) {
	if t.retention <= 0 {
		delete(t.operations, operationID)
		return
	}
	time.AfterFunc(t.retention, func() {
		t.mu.Lock()
		delete(t.operations, operationID)
		t.mu.Unlock()
	})
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func isTerminal(status Status) bool {
	return status == StatusCompleted || status == StatusFailed || status == StatusTimeout
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
