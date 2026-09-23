package scheduler_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
)

func TestSchedulerLogT13_ListFilteredDerivesResultFromErrorMessage(t *testing.T) {
	repo := newFakeSchedulerLogRepo()
	base := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	repo.rows = []scheduler.SchedulerLog{
		{ID: 1, HappenedAt: base, Algorithm: "roundrobin", NodeID: 1, StreamID: "s1"},
		{ID: 2, HappenedAt: base.Add(time.Minute), Algorithm: "roundrobin", NodeID: 1, StreamID: "s2", ErrorMessage: "no active"},
		{ID: 3, HappenedAt: base.Add(2 * time.Minute), Algorithm: "weighted", NodeID: 2, StreamID: "s1"},
	}
	nodeID := int64(1)
	from := base.Add(-time.Second)
	to := base.Add(time.Minute + time.Second)
	svc := scheduler.NewLogService(repo, 10)
	rows, err := svc.ListFiltered(context.Background(), scheduler.SchedulerLogFilter{
		From: &from, To: &to, NodeID: &nodeID, Algorithm: "roundrobin",
		Result: scheduler.SchedulerLogResultSuccess, StreamID: "s1", Limit: 100,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(1), rows[0].ID)
}

func TestSchedulerLogT13_FallbackSortsBeforeApplyingLimit(t *testing.T) {
	base := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	repo := newFakeSchedulerLogRepo()
	// Legacy List implementations are not required to order their output.
	repo.rows = []scheduler.SchedulerLog{
		{ID: 1, HappenedAt: base, Algorithm: "roundrobin"},
		{ID: 3, HappenedAt: base.Add(2 * time.Minute), Algorithm: "roundrobin"},
		{ID: 2, HappenedAt: base.Add(time.Minute), Algorithm: "roundrobin"},
	}
	rows, err := scheduler.NewLogService(repo, 10).ListFiltered(context.Background(), scheduler.SchedulerLogFilter{Limit: 1})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(3), rows[0].ID)
}

func TestSchedulerLogT13_EmitAndStopConcurrentNoPanic(t *testing.T) {
	repo := newFakeSchedulerLogRepo()
	repo.sleepOnInsert = 100 * time.Microsecond
	svc := scheduler.NewLogService(repo, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	svc.Start(ctx)

	const emitters = 32
	const entriesPerEmitter = 1000
	ready := make(chan struct{})
	var readyWG sync.WaitGroup
	var emitWG sync.WaitGroup
	readyWG.Add(emitters)
	emitWG.Add(emitters)
	for emitter := 0; emitter < emitters; emitter++ {
		emitter := emitter
		go func() {
			defer emitWG.Done()
			readyWG.Done()
			<-ready
			for i := 0; i < entriesPerEmitter; i++ {
				svc.Emit(scheduler.SchedulerLog{HappenedAt: time.Now(), Algorithm: "roundrobin", NodeID: int64(emitter)})
			}
		}()
	}
	readyWG.Wait()
	close(ready)
	stopDone := make(chan struct{})
	go func() {
		time.Sleep(time.Millisecond)
		svc.Stop()
		close(stopDone)
	}()
	emitWG.Wait()
	<-stopDone

	// Stop is terminal and idempotent; repeated calls must not resurrect a worker.
	svc.Start(ctx)
	svc.Stop()
	svc.Stop()
}
