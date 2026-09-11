package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
)

func setupSchedulerLogRepoDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newSQLiteBaselineRepoDB(t)
}

func TestSchedulerLogRepoT13_ListFilteredUsesPolicyAliasAndTypedPredicates(t *testing.T) {
	db := setupSchedulerLogRepoDB(t)
	r := repo.NewGormSchedulerLogRepo(db)
	ctx := context.Background()
	base := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	for _, row := range []repo.SchedulerLogRow{
		{ID: 1, HappenedAt: base, Algorithm: "roundrobin", NodeID: 7, StreamID: "stream-1"},
		{ID: 2, HappenedAt: base.Add(time.Minute), Algorithm: "weighted", NodeID: 7, StreamID: "stream-1", ErrorMessage: "failed"},
		{ID: 3, HappenedAt: base.Add(2 * time.Minute), Algorithm: "weighted", NodeID: 8, StreamID: "stream-2"},
	} {
		require.NoError(t, r.Insert(ctx, row))
	}

	nodeID := int64(7)
	rows, err := r.ListFiltered(ctx, repo.SchedulerLogFilter{
		From: &base, To: timePtr(base.Add(90 * time.Second)), NodeID: &nodeID,
		Policy: "weighted", Result: "error", StreamID: "stream-1", Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(2), rows[0].ID)
}

func timePtr(value time.Time) *time.Time { return &value }
