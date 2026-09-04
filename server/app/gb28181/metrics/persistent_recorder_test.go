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
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricFlush{}))
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

func TestPersistentRecorderRetainsBatchWhenFlushFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	recorder := NewPersistentRecorder(db, nil)
	recorder.Begin(Transaction{Kind: TxInvite, Direction: DirOut, CallID: "a", CSeq: "1", StartedAt: time.Now()})
	require.Error(t, recorder.Flush(context.Background()))
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricFlush{}))
	require.NoError(t, recorder.Flush(context.Background()))
	var row gbmodels.GbSipMetricMinute
	require.NoError(t, db.First(&row).Error)
	require.EqualValues(t, 1, row.RequestCount)
}

func TestPersistentRecorderCreatesAndUpdatesWhenRecordNotFoundIsMasked(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricFlush{}))
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
