package heartbeat

import (
	"context"
	"time"

	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// ThreadLoadFetcher 从 ZLM 拉 NetThread / WorkThread 负载(0-1)
//
// 由 zlm.Client 实现(每节点一个临时 Client,避免持连接池);测试用 mock。
type ThreadLoadFetcher interface {
	GetThreadsLoad(ctx context.Context, n *node.Node) (float64, error)
	GetWorkThreadsLoad(ctx context.Context, n *node.Node) (float64, error)
}

// ThreadLoadPoller 周期主动拉所有 active 节点的线程负载,写 Registry.Stats
//
// 为什么主动拉而非 keepalive 解析:ZLM on_server_keepalive payload **不含**
// NetThreadLoad / WorkThreadLoad 字段,只能调 REST `/index/api/getThreadsLoad`
// + `/getWorkThreadsLoad` 拿。Poller 跟 Watcher 同频率(30s)运行。
type ThreadLoadPoller struct {
	registry *node.Registry
	fetcher  ThreadLoadFetcher
	interval time.Duration
}

// NewThreadLoadPoller 构造
func NewThreadLoadPoller(reg *node.Registry, fetcher ThreadLoadFetcher, interval time.Duration) *ThreadLoadPoller {
	return &ThreadLoadPoller{registry: reg, fetcher: fetcher, interval: interval}
}

// Tick 一次轮询:并发拉所有 active 节点的 2 个负载,写回 Stats
func (p *ThreadLoadPoller) Tick(ctx context.Context) {
	active := p.registry.ListActive()
	for _, n := range active {
		nCopy := n
		go p.fetchOne(ctx, nCopy)
	}
}

func (p *ThreadLoadPoller) fetchOne(ctx context.Context, n *node.Node) {
	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	netLoad, errNet := p.fetcher.GetThreadsLoad(fetchCtx, n)
	workLoad, errWork := p.fetcher.GetWorkThreadsLoad(fetchCtx, n)
	if errNet != nil || errWork != nil {
		if app.ZapLog != nil {
			app.ZapLog.Debug("GB28181 ZLM 拉线程负载失败(可能 ZLM 不可达)",
				zap.Int64("nodeId", n.ID),
				zap.String("uuid", n.MediaServerUUID),
				zap.NamedError("netErr", errNet),
				zap.NamedError("workErr", errWork))
		}
		return
	}
	// 不覆盖 Collector 已经写入的 MediaSource/Session/LastHeartbeatAt,只写线程负载
	cur, ok := p.registry.GetByUUID(n.MediaServerUUID)
	if !ok {
		return
	}
	stats := cur.Stats
	stats.NetThreadLoadAvg = netLoad
	stats.WorkThreadLoadAvg = workLoad
	p.registry.UpdateStats(n.MediaServerUUID, stats)
}

// Start 启动 goroutine,周期跑 Tick;ctx 取消 → 退出
func (p *ThreadLoadPoller) Start(ctx context.Context) {
	go func() {
		// 启动 5s 后立即跑一次(让 UI 不用等 30s 才看到负载值)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			p.Tick(ctx)
		}
		tk := time.NewTicker(p.interval)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				p.Tick(ctx)
			}
		}
	}()
}
