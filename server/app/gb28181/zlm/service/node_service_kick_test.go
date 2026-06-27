package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

// kickProbe 扩展 mockProbe,加 KickSessions / RestartServer 记录
//
// 单独再写一份避免污染 node_service_test.go 中的 mockProbe(老用例不需要新方法)
type kickProbe struct {
	mockProbe

	kickReturn  int
	kickErr     error
	kickCalls   int

	restartErr      error
	restartCalls    int
	lastRestartGrace int
}

func (p *kickProbe) KickSessions(_ context.Context, _ *node.Node) (int, error) {
	p.kickCalls++
	if p.kickErr != nil {
		return 0, p.kickErr
	}
	return p.kickReturn, nil
}

func (p *kickProbe) RestartServer(_ context.Context, _ *node.Node, graceMS int) error {
	p.restartCalls++
	p.lastRestartGrace = graceMS
	return p.restartErr
}

func newSvcWithKickProbe(repo *memoryRepo, probe *kickProbe) *service.NodeService {
	reg := node.NewRegistry(repo)
	return service.NewNodeService(reg, probe, service.MediaTuning{})
}

// TestNodeService_KickAllSessions_ReturnsCount T3.5-R: KickAllSessions 把 client 返回的 count 透传出去
func TestNodeService_KickAllSessions_ReturnsCount(t *testing.T) {
	repo := newMemoryRepo()
	probe := &kickProbe{kickReturn: 50}
	svc := newSvcWithKickProbe(repo, probe)

	n, err := svc.Create(context.Background(), service.CreateNodeReq{
		Name: "n1", Host: "1.2.3.4", APIPort: 18080, APISecret: "s",
	})
	require.NoError(t, err)

	count, err := svc.KickAllSessions(context.Background(), n.ID)
	require.NoError(t, err)
	require.Equal(t, 50, count)
	require.Equal(t, 1, probe.kickCalls)
}

// TestNodeService_KickAllSessions_NodeNotFound T3.5-R: 节点不存在应该返 ErrNodeNotFound
func TestNodeService_KickAllSessions_NodeNotFound(t *testing.T) {
	repo := newMemoryRepo()
	probe := &kickProbe{}
	svc := newSvcWithKickProbe(repo, probe)

	_, err := svc.KickAllSessions(context.Background(), 9999)
	require.ErrorIs(t, err, service.ErrNodeNotFound)
	require.Equal(t, 0, probe.kickCalls, "节点不存在不应触达 probe")
}

// TestNodeService_KickAllSessions_ProbeError T3.5-R: probe 报错应透传
func TestNodeService_KickAllSessions_ProbeError(t *testing.T) {
	repo := newMemoryRepo()
	probe := &kickProbe{kickErr: errors.New("zlm down")}
	svc := newSvcWithKickProbe(repo, probe)

	n, _ := svc.Create(context.Background(), service.CreateNodeReq{
		Name: "n1", Host: "1.2.3.4", APIPort: 18080, APISecret: "s",
	})

	_, err := svc.KickAllSessions(context.Background(), n.ID)
	require.Error(t, err)
}

// TestNodeService_Restart_PassesGraceMS T3.5-R: graceMS 参数透传给 probe
func TestNodeService_Restart_PassesGraceMS(t *testing.T) {
	repo := newMemoryRepo()
	probe := &kickProbe{}
	svc := newSvcWithKickProbe(repo, probe)

	n, _ := svc.Create(context.Background(), service.CreateNodeReq{
		Name: "n1", Host: "1.2.3.4", APIPort: 18080, APISecret: "s",
	})

	require.NoError(t, svc.Restart(context.Background(), n.ID, 5000))
	require.Equal(t, 1, probe.restartCalls)
	require.Equal(t, 5000, probe.lastRestartGrace)
}

// TestNodeService_Restart_NodeNotFound T3.5-R: 节点不存在 ErrNodeNotFound
func TestNodeService_Restart_NodeNotFound(t *testing.T) {
	repo := newMemoryRepo()
	probe := &kickProbe{}
	svc := newSvcWithKickProbe(repo, probe)

	err := svc.Restart(context.Background(), 9999, 0)
	require.ErrorIs(t, err, service.ErrNodeNotFound)
	require.Equal(t, 0, probe.restartCalls)
}
