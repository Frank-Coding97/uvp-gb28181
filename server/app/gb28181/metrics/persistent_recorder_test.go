package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
)

func TestPersistentRecorderBatchesRequestsAndTransactionsByMinute(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricFlush{}, &gbmodels.GbSipMetricGap{}))
	now := time.Date(2026, 9, 2, 13, 5, 23, 0, time.Local)
	recorder := NewPersistentRecorder(db, nil)
	recorder.SetClock(func() time.Time { return now })
	recorder.Begin(Transaction{Kind: TxRegister, Direction: DirIn, CallID: "a", CSeq: "1", StartedAt: now})
	recorder.End("a", "1", 200, true)
	recorder.Begin(Transaction{Kind: TxRegister, Direction: DirIn, CallID: "b", CSeq: "1", StartedAt: now})
	recorder.End("b", "1", 500, false)
	require.NoError(t, recorder.Flush(context.Background()))

	var row gbmodels.GbSipMetricMinute
	require.NoError(t, db.First(&row).Error)
	require.Equal(t, minuteStart(now).Unix(), row.BucketStart.Unix())
	require.EqualValues(t, 2, row.RequestCount)
	require.EqualValues(t, 2, row.TransactionCount)
	require.EqualValues(t, 1, row.TransactionSuccess)
	require.EqualValues(t, 1, row.TransactionFailure)
	var flushes int64
	require.NoError(t, db.Model(&gbmodels.GbSipMetricFlush{}).Count(&flushes).Error)
	require.EqualValues(t, 1, flushes)
}

func TestPersistentRecorderFlushBatchIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricFlush{}, &gbmodels.GbSipMetricGap{}))
	now := time.Date(2026, 9, 5, 13, 5, 23, 0, time.UTC)
	recorder := NewPersistentRecorder(db, nil)
	batch := &metricFlushBatch{
		ID: "stable-flush-id",
		Deltas: map[metricBucketKey]metricDelta{
			{BucketStart: minuteStart(now), Method: "REGISTER", Direction: "in"}: {Requests: 1, Transactions: 1, Success: 1},
		},
	}

	_, err = recorder.persistBatch(context.Background(), batch, now)
	require.NoError(t, err)
	_, err = recorder.persistBatch(context.Background(), batch, now.Add(time.Second))
	require.NoError(t, err)

	var row gbmodels.GbSipMetricMinute
	require.NoError(t, db.First(&row).Error)
	require.EqualValues(t, 1, row.RequestCount)
	require.EqualValues(t, 1, row.TransactionCount)
	var flushes int64
	require.NoError(t, db.Model(&gbmodels.GbSipMetricFlush{}).Count(&flushes).Error)
	require.EqualValues(t, 1, flushes)
}

func TestPersistentRecorderRetainsBatchWhenFlushFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	recorder := NewPersistentRecorder(db, nil)
	recorder.Begin(Transaction{Kind: TxInvite, Direction: DirOut, CallID: "a", CSeq: "1", StartedAt: time.Now()})
	require.Error(t, recorder.Flush(context.Background()))
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricFlush{}, &gbmodels.GbSipMetricGap{}))
	require.NoError(t, recorder.Flush(context.Background()))
	var row gbmodels.GbSipMetricMinute
	require.NoError(t, db.First(&row).Error)
	require.EqualValues(t, 1, row.RequestCount)
}

func TestPersistentRecorderKeepsNewEventsSeparateFromRetryBatch(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	now := time.Date(2026, 9, 5, 13, 5, 23, 0, time.UTC)
	recorder := NewPersistentRecorder(db, nil)
	recorder.SetClock(func() time.Time { return now })
	recorder.Begin(Transaction{Kind: TxRegister, Direction: DirIn, CallID: "first", CSeq: "1", StartedAt: now})
	recorder.End("first", "1", 200, true)
	require.Error(t, recorder.Flush(context.Background()))

	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricFlush{}, &gbmodels.GbSipMetricGap{}))
	recorder.Begin(Transaction{Kind: TxRegister, Direction: DirIn, CallID: "second", CSeq: "1", StartedAt: now})
	recorder.End("second", "1", 200, true)
	require.NoError(t, recorder.Flush(context.Background()))
	require.NoError(t, recorder.Flush(context.Background()))

	var row gbmodels.GbSipMetricMinute
	require.NoError(t, db.First(&row).Error)
	require.EqualValues(t, 2, row.TransactionCount)
	var flushes int64
	require.NoError(t, db.Model(&gbmodels.GbSipMetricFlush{}).Count(&flushes).Error)
	require.EqualValues(t, 2, flushes)
}

func TestPersistentRecorderCreatesAndUpdatesWhenRecordNotFoundIsMasked(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricFlush{}, &gbmodels.GbSipMetricGap{}))
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("disable_raise_record_not_found", gormhelper.MaskNotDataError))

	now := time.Date(2026, 9, 4, 9, 17, 38, 0, time.Local)
	recorder := NewPersistentRecorder(db, nil)
	recorder.SetClock(func() time.Time { return now })

	recorder.Begin(Transaction{Kind: TxRegister, Direction: DirIn, CallID: "first", CSeq: "1", StartedAt: now})
	recorder.End("first", "1", 200, true)
	require.NoError(t, recorder.Flush(context.Background()))

	recorder.Begin(Transaction{Kind: TxRegister, Direction: DirIn, CallID: "second", CSeq: "1", StartedAt: now})
	recorder.End("second", "1", 200, true)
	require.NoError(t, recorder.Flush(context.Background()))

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbSipMetricMinute{}).Count(&count).Error)
	require.EqualValues(t, 1, count)

	var row gbmodels.GbSipMetricMinute
	require.NoError(t, db.First(&row).Error)
	require.EqualValues(t, 2, row.RequestCount)
	require.EqualValues(t, 2, row.TransactionCount)
	require.EqualValues(t, 2, row.TransactionSuccess)
}

func TestPersistentRecorderWritesIdleHeartbeatOncePerMinute(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricFlush{}, &gbmodels.GbSipMetricGap{}))

	now := time.Date(2026, 9, 4, 10, 10, 20, 0, time.Local)
	recorder := NewPersistentRecorder(db, nil)
	recorder.SetClock(func() time.Time { return now })
	require.NoError(t, recorder.Flush(context.Background()))
	require.NoError(t, recorder.Flush(context.Background()))

	var flushes int64
	require.NoError(t, db.Model(&gbmodels.GbSipMetricFlush{}).Count(&flushes).Error)
	require.EqualValues(t, 1, flushes)
	var metrics int64
	require.NoError(t, db.Model(&gbmodels.GbSipMetricMinute{}).Count(&metrics).Error)
	require.Zero(t, metrics)

	now = now.Add(time.Minute)
	require.NoError(t, recorder.Flush(context.Background()))
	require.NoError(t, db.Model(&gbmodels.GbSipMetricFlush{}).Count(&flushes).Error)
	require.EqualValues(t, 2, flushes)
}

func TestPersistentRecorderRecordsRestartGapAfterHeartbeatTolerance(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricFlush{}, &gbmodels.GbSipMetricGap{}))

	now := time.Date(2026, 9, 4, 10, 20, 20, 0, time.Local)
	last := now.Add(-10 * time.Minute)
	require.NoError(t, db.Create(&gbmodels.GbSipMetricFlush{FlushID: "before-restart", CreatedAt: last}).Error)
	recorder := NewPersistentRecorder(db, nil)
	recorder.SetClock(func() time.Time { return now })
	require.NoError(t, recorder.recordRestartGap(context.Background()))

	var gap gbmodels.GbSipMetricGap
	require.NoError(t, db.First(&gap).Error)
	require.Equal(t, minuteStart(last).Add(time.Minute).Unix(), gap.StartedAt.Unix())
	require.Equal(t, minuteStart(now).Unix(), gap.EndedAt.Unix())
	require.Equal(t, "restart", gap.Reason)
}

func TestPersistentRecorderDoesNotRecordRestartGapWithinTolerance(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricFlush{}, &gbmodels.GbSipMetricGap{}))

	now := time.Date(2026, 9, 4, 10, 20, 20, 0, time.Local)
	require.NoError(t, db.Create(&gbmodels.GbSipMetricFlush{FlushID: "recent", CreatedAt: now.Add(-time.Minute)}).Error)
	recorder := NewPersistentRecorder(db, nil)
	recorder.SetClock(func() time.Time { return now })
	require.NoError(t, recorder.recordRestartGap(context.Background()))

	var gaps int64
	require.NoError(t, db.Model(&gbmodels.GbSipMetricGap{}).Count(&gaps).Error)
	require.Zero(t, gaps)
}

func TestPersistentRecorderRecordsPersistenceFailureGapAfterRecovery(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricFlush{}, &gbmodels.GbSipMetricGap{}))

	now := time.Date(2026, 9, 4, 10, 30, 20, 0, time.Local)
	recorder := NewPersistentRecorder(db, nil)
	recorder.SetClock(func() time.Time { return now })
	recorder.Begin(Transaction{Kind: TxInvite, Direction: DirOut, CallID: "failure", CSeq: "1", StartedAt: now})
	require.Error(t, recorder.Flush(context.Background()))

	now = now.Add(2 * time.Minute)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}))
	require.NoError(t, recorder.Flush(context.Background()))

	var gap gbmodels.GbSipMetricGap
	require.NoError(t, db.Where("reason = ?", "persist_failure").First(&gap).Error)
	require.Equal(t, time.Date(2026, 9, 4, 10, 30, 20, 0, time.Local).Unix(), gap.StartedAt.Unix())
	require.Equal(t, now.Unix(), gap.EndedAt.Unix())
	var row gbmodels.GbSipMetricMinute
	require.NoError(t, db.First(&row).Error)
	require.EqualValues(t, 1, row.RequestCount)
}
