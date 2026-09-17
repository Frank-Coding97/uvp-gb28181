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
//	成功且端点自报的 mediaServerId 就是本节点 UUID → registry.MarkActive(id)
//	                                  → State=active + LastHeartbeatAt=now + 落 DB
//	端点可达但自报身份是别的节点 → 跳过激活(保持原状态,记 WARN)
//	失败 → 只 warn,不改状态(下次 ZLM 心跳到达时 Collector 会自愈)
//
// 为什么探活必须核对身份,而不只是"端口活着":
//
//	同一台 ZLM(host:port)被登记成两行节点时(手工重复添加、改端口后重新添加),
//	两行的 UUID 不同,而 ZLM 的 general.mediaServerId 只可能是其中一行。
//	只按可达性激活,会把"影子行"翻成 active → 调度器把点播/对讲会话绑到影子行 →
//	ZLM 回调里带的是它自己认的 UUID → 反查到的节点与会话预留的节点不是同一个 →
//	节点级鉴权失败(典型:对讲 `talk publish denied` / WHIP 406)。
//	影子行保持 offline 才是自洽状态:它一被调度出去就是坏会话。
//
// 独立包好处:HTTP + State 翻转的组合逻辑单独可测,不跟 heartbeat 心跳语义混。
package probe

import (
	"context"
	"strings"
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

// mediaServerIDKey 端点自报身份的配置键。
//
// 平台在 zlm.ApplyConfigForNode 里把 `node.MediaServerUUID` 写进 ZLM 的
// `general.mediaServerId`,并回读校验 —— 也就是说**同一时刻一台 ZLM 只会认一个
// 节点行的 UUID**。因此它是"这台端点是不是这个节点"的唯一权威答案。
const mediaServerIDKey = "general.mediaServerId"

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
	NodeID      int64
	Name        string
	Host        string
	StateBefore node.State
	StateAfter  node.State
	Pass        bool
	Skipped     bool // 未激活:maintenance 节点,或端点自报身份不是本节点
	DurationMS  int64
	Err         error
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
			config, err := client.GetServerConfig(cctx)
			r.DurationMS = time.Since(start).Milliseconds()

			if err != nil {
				r.Err = err
				atomic.AddInt64(&failCount, 1)
				p.logger.Warn("GB28181 ZLM 启动探活 fail", zap.String("event", "zlm.probe.failed"),
					zap.Int64("node_id", target.ID),
					zap.String("name", target.Name),
					zap.String("endpoint", target.HTTPEndpoint()),
					zap.Int64("duration_ms", r.DurationMS),
					zap.String("state_before", string(r.StateBefore)),
					zap.Error(err))
				return
			}

			// 身份核对:端点自报的 mediaServerId 必须就是本节点登记的 UUID。
			// 只证"端口活着"不够 —— 同一台 ZLM 被登记成两行节点时,它只认其中一行,
			// 激活影子行会让调度器把会话绑到 ZLM 不承认的节点上(见包注释)。
			// 基准或自报值缺失时无从比对,沿用旧行为(激活),避免把可达端点判死。
			nodeUUID := strings.TrimSpace(target.MediaServerUUID)
			reportedUUID := strings.TrimSpace(config[mediaServerIDKey])
			if nodeUUID != "" && reportedUUID != "" && reportedUUID != nodeUUID {
				r.Skipped = true
				atomic.AddInt64(&skipCount, 1)
				p.logger.Warn("GB28181 ZLM 启动探活跳过:端点自报身份不是本节点",
					zap.String("event", "zlm.probe.identity_mismatch"),
					zap.Int64("node_id", target.ID),
					zap.String("name", target.Name),
					zap.String("endpoint", target.HTTPEndpoint()),
					zap.String("node_uuid", nodeUUID),
					zap.String("reported_uuid", reportedUUID),
					zap.String("state_before", string(r.StateBefore)))
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
					zap.String("endpoint", target.HTTPEndpoint()),
					zap.Int64("duration_ms", r.DurationMS))
			} else {
				p.logger.Info("GB28181 ZLM 启动探活 pass", zap.String("event", "zlm.probe.passed"),
					zap.Int64("node_id", target.ID),
					zap.String("name", target.Name),
					zap.String("endpoint", target.HTTPEndpoint()),
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
