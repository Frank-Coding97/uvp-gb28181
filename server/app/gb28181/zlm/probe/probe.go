// Package probe 进程启动时对 ZLM 节点做一次主动探活,治"重启后 30 秒点播黑洞"。
//
// 背景见 spec:wiki/projects/uvp/specs/zlm-startup-probe.md
//
// 时序:
//
//	setupZLMRegistry LoadAll 完成 (state 继承 DB)
//	    ↓
//	probe.Run 并发对每节点跑 GetServerConfig (3s 超时)
//	    ↓
//	成功 → registry.MarkActive(id) → State=active + LastHeartbeatAt=now + 落 DB
//	失败 → 只 warn,不改状态(下次 ZLM 心跳到达时 Collector 会自愈)
//
// 独立包好处:HTTP + State 翻转的组合逻辑单独可测,不跟 heartbeat 心跳语义混。
package probe

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// Client probe 只需要一个探活能力,不依赖 zlm.Client 具体类型(避免循环 + 便于打桩)
type Client interface {
	GetServerConfig(ctx context.Context) (map[string]string, error)
}

// ClientFactory 从节点造 Client(生产:zlm.NewClientForNode 适配;测试:直接返回 fake)
type ClientFactory func(n *node.Node) Client

// Registry probe 只依赖 List + MarkActive(便于打桩,同时避免直接依赖具体 *node.Registry)
type Registry interface {
	List() []*node.Node
	MarkActive(ctx context.Context, id int64) error
}

// Prober 启动探活器
type Prober struct {
	reg     Registry
	factory ClientFactory
	timeout time.Duration
	logger  *zap.Logger
}

// Result 单节点探活结果(供测试断言 + summary 聚合)
type Result struct {
	NodeID       int64
	Name         string
	Host         string
	StateBefore  node.State
	StateAfter   node.State
	Pass         bool
	Skipped      bool // maintenance 节点
	DurationMS   int64
	Err          error
}

// New 构造 Prober
//
//   - reg:      节点注册表(需要 List + MarkActive)
//   - factory:  节点 → Client 的构造函数
//   - timeout:  单节点单次探活超时(建议 3s)
//   - logger:   可为 nil(内部 nop 化)
func New(reg Registry, factory ClientFactory, timeout time.Duration, logger *zap.Logger) *Prober {
	if logger == nil {
		logger = zap.NewNop()
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &Prober{reg: reg, factory: factory, timeout: timeout, logger: logger.Named("zlm.probe")}
}

// Run 并发对所有节点跑一次探活。maintenance 节点直接跳过。
//
// 返回按节点顺序的 Result 列表 + pass / fail / skipped 计数。ctx 取消时未开始的节点标 Err=ctx.Err(),已开始的等自己超时结束。
//
// 幂等安全:重复调用只会重复探活,MarkActive 幂等,不产生副作用。
func (p *Prober) Run(ctx context.Context) []Result {
	nodes := p.reg.List()
	if len(nodes) == 0 {
		p.logger.Info("GB28181 ZLM 启动探活 skip:注册表为空", zap.String("event", "zlm.probe.empty"))
		return nil
	}

	results := make([]Result, len(nodes))
	var wg sync.WaitGroup
	var passCount, failCount, skipCount int64

	for i, n := range nodes {
		results[i] = Result{
			NodeID:      n.ID,
			Name:        n.Name,
			Host:        n.Host,
			StateBefore: n.State,
			StateAfter:  n.State,
		}
		if n.State == node.StateMaintenance {
			results[i].Skipped = true
			atomic.AddInt64(&skipCount, 1)
			p.logger.Info("GB28181 ZLM 启动探活 skip(maintenance)", zap.String("event", "zlm.probe.maintenance_skipped"),
				zap.Int64("node_id", n.ID), zap.String("name", n.Name))
			continue
		}

		wg.Add(1)
		go func(idx int, target *node.Node) {
			defer wg.Done()
			r := &results[idx]
			start := time.Now()

			cctx, cancel := context.WithTimeout(ctx, p.timeout)
			defer cancel()

			client := p.factory(target)
			_, err := client.GetServerConfig(cctx)
			r.DurationMS = time.Since(start).Milliseconds()

			if err != nil {
				r.Err = err
				atomic.AddInt64(&failCount, 1)
				p.logger.Warn("GB28181 ZLM 启动探活 fail", zap.String("event", "zlm.probe.failed"),
					zap.Int64("node_id", target.ID),
					zap.String("name", target.Name),
					zap.String("host", target.Host),
					zap.Int64("duration_ms", r.DurationMS),
					zap.String("state_before", string(r.StateBefore)),
					zap.Error(err))
				return
			}

			// 探活通过:翻 State 到 active + 刷 LastHeartbeatAt + 落库
			if err := p.reg.MarkActive(ctx, target.ID); err != nil {
				// 内存翻了但 DB 没写成,记 warn,不算 fail(下次心跳到时 Watcher 也不会误伤)
				p.logger.Warn("GB28181 ZLM 启动探活 pass 但 MarkActive 失败", zap.String("event", "zlm.probe.activation_failed"),
					zap.Int64("node_id", target.ID),
					zap.String("name", target.Name),
					zap.Error(err))
			}
			r.Pass = true
			r.StateAfter = node.StateActive
			atomic.AddInt64(&passCount, 1)

			if r.StateBefore == node.StateOffline {
				p.logger.Info("GB28181 ZLM 节点启动探活翻转 offline→active", zap.String("event", "zlm.probe.node_active"),
					zap.Int64("node_id", target.ID),
					zap.String("name", target.Name),
					zap.String("host", target.Host),
					zap.Int64("duration_ms", r.DurationMS))
			} else {
				p.logger.Info("GB28181 ZLM 启动探活 pass", zap.String("event", "zlm.probe.passed"),
					zap.Int64("node_id", target.ID),
					zap.String("name", target.Name),
					zap.String("host", target.Host),
					zap.Int64("duration_ms", r.DurationMS),
					zap.String("state_before", string(r.StateBefore)))
			}
		}(i, n)
	}
	wg.Wait()

	p.logger.Info("GB28181 ZLM 启动探活完成", zap.String("event", "zlm.probe.completed"),
		zap.Int("total", len(nodes)),
		zap.Int64("pass", atomic.LoadInt64(&passCount)),
		zap.Int64("fail", atomic.LoadInt64(&failCount)),
		zap.Int64("skipped", atomic.LoadInt64(&skipCount)))
	return results
}
