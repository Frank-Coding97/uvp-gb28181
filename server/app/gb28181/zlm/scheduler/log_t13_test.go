package scheduler_test

import (
	"context"
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
