package heartbeat

import (
	"context"
	"time"

	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

const (
	nodeOfflinePersistFailedEvent = "zlm.node.offline_persist_failed"
	nodeOfflineEvent              = "zlm.node.offline"
	threadLoadNetFailedEvent      = "zlm.thread_load.net_failed"
	threadLoadWorkFailedEvent     = "zlm.thread_load.work_failed"
)

func repeatFailure(ctx context.Context, key logging.RepeatKey, err error) {
	if runtime := app.LogRuntime; runtime != nil {
		if repeats := runtime.Repeats(); repeats != nil {
			repeats.Fail(key, err)
			return
		}
	}
	app.Log(ctx).Named(key.Component).Warn("Background operation failed",
		zap.String("event", key.Event),
		zap.Int64("node_id", key.NodeID),
		logging.Error(err),
	)
}

func repeatRecovered(key logging.RepeatKey) {
	if runtime := app.LogRuntime; runtime != nil {
		if repeats := runtime.Repeats(); repeats != nil {
			repeats.Recovered(key)
		}
	}
}

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
	notifier         NodeEventNotifier
}

// NodeEventNotifier is optional so existing Watcher construction remains
// compatible. Restart coordination consumes only successful state changes.
type NodeEventNotifier interface {
	OnNodeOffline(nodeID int64)
	OnNodeHeartbeat(nodeID int64)
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

// NewWatcherWithNotifier is the T13-compatible constructor for T14 wiring.
func NewWatcherWithNotifier(reg *node.Registry, clock Clock, checkInterval, offlineThreshold time.Duration, notifier NodeEventNotifier) *Watcher {
	w := NewWatcher(reg, clock, checkInterval, offlineThreshold)
	w.notifier = notifier
	return w
}

func (w *Watcher) SetNotifier(notifier NodeEventNotifier) { w.notifier = notifier }

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
			repeatFailure(context.Background(), logging.RepeatKey{
				Component: "zlm",
				Event:     nodeOfflinePersistFailedEvent,
				NodeID:    n.ID,
			}, err)
			continue
		}
		repeatRecovered(logging.RepeatKey{
			Component: "zlm",
			Event:     nodeOfflinePersistFailedEvent,
			NodeID:    n.ID,
		})
		if w.notifier != nil {
			w.notifier.OnNodeOffline(n.ID)
		}
		app.Log(context.Background()).Named("zlm").Info("GB28181 ZLM 节点已标记离线",
			zap.String("event", nodeOfflineEvent),
			zap.Int64("node_id", n.ID),
			zap.String("name", n.Name),
			zap.String("uuid", n.MediaServerUUID),
			zap.Duration("heartbeat_gap", gap))
	}
}

// Start 启动后台 goroutine,周期跑 Tick,直到 ctx 取消。
// 返回的 channel 会在 Tick 不再执行后关闭,调用方可用它完成有界停机。
//
// 用法:
//
//	watcher := heartbeat.NewWatcher(reg, heartbeat.RealClock(), 30*time.Second, 90*time.Second)
//	ctx, cancel := context.WithCancel(context.Background())
//	watcher.Start(ctx)
//	// ... cancel() 时停止
func (w *Watcher) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
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
	return done
}
