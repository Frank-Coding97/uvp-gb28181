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
	// ErrRevisionConflict means that a caller attempted to commit a stale
	// snapshot. The caller must reload before retrying; no in-memory state is
	// changed when this is returned.
	ErrRevisionConflict = errors.New("node revision conflict")
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

// RevisionedRepo adds the persistent compare-and-swap needed to linearize
// full-row node updates across service, heartbeat, and other processes. The
// legacy Repo methods remain in the base interface so older test doubles and
// integrations stay source-compatible; Registry uses this method whenever the
// concrete repository supports it.
type RevisionedRepo interface {
	UpdateCAS(ctx context.Context, n Node, expectedRevision uint64) (bool, error)
}

// CASRepo is a descriptive compatibility alias for RevisionedRepo.
type CASRepo = RevisionedRepo

// Registry 节点注册表(内存缓存 + 持久化)
//
// 线程安全:所有公开方法可并发调用。
// 内存表 + DB 双写:Add/Update/Delete/MarkOffline 都同步写 DB;
// UpdateStats 不写 DB(高频心跳数据只在内存)。
type Registry struct {
	mu                sync.RWMutex
	nodeLocksMu       sync.Mutex
	nodeLocks         map[int64]*sync.Mutex // serializes persistence per node
	nodes             map[int64]*Node       // ID -> Node
	uuids             map[string]int64      // mediaServerUUID -> ID(Hook 反查)
	autoOnDemandReady map[int64]bool        // 当前进程已写入并回读确认缺流 Hook
	admissionBlocked  map[int64]bool        // 显式运维/恢复 gate,不改变普通 active 语义
	repo              Repo
}

func cloneTags(tags map[string]string) map[string]string {
	if tags == nil {
		return nil
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		out[key] = value
	}
	return out
}

func cloneNode(n Node) Node {
	n.Tags = cloneTags(n.Tags)
	return n
}

func (r *Registry) nodeMutationLock(id int64) *sync.Mutex {
	r.nodeLocksMu.Lock()
	defer r.nodeLocksMu.Unlock()
	if lock, ok := r.nodeLocks[id]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	r.nodeLocks[id] = lock
	return lock
}

func nextRevision(revision uint64) uint64 {
	if revision == ^uint64(0) {
		return revision
	}
	return revision + 1
}

// persistCAS commits a full row using the repository's durable CAS when
// available. Legacy repositories are still serialized by the per-node lock;
// their Update method receives the incremented revision so their rows remain
// monotonic in-process.
func (r *Registry) persistCAS(ctx context.Context, n Node, expectedRevision uint64) (bool, error) {
	n.Revision = nextRevision(expectedRevision)
	if versioned, ok := r.repo.(RevisionedRepo); ok {
		return versioned.UpdateCAS(ctx, n, expectedRevision)
	}
	return true, r.repo.Update(ctx, n)
}

// NewRegistry 构造,不会自动 LoadAll(由调用方控制时机)
func NewRegistry(repo Repo) *Registry {
	return &Registry{
		nodes:             make(map[int64]*Node),
		nodeLocks:         make(map[int64]*sync.Mutex),
		uuids:             make(map[string]int64),
		autoOnDemandReady: make(map[int64]bool),
		admissionBlocked:  make(map[int64]bool),
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
	nextNodes := make(map[int64]*Node, len(rows))
	nextUUIDs := make(map[string]int64, len(rows))
	nextReady := make(map[int64]bool, len(rows))
	nextBlocked := make(map[int64]bool, len(rows))
	for i := range rows {
		n := cloneNode(rows[i])
		current, exists := r.nodes[n.ID]
		staleReload := exists && current.Revision > n.Revision
		if staleReload {
			// List may have started before a local CAS commit completed. Do not
			// let that older read roll the process-local registry backwards.
			n = cloneNode(*current)
		}
		nextNodes[n.ID] = &n
		if n.MediaServerUUID != "" {
			nextUUIDs[n.MediaServerUUID] = n.ID
		}
		if staleReload {
			nextReady[n.ID] = r.autoOnDemandReady[n.ID]
			nextBlocked[n.ID] = r.admissionBlocked[n.ID]
		} else {
			nextReady[n.ID] = false
			nextBlocked[n.ID] = n.RecoveryRequired
		}
	}
	r.nodes = nextNodes
	r.uuids = nextUUIDs
	r.autoOnDemandReady = nextReady
	r.admissionBlocked = nextBlocked
	return nil
}

// Add 持久化 + 写内存
func (r *Registry) Add(ctx context.Context, n Node) (*Node, error) {
	n = cloneNode(n)
	if n.Revision == 0 {
		n.Revision = 1
	}
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
	r.admissionBlocked[id] = n.RecoveryRequired
	if n.MediaServerUUID != "" {
		r.uuids[n.MediaServerUUID] = id
	}
	return &stored, nil
}

// Update 写 DB + 同步内存(保留 Stats,因为 Stats 只在内存)
func (r *Registry) Update(ctx context.Context, n Node) error {
	lock := r.nodeMutationLock(n.ID)
	lock.Lock()
	defer lock.Unlock()

	r.mu.RLock()
	cur, ok := r.nodes[n.ID]
	if !ok {
		r.mu.RUnlock()
		return ErrNotFound
	}
	current := cloneNode(*cur)
	r.mu.RUnlock()

	// A zero revision is accepted only for legacy callers that did not carry a
	// version; it is upgraded to the current in-memory version before commit.
	// Repositories with durable CAS reject explicit stale revisions before
	// touching the DB. Legacy repositories have no cross-process revision
	// contract, so retain their historical snapshot-update compatibility while
	// the per-node lock still serializes this process's writes.
	if n.Revision != 0 && n.Revision != current.Revision {
		if _, versioned := r.repo.(RevisionedRepo); !versioned {
			n.Revision = current.Revision
		} else {
			return ErrRevisionConflict
		}
	}
	expectedRevision := current.Revision
	n = cloneNode(n)
	n.Revision = expectedRevision
	n.Stats = current.Stats
	n.UpdatedAt = time.Now()
	committed, err := r.persistCAS(ctx, n, expectedRevision)
	if err != nil {
		return err
	}
	if !committed {
		return ErrRevisionConflict
	}
	n.Revision = nextRevision(expectedRevision)

	r.mu.Lock()
	defer r.mu.Unlock()
	latest, ok := r.nodes[n.ID]
	if !ok {
		return ErrNotFound
	}
	if latest.Revision != expectedRevision {
		return ErrRevisionConflict
	}
	// 移除旧 UUID 索引
	if latest.MediaServerUUID != "" && latest.MediaServerUUID != n.MediaServerUUID {
		delete(r.uuids, latest.MediaServerUUID)
	}
	next := cloneNode(n)
	r.nodes[n.ID] = &next
	if n.MediaServerUUID != "" {
		r.uuids[n.MediaServerUUID] = n.ID
	}
	r.autoOnDemandReady[n.ID] = false
	if n.RecoveryRequired {
		r.admissionBlocked[n.ID] = true
	} else if latest.RecoveryRequired {
		// Clearing persisted recovery is an explicit successful reconciliation;
		// this is the one ordinary update path allowed to reopen admission.
		r.admissionBlocked[n.ID] = false
	} else if _, exists := r.admissionBlocked[n.ID]; !exists {
		r.admissionBlocked[n.ID] = false
	}
	return nil
}

// Delete 删 DB + 删内存
func (r *Registry) Delete(ctx context.Context, id int64) error {
	lock := r.nodeMutationLock(id)
	lock.Lock()
	defer lock.Unlock()
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
		delete(r.admissionBlocked, id)
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

// SetAutoOnDemandReadyIfRevision publishes readiness only for the snapshot
// that was just reconciled. It serializes with state persistence so an
// offline/maintenance transition cannot be followed by a stale ready=true.
func (r *Registry) SetAutoOnDemandReadyIfRevision(id int64, revision uint64, ready bool) bool {
	lock := r.nodeMutationLock(id)
	lock.Lock()
	defer lock.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.nodes[id]
	if !ok || current.Revision != revision {
		return false
	}
	if ready && (!current.IsActive() || current.RecoveryRequired) {
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

// SetAdmissionBlocked toggles an explicit admission gate. It is intentionally
// separate from AutoOnDemandReady: existing scheduling keeps its historical
// active/capacity semantics, while restart and uncertain rollback can opt a
// node out without pretending that a failed convergence succeeded.
func (r *Registry) SetAdmissionBlocked(id int64, blocked bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.nodes[id]; !ok {
		return false
	}
	r.admissionBlocked[id] = blocked
	return true
}

// MarkRecoveryRequired refreshes the persisted row before quarantining it.
// It is used after a failed CAS rollback: the local snapshot may be stale
// because another process committed a newer endpoint/config, so this method
// must never write the caller's old fields back to the database.
func (r *Registry) MarkRecoveryRequired(ctx context.Context, id int64, reason, fingerprint string) error {
	lock := r.nodeMutationLock(id)
	lock.Lock()
	defer lock.Unlock()

	for attempt := 0; attempt < 8; attempt++ {
		persisted, err := r.repo.Get(ctx, id)
		if err != nil {
			return err
		}
		if persisted == nil {
			return ErrNotFound
		}
		persistedValue := cloneNode(*persisted)
		persisted = &persistedValue
		expectedRevision := persisted.Revision
		persisted.RecoveryRequired = true
		persisted.RecoveryReason = reason
		persisted.RecoveryFingerprint = fingerprint

		r.mu.RLock()
		current, currentOK := r.nodes[id]
		var currentSnapshot Node
		if currentOK {
			currentSnapshot = cloneNode(*current)
		}
		r.mu.RUnlock()
		if currentOK && currentSnapshot.Revision == expectedRevision {
			// Stats are process-local and are not part of the durable row's
			// scheduling decision; preserve them when the revision is current.
			persisted.Stats = currentSnapshot.Stats
		}

		committed, err := r.persistCAS(ctx, *persisted, expectedRevision)
		if err != nil {
			return err
		}
		if !committed {
			continue
		}
		persisted.Revision = nextRevision(expectedRevision)

		r.mu.Lock()
		latest, latestOK := r.nodes[id]
		if !latestOK {
			r.mu.Unlock()
			return ErrNotFound
		}
		if latest.Revision > expectedRevision {
			// LoadAll or another local commit raced the refresh. Retry from
			// the now-latest durable row rather than replacing it in memory.
			r.mu.Unlock()
			continue
		}
		if latest.Revision == expectedRevision {
			persisted.Stats = latest.Stats
		}
		next := cloneNode(*persisted)
		r.nodes[id] = &next
		r.autoOnDemandReady[id] = false
		r.admissionBlocked[id] = true
		r.mu.Unlock()
		return nil
	}
	return ErrRevisionConflict
}

func (r *Registry) IsAdmissionBlocked(id int64) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.admissionBlocked[id]
}

// Get 按 ID 取节点(包含最新 Stats,内存优先)
func (r *Registry) Get(id int64) (*Node, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if n, ok := r.nodes[id]; ok {
		copy := cloneNode(*n)
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
	copy := cloneNode(*n)
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
	if !ok || !r.autoOnDemandReady[id] || r.admissionBlocked[id] {
		return nil, false
	}
	n, ok := r.nodes[id]
	if !ok || !n.IsActive() || n.IsNearCapacity() {
		return nil, false
	}
	copy := cloneNode(*n)
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
		copy := cloneNode(*n)
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
			copy := cloneNode(*n)
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
		if n.IsActive() && !n.IsNearCapacity() && !r.admissionBlocked[n.ID] {
			copy := cloneNode(*n)
			out = append(out, &copy)
		}
	}
	return out
}

// UpdateStats 心跳上报数据更新(不写 DB)
// uuid 未知则静默忽略(节点可能刚删除)
func (r *Registry) UpdateStats(uuid string, stats Stats) {
	r.mu.RLock()
	id, ok := r.uuids[uuid]
	r.mu.RUnlock()
	if !ok {
		return
	}
	lock := r.nodeMutationLock(id)
	lock.Lock()
	defer lock.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.RLock()
	id, ok := r.uuids[uuid]
	r.mu.RUnlock()
	if !ok {
		return
	}
	lock := r.nodeMutationLock(id)
	lock.Lock()
	defer lock.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.RLock()
	id, ok := r.uuids[uuid]
	r.mu.RUnlock()
	if !ok {
		return
	}
	lock := r.nodeMutationLock(id)
	lock.Lock()
	defer lock.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.nodes[id]
	if !ok {
		return
	}
	n.Stats.NetThreadLoadAvg = netLoad
	n.Stats.WorkThreadLoadAvg = workLoad
}

// MarkOffline 标记节点离线(由 Watcher 调用,写 DB + 内存)
func (r *Registry) MarkOffline(ctx context.Context, id int64) error {
	lock := r.nodeMutationLock(id)
	lock.Lock()
	defer lock.Unlock()

	r.mu.RLock()
	cur, ok := r.nodes[id]
	if !ok {
		r.mu.RUnlock()
		return ErrNotFound
	}
	expectedRevision := cur.Revision
	snapshot := cloneNode(*cur)
	r.mu.RUnlock()
	// The full snapshot is committed with a durable CAS. No Registry global
	// lock is held while the repository (which may block) is called.
	snapshot.State = StateOffline
	snapshot.UpdatedAt = time.Now()

	// DB 写在全局锁外并带有界超时:DB 阻塞不得拖垮所有 Get/List/心跳更新
	updateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	committed, err := r.persistCAS(updateCtx, snapshot, expectedRevision)
	if err != nil {
		return err
	}
	if !committed {
		return ErrRevisionConflict
	}
	snapshot.Revision = nextRevision(expectedRevision)

	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.nodes[id]; ok && current.Revision == expectedRevision {
		current.State = StateOffline
		current.Revision = snapshot.Revision
		current.UpdatedAt = snapshot.UpdatedAt
		r.autoOnDemandReady[id] = false
		return nil
	}
	// LoadAll is the only local operation outside the per-node lock. If it
	// replaced this row while the DB CAS was in flight, do not claim success
	// with a stale in-memory snapshot.
	return ErrRevisionConflict
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
	lock := r.nodeMutationLock(id)
	lock.Lock()
	defer lock.Unlock()

	r.mu.RLock()
	cur, ok := r.nodes[id]
	if !ok {
		r.mu.RUnlock()
		return ErrNotFound
	}
	expectedRevision := cur.Revision
	snapshot := cloneNode(*cur)
	r.mu.RUnlock()
	now := time.Now()
	snapshot.State = StateActive
	snapshot.Stats.LastHeartbeatAt = now
	snapshot.UpdatedAt = now

	updateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	committed, err := r.persistCAS(updateCtx, snapshot, expectedRevision)
	if err != nil {
		return err
	}
	if !committed {
		return ErrRevisionConflict
	}
	snapshot.Revision = nextRevision(expectedRevision)

	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.nodes[id]; ok && current.Revision == expectedRevision {
		current.State = StateActive
		current.Revision = snapshot.Revision
		current.Stats.LastHeartbeatAt = now
		current.UpdatedAt = now
		return nil
	}
	return ErrRevisionConflict
}
