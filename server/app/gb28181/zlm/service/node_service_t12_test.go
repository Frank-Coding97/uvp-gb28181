package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

func TestNodeServiceT12StopContextWaitsForScheduledConvergence(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseConvergence := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseConvergence)
	probe := &t13Probe{applyEntered: entered, applyRelease: release}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})

	require.True(t, svc.ScheduleConfigConvergence(n.ID))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("scheduled convergence did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	err := svc.StopContext(ctx)
	cancel()
	require.ErrorIs(t, err, context.DeadlineExceeded)

	releaseConvergence()
	require.NoError(t, svc.StopContext(context.Background()))
	expired, cancelExpired := context.WithCancel(context.Background())
	cancelExpired()
	require.NoError(t, svc.StopContext(expired), "completed stop wins over an already expired context")
	require.False(t, svc.ScheduleConfigConvergence(n.ID))
}

func TestNodeServiceT12StopContextDoesNotBlockBehindSecondSchedule(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseConvergence := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseConvergence)
	probe := &t13Probe{applyEntered: entered, applyRelease: release}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})

	require.True(t, svc.ScheduleConfigConvergence(n.ID))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("scheduled convergence did not start")
	}

	secondStarted := make(chan struct{})
	secondReturned := make(chan bool, 1)
	go func() {
		close(secondStarted)
		secondReturned <- svc.ScheduleConfigConvergence(n.ID)
	}()
	<-secondStarted
	// The first worker owns the per-node lock while the fake ZLM call is
	// blocked; let the second scheduler reach that lock before stopping.
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	stopReturned := make(chan error, 1)
	go func() { stopReturned <- svc.StopContext(ctx) }()
	select {
	case err := <-stopReturned:
		cancel()
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(100 * time.Millisecond):
		cancel()
		releaseConvergence()
		t.Fatal("StopContext blocked behind a scheduler waiting for the node lock")
	}

	releaseConvergence()
	select {
	case <-secondReturned:
	case <-time.After(time.Second):
		t.Fatal("second scheduler did not finish after the first convergence returned")
	}
	require.NoError(t, svc.StopContext(context.Background()))
}
