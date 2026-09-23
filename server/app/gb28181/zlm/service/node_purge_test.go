package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

// purgeImpactProvider 可读 / 不可达两态可切的影响探针
type purgeImpactProvider struct {
	impact service.NodeImpact
	err    error
	calls  int
	seen   []int64
}

func (p *purgeImpactProvider) ReadNodeImpact(_ context.Context, n *node.Node) (service.NodeImpact, error) {
	p.calls++
	if n != nil {
		p.seen = append(p.seen, n.ID)
	}
	if p.err != nil {
		return service.NodeImpact{}, p.err
	}
	return p.impact, nil
}

func reachableImpactProvider() *purgeImpactProvider {
	return &purgeImpactProvider{impact: service.NodeImpact{EvidenceFingerprint: "sha256-reachable"}}
}

func unreachableImpactProvider() *purgeImpactProvider {
	return &purgeImpactProvider{err: errors.New("dial tcp: connect: connection refused")}
}

// TestPurgeUnreachable_DeletesOnlyWhenTheNodeCannotBeRead 是这条旁路的存在理由:
// 不可达节点原本既删不掉也转不了维护态(两条路共用同一个影响预检)。
func TestPurgeUnreachable_DeletesOnlyWhenTheNodeCannotBeRead(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	provider := unreachableImpactProvider()
	svc := service.NewNodeService(reg, &t13Probe{}, service.MediaTuning{})
	svc.SetNodeImpactProvider(provider)

	cleaned := make([]int64, 0, 1)
	svc.SetNodeReferenceCleaner(func(_ context.Context, nodeID int64) (int64, error) {
		cleaned = append(cleaned, nodeID)
		return 4, nil
	})

	result, err := svc.PurgeUnreachable(context.Background(), n.ID)
	require.NoError(t, err)
	require.Equal(t, n.ID, result.NodeID)
	require.Equal(t, int64(4), result.DetachedRows)
	// 读失败不等于「没有影响」,这一点必须显式传给调用方。
	require.True(t, result.ImpactUncertain)

	require.Equal(t, []int64{n.ID}, cleaned)
	_, stillThere := reg.Get(n.ID)
	require.False(t, stillThere, "节点行应已删除")
}

// TestPurgeUnreachable_RefusesWhenTheNodeIsReachable 锁死最关键的安全属性:
// force 只允许用于确实够不着的节点,绝不能变成绕过「转维护态 + 影响为空」的后门。
func TestPurgeUnreachable_RefusesWhenTheNodeIsReachable(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	svc := service.NewNodeService(reg, &t13Probe{}, service.MediaTuning{})
	svc.SetNodeImpactProvider(reachableImpactProvider())

	cleanerCalls := 0
	svc.SetNodeReferenceCleaner(func(context.Context, int64) (int64, error) {
		cleanerCalls++
		return 0, nil
	})

	// 即使节点上还有活跃的媒体/会话,这里也必须因为「可达」被拒,而不是因为影响非空。
	_, err := svc.PurgeUnreachable(context.Background(), n.ID)
	require.ErrorIs(t, err, service.ErrNodeReachable)
	require.Equal(t, 0, cleanerCalls, "节点可达时不许动任何引用")
	_, stillThere := reg.Get(n.ID)
	require.True(t, stillThere, "节点被拒绝后必须还在")
}

// TestPurgeUnreachable_FailsClosedWithoutReferenceCleaner:
// 没有引用清理器就删会留下指向已删节点的 zlm_node_id,宁可不删。
func TestPurgeUnreachable_FailsClosedWithoutReferenceCleaner(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	svc := service.NewNodeService(reg, &t13Probe{}, service.MediaTuning{})
	svc.SetNodeImpactProvider(unreachableImpactProvider())

	_, err := svc.PurgeUnreachable(context.Background(), n.ID)
	require.ErrorIs(t, err, service.ErrNodeReferenceCleanerUnavailable)
	_, stillThere := reg.Get(n.ID)
	require.True(t, stillThere)
}

func TestPurgeUnreachable_RejectsMissingNodeAndMissingProvider(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	svc := service.NewNodeService(reg, &t13Probe{}, service.MediaTuning{})
	svc.SetNodeReferenceCleaner(func(context.Context, int64) (int64, error) { return 0, nil })

	_, err := svc.PurgeUnreachable(context.Background(), 4242)
	require.ErrorIs(t, err, service.ErrNodeNotFound)

	n := t13Node(t, reg)
	_, err = svc.PurgeUnreachable(context.Background(), n.ID)
	require.ErrorIs(t, err, service.ErrNodeImpactProviderUnavailable)
	_, stillThere := reg.Get(n.ID)
	require.True(t, stillThere)
}

// TestPurgeUnreachable_CleanerFailureKeepsTheNode 清理引用失败时留在原地,
// 不能出现「引用已改、节点还在」的半截状态。
func TestPurgeUnreachable_CleanerFailureKeepsTheNode(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	svc := service.NewNodeService(reg, &t13Probe{}, service.MediaTuning{})
	svc.SetNodeImpactProvider(unreachableImpactProvider())
	svc.SetNodeReferenceCleaner(func(context.Context, int64) (int64, error) {
		return 0, errors.New("update gb_device failed")
	})

	_, err := svc.PurgeUnreachable(context.Background(), n.ID)
	require.Error(t, err)
	_, stillThere := reg.Get(n.ID)
	require.True(t, stillThere, "清理失败时节点必须还在")
}
