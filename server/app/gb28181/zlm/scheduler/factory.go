package scheduler

import (
	"fmt"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// Factory 按算法名构造 Scheduler
//
// 当前支持:roundrobin(M2 实装)。
// weighted / leastload 占位返错,M3 实现。
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
//   - "weighted"   → M3 未实装,返错
//   - "leastload"  → M3 未实装,返错
//   - 其它         → 未知算法,返错
func (f *Factory) Build(name string) (Scheduler, error) {
	switch name {
	case "roundrobin":
		return NewRoundRobin(f.reg), nil
	case "weighted":
		return nil, fmt.Errorf("scheduler weighted not implemented yet (M3)")
	case "leastload":
		return nil, fmt.Errorf("scheduler leastload not implemented yet (M3)")
	default:
		return nil, fmt.Errorf("unknown scheduler algorithm: %q", name)
	}
}
