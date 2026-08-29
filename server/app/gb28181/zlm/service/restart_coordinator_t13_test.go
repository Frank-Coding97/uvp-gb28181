package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
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
