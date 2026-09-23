package node

import (
	"context"
	"errors"
	"sync"
	"time"
)

// 退休节点的「解约」状态。
//
// 语义与设计动机见迁移 `2026-09-21-retired-media-node-credentials.sql`：
// `Registry.Delete` 是硬删且**不通知对端**，被删掉的 ZLM 会继续按自己的周期回调，
// 平台只能把它当陌生人（`node_unknown`）。要能撤销，就必须在删行前把
// `uuid + host + api_port + api_secret` 留在这张独立表里。
const (
	// RetireUnprovisionPending 尚未确认撤销成功（含"还没试过"）。
	RetireUnprovisionPending = "pending"
	// RetireUnprovisionDone 已回读确认对端不再回调我们。
	RetireUnprovisionDone = "done"
	// RetireUnprovisionUnreachable 试过了但撤不掉（不可达 / 凭据已改 / 对端拒改）。
	// ⛔ 这是一个**显式的失配记录**，不是"暂时没好"：它需要人去处理，
	// 绝不可以在代码里被当成成功（否则残骸会被静默遗忘）。
	RetireUnprovisionUnreachable = "unreachable"
)

// RetiredCredential 是「撤销对端 hook」所需的最小集合。
//
// ⚠️ `APISecret` 是明文，与 `meta_node.api_secret` 同等级（同库、同权限）。
type RetiredCredential struct {
	MediaServerUUID     string
	Name                string
	Host                string
	APIPort             int
	APISecret           string
	RetireReason        string
	UnprovisionState    string
	UnprovisionAttempts int
	LastAttemptAt       *time.Time
	RetiredAt           time.Time
}

// Resolved 报告这条凭据是否已经"结清"——结清之后不需要再重试解约。
func (c RetiredCredential) Resolved() bool {
	return c.UnprovisionState == RetireUnprovisionDone
}

// Node 把凭据还原成一个够用的 Node 视图，供 `zlm.NewClientForNode` 发解约请求。
//
// 只填**定位与鉴权**所需的最少字段：host / api_port / api_secret 定出"找谁、拿什么凭据"，
// media_server_uuid 用来判定对端哪几条 hook 是我们写的。
// ⛔ 这不是"把节点复活"：ID 保持 0，没有任何状态字段，它进不了注册表也进不了调度池。
func (c RetiredCredential) Node() *Node {
	return &Node{
		Name:            c.Name,
		Host:            c.Host,
		APIPort:         c.APIPort,
		APISecret:       c.APISecret,
		MediaServerUUID: c.MediaServerUUID,
	}
}

// RetirementRepo 退休凭据的持久化抽象。
// 实现见 `app/gb28181/zlm/repo.MetaNodeRetiredRepo`。
type RetirementRepo interface {
	ListRetired(ctx context.Context) ([]RetiredCredential, error)
	SaveRetired(ctx context.Context, c RetiredCredential) error
	UpdateRetiredUnprovision(ctx context.Context, uuid, state string, attempts int, at time.Time) error
}

// RetirementIndex 是 `meta_node_retired` 的进程内索引：uuid → 凭据。
//
// 存在的理由是 hook 热路径——每条被拒的 hook 都要问一次"这个 uuid 是不是我们
// 主动退休的节点"。这问题必须在**内存里**回答（hook 是 HTTP 回调，不能每条都打 DB），
// 所以启动时全量装载，退休时同步写入。
//
// 与 Registry 的分工：Registry 只认识**活着的**节点；本索引只认识**已退休的**节点。
// 两者不重叠——`Registry.Delete` 删掉行的同一时刻，凭据进本索引。
type RetirementIndex struct {
	mu     sync.RWMutex
	byUUID map[string]RetiredCredential
	repo   RetirementRepo
}

// NewRetirementIndex 构造。repo 允许为 nil（未装配持久化时索引仍可工作，
// 只是重启后失去记忆——测试与降级场景）。
func NewRetirementIndex(repo RetirementRepo) *RetirementIndex {
	return &RetirementIndex{byUUID: make(map[string]RetiredCredential), repo: repo}
}

// LoadAll 从持久化装载全量凭据（进程启动时一次性调用）。
func (i *RetirementIndex) LoadAll(ctx context.Context) error {
	if i == nil || i.repo == nil {
		return nil
	}
	rows, err := i.repo.ListRetired(ctx)
	if err != nil {
		return err
	}
	next := make(map[string]RetiredCredential, len(rows))
	for _, row := range rows {
		if row.MediaServerUUID == "" {
			continue
		}
		next[row.MediaServerUUID] = row
	}
	i.mu.Lock()
	i.byUUID = next
	i.mu.Unlock()
	return nil
}

// Lookup 按 uuid 取退休凭据。
func (i *RetirementIndex) Lookup(uuid string) (RetiredCredential, bool) {
	if i == nil || uuid == "" {
		return RetiredCredential{}, false
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	row, ok := i.byUUID[uuid]
	return row, ok
}

// Remember 记下一条退休凭据（持久化 + 索引）。
//
// ⛔ 顺序是"先持久化、后写索引"，但**持久化失败也照样写索引**：此刻 `meta_node`
// 的那一行已经（或即将）被物理删除，DB 写失败若同时让索引也空着，这个进程就
// 完全失去了"这是谁"的知识，只能回到节点被当陌生人的老问题。索引多一条记录
// 是无害的，少一条才是不可恢复的。调用方据返回的 error 决定是否记审计。
func (i *RetirementIndex) Remember(ctx context.Context, cred RetiredCredential) error {
	if i == nil {
		return errors.New("retirement index unavailable")
	}
	if cred.MediaServerUUID == "" {
		return errors.New("retired credential uuid required")
	}
	if cred.UnprovisionState == "" {
		cred.UnprovisionState = RetireUnprovisionPending
	}
	if cred.RetiredAt.IsZero() {
		cred.RetiredAt = time.Now()
	}
	var persistErr error
	if i.repo != nil {
		persistErr = i.repo.SaveRetired(ctx, cred)
	}
	i.mu.Lock()
	i.byUUID[cred.MediaServerUUID] = cred
	i.mu.Unlock()
	return persistErr
}

// MarkUnprovisioned 记一次解约尝试的结果（内存 + 持久化）。
//
// 同样"先改内存"：状态是运行期事实，落库失败不该让热路径读到过期状态。
func (i *RetirementIndex) MarkUnprovisioned(ctx context.Context, uuid, state string, attempts int, at time.Time) error {
	if i == nil || uuid == "" {
		return errors.New("retired credential uuid required")
	}
	i.mu.Lock()
	row, ok := i.byUUID[uuid]
	if ok {
		row.UnprovisionState = state
		row.UnprovisionAttempts = attempts
		attempt := at
		row.LastAttemptAt = &attempt
		i.byUUID[uuid] = row
	}
	i.mu.Unlock()
	if !ok || i.repo == nil {
		return nil
	}
	return i.repo.UpdateRetiredUnprovision(ctx, uuid, state, attempts, at)
}

// Len 返回索引中的凭据条数（观测用）。
func (i *RetirementIndex) Len() int {
	if i == nil {
		return 0
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.byUUID)
}

// List 返回全部凭据快照（按 uuid 稳定排序，便于对账与测试）。
func (i *RetirementIndex) List() []RetiredCredential {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]RetiredCredential, 0, len(i.byUUID))
	for _, row := range i.byUUID {
		out = append(out, row)
	}
	sortRetiredByUUID(out)
	return out
}

// sortRetiredByUUID 简单插入排序：凭据条数是个位到十位量级，
// 不值得为它引入 sort 包依赖与比较函数。
func sortRetiredByUUID(rows []RetiredCredential) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].MediaServerUUID < rows[j-1].MediaServerUUID; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}
