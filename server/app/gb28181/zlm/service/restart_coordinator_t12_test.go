package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

func newT12Coordinator(t *testing.T, timeout time.Duration, converge func(context.Context, int64) error) (*service.RestartCoordinator, *node.Node) {
	t.Helper()
	reg := node.NewRegistry(newMemoryRepo())
	n := t13Node(t, reg)
	coordinator := service.NewRestartCoordinator(reg, timeout)
	coordinator.SetConverger(converge)
	return coordinator, n
}

func advanceT12Coordinator(t *testing.T, coordinator *service.RestartCoordinator, n *node.Node) service.RestartOperation {
	t.Helper()
	accepted, err := coordinator.Begin(n.ID)
	require.NoError(t, err)
	require.True(t, coordinator.AdvanceToWaitingOffline(n.ID, accepted.Generation))
	require.True(t, coordinator.MarkOfflineForGeneration(n.ID, accepted.Generation))
	require.True(t, coordinator.MarkHeartbeatForGeneration(n.ID, accepted.Generation))
	return accepted
}

func TestRestartT12StopContextReportsDeadlineUntilConvergenceReturns(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseConvergence := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseConvergence)
	coordinator, n := newT12Coordinator(t, time.Second, func(context.Context, int64) error {
		close(started)
		<-release
		return nil
	})
	advanceT12Coordinator(t, coordinator, n)
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	err := coordinator.StopContext(ctx)
	cancel()
	require.ErrorIs(t, err, context.DeadlineExceeded)

	releaseConvergence()
	require.NoError(t, coordinator.StopContext(context.Background()))
}

func TestRestartT12CloseWaitsForAcceptedConvergence(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseConvergence := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseConvergence)
	coordinator, n := newT12Coordinator(t, time.Second, func(context.Context, int64) error {
		close(started)
		<-release
		return nil
	})
	advanceT12Coordinator(t, coordinator, n)
	<-started

	closed := make(chan struct{})
	go func() {
		coordinator.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned before accepted convergence exited")
	case <-time.After(20 * time.Millisecond):
	}

	releaseConvergence()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not return after accepted convergence exited")
	}
	_, err := coordinator.Begin(n.ID)
	require.ErrorIs(t, err, service.ErrRestartCoordinatorClosed)
}

func TestRestartT12AdmissionStopOrdered100Rounds(t *testing.T) {
	for round := 0; round < 100; round++ {
		started := make(chan struct{})
		release := make(chan struct{})
		var releaseOnce sync.Once
		coordinator, n := newT12Coordinator(t, time.Second, func(context.Context, int64) error {
			close(started)
			<-release
			return nil
		})
		advanceT12Coordinator(t, coordinator, n)
		<-started

		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		err := coordinator.StopContext(ctx)
		cancel()
		require.Truef(t, errors.Is(err, context.DeadlineExceeded), "round %d: got %v", round, err)
		releaseOnce.Do(func() { close(release) })
		require.NoError(t, coordinator.StopContext(context.Background()), "round %d", round)
	}
}
