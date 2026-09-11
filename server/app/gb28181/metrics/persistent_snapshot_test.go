package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestPersistentDashboardSnapshotSurvivesProcessRestart(t *testing.T) {
	db := newPersistentSnapshotDB(t)
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 9, 5, 12, 5, 30, 0, location)
	bucket := now.Truncate(time.Minute)
	require.NoError(t, db.Create(&[]gbmodels.GbSipMetricMinute{
		{BucketStart: bucket, Method: "REGISTER", Direction: "in", RequestCount: 3, TransactionCount: 3, TransactionSuccess: 2, TransactionFailure: 1},
		{BucketStart: bucket, Method: "KEEPALIVE", Direction: "in", RequestCount: 5, TransactionCount: 5, TransactionSuccess: 5},
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbSipMetricFlush{FlushID: "current", CreatedAt: now}).Error)

	service := NewPersistentDashboardSnapshot(db, nil, location)
	service.SetClock(func() time.Time { return now })
	first, err := service.Snapshot(context.Background(), time.Hour, time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 8, first.TodayTotal)
	require.EqualValues(t, 1, first.TodayAbnormal)
	require.EqualValues(t, 3, transactionByKind(t, first.Transactions, "REGISTER").TodayCount)
	require.InDelta(t, 2.0/3.0, transactionByKind(t, first.Transactions, "REGISTER").SuccessRate, 0.0001)
	point := pulseByTime(t, first.Pulse.Samples, bucket)
	require.True(t, point.Known)
	require.Equal(t, 8, point.MsgPerSec)
	require.Equal(t, 125, point.FailPct)

	// A new service instance simulates a backend restart. The snapshot must be
	// rebuilt from the durable ledger instead of starting from zero.
	restarted := NewPersistentDashboardSnapshot(db, nil, location)
	restarted.SetClock(func() time.Time { return now })
	afterRestart, err := restarted.Snapshot(context.Background(), time.Hour, time.Minute)
	require.NoError(t, err)
	require.Equal(t, first.TodayTotal, afterRestart.TodayTotal)
	require.Equal(t, first.TodayAbnormal, afterRestart.TodayAbnormal)
	require.Equal(t, first.Transactions, afterRestart.Transactions)
}

func TestPersistentDashboardSnapshotDistinguishesIdleFromUnknown(t *testing.T) {
	db := newPersistentSnapshotDB(t)
	location := time.UTC
	now := time.Date(2026, 9, 5, 10, 3, 20, 0, location)
	idle := time.Date(2026, 9, 5, 10, 1, 10, 0, location)
	gapStart := time.Date(2026, 9, 5, 10, 2, 0, 0, location)
	require.NoError(t, db.Create(&gbmodels.GbSipMetricFlush{FlushID: "idle", CreatedAt: idle}).Error)
	require.NoError(t, db.Create(&gbmodels.GbSipMetricGap{StartedAt: gapStart, EndedAt: gapStart.Add(time.Minute), Reason: "restart"}).Error)

	service := NewPersistentDashboardSnapshot(db, nil, location)
	service.SetClock(func() time.Time { return now })
	snapshot, err := service.Snapshot(context.Background(), 3*time.Minute, time.Minute)
	require.NoError(t, err)
	require.True(t, pulseByTime(t, snapshot.Pulse.Samples, idle.Truncate(time.Minute)).Known)
	require.False(t, pulseByTime(t, snapshot.Pulse.Samples, gapStart).Known)
	require.True(t, snapshot.Partial)
}

func TestPersistentDashboardSnapshotCachesDatabaseViewForFiveSeconds(t *testing.T) {
	db := newPersistentSnapshotDB(t)
	now := time.Date(2026, 9, 5, 12, 5, 30, 0, time.UTC)
	row := gbmodels.GbSipMetricMinute{BucketStart: now.Truncate(time.Minute), Method: "KEEPALIVE", Direction: "in", RequestCount: 1, TransactionCount: 1, TransactionSuccess: 1}
	require.NoError(t, db.Create(&row).Error)
	require.NoError(t, db.Create(&gbmodels.GbSipMetricFlush{FlushID: "initial", CreatedAt: now}).Error)

	service := NewPersistentDashboardSnapshot(db, nil, time.UTC)
	service.SetClock(func() time.Time { return now })
	first, err := service.Snapshot(context.Background(), time.Hour, time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 1, first.TodayTotal)
	require.NoError(t, db.Model(&row).Updates(map[string]any{"request_count": 2, "transaction_count": 2, "transaction_success": 2}).Error)

	cached, err := service.Snapshot(context.Background(), time.Hour, time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 1, cached.TodayTotal)
	now = now.Add(6 * time.Second)
	refreshed, err := service.Snapshot(context.Background(), time.Hour, time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 2, refreshed.TodayTotal)
}

func TestPersistentDashboardSnapshotKeepsSubMinuteWindowRealtime(t *testing.T) {
	db := newPersistentSnapshotDB(t)
	now := time.Date(2026, 9, 5, 12, 5, 30, 0, time.UTC)
	require.NoError(t, db.Create(&gbmodels.GbSipMetricMinute{BucketStart: now.Truncate(time.Minute), Method: "KEEPALIVE", Direction: "in", RequestCount: 10, TransactionCount: 10, TransactionSuccess: 10}).Error)
	realtime := NewAggregator()
	realtime.SetClock(func() time.Time { return now })
	realtime.Begin(Transaction{Kind: TxKeepalive, Direction: DirIn, CallID: "live", CSeq: "1", StartedAt: now})
	realtime.End("live", "1", 200, true)

	service := NewPersistentDashboardSnapshot(db, realtime, time.UTC)
	service.SetClock(func() time.Time { return now })
	snapshot, err := service.Snapshot(context.Background(), time.Minute, time.Second)
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.TodayTotal)

	shortWindow, err := service.Snapshot(context.Background(), 30*time.Second, time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 1, shortWindow.TodayTotal)
}

func newPersistentSnapshotDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricFlush{}, &gbmodels.GbSipMetricGap{}))
	return db
}

func transactionByKind(t *testing.T, values []TransactionStat, kind string) TransactionStat {
	t.Helper()
	for _, value := range values {
		if value.KindStr == kind {
			return value
		}
	}
	t.Fatalf("transaction %s not found", kind)
	return TransactionStat{}
}

func pulseByTime(t *testing.T, values []PulseSample, at time.Time) PulseSample {
	t.Helper()
	for _, value := range values {
		if value.T == at.Unix() {
			return value
		}
	}
	t.Fatalf("pulse point %s not found", at)
	return PulseSample{}
}
