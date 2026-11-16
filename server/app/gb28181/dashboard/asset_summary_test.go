package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestAssetSummaryAppliesVisibilityScopesBeforeAggregation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbDeviceTrafficDaily{}, &gbmodels.GbDeviceTrafficHourly{}, &gbmodels.GbDeviceTrafficGap{}))
	require.NoError(t, db.Create(&[]gbmodels.GbDevice{{DeviceID: "D1", OwnerDeptID: 10, Status: 1}, {DeviceID: "D2", OwnerDeptID: 10, Status: 0}, {DeviceID: "D3", OwnerDeptID: 20, Status: 1}}).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbChannel{{DeviceID: "D1", ChannelID: "C1", OwnerDeptID: 10, Status: 1}, {DeviceID: "D2", ChannelID: "C2", OwnerDeptID: 10, Status: 0}, {DeviceID: "D3", ChannelID: "C3", OwnerDeptID: 20, Status: 1}}).Error)
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&[]gbmodels.GbDeviceTrafficDaily{{StatDate: now.Truncate(24 * time.Hour), DeviceCode: "D1", OwnerDeptID: 10, UpstreamBytes: 100, DownstreamBytes: 200}, {StatDate: now.Truncate(24 * time.Hour), DeviceCode: "D3", OwnerDeptID: 20, UpstreamBytes: 900, DownstreamBytes: 900}}).Error)
	// 小时表只用来画趋势：12:00 这一桶 D1 有 40/80，D3 的 500/500 必须被数据范围过滤掉。
	require.NoError(t, db.Create(&[]gbmodels.GbDeviceTrafficHourly{
		{StatHour: now.Add(-time.Hour), DeviceCode: "D1", OwnerDeptID: 10, UpstreamBytes: 40, DownstreamBytes: 80},
		{StatHour: now.Add(-time.Hour), DeviceCode: "D3", OwnerDeptID: 20, UpstreamBytes: 500, DownstreamBytes: 500},
	}).Error)
	scope := func(db *gorm.DB) *gorm.DB { return db.Where("owner_dept_id = ?", 10) }
	trafficScope := func(db *gorm.DB) *gorm.DB { return db.Where("traffic.owner_dept_id = ?", 10) }
	result, err := NewAssetSummaryService(db, time.UTC).Summary(context.Background(), now, scope, scope, trafficScope)
	require.NoError(t, err)
	require.Equal(t, OnlineRateSummary{Total: 2, Online: 1, Offline: 1, Rate: .5}, result.Devices)
	require.Equal(t, OnlineRateSummary{Total: 2, Online: 1, Offline: 1, Rate: .5}, result.Channels)
	require.EqualValues(t, 100, result.Traffic.UpstreamBytes)
	require.EqualValues(t, 200, result.Traffic.DownstreamBytes)
	// 趋势序列：当日 00:00..13:00 逐小时补齐（14 个点），空桶填 0 而不是跳过。
	require.Len(t, result.Traffic.Series, 14)
	require.Equal(t, now.Truncate(24*time.Hour), result.Traffic.Series[0].BucketStart)
	require.EqualValues(t, 0, result.Traffic.Series[11].UpstreamBytes)
	require.EqualValues(t, 40, result.Traffic.Series[12].UpstreamBytes)
	require.EqualValues(t, 80, result.Traffic.Series[12].DownstreamBytes)
}
