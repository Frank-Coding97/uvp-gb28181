package scheduler

import (
	"context"
	"sort"
	"sync"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// Weighted Nginx Smooth Weighted Round Robin
//
// 算法参考 nginx upstream:
// https://github.com/phusion/nginx/commit/27e94984486058d73157038f7950a0a36ecc6e35
//
// 每个 eligible 节点维护 current 累加器(effective weight 固定为 node.Weight)。每次 Pick:
//   - 对每个 eligible 节点 current += weight
//   - 选 current 最大的节点
//   - 选中节点 current -= total_weight
//
// 这样输出序列平滑(不会连续重复同一节点),且各节点出现比例 ≈ weight 比例。
// 例:weight=5/1/1 序列 a a b a c a a(7 次内 a 出现 5 次,b/c 各 1 次,无连续 a a a a a)。
//
// weight=0 节点被显式过滤(明确禁用调度);
// 离线 / 维护态节点由 Registry.ListActive 已过滤。
//
// 并发安全:current map 由 sync.Mutex 保护。
type Weighted struct {
	reg     *node.Registry
	mu      sync.Mutex
	current map[int64]int
}

// NewWeighted 构造
func NewWeighted(reg *node.Registry) *Weighted {
	return &Weighted{reg: reg, current: make(map[int64]int)}
}

// Name 算法名(跟 scheduler_setting.algorithm 对齐)
func (w *Weighted) Name() string { return "weighted" }

// Pick 加权选一个 active 节点
//
// 返回 ErrNoActiveNode 的情形:
//   - 无 active 节点
//   - active 节点全部 weight=0
func (w *Weighted) Pick(_ context.Context, _ InviteContext) (*node.Node, error) {
	active := w.reg.ListSchedulable()
	if len(active) == 0 {
		return nil, ErrNoActiveNode
	}
	// 按 ID 稳定排序(map 遍历无序,需保证算法可重现)
	sort.Slice(active, func(i, j int) bool { return active[i].ID < active[j].ID })

	// 过滤 weight=0 节点(明确禁用调度)
	eligible := active[:0]
	for _, n := range active {
		if n.Weight > 0 {
			eligible = append(eligible, n)
		}
	}
	if len(eligible) == 0 {
		return nil, ErrNoActiveNode
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	total := 0
	for _, n := range eligible {
		total += n.Weight
		w.current[n.ID] += n.Weight
	}

	// 选 current 最大的节点(并列时按 ID 升序取第一个)
	bestID := int64(-1)
	bestCurrent := -1 << 30
	for _, n := range eligible {
		if w.current[n.ID] > bestCurrent {
			bestCurrent = w.current[n.ID]
			bestID = n.ID
		}
	}
	w.current[bestID] -= total

	// GC:删除已不在 eligible 的旧 entries(节点被删 / 转 offline / weight 改 0)
	keep := make(map[int64]bool, len(eligible))
	for _, n := range eligible {
		keep[n.ID] = true
	}
	for id := range w.current {
		if !keep[id] {
			delete(w.current, id)
		}
	}

	for _, n := range eligible {
		if n.ID == bestID {
			return n, nil
		}
	}
	return nil, ErrNoActiveNode
}
