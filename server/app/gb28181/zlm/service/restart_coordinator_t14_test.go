package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

func TestRestartT14_CloseCancelsConvergenceAndRejectsNewOperations(t *testing.T) {
	reg := node.NewRegistry(newMemoryRepo())
	current, err := reg.Add(context.Background(), node.Node{
		Name: "n1", Host: "zlm", MediaServerUUID: "uuid-1", State: node.StateActive,
	})
	require.NoError(t, err)

	coordinator := service.NewRestartCoordinator(reg, time.Second)
	started := make(chan struct{})
	stopped := make(chan struct{})
	coordinator.SetConverger(func(ctx context.Context, _ int64) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	})

	op, err := coordinator.Begin(current.ID)
	require.NoError(t, err)
	require.True(t, coordinator.AdvanceToWaitingOffline(current.ID, op.Generation))
	coordinator.OnNodeOffline(current.ID)
	coordinator.OnNodeHeartbeat(current.ID)
	<-started

	coordinator.Close()
	coordinator.Close()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("restart convergence did not observe coordinator close")
	}

	_, err = coordinator.Begin(current.ID)
	require.ErrorIs(t, err, service.ErrRestartCoordinatorClosed)
	snapshot, ok := coordinator.Get(current.ID)
	require.True(t, ok)
	require.Equal(t, service.RestartStatusConverging, snapshot.Status,
		"shutdown must not publish a synthetic failed/ready terminal state")
}

func TestRestartT14_CloseStopsPendingTimeoutCallback(t *testing.T) {
	reg := node.NewRegistry(newMemoryRepo())
	current, err := reg.Add(context.Background(), node.Node{
		Name: "n1", Host: "zlm", MediaServerUUID: "uuid-1", State: node.StateActive,
	})
	require.NoError(t, err)

	coordinator := service.NewRestartCoordinator(reg, 20*time.Millisecond)
	op, err := coordinator.Begin(current.ID)
	require.NoError(t, err)
	coordinator.Close()
	time.Sleep(50 * time.Millisecond)

	snapshot, ok := coordinator.Get(current.ID)
	require.True(t, ok)
	require.Equal(t, op.OperationID, snapshot.OperationID)
	require.Equal(t, service.RestartStatusAccepted, snapshot.Status,
		"a stopped process must not run a stale timeout callback")
}
