package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

type blockingUpdateRepo struct {
	*memoryRepo
	entered chan struct{}
	release chan struct{}
}

func (r *blockingUpdateRepo) Update(ctx context.Context, n node.Node) error {
	close(r.entered)
	<-r.release
	return r.memoryRepo.Update(ctx, n)
}

func TestNodeServiceShutdownWaitsForScheduledConvergenceAndClosesAdmission(t *testing.T) {
	repo := newMemoryRepo()
	reg := node.NewRegistry(repo)
	current, err := reg.Add(context.Background(), node.Node{
		Name: "n1", Host: "zlm", MediaServerUUID: "uuid-1", State: node.StateActive,
	})
	require.NoError(t, err)
	probe := &blockingApplyProbe{entered: make(chan struct{}), release: make(chan struct{})}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})

	require.True(t, svc.ScheduleConfigConvergence(current.ID))
	<-probe.entered

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- svc.Shutdown(context.Background()) }()
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned before accepted convergence released: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	close(probe.release)
	require.NoError(t, <-shutdownDone)
	require.NoError(t, svc.Shutdown(context.Background()), "repeated shutdown must be idempotent")

	later, err := reg.Add(context.Background(), node.Node{
		Name: "n2", Host: "zlm", MediaServerUUID: "uuid-2", State: node.StateActive,
	})
	require.NoError(t, err)
	require.False(t, svc.ScheduleConfigConvergence(later.ID), "closed service must reject new background convergence")
}

func TestNodeServiceShutdownWaitsForApplyActiveConfigsDispatch(t *testing.T) {
	repo := newMemoryRepo()
	reg := node.NewRegistry(repo)
	_, err := reg.Add(context.Background(), node.Node{
		Name: "n1", Host: "zlm", MediaServerUUID: "uuid-1", State: node.StateActive,
	})
	require.NoError(t, err)
	probe := &blockingApplyProbe{entered: make(chan struct{}), release: make(chan struct{})}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})

	applyDone := make(chan []service.ConfigApplyResult, 1)
	go func() { applyDone <- svc.ApplyActiveConfigs(context.Background()) }()
	<-probe.entered

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- svc.Shutdown(context.Background()) }()
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned while ApplyActiveConfigs was still running: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	close(probe.release)
	results := <-applyDone
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	require.NoError(t, <-shutdownDone)
}

func TestNodeServiceShutdownWaitsForRegistryPersistence(t *testing.T) {
	repo := &blockingUpdateRepo{
		memoryRepo: newMemoryRepo(),
		entered:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	reg := node.NewRegistry(repo)
	_, err := reg.Add(context.Background(), node.Node{
		Name: "n1", Host: "zlm", MediaServerUUID: "uuid-1", State: node.StateActive,
		RecoveryRequired: true, RecoveryReason: "external state uncertain",
	})
	require.NoError(t, err)
	svc := service.NewNodeService(reg, &mockProbe{}, service.MediaTuning{})

	applyDone := make(chan []service.ConfigApplyResult, 1)
	go func() { applyDone <- svc.ApplyActiveConfigs(context.Background()) }()
	<-repo.entered

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- svc.Shutdown(context.Background()) }()
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned while registry persistence was still running: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	close(repo.release)
	results := <-applyDone
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	require.NoError(t, <-shutdownDone)
}

func TestNodeServiceShutdownDeadlineThenCompletion(t *testing.T) {
	repo := newMemoryRepo()
	reg := node.NewRegistry(repo)
	current, err := reg.Add(context.Background(), node.Node{
		Name: "n1", Host: "zlm", MediaServerUUID: "uuid-1", State: node.StateActive,
	})
	require.NoError(t, err)
	probe := &blockingApplyProbe{entered: make(chan struct{}), release: make(chan struct{})}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	require.True(t, svc.ScheduleConfigConvergence(current.ID))
	<-probe.entered

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, svc.Shutdown(ctx), context.DeadlineExceeded)

	close(probe.release)
	require.NoError(t, svc.Shutdown(context.Background()))
	require.NoError(t, svc.Shutdown(context.Background()), "shutdown after a deadline must remain repeatable")
}
