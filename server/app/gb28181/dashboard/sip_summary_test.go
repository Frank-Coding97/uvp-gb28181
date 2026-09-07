package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestSIPMetricSummaryUsesMinuteLedgerAndMarksRestartCoveragePartial(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricGap{}))
	location := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 9, 2, 13, 15, 20, 0, location)
	require.NoError(t, db.Create(&gbmodels.GbSipMetricMinute{BucketStart: now.Truncate(time.Minute), Method: "REGISTER", Direction: "in", RequestCount: 7, TransactionCount: 6, TransactionSuccess: 5, TransactionFailure: 1}).Error)

	result, err := NewSIPMetricQuery(db, location).Summary(context.Background(), now)
	require.NoError(t, err)
	require.EqualValues(t, 7, result.RPM)
	require.EqualValues(t, 7, result.TodayRequests)
	require.EqualValues(t, 6, result.Transactions)
	require.Equal(t, StatusPartial, result.Status)
	require.Equal(t, CoveragePartial, result.Coverage)
}

func TestSIPMetricSummaryDistinguishesNoSamples(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricGap{}))
	result, err := NewSIPMetricQuery(db, time.UTC).Summary(context.Background(), time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, StatusEmpty, result.Status)
	require.Equal(t, CoverageNotStarted, result.Coverage)
	require.Empty(t, result.Series)
}
