package metrics

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func newMetricsSQLiteBaselineDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gormhelper.NewSQLiteClient(filepath.Join(t.TempDir(), "metrics.db"))
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)
	return db
}

func TestPersistentRecorderSQLiteBaselineEmptyLedgerDoesNotCreateRestartGap(t *testing.T) {
	db := newMetricsSQLiteBaselineDB(t)
	recorder := NewPersistentRecorder(db, nil)
	now := time.Date(2026, 9, 7, 16, 0, 0, 0, time.UTC)
	recorder.SetClock(func() time.Time { return now })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, recorder.recordRestartGap(ctx))
	var gaps int64
	require.NoError(t, db.Model(&gbmodels.GbSipMetricGap{}).Count(&gaps).Error)
	require.Zero(t, gaps)
}

func TestPersistentRecorderSQLiteBaselineFlushesOnceAndRetainsFailure(t *testing.T) {
	db := newMetricsSQLiteBaselineDB(t)
	recorder := NewPersistentRecorder(db, nil)
	now := time.Date(2026, 9, 7, 16, 0, 23, 0, time.UTC)
	recorder.SetClock(func() time.Time { return now })
	recorder.Begin(Transaction{Kind: TxRegister, Direction: DirIn, CallID: "sqlite", CSeq: "1", StartedAt: now})
	recorder.End("sqlite", "1", 200, true)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, recorder.Flush(ctx))
	require.NoError(t, recorder.Flush(ctx))
	var row gbmodels.GbSipMetricMinute
	require.NoError(t, db.First(&row).Error)
	require.EqualValues(t, 1, row.RequestCount)
	require.EqualValues(t, 1, row.TransactionSuccess)
	var flushes int64
	require.NoError(t, db.Model(&gbmodels.GbSipMetricFlush{}).Count(&flushes).Error)
	require.EqualValues(t, 1, flushes)

	raw, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	recorder.Begin(Transaction{Kind: TxRegister, Direction: DirIn, CallID: "sqlite-failed", CSeq: "1", StartedAt: now})
	recorder.End("sqlite-failed", "1", 500, false)
	failureCtx, failureCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer failureCancel()
	require.Error(t, recorder.Flush(failureCtx))
}
