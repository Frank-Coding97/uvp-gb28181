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

func TestTrafficHistoryUsesHourlyFor24HoursAndAppliesScope(t *testing.T) {
	db := newTrafficHistoryDB(t)
	now := time.Date(2026, 9, 4, 10, 3, 0, 0, time.UTC)
	require.NoError(t, db.Create(&[]gbmodels.GbDeviceTrafficHourly{
		{StatHour: now.Add(-time.Hour).Truncate(time.Hour), DeviceCode: "D1", ChannelCode: "C1", OwnerDeptID: 10, UpstreamBytes: 100, DownstreamBytes: 200},
		{StatHour: now.Add(-time.Hour).Truncate(time.Hour), DeviceCode: "D2", ChannelCode: "C2", OwnerDeptID: 20, UpstreamBytes: 900, DownstreamBytes: 900},
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDeviceTrafficDaily{StatDate: now.Truncate(24 * time.Hour), DeviceCode: "D1", ChannelCode: "C1", OwnerDeptID: 10, UpstreamBytes: 9999, DownstreamBytes: 9999}).Error)
	window, err := ResolveTrafficHistoryWindow("24h", now, time.UTC)
	require.NoError(t, err)
	history, err := NewAssetSummaryService(db, time.UTC).TrafficHistory(context.Background(), window, 1, 20, func(query *gorm.DB) *gorm.DB {
		return query.Where("traffic.owner_dept_id = ?", 10)
	})
	require.NoError(t, err)
	require.EqualValues(t, 100, history.Summary.UpstreamBytes)
	require.EqualValues(t, 200, history.Summary.DownstreamBytes)
	require.Len(t, history.Ledger.Rows, 1)
	require.Equal(t, "D1", history.Ledger.Rows[0].DeviceCode)
	require.Len(t, history.Points, 24)
}

func TestTrafficHistoryUsesDailyFor7DaysAndPaginatesLedger(t *testing.T) {
	db := newTrafficHistoryDB(t)
	now := time.Date(2026, 9, 4, 10, 3, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		require.NoError(t, db.Create(&gbmodels.GbDeviceTrafficDaily{
			StatDate: now.AddDate(0, 0, -i).Truncate(24 * time.Hour), DeviceCode: "D" + string(rune('1'+i)), ChannelCode: "C", OwnerDeptID: 10,
			UpstreamBytes: uint64(10 + i), DownstreamBytes: uint64(20 + i),
		}).Error)
	}
	window, err := ResolveTrafficHistoryWindow("7d", now, time.UTC)
	require.NoError(t, err)
	history, err := NewAssetSummaryService(db, time.UTC).TrafficHistory(context.Background(), window, 2, 2, nil)
	require.NoError(t, err)
	require.Equal(t, 3, history.Ledger.Total)
	require.Len(t, history.Ledger.Rows, 1)
	require.Equal(t, 2, history.Ledger.Page)
	require.Len(t, history.Points, 7)
	require.EqualValues(t, 33, history.Summary.UpstreamBytes)
}

func TestTrafficHistoryFillsGapBucketWithZeroAndKeepsPartialCoverage(t *testing.T) {
	db := newTrafficHistoryDB(t)
	now := time.Date(2026, 9, 4, 10, 3, 0, 0, time.UTC)
	gapStart := now.Add(-time.Hour).Truncate(time.Hour)
	gapEnd := gapStart.Add(time.Hour)
	require.NoError(t, db.Create(&gbmodels.GbDeviceTrafficGap{NodeID: 1, Reason: "node_unavailable", State: "closed", StartedAt: gapStart, EndedAt: &gapEnd}).Error)
	window, err := ResolveTrafficHistoryWindow("24h", now, time.UTC)
	require.NoError(t, err)
	history, err := NewAssetSummaryService(db, time.UTC).TrafficHistory(context.Background(), window, 1, 20, nil)
	require.NoError(t, err)
	require.Equal(t, CoveragePartial, history.Coverage)
	require.Equal(t, StatusPartial, history.Status)
	require.Len(t, history.Gaps, 1)
	for _, point := range history.Points {
		if point.BucketStart.Unix() == gapStart.Unix() {
			require.Zero(t, point.UpstreamBytes)
			require.Zero(t, point.DownstreamBytes)
			return
		}
	}
	t.Fatal("gap bucket not found")
}

func newTrafficHistoryDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceTrafficHourly{}, &gbmodels.GbDeviceTrafficDaily{}, &gbmodels.GbDeviceTrafficGap{}))
	return db
}
