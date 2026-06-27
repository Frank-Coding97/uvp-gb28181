package scheduler

import (
	"context"
	"sync"
	"time"

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
// logService(T3.3 新增)在 Pick 后异步 Emit 一条日志,可为 nil 降级。
type Manager struct {
	mu      sync.RWMutex
	current Scheduler
	factory *Factory

	logService *LogService // 可为 nil(降级:不记日志)
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
//
// T3.3:Pick 后异步 Emit 一条调度日志(命中节点 + 错误),logService 为 nil 时跳过。
func (m *Manager) Pick(ctx context.Context, inv InviteContext) (*node.Node, error) {
	m.mu.RLock()
	s := m.current
	log := m.logService
	m.mu.RUnlock()
	if s == nil {
		// 即便没 scheduler,也记一条空载日志(便于排查"为什么没节点")
		if log != nil {
			log.Emit(SchedulerLog{
				HappenedAt:   time.Now(),
				Algorithm:    "",
				StreamID:     inv.StreamID,
				DeviceID:     inv.DeviceID,
				ChannelID:    inv.ChannelID,
				ErrorMessage: ErrNoSchedulerSet.Error(),
			})
		}
		return nil, ErrNoSchedulerSet
	}
	picked, err := s.Pick(ctx, inv)
	if log != nil {
		entry := SchedulerLog{
			HappenedAt: time.Now(),
			Algorithm:  s.Name(),
			StreamID:   inv.StreamID,
			DeviceID:   inv.DeviceID,
			ChannelID:  inv.ChannelID,
		}
		if picked != nil {
			entry.NodeID = picked.ID
			entry.NodeName = picked.Name
		}
		if err != nil {
			entry.ErrorMessage = truncate(err.Error(), 255)
		}
		log.Emit(entry)
	}
	return picked, err
}

// SetLogService 注入调度日志服务(T3.3 新增,bootstrap 装配后调)
//
// 可重复调用(切换底层 service);nil 则关闭日志输出。
func (m *Manager) SetLogService(svc *LogService) {
	m.mu.Lock()
	m.logService = svc
	m.mu.Unlock()
}

// truncate 截断字符串到 max byte 长度(error_message varchar(255) 防溢出)
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
