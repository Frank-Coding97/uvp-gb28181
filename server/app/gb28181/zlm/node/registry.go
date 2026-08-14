package node

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	// ErrNotFound 节点不存在
	ErrNotFound = errors.New("node not found")
)

// Repo 持久化抽象
// 实现:repo.MetaNodeRepo(M1 gorm 版)
type Repo interface {
	List(ctx context.Context) ([]Node, error)
	Get(ctx context.Context, id int64) (*Node, error)
	Create(ctx context.Context, n Node) (int64, error)
	Update(ctx context.Context, n Node) error
	Delete(ctx context.Context, id int64) error
}

// Registry 节点注册表(内存缓存 + 持久化)
//
// 线程安全:所有公开方法可并发调用。
// 内存表 + DB 双写:Add/Update/Delete/MarkOffline 都同步写 DB;
// UpdateStats 不写 DB(高频心跳数据只在内存)。
type Registry struct {
	mu                sync.RWMutex
	nodes             map[int64]*Node  // ID -> Node
	uuids             map[string]int64 // mediaServerUUID -> ID(Hook 反查)
	autoOnDemandReady map[int64]bool   // 当前进程已写入并回读确认缺流 Hook
	repo              Repo
}

// NewRegistry 构造,不会自动 LoadAll(由调用方控制时机)
func NewRegistry(repo Repo) *Registry {
	return &Registry{
		nodes:             make(map[int64]*Node),
		uuids:             make(map[string]int64),
		autoOnDemandReady: make(map[int64]bool),
		repo:              repo,
	}
}

// LoadAll 从 repo 加载所有节点到内存
func (r *Registry) LoadAll(ctx context.Context) error {
	rows, err := r.repo.List(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes = make(map[int64]*Node, len(rows))
	r.uuids = make(map[string]int64, len(rows))
	r.autoOnDemandReady = make(map[int64]bool, len(rows))
	for i := range rows {
		n := rows[i]
		r.nodes[n.ID] = &n
		if n.MediaServerUUID != "" {
			r.uuids[n.MediaServerUUID] = n.ID
		}
		r.autoOnDemandReady[n.ID] = false
	}
	return nil
}

// Add 持久化 + 写内存
func (r *Registry) Add(ctx context.Context, n Node) (*Node, error) {
	now := time.Now()
	if n.CreatedAt.IsZero() {
		n.CreatedAt = now
	}
	n.UpdatedAt = now

	id, err := r.repo.Create(ctx, n)
	if err != nil {
		return nil, err
	}
	n.ID = id

	r.mu.Lock()
	defer r.mu.Unlock()
	stored := n
	r.nodes[id] = &stored
	r.autoOnDemandReady[id] = false
	if n.MediaServerUUID != "" {
		r.uuids[n.MediaServerUUID] = id
	}
	return &stored, nil
}

// Update 写 DB + 同步内存(保留 Stats,因为 Stats 只在内存)
func (r *Registry) Update(ctx context.Context, n Node) error {
	n.UpdatedAt = time.Now()
	if err := r.repo.Update(ctx, n); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.nodes[n.ID]
	if ok {
		stats := cur.Stats
		// 移除旧 UUID 索引
		if cur.MediaServerUUID != "" && cur.MediaServerUUID != n.MediaServerUUID {
			delete(r.uuids, cur.MediaServerUUID)
		}
		next := n
		next.Stats = stats
		r.nodes[n.ID] = &next
	} else {
		next := n
		r.nodes[n.ID] = &next
	}
	if n.MediaServerUUID != "" {
		r.uuids[n.MediaServerUUID] = n.ID
	}
	r.autoOnDemandReady[n.ID] = false
	return nil
}

// Delete 删 DB + 删内存
func (r *Registry) Delete(ctx context.Context, id int64) error {
	if err := r.repo.Delete(ctx, id); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur, ok := r.nodes[id]; ok {
		if cur.MediaServerUUID != "" {
			delete(r.uuids, cur.MediaServerUUID)
		}
		delete(r.nodes, id)
		delete(r.autoOnDemandReady, id)
	}
	return nil
}

// SetAutoOnDemandReady records transient node readiness. It is deliberately
// not persisted because every process start must verify the effective ZLM
// configuration again.
func (r *Registry) SetAutoOnDemandReady(id int64, ready bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.nodes[id]; !ok {
		return false
	}
	r.autoOnDemandReady[id] = ready
	return true
}

func (r *Registry) IsAutoOnDemandReady(id int64) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.autoOnDemandReady[id]
}

// Get 按 ID 取节点(包含最新 Stats,内存优先)
func (r *Registry) Get(id int64) (*Node, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if n, ok := r.nodes[id]; ok {
		copy := *n
		return &copy, true
	}
	return nil, false
}

// GetByUUID 按 ZLM mediaServerId 反查
func (r *Registry) GetByUUID(uuid string) (*Node, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.uuids[uuid]
	if !ok {
		return nil, false
	}
	n, ok := r.nodes[id]
	if !ok {
		return nil, false
	}
	copy := *n
	return &copy, true
}

// ResolveAutoOnDemandNode atomically resolves a callback UUID and admits only
// nodes whose managed hook configuration is ready and which can accept a new
// live stream. The returned value is a snapshot; callers never receive the
// registry's mutable node pointer.
func (r *Registry) ResolveAutoOnDemandNode(uuid string) (*Node, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.uuids[uuid]
	if !ok || !r.autoOnDemandReady[id] {
		return nil, false
	}
	n, ok := r.nodes[id]
	if !ok || !n.IsActive() || n.IsNearCapacity() {
		return nil, false
	}
	copy := *n
	return &copy, true
}

// IDForUUID 按 ZLM mediaServerId 只反查 nodeID(轻量,不复制 Node)
//
// hook 端点用:OnStreamChanged 收到 payload.mediaServerId 后,反查 nodeID 给 LocationMap.Bind。
func (r *Registry) IDForUUID(uuid string) (int64, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.uuids[uuid]
	return id, ok
}

// List 返回全部节点拷贝
func (r *Registry) List() []*Node {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Node, 0, len(r.nodes))
	for _, n := range r.nodes {
		copy := *n
		out = append(out, &copy)
	}
	return out
}

// ListActive 仅 active 节点(给 scheduler)
func (r *Registry) ListActive() []*Node {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Node, 0, len(r.nodes))
	for _, n := range r.nodes {
		if n.IsActive() {
			copy := *n
			out = append(out, &copy)
		}
	}
	return out
}

// ListSchedulable 仅可调度节点(active + 非 NearCapacity)
//
// 比 ListActive 多一层容量过滤:port_usage >= 80% 或 cpu >= 80% 的节点
// 暂时摘除调度池,等流自然结束或心跳下降。
// scheduler.Pick 用此方法替代 ListActive,实现容量预警自动剔除。
func (r *Registry) ListSchedulable() []*Node {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Node, 0, len(r.nodes))
	for _, n := range r.nodes {
		if n.IsActive() && !n.IsNearCapacity() {
			copy := *n
			out = append(out, &copy)
		}
	}
	return out
}

// UpdateStats 心跳上报数据更新(不写 DB)
// uuid 未知则静默忽略(节点可能刚删除)
func (r *Registry) UpdateStats(uuid string, stats Stats) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.uuids[uuid]
	if !ok {
		return
	}
	n, ok := r.nodes[id]
	if !ok {
		return
	}
	n.Stats = stats
	// 心跳到达 = 节点 alive,如果之前是 offline 自动恢复 active
	if n.State == StateOffline {
		n.State = StateActive
	}
}

// UpdateHeartbeatFields 在锁内合并心跳字段:与 UpdateLoadFields 并发时
// 各自只改自己的字段,不会出现"读整体→改部分→覆盖整体"的丢失更新
func (r *Registry) UpdateHeartbeatFields(uuid string, mediaSourceCount, sessionCount int, heartbeatAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.uuids[uuid]
	if !ok {
		return
	}
	n, ok := r.nodes[id]
	if !ok {
		return
	}
	n.Stats.LastHeartbeatAt = heartbeatAt
	n.Stats.MediaSourceCount = mediaSourceCount
	n.Stats.SessionCount = sessionCount
	// 心跳到达 = 节点 alive,如果之前是 offline 自动恢复 active
	if n.State == StateOffline {
		n.State = StateActive
	}
}

// UpdateLoadFields 在锁内合并线程负载字段(见 UpdateHeartbeatFields)
func (r *Registry) UpdateLoadFields(uuid string, netLoad, workLoad float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.uuids[uuid]
	if !ok {
		return
	}
	n, ok := r.nodes[id]
	if !ok {
		return
	}
	n.Stats.NetThreadLoadAvg = netLoad
	n.Stats.WorkThreadLoadAvg = workLoad
}

// MarkOffline 标记节点离线(由 Watcher 调用,写 DB + 内存)
func (r *Registry) MarkOffline(ctx context.Context, id int64) error {
	r.mu.Lock()
	cur, ok := r.nodes[id]
	if !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	// 先写 DB、成功后再提交内存:DB 失败时运行态与数据库分叉,
	// Watcher 不会重试已从 ListActive 消失的节点,重启后状态还会反转
	snapshot := *cur
	snapshot.State = StateOffline
	snapshot.UpdatedAt = time.Now()
	if err := r.repo.Update(ctx, snapshot); err != nil {
		r.mu.Unlock()
		return err
	}
	cur.State = StateOffline
	r.autoOnDemandReady[id] = false
	cur.UpdatedAt = snapshot.UpdatedAt
	r.mu.Unlock()
	return nil
}

// MarkActive 标记节点活跃(由启动探活 probe 调用,写 DB + 内存)
//
// 用于两类场景:
//   - 进程启动时主动探活成功 → 从 DB 里读到的 offline 翻回 active,填 LastHeartbeatAt
//     让 Watcher 90 秒窗口从此刻起算,而不是从进程启动起算
//   - 运维手动拉回节点(maintenance → active 走 Update,不走本方法)
//
// 幂等:已经是 active 的节点再调只刷 LastHeartbeatAt + UpdatedAt,不 panic。
// maintenance 节点由调用方判断跳过(本方法不做状态守卫)。
func (r *Registry) MarkActive(ctx context.Context, id int64) error {
	r.mu.Lock()
	cur, ok := r.nodes[id]
	if !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	now := time.Now()
	// 先写 DB、成功后再提交内存(同 MarkOffline,防状态分叉)
	snapshot := *cur
	snapshot.State = StateActive
	snapshot.Stats.LastHeartbeatAt = now
	snapshot.UpdatedAt = now
	if err := r.repo.Update(ctx, snapshot); err != nil {
		r.mu.Unlock()
		return err
	}
	cur.State = StateActive
	cur.Stats.LastHeartbeatAt = now
	cur.UpdatedAt = now
	r.mu.Unlock()
	return nil
}
