package scheduler

import (
	"context"
	"sort"
	"sync/atomic"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// LeastLoad 选综合负载最低的活跃节点
//
// 综合负载公式:
//
//	load = NetThreadLoadAvg * 0.6 + WorkThreadLoadAvg * 0.4
//
// 网络 I/O 是 RTP 转发的主要瓶颈,权重 0.6;工作线程权重 0.4。
//
// 边界:
//   - 无活跃节点:返 ErrNoActiveNode
//   - 全部节点 Stats 零值(从未上报心跳):退化为 RoundRobin
//   - 多节点综合负载相等:用本地 atomic counter 做平局轮询
//
// 并发安全:counter 用 atomic.Int64,Registry.ListActive 内部已 RLock。
type LeastLoad struct {
	reg     *node.Registry
	counter atomic.Int64
}

// NewLeastLoad 构造
func NewLeastLoad(reg *node.Registry) *LeastLoad {
	return &LeastLoad{reg: reg}
}

// Name 算法名(跟 scheduler_setting.algorithm 对齐)
func (l *LeastLoad) Name() string { return "leastload" }

// loadOf 计算节点综合负载
func loadOf(n *node.Node) float64 {
	return n.Stats.NetThreadLoadAvg*0.6 + n.Stats.WorkThreadLoadAvg*0.4
}

// Pick 选综合负载最低的活跃节点
//
// 算法:
//  1. ListActive 取活跃节点,按 ID 升序排序(map 遍历无序,确定性需要)
//  2. 若全部 Stats 零值(从未心跳),退化为 RoundRobin
//  3. 一轮扫描找出 minLoad 和 candidates(浮点容差 0.0001 内视为相等)
//  4. 单候选直接返;多候选(平局)用 counter 轮询
func (l *LeastLoad) Pick(_ context.Context, _ InviteContext) (*node.Node, error) {
	active := l.reg.ListActive()
	if len(active) == 0 {
		return nil, ErrNoActiveNode
	}

	// 按 ID 稳定排序(Registry.ListActive 内部 map 遍历无固定顺序)
	sort.Slice(active, func(i, j int) bool { return active[i].ID < active[j].ID })

	// 全零 → 退化为 RoundRobin(确定性轮询)
	allZero := true
	for _, n := range active {
		if n.Stats.NetThreadLoadAvg > 0 || n.Stats.WorkThreadLoadAvg > 0 {
			allZero = false
			break
		}
	}
	if allZero {
		idx := (l.counter.Add(1) - 1) % int64(len(active))
		return active[idx], nil
	}

	// 找综合负载最低的候选集(浮点容差防误差)
	const eps = 0.0001
	minLoad := loadOf(active[0])
	candidates := []*node.Node{active[0]}
	for _, n := range active[1:] {
		ld := loadOf(n)
		switch {
		case ld < minLoad-eps:
			minLoad = ld
			candidates = []*node.Node{n}
		case ld < minLoad+eps:
			candidates = append(candidates, n)
		}
	}

	if len(candidates) == 1 {
		return candidates[0], nil
	}

	// 平局轮询
	idx := (l.counter.Add(1) - 1) % int64(len(candidates))
	return candidates[idx], nil
}
