package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestDashboardRetentionPrunesOnlyExpiredHistoryAndProtectsStartedAttempts(t *testing.T) {
	db := newRetentionDB(t)
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	old, boundary, recent := now.Add(-31*24*time.Hour), now.Add(-30*24*time.Hour), now.Add(-29*24*time.Hour)
	require.NoError(t, db.Create(&[]gbmodels.GbSipMetricMinute{
		{BucketStart: old, Method: "REGISTER", Direction: "in"}, {BucketStart: boundary, Method: "INVITE", Direction: "out"}, {BucketStart: recent, Method: "MESSAGE", Direction: "in"},
	}).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbSipMetricFlush{
		{FlushID: "old", CreatedAt: old}, {FlushID: "boundary", CreatedAt: boundary}, {FlushID: "recent", CreatedAt: recent},
	}).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbSipMetricGap{
		{StartedAt: old.Add(-time.Hour), EndedAt: old, Reason: "restart"}, {StartedAt: boundary.Add(-time.Hour), EndedAt: boundary, Reason: "restart"},
	}).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbPlayAttempt{
		{CorrelationID: "old-terminal", Outcome: PlayOutcomeSuccess, StartedAt: old, FinishedAt: &old},
		{CorrelationID: "boundary-terminal", Outcome: PlayOutcomeFailure, StartedAt: boundary, FinishedAt: &boundary},
		{CorrelationID: "old-started", Outcome: PlayOutcomeStarted, StartedAt: old},
	}).Error)
	service := NewDashboardRetention(db, 2)
	service.SetClock(func() time.Time { return now })
	result, err := service.Prune(context.Background())
	require.NoError(t, err)
	require.Equal(t, RetentionResult{SIPMinutes: 1, SIPFlushes: 1, SIPGaps: 1, PlayAttempts: 1}, result)

	require.EqualValues(t, 2, countRows(t, db, &gbmodels.GbSipMetricMinute{}))
	require.EqualValues(t, 2, countRows(t, db, &gbmodels.GbSipMetricFlush{}))
	require.EqualValues(t, 1, countRows(t, db, &gbmodels.GbSipMetricGap{}))
	require.EqualValues(t, 2, countRows(t, db, &gbmodels.GbPlayAttempt{}))
	var pending gbmodels.GbPlayAttempt
	require.NoError(t, db.Where("correlation_id = ?", "old-started").First(&pending).Error)
}

func TestDashboardRetentionDeletesAcrossBoundedBatches(t *testing.T) {
	db := newRetentionDB(t)
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		require.NoError(t, db.Create(&gbmodels.GbSipMetricFlush{FlushID: string(rune('a' + i)), CreatedAt: now.Add(-31 * 24 * time.Hour)}).Error)
	}
	service := NewDashboardRetention(db, 2)
	service.SetClock(func() time.Time { return now })
	result, err := service.Prune(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 5, result.SIPFlushes)
}

func TestDashboardRetentionRunsImmediatelyAndStopsWithContext(t *testing.T) {
	db := newRetentionDB(t)
	service := NewDashboardRetention(db, 2)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	reports := make(chan RetentionResult, 2)
	go func() {
		service.Run(ctx, time.Hour, func(result RetentionResult, err error) {
			require.NoError(t, err)
			reports <- result
		})
		close(done)
	}()
	select {
	case <-reports:
	case <-time.After(time.Second):
		t.Fatal("retention did not run immediately")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retention did not stop after cancellation")
	}
}

func newRetentionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricFlush{}, &gbmodels.GbSipMetricGap{}, &gbmodels.GbPlayAttempt{}))
	return db
}

func countRows(t *testing.T, db *gorm.DB, model any) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(model).Count(&count).Error)
	return count
}
