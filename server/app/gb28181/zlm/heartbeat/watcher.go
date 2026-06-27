package heartbeat

import (
	"context"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// Clock 可注入的时钟抽象(便于测试用 FakeClock 推进时间)
type Clock interface {
	Now() time.Time
}

// realClock 生产默认实现
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// RealClock 默认时钟(bootstrap 用)
func RealClock() Clock { return realClock{} }

// Watcher 周期扫描 Registry 中所有 active 节点,LastHeartbeatAt 超阈值则 MarkOffline。
//
// 不处理 maintenance / offline 节点:
//   - maintenance:运维主动操作态,不归 Watcher 管
//   - offline:已离线,等心跳恢复时 UpdateStats 自动翻 active(见 Registry.UpdateStats)
//
// 不处理"从未心跳"的节点(LastHeartbeatAt 零值):
//   - 节点刚启动 / 刚加进集群,等 ZLM 真正打过来一次心跳后再判
type Watcher struct {
	registry         *node.Registry
	clock            Clock
	checkInterval    time.Duration
	offlineThreshold time.Duration
}

// NewWatcher 构造。
//
//   - checkInterval:Tick 频率(典型 30s)
//   - offlineThreshold:LastHeartbeatAt 距 now 超过此值则标 offline(典型 90s = 3 个 30s 心跳)
func NewWatcher(reg *node.Registry, clock Clock, checkInterval, offlineThreshold time.Duration) *Watcher {
	return &Watcher{
		registry:         reg,
		clock:            clock,
		checkInterval:    checkInterval,
		offlineThreshold: offlineThreshold,
	}
}

// Tick 一次扫描:遍历 active 节点,LastHeartbeatAt 超阈值 → MarkOffline。
//
// 错误处理:MarkOffline 失败仅记 errs 计数返回(供调用方观测),不中断扫描。
// 当前实现忽略错误(由调用方决定要不要打日志);可后续接 metrics。
func (w *Watcher) Tick() {
	now := w.clock.Now()
	for _, n := range w.registry.ListActive() {
		// 从未心跳过 — 不误判
		if n.Stats.LastHeartbeatAt.IsZero() {
			continue
		}
		if now.Sub(n.Stats.LastHeartbeatAt) > w.offlineThreshold {
			// MarkOffline 写 DB + 内存;失败暂忽略(下个 Tick 还会重试)
			_ = w.registry.MarkOffline(context.Background(), n.ID)
		}
	}
}

// Start 启动后台 goroutine,周期跑 Tick,直到 ctx 取消。
//
// 用法:
//
//	watcher := heartbeat.NewWatcher(reg, heartbeat.RealClock(), 30*time.Second, 90*time.Second)
//	ctx, cancel := context.WithCancel(context.Background())
//	watcher.Start(ctx)
//	// ... cancel() 时停止
func (w *Watcher) Start(ctx context.Context) {
	go func() {
		tk := time.NewTicker(w.checkInterval)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				w.Tick()
			}
		}
	}()
}
