package scheduler

import (
	"context"
	"sort"
	"sync/atomic"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// RoundRobin 朴素轮询调度
//
// 实现:从 Registry.ListActive 取活跃节点切片,用 atomic counter 取模选 index。
// 节点池由 Registry 维护,Pick 不写状态;maintenance / offline 节点已被 ListActive 自动过滤。
//
// 并发安全:counter 用 atomic.Int64,ListActive 内部 RLock。
type RoundRobin struct {
	reg     *node.Registry
	counter atomic.Int64
}

// NewRoundRobin 构造
func NewRoundRobin(reg *node.Registry) *RoundRobin {
	return &RoundRobin{reg: reg}
}

// Name 算法名(跟 scheduler_setting.algorithm 对齐)
func (r *RoundRobin) Name() string { return "roundrobin" }

// Pick 选下一个活跃节点
//
// 返回值:命中的节点拷贝(Registry.ListActive 已返回拷贝)或 ErrNoActiveNode。
//
// 实现细节:Registry.ListActive 内部 map 遍历无固定顺序,
// 这里按 ID 升序排序后再 modulo,保证轮询稳定。
func (r *RoundRobin) Pick(_ context.Context, _ InviteContext) (*node.Node, error) {
	active := r.reg.ListActive()
	if len(active) == 0 {
		return nil, ErrNoActiveNode
	}
	sort.Slice(active, func(i, j int) bool { return active[i].ID < active[j].ID })
	// counter.Add 返回累加后的新值,从 1 起,modulo 长度落到 [0, len-1]
	idx := (r.counter.Add(1) - 1) % int64(len(active))
	return active[idx], nil
}
