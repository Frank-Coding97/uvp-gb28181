// Package scheduler ZLM 节点调度
//
// 抽象一个 Scheduler 接口,M2 实现 RoundRobin;M3 再扩 Weighted / LeastLoad。
// Manager 持有当前激活的 Scheduler,支持热切换算法(配合 scheduler_setting 表)。
package scheduler

import (
	"context"
	"errors"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// InviteContext 一次 Invite 的上下文(给调度算法做决策)
//
// M2 RoundRobin 不依赖任何字段;M3 Weighted / LeastLoad / Tags 选路时会用到。
type InviteContext struct {
	DeviceID  string
	ChannelID string
	StreamID  string
	Tags      map[string]string
}

// Scheduler 节点调度算法抽象
//
// 实现要求线程安全(并发 Pick)。Pick 不变更节点状态。
type Scheduler interface {
	// Pick 从活跃节点中挑一个;若无活跃节点返回 ErrNoActiveNode
	Pick(ctx context.Context, inv InviteContext) (*node.Node, error)
	// Name 算法名,跟 scheduler_setting.algorithm 取值对齐
	Name() string
}

// ErrNoActiveNode 没有可用的活跃节点
var ErrNoActiveNode = errors.New("no active zlm node")

// ErrNoSchedulerSet Manager 未设置当前算法
var ErrNoSchedulerSet = errors.New("scheduler not set")
