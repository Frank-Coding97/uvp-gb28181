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

func newRestartShutdownFixture(t *testing.T, converge func(context.Context, int64) error) (*service.RestartCoordinator, int64) {
	t.Helper()
	repo := newMemoryRepo()
	reg := node.NewRegistry(repo)
	current, err := reg.Add(context.Background(), node.Node{
		Name: "n1", Host: "zlm", MediaServerUUID: "uuid-1", State: node.StateActive,
	})
	require.NoError(t, err)
	coordinator := service.NewRestartCoordinator(reg, time.Second)
	coordinator.SetConverger(converge)
	operation, err := coordinator.Begin(current.ID)
	require.NoError(t, err)
	require.True(t, coordinator.AdvanceToWaitingOffline(current.ID, operation.Generation))
	coordinator.OnNodeOffline(current.ID)
	coordinator.OnNodeHeartbeat(current.ID)
	return coordinator, current.ID
}

func TestRestartCoordinatorShutdownWaitsForAcceptedConvergenceAndClosesAdmission(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	coordinator, nodeID := newRestartShutdownFixture(t, func(context.Context, int64) error {
		close(started)
		<-release
		return nil
	})
	<-started

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- coordinator.Shutdown(context.Background()) }()
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned before accepted convergence released: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	close(release)
	require.NoError(t, <-shutdownDone)
	require.NoError(t, coordinator.Shutdown(context.Background()), "repeated shutdown must be idempotent")

	_, err := coordinator.Begin(nodeID)
	require.ErrorIs(t, err, service.ErrRestartCoordinatorClosed)
}

func TestRestartCoordinatorShutdownDeadlineThenCompletion(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	coordinator, _ := newRestartShutdownFixture(t, func(context.Context, int64) error {
		close(started)
		<-release
		return errors.New("released convergence failure")
	})
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, coordinator.Shutdown(ctx), context.DeadlineExceeded)

	close(release)
	require.NoError(t, coordinator.Shutdown(context.Background()))
}
