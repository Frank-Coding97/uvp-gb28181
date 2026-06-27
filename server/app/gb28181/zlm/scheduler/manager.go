package scheduler

import (
	"context"
	"sync"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// Manager 持有当前激活的 Scheduler,支持热切换算法
//
// 用法:bootstrap 装配时:
//
//	factory := scheduler.NewFactory(registry)
//	m := scheduler.NewManager(factory)
//	_ = m.Switch("roundrobin")  // 从 DB scheduler_setting 读 algorithm 名
//
// SIP play 路径调用 m.Pick(ctx, inv) 拿目标节点。
//
// 并发安全:current 受 RWMutex 保护,Pick 走读锁,Switch 走写锁。
type Manager struct {
	mu      sync.RWMutex
	current Scheduler
	factory *Factory
}

// NewManager 构造(初始无 current,需调 Switch 设置)
func NewManager(factory *Factory) *Manager {
	return &Manager{factory: factory}
}

// Switch 切换到指定算法(原子替换 current)
//
// 若 factory.Build 失败则 current 不变。
func (m *Manager) Switch(name string) error {
	s, err := m.factory.Build(name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.current = s
	m.mu.Unlock()
	return nil
}

// CurrentName 当前激活算法名(无则返空串)
func (m *Manager) CurrentName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current == nil {
		return ""
	}
	return m.current.Name()
}

// Pick 委托给当前 Scheduler;若未设置返 ErrNoSchedulerSet
func (m *Manager) Pick(ctx context.Context, inv InviteContext) (*node.Node, error) {
	m.mu.RLock()
	s := m.current
	m.mu.RUnlock()
	if s == nil {
		return nil, ErrNoSchedulerSet
	}
	return s.Pick(ctx, inv)
}
