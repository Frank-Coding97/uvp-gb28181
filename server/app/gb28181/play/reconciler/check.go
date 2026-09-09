package reconciler

import (
	"context"

	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// judgeResult 单条通道对账判定结果.
type judgeResult int

const (
	judgeOnline     judgeResult = iota // ZLM 说在播,保留
	judgeSkip                          // 节点不可达(Q4)或其他保守跳过场景
	judgeStale                         // 判定为假阳性,应清理
	judgeProbeError                    // 探测过程报错,不清也不算 online(计 Failed)
)

// judgeOne 对单条流做在线判定,实现 spec Q4 / Q5 决策.
//
// 单节点路径(无 registry):直接问唯一 ZLM.
//
// 多节点路径 - LocationMap 命中:
//   - 找到 node -> node 不 active(Q4) -> judgeSkip
//   - 找到 node -> node active -> IsMediaOnline
//   - registry 里查不到 node(节点已删)-> fallback 到跨节点遍历
//
// 多节点路径 - LocationMap miss(进程重启后 in-memory 丢失):
//   - Q5 决策:遍历所有 active 节点,任一说 online 就保留.
//   - 遍历中单节点报错不算整体失败,继续.
//   - 全部 offline 才判定为假阳性.
func (r *Reconciler) judgeOne(ctx context.Context, streamID string) judgeResult {
	// 单节点路径:registry / locationMap 都没配
	if r.registry == nil || r.locationMap == nil {
		return r.judgeSingleNode(ctx, streamID)
	}

	// 多节点路径 - LocationMap 命中
	if nodeID, ok := r.locationMap.Lookup(streamID); ok {
		n, exists := r.registry.Get(nodeID)
		if !exists {
			app.Log(ctx).Named("play.reconcile").Debug("reconciler LocationMap 命中但 registry 无此 node,fallback 跨节点",
				zap.String("event", "play.reconcile.location_fallback"),
				zap.String("streamID", streamID),
				zap.Int64("nodeID", nodeID))
			return r.judgeAcrossAllNodes(ctx, streamID)
		}
		if !n.IsActive() {
			// Q4: 节点不可达 -> 跳过等恢复,不清也不算 online
			app.Log(ctx).Named("play.reconcile").Debug("reconciler 节点非 active 状态,跳过",
				zap.String("event", "play.reconcile.node_inactive"),
				zap.String("streamID", streamID),
				zap.Int64("nodeID", nodeID),
				zap.String("state", string(n.State)))
			return judgeSkip
		}
		online, err := r.probe.IsMediaOnline(ctx, n, streamID)
		if err != nil {
			app.Log(ctx).Named("play.reconcile").Warn("reconciler 探测流状态失败",
				zap.String("event", "play.reconcile.probe_failed"),
				zap.String("streamID", streamID),
				zap.Int64("nodeID", nodeID),
				zap.Error(err))
			return judgeProbeError
		}
		if online {
			return judgeOnline
		}
		return judgeStale
	}

	// LocationMap miss -> Q5: 跨所有节点遍历
	return r.judgeAcrossAllNodes(ctx, streamID)
}

// judgeSingleNode 单节点场景.
// 单节点场景下没有 registry,只有一个 ZLM,直接调 probe.
// probe.IsMediaOnline 的 n 参数在单节点场景可能为 nil,defaultProbeImpl 会兜底走单节点 client.
func (r *Reconciler) judgeSingleNode(ctx context.Context, streamID string) judgeResult {
	online, err := r.probe.IsMediaOnline(ctx, nil, streamID)
	if err != nil {
		app.Log(ctx).Named("play.reconcile").Warn("reconciler 单节点探测流状态失败",
			zap.String("event", "play.reconcile.probe_failed"),
			zap.String("streamID", streamID),
			zap.Error(err))
		return judgeProbeError
	}
	if online {
		return judgeOnline
	}
	return judgeStale
}

// judgeAcrossAllNodes 遍历所有 active 节点查这条流(Q5 决策).
// 任一节点说 online -> judgeOnline.
// 全部说 offline 或全部报错 -> judgeStale(保守清).
// 至少一个节点报错 + 其他都说 offline -> judgeProbeError(不清,等下轮).
func (r *Reconciler) judgeAcrossAllNodes(ctx context.Context, streamID string) judgeResult {
	nodes := r.registry.List()
	if len(nodes) == 0 {
		app.Log(ctx).Named("play.reconcile").Warn("reconciler 无任何 registered node,判定假阳性",
			zap.String("event", "play.reconcile.no_nodes"),
			zap.String("streamID", streamID))
		return judgeStale
	}
	anyProbed := false
	anyError := false
	for _, n := range nodes {
		if !n.IsActive() {
			continue
		}
		online, err := r.probe.IsMediaOnline(ctx, n, streamID)
		if err != nil {
			app.Log(ctx).Named("play.reconcile").Debug("reconciler 跨节点探测单个 node 失败,继续下一个",
				zap.String("event", "play.reconcile.probe_failed"),
				zap.String("streamID", streamID),
				zap.Int64("nodeID", n.ID),
				zap.Error(err))
			anyError = true
			continue
		}
		anyProbed = true
		if online {
			app.Log(ctx).Named("play.reconcile").Debug("reconciler LocationMap miss 但在跨节点遍历命中",
				zap.String("event", "play.reconcile.cross_node_hit"),
				zap.String("streamID", streamID),
				zap.Int64("nodeID", n.ID))
			return judgeOnline
		}
	}
	// 没有一个 active 节点被成功探测 -> 结果不确定,保守判 error 不清
	if !anyProbed && anyError {
		return judgeProbeError
	}
	// 没有 active 节点(全部 inactive) -> 保守跳过等恢复
	if !anyProbed && !anyError {
		return judgeSkip
	}
	// 至少一个节点成功探测且都说 offline -> 判定假阳性
	return judgeStale
}
