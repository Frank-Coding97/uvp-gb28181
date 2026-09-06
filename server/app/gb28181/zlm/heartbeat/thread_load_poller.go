package heartbeat

import (
	"context"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
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
	netKey := logging.RepeatKey{Component: "zlm", Event: threadLoadNetFailedEvent, NodeID: n.ID}
	if errNet != nil {
		repeatFailure(fetchCtx, netKey, errNet)
	} else {
		repeatRecovered(netKey)
	}
	workLoad, errWork := p.fetcher.GetWorkThreadsLoad(fetchCtx, n)
	workKey := logging.RepeatKey{Component: "zlm", Event: threadLoadWorkFailedEvent, NodeID: n.ID}
	if errWork != nil {
		repeatFailure(fetchCtx, workKey, errWork)
	} else {
		repeatRecovered(workKey)
	}
	if errNet != nil || errWork != nil {
		return
	}
	// 锁内字段级更新:与 Collector 的心跳字段互不覆盖
	p.registry.UpdateLoadFields(n.MediaServerUUID, netLoad, workLoad)
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
