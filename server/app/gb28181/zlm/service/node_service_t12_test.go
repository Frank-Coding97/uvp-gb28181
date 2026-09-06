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
	require.False(t, svc.ScheduleConfigConvergence(n.ID))
}
