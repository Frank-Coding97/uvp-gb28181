package handler

import (
	"sort"
	"sync"
	"time"
)

// ZLM hook 的 `node` 参数是**调用方自称**的，平台删掉一个节点之后，对端不会知道 ——
// 它只是继续按自己的周期回调。实测过的现场：220 上一个已被移除的 ZLM 实例
// (`eeyelog-zlm`) 仍保留着指向平台的 hook 配置，`on_server_keepalive` 每 10s 一条，
// 于是控制台以每天 8640 行的速度重复同一条事实。
//
// 这类"慢性重复"不是故障信号，而是同一条事实的第 N 次播报；它把真正的告警挤下去，
// 却又不能彻底静默 —— 「有个来源在持续被拒」本身是需要人看见的（凭据被改、被冒充、
// 或退役没退干净，三种原因都要求有人去核）。所以处置是**折叠**而不是**关掉**：
//
//   - 同一「原因 + 自称节点 + 对端地址 + 哪个 hook」在窗口内**只打首次明细**；
//   - 窗口到期时补一条带 `count` 的汇总，说明这一段时间里被折叠了多少条。
//
// 30 分钟窗口下，10s 一次的心跳从 180 条降到 1 条，信号仍在。
//
// ⚠️ 这层与 `HookAuthenticator.limiter`（全局令牌桶 8/s）**不是一回事**，两者都要留：
// 令牌桶防的是**秒级风暴**（伪造一批随机 node_id 打过来），对 0.1/s 的慢性重复完全无效；
// 本表防的是**慢性重复**，但对"每个来源只来一次"的分布式刷屏无能为力。
//
// ⚠️ 也**不复用** `app/utils/logging.Repeater`（T11 的重复失败聚合器），虽然它形状相近：
//   - 它的身份维度是 `NodeID int64` + `JobID`（平台自己的实体），而这边的"谁"是
//     对方**自报的字符串** `node` + 真实对端 `source_ip`，一个也塞不进去；
//   - 它的语义是"后台操作失败了、好没好"，恢复靠 `Recovered(key)`；被拒的未知节点
//     没有对应的"恢复"信号（它只会一直打，或被人去关掉）；
//   - 它的窗口是 1 分钟、文案是 `Background operation failed`。10s 级周期在这里
//     只会被折成 1/6，而且把"未认证的 HTTP 回调"说成"后台操作失败"是错的。
//
// 结论：形状相近不等于同一个东西，硬并会同时污染两边的语义。但**窗口、结算字段
// （`count` / `window_seconds`）的命名向 T11 看齐**，免得同一个概念出现两种叫法。
const (
	// hookRejectionWindow 是同一来源被折叠进一条汇总的时间长度。
	hookRejectionWindow = 30 * time.Minute
	// hookRejectionMaxTracked 限制去重表的规模。键里的 node_id / source_ip 都由
	// 调用方自报，不设上限的话，伪造一批随机 node_id 就能把这张表撑大。
	hookRejectionMaxTracked = 512
)

// hookRejectionKey 是「谁被拒 + 为什么」的最小组合。
type hookRejectionKey struct {
	reason    string
	nodeID    string
	sourceIP  string
	hookEvent string
}

type hookRejectionRecord struct {
	firstSeen time.Time
	lastSeen  time.Time
	// suppressed 是本窗口内被折叠掉的条数（**不含**窗口里那条明细本身）。
	// 用词向 T11 的 `logging.Repeater` 看齐：同是"被压掉的重复条数"。
	suppressed int
}

// HookRejectionVerdict 告诉调用方这一次拒绝该怎么记。
type HookRejectionVerdict uint8

const (
	// HookRejectionFirst：这个来源第一次被拒（或窗口刚重置），打一条明细。
	HookRejectionFirst HookRejectionVerdict = iota
	// HookRejectionSummary：窗口到期，把上一窗口折叠掉的那一批补成一条汇总。
	// 本次同时也是新窗口的第一条，但**不再另打明细** —— 否则每个窗口又变成两行。
	HookRejectionSummary
	// HookRejectionSuppressed：同一来源在窗口内重复，不单独成行。
	HookRejectionSuppressed
)

// hookRejectionFoldable 只对**对方造成的**拒绝折叠。
//
// `auth_runtime_unavailable`（中间件未装配 / 解析器为空）是**平台自己坏了**，
// 那一路要按次打，不能因为"折叠"把自身故障一起变安静；将来新增的 reason_code
// 也默认**不折叠** —— 未知原因按老行为逐条打，等有人判过再放进这张表。
var hookRejectionFoldable = map[string]bool{
	"credentials_invalid": true,
	"node_unknown":        true,
	"capability_invalid":  true,
	// node_retired：平台**自己删过**这个节点，对端还没被撤干净（见 retired_node.go）。
	// 它同样属于"对方在重复问、要人去核一次"，折叠频率而不是降低等级；
	// 且它比 node_unknown 更该被折叠 —— 撤销成功之前，对端的回调频率是固定的。
	"node_retired": true,
}

// hookRejectionTracker 记录每个来源的窗口状态。并发安全：hook 是 HTTP 回调，
// 多个来源会同时进来。
type hookRejectionTracker struct {
	mu      sync.Mutex
	records map[hookRejectionKey]hookRejectionRecord
}

func newHookRejectionTracker() *hookRejectionTracker {
	return &hookRejectionTracker{records: make(map[hookRejectionKey]hookRejectionRecord)}
}

// observe 记录一次拒绝，并返回这一次该怎么记。now 由调用方传入，便于测试窗口边界。
//
// 返回的 record 在 HookRejectionSummary 时是**上一窗口**的统计（suppressed 即被折叠的条数），
// 其余情况是当前窗口的统计。
func (t *hookRejectionTracker) observe(key hookRejectionKey, now time.Time) (HookRejectionVerdict, hookRejectionRecord) {
	fresh := hookRejectionRecord{firstSeen: now, lastSeen: now}
	if t == nil {
		// 认证器未装配时不做折叠：宁可多打，也不要在"中间件本身坏了"的路径上少打。
		return HookRejectionFirst, fresh
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	previous, seen := t.records[key]
	switch {
	case !seen:
		t.reserveLocked(now)
		t.records[key] = fresh
		return HookRejectionFirst, fresh
	case now.Sub(previous.firstSeen) < hookRejectionWindow:
		previous.lastSeen = now
		previous.suppressed++
		t.records[key] = previous
		return HookRejectionSuppressed, previous
	default:
		t.records[key] = fresh
		return HookRejectionSummary, previous
	}
}

// tracked 返回当前表内条目数，仅供测试与观测使用。
func (t *hookRejectionTracker) tracked() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.records)
}

// reserveLocked 在插入新键前保证表不会无限增长：先清掉窗口已过期的条目，
// 仍满则按「最近一次出现」淘汰最旧的四分之一。
func (t *hookRejectionTracker) reserveLocked(now time.Time) {
	if len(t.records) < hookRejectionMaxTracked {
		return
	}
	for key, record := range t.records {
		if now.Sub(record.firstSeen) >= hookRejectionWindow {
			delete(t.records, key)
		}
	}
	if len(t.records) < hookRejectionMaxTracked {
		return
	}
	keys := make([]hookRejectionKey, 0, len(t.records))
	for key := range t.records {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return t.records[keys[i]].lastSeen.Before(t.records[keys[j]].lastSeen)
	})
	for _, key := range keys[:len(keys)/4+1] {
		delete(t.records, key)
	}
}
