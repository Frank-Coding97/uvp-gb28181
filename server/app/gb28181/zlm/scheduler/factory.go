package scheduler

import (
	"fmt"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// Factory 按算法名构造 Scheduler
//
// 当前支持:roundrobin(M2 实装)、weighted(M3 T3.1,Nginx 平滑加权)、
// leastload(M3 T3.2,综合负载最低)。
type Factory struct {
	reg *node.Registry
}

// NewFactory 构造
func NewFactory(reg *node.Registry) *Factory {
	return &Factory{reg: reg}
}

// Build 按算法名造 Scheduler
//
// 算法名跟 scheduler_setting.algorithm DB 取值对齐:
//   - "roundrobin" → RoundRobin(M2)
//   - "weighted"   → Weighted(M3 T3.1,Nginx 平滑加权)
//   - "leastload"  → LeastLoad(M3 T3.2,综合负载最低 + 全零 fallback RR)
//   - 其它         → 未知算法,返错
func (f *Factory) Build(name string) (Scheduler, error) {
	switch name {
	case "roundrobin":
		return NewRoundRobin(f.reg), nil
	case "weighted":
		return NewWeighted(f.reg), nil
	case "leastload":
		return NewLeastLoad(f.reg), nil
	default:
		return nil, fmt.Errorf("unknown scheduler algorithm: %q", name)
	}
}
