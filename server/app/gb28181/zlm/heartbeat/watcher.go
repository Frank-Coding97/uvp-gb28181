package heartbeat

import (
	"context"
	"time"

	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
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
// 标记成功时 log info(给运维看哪个节点掉了);标记失败时 log warn,下 Tick 重试。
func (w *Watcher) Tick() {
	now := w.clock.Now()
	for _, n := range w.registry.ListActive() {
		// 从未心跳过 — 不误判
		if n.Stats.LastHeartbeatAt.IsZero() {
			continue
		}
		gap := now.Sub(n.Stats.LastHeartbeatAt)
		if gap <= w.offlineThreshold {
			continue
		}
		if err := w.registry.MarkOffline(context.Background(), n.ID); err != nil {
			if app.ZapLog != nil {
				app.ZapLog.Warn("GB28181 ZLM 标节点离线失败",
					zap.Int64("nodeId", n.ID),
					zap.String("uuid", n.MediaServerUUID),
					zap.Error(err))
			}
			continue
		}
		if app.ZapLog != nil {
			app.ZapLog.Info("GB28181 ZLM 节点已标记离线",
				zap.Int64("nodeId", n.ID),
				zap.String("name", n.Name),
				zap.String("uuid", n.MediaServerUUID),
				zap.Duration("heartbeatGap", gap))
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
