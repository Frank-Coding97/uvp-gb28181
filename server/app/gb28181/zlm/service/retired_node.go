package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// HookUnprovisioner 是"撤销对端 hook"的能力抽象。
//
// ⛔ 刻意**不并进 ZLMProbe**：它是可选能力（未装配时退役流程照常走，只是不再自愈），
// 而且单独的窄接口让测试替身只需实现这一个方法。实现见 `zlm.ServiceAdapter`。
type HookUnprovisioner interface {
	UnprovisionHooks(ctx context.Context, n *node.Node) error
}

const (
	// retiredUnprovisionInitialBackoff 首次失败后的等待。取 1 分钟而不是更短：
	// 撤销失败的常见原因是"对端暂时不可达"，1 分钟内重试大概率还是不可达。
	retiredUnprovisionInitialBackoff = time.Minute
	// retiredUnprovisionMaxBackoff 退避上限。30 分钟是为了与 L1 折叠窗口（同样 30 分钟）
	// 对齐 —— 再长就没有意义了，因为那期间日志本身也已经折叠成每窗口一条。
	retiredUnprovisionMaxBackoff = 30 * time.Minute
	// retiredUnprovisionTimeout 单次解约的总超时。对端不可达时要尽快放弃并退避，
	// 不能让异步任务长期挂住。
	retiredUnprovisionTimeout = 15 * time.Second
)

// ErrRetiredNodeUUIDMissing 退役时节点没有 uuid，无法保留可用的撤销凭据。
var ErrRetiredNodeUUIDMissing = errors.New("retired node media_server_uuid missing")

// RetiredNodeCoordinator 把"退役一个媒体节点"这件事在平台侧闭合成三拍：
//
//	① 删行**前**  RetainCredentials —— 把 uuid/host/api_port/api_secret 落进退休表；
//	② 删行         （调用方自己做：`Registry.Delete`）
//	③ 删行**后**  RevokeAfterRetire —— 异步清掉对端那几条指向本平台的 hook。
//
// 以及一拍自愈：
//
//	④ 此后若仍收到该 uuid 的回调（对端还没被清掉 / ① 时它不可达），
//	   `ObserveRetiredHook` 命中退休表 ⇒ 按退避重试 ③，直到成功。
//
// ⛔ 顺序不能颠倒成"先解约再删行"：解约成功而删行失败时，节点还活着但已经不再回调
// 平台 —— 心跳停了、Watcher 会把它判离线，运维看到的是"删不掉还变成离线的僵尸"。
// 删行先做，失败就什么都没发生，是唯一无副作用的方向。
type RetiredNodeCoordinator struct {
	index       *node.RetirementIndex
	provisioner HookUnprovisioner
	logger      *zap.Logger
	// dispatch 派发异步解约，测试可替换成"同步立即执行"以便断言。
	dispatch func(func())
	now      func() time.Time

	mu       sync.Mutex
	backoff  map[string]time.Duration
	nextAt   map[string]time.Time
	inflight map[string]bool
}

// NewRetiredNodeCoordinator 构造。provisioner 允许为 nil（未装配时只留凭据、不解约）。
func NewRetiredNodeCoordinator(index *node.RetirementIndex, provisioner HookUnprovisioner) *RetiredNodeCoordinator {
	return &RetiredNodeCoordinator{
		index:       index,
		provisioner: provisioner,
		logger:      zap.NewNop(),
		dispatch:    func(f func()) { go f() },
		now:         time.Now,
		backoff:     make(map[string]time.Duration),
		nextAt:      make(map[string]time.Time),
		inflight:    make(map[string]bool),
	}
}

// SetLogger 装配日志。
func (c *RetiredNodeCoordinator) SetLogger(logger *zap.Logger) {
	if c == nil || logger == nil {
		return
	}
	c.logger = logger.Named("zlm.node.retired")
}

// SetDispatcher 替换异步派发方式（测试用：同步执行让断言不必等 goroutine）。
func (c *RetiredNodeCoordinator) SetDispatcher(dispatch func(func())) {
	if c == nil || dispatch == nil {
		return
	}
	c.dispatch = dispatch
}

// SetClock 替换时钟（测试用：退避窗口是分钟级，不注入时钟就只能靠 sleep）。
func (c *RetiredNodeCoordinator) SetClock(now func() time.Time) {
	if c == nil || now == nil {
		return
	}
	c.now = now
}

// RetainCredentials 在删行后调用：把撤销对端 hook 所需的凭据落下来。
//
// 返回 error 只用于审计 —— 凭据保留失败**不可以阻断节点删除**：删不掉一个节点
// 是运维事故，而少留一条凭据只是退回到"以后只能靠日志折叠兜底"。
//
// ⚠️ 同一个 uuid **再次**退役（重新加入平台又被删掉）时，本方法会把解约状态推回
// `pending`、尝试计数归零：一条退休记录对应**一次**退役事件。否则第二次删除会继承
// 第一次的 `done`，于是永远不再尝试撤销 —— 而重新加入时 `ApplyConfigForNode`
// 可能已经把 hook 又写回去了。
func (c *RetiredNodeCoordinator) RetainCredentials(ctx context.Context, n *node.Node, reason string) error {
	if c == nil || c.index == nil || n == nil {
		return nil
	}
	uuid := strings.TrimSpace(n.MediaServerUUID)
	if uuid == "" {
		return ErrRetiredNodeUUIDMissing
	}
	return c.index.Remember(ctx, node.RetiredCredential{
		MediaServerUUID:  uuid,
		Name:             n.Name,
		Host:             n.Host,
		APIPort:          n.APIPort,
		APISecret:        n.APISecret,
		RetireReason:     truncateRetireReason(reason),
		UnprovisionState: node.RetireUnprovisionPending,
		RetiredAt:        c.now(),
	})
}

// RevokeAfterRetire 在删行**后**调用：异步尝试撤销对端 hook。
//
// 异步是刻意的：解约要发三次 HTTP（读配置 → 写配置 → 回读），而删除接口不该为一个
// 可能已经不可达的对端阻塞（`PurgeUnreachable` 这类旁路存在本身就说明节点会挂）。
func (c *RetiredNodeCoordinator) RevokeAfterRetire(uuid string) {
	if c == nil || c.index == nil || strings.TrimSpace(uuid) == "" {
		return
	}
	c.scheduleUnprovision(strings.TrimSpace(uuid), "node_retired")
}

// ObserveRetiredHook 是 hook 热路径的入口：回答"这个自称 node_id 的 uuid 是不是我们
// 主动退休掉的节点"，并在未结清时顺带安排一次退避重试。
//
// 返回 true 时调用方应使用 `node_retired` 作为拒绝原因，而不是 `node_unknown`
// —— 两者的事实完全不同：一个是"平台自己删过它"，一个是"平台根本不认识它"。
func (c *RetiredNodeCoordinator) ObserveRetiredHook(uuid, sourceIP string) bool {
	if c == nil || c.index == nil {
		return false
	}
	uuid = strings.TrimSpace(uuid)
	if _, ok := c.index.Lookup(uuid); !ok {
		return false
	}
	// 这是"自愈"那一拍：只有真的又收到回调时才重试，所以重试次数天然有界
	// （对端被清掉之后就不再回调，也就没有触发源了）。要不要重试、隔多久重试
	// 由 scheduleUnprovision 统一裁决（含"已结清就不再打对端"）。
	c.scheduleUnprovision(uuid, "retired_callback@"+strings.TrimSpace(sourceIP))
	return true
}

// RetiredCredential 读一条退休凭据（对账/测试用）。
func (c *RetiredNodeCoordinator) RetiredCredential(uuid string) (node.RetiredCredential, bool) {
	if c == nil || c.index == nil {
		return node.RetiredCredential{}, false
	}
	return c.index.Lookup(uuid)
}

// scheduleUnprovision 按退避决定"现在要不要派发一次解约"。
func (c *RetiredNodeCoordinator) scheduleUnprovision(uuid, trigger string) {
	cred, ok := c.index.Lookup(uuid)
	if !ok {
		return
	}
	if cred.Resolved() {
		// 已结清：对端不再回调我们，没必要再去打它。
		// （重新退役同一个 uuid 会让 RetainCredentials 把它推回 pending，
		//   所以"再删一次"仍然会解约，见 RetainCredentials 的说明。）
		return
	}

	c.mu.Lock()
	if c.inflight[uuid] {
		c.mu.Unlock()
		return
	}
	if next, ok := c.nextAt[uuid]; ok && c.now().Before(next) {
		c.mu.Unlock()
		return
	}
	c.inflight[uuid] = true
	c.mu.Unlock()

	c.dispatch(func() { c.runUnprovision(uuid, trigger) })
}

// runUnprovision 执行一次解约并把结果写回退休表。
func (c *RetiredNodeCoordinator) runUnprovision(uuid, trigger string) {
	defer func() {
		c.mu.Lock()
		delete(c.inflight, uuid)
		c.mu.Unlock()
	}()

	cred, ok := c.index.Lookup(uuid)
	if !ok {
		return
	}
	attempts := cred.UnprovisionAttempts + 1

	err := c.unprovision(cred)

	now := c.now()
	state := node.RetireUnprovisionDone
	c.mu.Lock()
	if err != nil {
		backoff := c.backoff[uuid]
		if backoff <= 0 {
			backoff = retiredUnprovisionInitialBackoff
		} else {
			backoff *= 2
		}
		if backoff > retiredUnprovisionMaxBackoff {
			backoff = retiredUnprovisionMaxBackoff
		}
		c.backoff[uuid] = backoff
		c.nextAt[uuid] = now.Add(backoff)
		state = node.RetireUnprovisionUnreachable
	} else {
		delete(c.backoff, uuid)
		delete(c.nextAt, uuid)
	}
	c.mu.Unlock()

	// ⛔ 落库/落索引失败只记日志，不回滚上面的状态：解约的真实结果已经发生，
	// 内存与 DB 不一致时以"解约到底成没成"这个事实为准。
	if markErr := c.index.MarkUnprovisioned(context.Background(), uuid, state, attempts, now); markErr != nil {
		c.logger.Warn("退休节点解约状态落库失败",
			zap.String("event", "zlm.node.retire_state_persist_failed"),
			zap.String("node_id", uuid),
			zap.Int("attempts", attempts),
			zap.String("unprovision_state", state),
			zap.Error(markErr))
	}

	if err != nil {
		c.logger.Warn("已退休节点仍在回调，撤销对端 Hook 未成功",
			zap.String("event", "zlm.node.hook_revoke_failed"),
			zap.String("node_id", uuid),
			zap.String("node_host", cred.Host),
			zap.Int("attempts", attempts),
			zap.Duration("next_retry_in", c.currentBackoff(uuid)),
			zap.String("retire_trigger", trigger),
			zap.Error(err))
		return
	}
	c.logger.Info("已撤销对端媒体节点的 Hook 回调",
		zap.String("event", "zlm.node.hook_revoked"),
		zap.String("node_id", uuid),
		zap.String("node_host", cred.Host),
		zap.Int("attempts", attempts),
		zap.String("retire_trigger", trigger))
}

func (c *RetiredNodeCoordinator) unprovision(cred node.RetiredCredential) error {
	if c.provisioner == nil {
		return errors.New("hook unprovisioner unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), retiredUnprovisionTimeout)
	defer cancel()
	return c.provisioner.UnprovisionHooks(ctx, cred.Node())
}

func (c *RetiredNodeCoordinator) currentBackoff(uuid string) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.backoff[uuid]
}

func truncateRetireReason(reason string) string {
	reason = strings.TrimSpace(reason)
	const max = 255
	if len(reason) <= max {
		return reason
	}
	return reason[:max]
}

// SetRetiredNodeCoordinator 注入退役协调器（装配在 bootstrap，service 本身不碰 DB）。
func (s *NodeService) SetRetiredNodeCoordinator(coordinator *RetiredNodeCoordinator) {
	if s == nil {
		return
	}
	s.retiredMu.Lock()
	defer s.retiredMu.Unlock()
	s.retired = coordinator
}

func (s *NodeService) retiredCoordinator() *RetiredNodeCoordinator {
	if s == nil {
		return nil
	}
	s.retiredMu.RLock()
	defer s.retiredMu.RUnlock()
	return s.retired
}

// 退役原因：写进退休表的 `retire_reason`，用于事后回答"这条凭据是哪种删除留下的"。
const (
	retireReasonOperatorDelete   = "operator_delete"
	retireReasonPurgeUnreachable = "purge_unreachable"
	// retireReasonFailedCreate 新建失败回滚：节点从未真正上线过，但收敛失败可能
	// 发生在"hook 已写进对端、回读对不上"那一步 ⇒ 同样要留凭据并把它撤掉。
	retireReasonFailedCreate = "failed_create_rollback"
)

// retireDeletedNode 给一个**已经被删掉**的节点收尾：留下撤销凭据，并异步撤销对端 hook。
//
// 三条删除路径（普通删除 / 影响预检确认删除 / 强制移除不可达）都要调它。
//
// ⛔ 刻意放在 `Registry.Delete` **之后**，而调用方此刻手里就有 `cur` 的完整内存快照：
//   - 放在删行**前**，任何一次被拒的删除（影响预检冲突、或"强制移除"时发现节点其实可达）
//     都会在退休表里留下一条"其实还活着"的记录，让这张表的语义失真 ——
//     而被拒的删除是**常态**，不是异常；
//   - 放在删行后也不会丢凭据：`Registry.Delete` 删的是 DB 行与索引，不是这个副本。
func (s *NodeService) retireDeletedNode(ctx context.Context, cur *node.Node, reason string) {
	coordinator := s.retiredCoordinator()
	if coordinator == nil || cur == nil {
		return
	}
	if err := coordinator.RetainCredentials(ctx, cur, reason); err != nil {
		// 凭据保留失败**不阻断删除**（节点已经删掉了，此时也回不去）：
		// 后果只是退回到"以后只能靠日志折叠兜底"，所以记 WARN 让人看见。
		s.logger.Warn("保留已删除节点的撤约凭据失败",
			zap.String("event", "zlm.node.retire_credentials_failed"),
			zap.Int64("node_id", cur.ID),
			zap.String("node_uuid", cur.MediaServerUUID),
			zap.Error(err))
		return
	}
	coordinator.RevokeAfterRetire(cur.MediaServerUUID)
}
