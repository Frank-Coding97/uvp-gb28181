package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/service"
)

func waitRestartStatus(t *testing.T, svc *service.NodeService, nodeID int64, want service.RestartStatus) service.RestartOperation {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if op, ok := svc.RestartOperation(nodeID); ok && op.Status == want {
			return op
		}
		time.Sleep(time.Millisecond)
	}
	op, _ := svc.RestartOperation(nodeID)
	require.Equal(t, want, op.Status)
	return op
}

func TestRestartT13_OnlyCommandAcceptanceThenHeartbeatLifecycle(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	probe := &t13Probe{}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})

	accepted, err := svc.RestartAccepted(context.Background(), n.ID, 5000)
	require.NoError(t, err)
	require.True(t, accepted.Accepted)
	require.NotEmpty(t, accepted.OperationID)
	require.Equal(t, service.RestartStatusAccepted, accepted.Status)
	got, _ := reg.Get(n.ID)
	require.Equal(t, node.StateActive, got.State, "restart must not use maintenance as a wait state")
	require.True(t, svc.RestartPending(n.ID))
	require.True(t, reg.IsAdmissionBlocked(n.ID), "pending restart is an explicit scheduler gate")

	op, ok := svc.RestartOperation(n.ID)
	require.True(t, ok)
	require.Equal(t, service.RestartStatusWaitingOffline, op.Status)
	require.NotEqual(t, service.RestartStatusReady, op.Status)

	notifier := svc.RestartNotifier()
	notifier.OnNodeOffline(n.ID)
	op = waitRestartStatus(t, svc, n.ID, service.RestartStatusWaitingHeartbeat)
	require.Equal(t, op.Generation, func() uint64 { current, _ := svc.RestartOperation(n.ID); return current.Generation }())
	notifier.OnNodeHeartbeat(n.ID)
	ready := waitRestartStatus(t, svc, n.ID, service.RestartStatusReady)
	require.Equal(t, op.Generation, ready.Generation)
	require.False(t, svc.RestartPending(n.ID))
	require.False(t, reg.IsAdmissionBlocked(n.ID))
	require.True(t, reg.IsAutoOnDemandReady(n.ID))
}

func TestRestartT13_NodeStartedAdvancesWaitingOffline(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	c := service.NewRestartCoordinator(reg, time.Second)

	accepted, err := c.Begin(n.ID)
	require.NoError(t, err)
	require.True(t, c.AdvanceToWaitingOffline(n.ID, accepted.Generation))
	c.OnNodeStarted(n.ID)

	operation, ok := c.Get(n.ID)
	require.True(t, ok)
	require.Equal(t, service.RestartStatusWaitingHeartbeat, operation.Status)
	c.FailGeneration(n.ID, accepted.Generation, errors.New("test cleanup"))
}

// 回归（2026-10-03 真机实测）：ZLM 快速重启时停机窗口只有 27s，短于 Watcher 的
// offlineThreshold(90s，即 3 个 30s tick)，因此 MarkOffline 一次都不会触发；
// 而 on_server_started 这条唯一兜底也可能整条丢失（后端恰好在那一刻重启时，
// ZLM 的 hook 回调只会被 connection refused）。结果是：重启明明成功了，操作却一直
// 停在 waiting_offline，直到两分钟超时被判 failed —— 现场表现为"节点重启不起来"。
// 修复后，命令下发且过了宽限期之后到达的心跳，本身就是新进程已上线的直接证据。
func TestRestartT13_HeartbeatAdvancesWaitingOfflineForFastRestart(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	c := service.NewRestartCoordinator(reg, 30*time.Second)
	c.SetHeartbeatGrace(0)
	c.SetConverger(func(context.Context, int64) error {
		_ = reg.SetAutoOnDemandReady(n.ID, true)
		return nil
	})

	accepted, err := c.Begin(n.ID)
	require.NoError(t, err)
	require.True(t, c.AdvanceToWaitingOffline(n.ID, accepted.Generation))

	// 既没有 OnNodeOffline，也没有 OnNodeStarted —— 只有心跳。
	c.OnNodeHeartbeat(n.ID)

	require.Eventually(t, func() bool {
		op, ok := c.Get(n.ID)
		return ok && op.Status == service.RestartStatusReady
	}, time.Second, time.Millisecond)
	require.False(t, c.IsPending(n.ID))
	require.False(t, reg.IsAdmissionBlocked(n.ID))
}

// 宽限期内的心跳必须被忽略：restartServer 刚被受理时，旧进程可能还没退干净，
// 这条心跳不能拿来当作"新进程已上线"，否则会在旧进程上跑一次收敛。
func TestRestartT13_HeartbeatInsideGraceWindowDoesNotAdvance(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	c := service.NewRestartCoordinator(reg, 30*time.Second)
	c.SetHeartbeatGrace(time.Hour)

	accepted, err := c.Begin(n.ID)
	require.NoError(t, err)
	require.True(t, c.AdvanceToWaitingOffline(n.ID, accepted.Generation))

	c.OnNodeHeartbeat(n.ID)

	operation, ok := c.Get(n.ID)
	require.True(t, ok)
	require.Equal(t, service.RestartStatusWaitingOffline, operation.Status)
	require.False(t, c.MarkHeartbeatForGeneration(n.ID, accepted.Generation))
	c.FailGeneration(n.ID, accepted.Generation, errors.New("test cleanup"))
}

func TestRestartT13_CommandFailureIsFailedAndNotPending(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	probe := &t13Probe{restartErr: errors.New("restart failed with old-secret")}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})

	_, err := svc.RestartAccepted(context.Background(), n.ID, 0)
	require.Error(t, err)
	require.NotContains(t, err.Error(), n.APISecret)
	op, ok := svc.RestartOperation(n.ID)
	require.True(t, ok)
	require.Equal(t, service.RestartStatusFailed, op.Status)
	require.False(t, svc.RestartPending(n.ID))
	require.True(t, reg.IsAdmissionBlocked(n.ID))
	require.False(t, reg.IsAutoOnDemandReady(n.ID))
}

func TestRestartT13_TimeoutAndGenerationGuard(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	c := service.NewRestartCoordinator(reg, 20*time.Millisecond)
	c.SetConverger(func(context.Context, int64) error { return nil })

	one, err := c.Begin(n.ID)
	require.NoError(t, err)
	require.True(t, c.AdvanceToWaitingOffline(n.ID, one.Generation))
	time.Sleep(50 * time.Millisecond)
	failed, ok := c.Get(n.ID)
	require.True(t, ok)
	require.Equal(t, service.RestartStatusFailed, failed.Status)

	two, err := c.Begin(n.ID)
	require.NoError(t, err)
	require.Greater(t, two.Generation, one.Generation)
	require.False(t, c.MarkOfflineForGeneration(n.ID, one.Generation), "stale generation cannot advance new operation")
	require.Equal(t, service.RestartStatusAccepted, func() service.RestartStatus { current, _ := c.Get(n.ID); return current.Status }())
	c.FailGeneration(n.ID, two.Generation, errors.New("test cleanup"))
}

func TestRestartT13_EquivalentConvergenceInProgressIsNotFailure(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	c := service.NewRestartCoordinator(reg, time.Second)
	ready := make(chan struct{})
	c.SetConverger(func(context.Context, int64) error {
		<-ready
		return service.ErrConfigConvergenceInProgress
	})
	one, err := c.Begin(n.ID)
	require.NoError(t, err)
	require.True(t, c.AdvanceToWaitingOffline(n.ID, one.Generation))
	require.True(t, c.MarkOfflineForGeneration(n.ID, one.Generation))
	require.True(t, c.MarkHeartbeatForGeneration(n.ID, one.Generation))

	// The generic scheduler owns an equivalent apply. Its readiness arrives
	// before the restart callback returns; the restart must join it rather than
	// turning ErrConfigConvergenceInProgress into failed.
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = reg.SetAutoOnDemandReady(n.ID, true)
		close(ready)
	}()
	require.Eventually(t, func() bool {
		op, ok := c.Get(n.ID)
		return ok && op.Status == service.RestartStatusReady
	}, time.Second, time.Millisecond)
}
