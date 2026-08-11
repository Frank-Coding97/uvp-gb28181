// Package reconciler 兜底对账 goroutine:定期比对 DB gb_channel.stream_id
// 与 ZLM 真实流状态,清除 hook 丢包 / 进程重启导致的假阳性.
//
// 独立包避免跟 play.Service 循环依赖:
// reconciler 依赖 play.Service.Stop(通过 Stopper 接口抽象).
//
// 用法:
//
//	rec := reconciler.New(5*time.Minute, playSvc,
//	    reconciler.WithRegistry(zlmRegistry),
//	    reconciler.WithLocationMap(locationMap))
//	rec.Start(ctx)
//	defer rec.Stop()
package reconciler

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// Stopper 抽象 play.Service.Stop,便于 mock.
type Stopper interface {
	Stop(ctx context.Context, streamID string) error
}

type ConditionalStopper interface {
	StopIfPersistedCurrent(ctx context.Context, streamID, ssrc string) error
}

// Registry 抽象 zlm/node.Registry,便于 mock.
type Registry interface {
	List() []*node.Node
	Get(id int64) (*node.Node, bool)
}

// MediaProbe 抽象"这条流在这个 node 上是否 online",便于 mock.
// 真实实现走 zlm.NewClientForNode(n).IsMediaOnline(ctx, "rtp", streamID).
type MediaProbe interface {
	IsMediaOnline(ctx context.Context, n *node.Node, streamID string) (bool, error)
}

// ChannelLister 抽象 model 层查询,便于 mock.
type ChannelLister interface {
	ListPlayingChannels(ctx context.Context) (gbmodels.GbChannelList, error)
}

// Stats 单轮对账统计.
type Stats struct {
	Scanned int // DB 里 stream_id != '' 的总数
	Cleaned int // 判定为假阳性并成功清理
	Skipped int // 节点不可达跳过(Q4)
	Failed  int // ZLM 查询失败 / Stop 失败
}

// Reconciler 对账器.
type Reconciler struct {
	interval time.Duration
	stopper  Stopper

	// 多节点模式(优先)
	registry    Registry
	locationMap *stream.LocationMap
	probe       MediaProbe

	// model 层(可注入 mock)
	lister ChannelLister

	// 运行时
	running atomic.Bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// Option 构造选项.
type Option func(*Reconciler)

// WithRegistry 多节点模式:传入 node.Registry.
func WithRegistry(r Registry) Option {
	return func(rec *Reconciler) { rec.registry = r }
}

// WithLocationMap 多节点模式:传入 stream.LocationMap.
func WithLocationMap(m *stream.LocationMap) Option {
	return func(rec *Reconciler) { rec.locationMap = m }
}

// WithMediaProbe 注入 media probe,测试用;生产默认走 defaultProbe(直连 ZLM).
func WithMediaProbe(p MediaProbe) Option {
	return func(rec *Reconciler) { rec.probe = p }
}

// WithChannelLister 注入 model lister,测试用;生产默认走 gbmodels.ListPlayingChannels.
func WithChannelLister(l ChannelLister) Option {
	return func(rec *Reconciler) { rec.lister = l }
}

// New 构造 Reconciler.interval <= 0 会导致 Start 立即返回不启动 goroutine.
// stopper 必传(会 panic 如果为 nil).
func New(interval time.Duration, stopper Stopper, opts ...Option) *Reconciler {
	if stopper == nil {
		panic("reconciler.New: stopper is required")
	}
	rec := &Reconciler{
		interval: interval,
		stopper:  stopper,
	}
	for _, opt := range opts {
		opt(rec)
	}
	if rec.probe == nil {
		rec.probe = defaultProbe{}
	}
	if rec.lister == nil {
		rec.lister = defaultLister{}
	}
	return rec
}

// Start 启动定时对账 goroutine.interval <= 0 直接返回.
// 同一 Reconciler 只能 Start 一次;重复 Start 无副作用(第二次会被忽略).
func (r *Reconciler) Start(ctx context.Context) {
	if r.interval <= 0 {
		app.ZapLog.Info("reconciler 未启动:interval <= 0",
			zap.Duration("interval", r.interval))
		return
	}
	if r.cancel != nil {
		app.ZapLog.Warn("reconciler 已启动,忽略重复 Start")
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	r.wg.Add(1)
	go r.loop(ctx)
}

// Stop 停止对账 goroutine 并等 loop 退出.
// 多次调用安全.
func (r *Reconciler) Stop() {
	if r.cancel == nil {
		return
	}
	r.cancel()
	r.wg.Wait()
	r.cancel = nil
}

func (r *Reconciler) loop(ctx context.Context) {
	defer r.wg.Done()
	defer func() {
		if rec := recover(); rec != nil {
			app.ZapLog.Error("reconciler loop panic",
				zap.Any("recover", rec))
		}
	}()

	// 启动首次立即跑一次(不等第一个 tick),让 hook 丢包 / 进程重启后
	// 残留的 stream_id 能在启动后 O(几秒) 内被清理,而不用等一整个 interval.
	r.runOnce(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			app.ZapLog.Info("reconciler 收到停止信号,loop 退出")
			return
		case <-ticker.C:
			r.runOnce(ctx)
		}
	}
}

// runOnce 单轮对账.
// atomic.CAS 抢占防重入:上一轮还没跑完时,ticker 又触发会被跳过.
// 返回 Stats 便于测试断言,内部日志已经记录.
func (r *Reconciler) runOnce(ctx context.Context) Stats {
	if !r.running.CompareAndSwap(false, true) {
		app.ZapLog.Debug("reconciler 上一轮未完成,本轮跳过")
		return Stats{}
	}
	defer r.running.Store(false)

	startAt := time.Now()
	var stats Stats

	channels, err := r.lister.ListPlayingChannels(ctx)
	if err != nil {
		app.ZapLog.Warn("reconciler 查 DB 失败,本轮跳过",
			zap.Error(err))
		return stats
	}
	stats.Scanned = len(channels)

	for _, ch := range channels {
		if ctx.Err() != nil {
			app.ZapLog.Info("reconciler 中途收到停止信号,提前退出")
			break
		}
		result := r.judgeOne(ctx, ch.StreamID)
		switch result {
		case judgeOnline:
			// 真在播,不动
		case judgeSkip:
			stats.Skipped++
		case judgeStale:
			var stopErr error
			if conditional, ok := r.stopper.(ConditionalStopper); ok && ch.CurrentSSRC != "" {
				stopErr = conditional.StopIfPersistedCurrent(ctx, ch.StreamID, ch.CurrentSSRC)
			} else {
				stopErr = r.stopper.Stop(ctx, ch.StreamID)
			}
			if stopErr != nil {
				stats.Failed++
				app.ZapLog.Error("reconciler 清理假阳性失败",
					zap.String("streamID", ch.StreamID),
					zap.String("deviceID", ch.DeviceID),
					zap.String("channelID", ch.ChannelID),
					zap.Error(stopErr))
			} else {
				stats.Cleaned++
				app.ZapLog.Info("reconciler 已清理假阳性",
					zap.String("streamID", ch.StreamID),
					zap.String("deviceID", ch.DeviceID),
					zap.String("channelID", ch.ChannelID))
			}
		case judgeProbeError:
			stats.Failed++
		}
	}

	app.ZapLog.Info("reconciler 一轮对账完成",
		zap.Int("scanned", stats.Scanned),
		zap.Int("cleaned", stats.Cleaned),
		zap.Int("skipped", stats.Skipped),
		zap.Int("failed", stats.Failed),
		zap.Duration("elapsed", time.Since(startAt)))
	return stats
}

// defaultProbe 生产环境 media probe:通过 zlm.NewClientForNode 直连.
// 单独抽出来是为了测试注入 mock(WithMediaProbe).
type defaultProbe struct{}

// IsMediaOnline 走 defaultProbeImpl(在 default_probe.go 里定义,避免本文件反向 import zlm).
func (defaultProbe) IsMediaOnline(ctx context.Context, n *node.Node, streamID string) (bool, error) {
	return defaultProbeImpl(ctx, n, streamID)
}

// defaultLister 生产环境 model lister.
type defaultLister struct{}

func (defaultLister) ListPlayingChannels(ctx context.Context) (gbmodels.GbChannelList, error) {
	return gbmodels.ListPlayingChannels(ctx)
}
