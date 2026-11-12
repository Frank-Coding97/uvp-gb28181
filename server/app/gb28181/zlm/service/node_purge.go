package service

import (
	"context"
	"errors"

	"go.uber.org/zap"
)

// ErrNodeReachable 节点仍可读到影响数据时拒绝强制移除。
//
// 这不是一个「稍后再试」的错误:读成功说明节点还活着,
// 必须回到「普通删除 → 自动停用 → 影响排空后重试」的正常删除链路。
var ErrNodeReachable = errors.New("node is reachable; use the normal delete flow")

// ErrNodeReferenceCleanerUnavailable 未装配引用清理器时拒绝强制移除。
// 不清引用就直接删节点会留下 gb_device.zlm_node_id 悬空指向不存在的节点,
// 所以这里 fail-close,宁可不删也不留脏引用。
var ErrNodeReferenceCleanerUnavailable = errors.New("node reference cleaner unavailable")

// NodeReferenceCleaner 在删除节点前摘掉其它表对它的引用,返回被处理的行数。
type NodeReferenceCleaner func(ctx context.Context, nodeID int64) (int64, error)

// NodePurgeResult 强制移除的结果摘要(用于审计与回显,不含敏感字段)。
type NodePurgeResult struct {
	NodeID          int64 `json:"nodeId"`
	DetachedRows    int64 `json:"detachedRows"`
	ImpactUncertain bool  `json:"impactUncertain"`
}

// PurgeUnreachable 删除一台**影响预检读不到**的节点旁路。
//
// 存在的唯一理由:普通删除的 PreflightNodeImpact 要实时读该节点的
// getMediaList/getAllSession。节点不可达时预检失败,记录在 UI 上无法删除。
//
// 关键安全属性:本方法**只能**删确实够不着的节点。探针一旦读取成功就立即返回
// ErrNodeReachable 并拒绝执行,force 因此不会退化成绕过普通删除影响确认的后门。
func (s *NodeService) PurgeUnreachable(ctx context.Context, id int64) (NodePurgeResult, error) {
	lock := s.nodeLock(id)
	lock.Lock()
	defer lock.Unlock()

	cur, ok := s.registry.Get(id)
	if !ok {
		return NodePurgeResult{}, ErrNodeNotFound
	}
	provider := s.nodeImpactProvider()
	if provider == nil {
		return NodePurgeResult{}, ErrNodeImpactProviderUnavailable
	}
	if _, err := provider.ReadNodeImpact(ctx, cloneNode(cur)); err == nil {
		return NodePurgeResult{}, ErrNodeReachable
	}

	s.cleanerMu.RLock()
	cleaner := s.referenceCleaner
	s.cleanerMu.RUnlock()
	if cleaner == nil {
		return NodePurgeResult{}, ErrNodeReferenceCleanerUnavailable
	}
	detached, err := cleaner(ctx, id)
	if err != nil {
		return NodePurgeResult{}, err
	}
	if err := s.registry.Delete(ctx, id); err != nil {
		return NodePurgeResult{}, err
	}
	// 这条路径上节点当下**够不着**，所以解约多半会失败 —— 但凭据恰恰在这里最值钱：
	// 对端一旦恢复回调，热路径就能用这份凭据把残留关掉（见 retired_node.go）。
	s.retireDeletedNode(ctx, cur, retireReasonPurgeUnreachable)
	if s.logger != nil {
		s.logger.Named("purge").Warn("强制移除不可达的 ZLM 节点",
			zap.String("event", "zlm.node.purged_unreachable"),
			zap.Int64("node_id", id), zap.Int64("detached_rows", detached))
	}
	return NodePurgeResult{NodeID: id, DetachedRows: detached, ImpactUncertain: true}, nil
}

// SetNodeReferenceCleaner 注入引用清理器(装配在 bootstrap,service 本身不碰 DB)。
func (s *NodeService) SetNodeReferenceCleaner(cleaner NodeReferenceCleaner) {
	s.cleanerMu.Lock()
	defer s.cleanerMu.Unlock()
	s.referenceCleaner = cleaner
}
