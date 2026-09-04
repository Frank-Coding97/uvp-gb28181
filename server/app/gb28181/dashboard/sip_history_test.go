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

func TestSIPHistoryAggregatesBucketsLedgerAndRPM(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricFlush{}, &gbmodels.GbSipMetricGap{}))
	location := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 9, 4, 10, 3, 0, 0, location)
	bucket := time.Date(2026, 9, 4, 10, 0, 0, 0, location)
	require.NoError(t, db.Create(&[]gbmodels.GbSipMetricMinute{
		{BucketStart: bucket, Method: "REGISTER", Direction: "in", RequestCount: 20, TransactionCount: 18, TransactionSuccess: 17, TransactionFailure: 1},
		{BucketStart: bucket.Add(time.Minute), Method: "INVITE", Direction: "out", RequestCount: 30, TransactionCount: 30, TransactionSuccess: 28, TransactionFailure: 2},
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbSipMetricFlush{FlushID: "heartbeat", CreatedAt: bucket.Add(2 * time.Minute)}).Error)

	window, err := ResolveHistoryWindow("24h", now, location)
	require.NoError(t, err)
	history, err := NewSIPMetricQuery(db, location).History(context.Background(), window)
	require.NoError(t, err)
	require.LessOrEqual(t, len(history.Points), window.MaxPoints)
	point := findSIPHistoryPoint(t, history.Points, bucket)
	require.EqualValues(t, 50, *point.Requests)
	require.Equal(t, 10.0, *point.RPM)
	require.EqualValues(t, 50, history.RollingRequests)
	require.EqualValues(t, 50, history.TodayRequests)
	require.Len(t, history.Ledger, 2)
	require.Equal(t, HistoryRange24H, history.Range)
	require.Equal(t, int64((5 * time.Minute).Seconds()), history.BucketSeconds)
}

func TestSIPHistoryDistinguishesHeartbeatZeroFromGap(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipMetricMinute{}, &gbmodels.GbSipMetricFlush{}, &gbmodels.GbSipMetricGap{}))
	location := time.UTC
	now := time.Date(2026, 9, 4, 10, 3, 0, 0, location)
	zeroBucket := time.Date(2026, 9, 4, 10, 1, 0, 0, location)
	gapBucket := time.Date(2026, 9, 4, 10, 2, 0, 0, location)
	require.NoError(t, db.Create(&gbmodels.GbSipMetricFlush{FlushID: "idle", CreatedAt: zeroBucket}).Error)
	require.NoError(t, db.Create(&gbmodels.GbSipMetricGap{StartedAt: gapBucket, EndedAt: gapBucket.Add(time.Minute), Reason: "restart"}).Error)

	window, err := ResolveHistoryWindow("1h", now, location)
	require.NoError(t, err)
	history, err := NewSIPMetricQuery(db, location).History(context.Background(), window)
	require.NoError(t, err)
	zero := findSIPHistoryPoint(t, history.Points, zeroBucket)
	require.NotNil(t, zero.Requests)
	require.Zero(t, *zero.Requests)
	unknown := findSIPHistoryPoint(t, history.Points, gapBucket)
	require.Nil(t, unknown.Requests)
	require.Equal(t, CoveragePartial, history.Coverage)
	require.Equal(t, StatusPartial, history.Status)
	require.Len(t, history.Gaps, 1)
}

func findSIPHistoryPoint(t *testing.T, points []SIPHistoryPoint, bucket time.Time) SIPHistoryPoint {
	t.Helper()
	for _, point := range points {
		if point.BucketStart.Unix() == bucket.Unix() {
			return point
		}
	}
	t.Fatalf("bucket %s not found", bucket)
	return SIPHistoryPoint{}
}
