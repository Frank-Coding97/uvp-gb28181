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
	// prune 表示这次刷新是**用户手动触发**的，应答收齐后允许按快照差集
	// 剪掉对端已不再提供的通道（见 catalog.Pipeline.PruneChannels）。
	prune bool
	timer *time.Timer
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

// RequestPrune 标记该设备当前活跃的刷新任务：应答收齐后需要剪枝。
//
// 只有「手动刷新目录」会调它（controllers.RefreshDeviceCatalog）。注册成功后
// 自动触发的 Catalog 查询不标记 ⇒ 自动路径的行为完全不变。
// ⛔ 必须在 Trigger 之前调用：Trigger 可能同步失败并 Finish，事后标记就没有意义了。
func (t *Tracker) RequestPrune(deviceID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if current := t.activeOperationLocked(deviceID); current != nil {
		current.prune = true
	}
}

// NeedPrune 报告本次应答（按 deviceID + sn 匹配）是否要求剪枝。
//
// 与 Finish 同款匹配：操作已不存在、或 sn 对不上（应答属于另一次查询）时一律
// 返回 false —— 剪枝是删除动作，宁可漏一次也不要错剪一批。
func (t *Tracker) NeedPrune(deviceID string, sn int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	current := t.activeOperationLocked(deviceID)
	if current == nil || (current.sn != 0 && current.sn != sn) {
		return false
	}
	return current.prune
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
