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

func TestAssetSummaryAppliesVisibilityScopesBeforeAggregation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbDeviceTrafficDaily{}, &gbmodels.GbDeviceTrafficGap{}))
	require.NoError(t, db.Create(&[]gbmodels.GbDevice{{DeviceID: "D1", OwnerDeptID: 10, Status: 1}, {DeviceID: "D2", OwnerDeptID: 10, Status: 0}, {DeviceID: "D3", OwnerDeptID: 20, Status: 1}}).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbChannel{{DeviceID: "D1", ChannelID: "C1", OwnerDeptID: 10, Status: 1}, {DeviceID: "D2", ChannelID: "C2", OwnerDeptID: 10, Status: 0}, {DeviceID: "D3", ChannelID: "C3", OwnerDeptID: 20, Status: 1}}).Error)
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&[]gbmodels.GbDeviceTrafficDaily{{StatDate: now.Truncate(24 * time.Hour), DeviceCode: "D1", OwnerDeptID: 10, UpstreamBytes: 100, DownstreamBytes: 200}, {StatDate: now.Truncate(24 * time.Hour), DeviceCode: "D3", OwnerDeptID: 20, UpstreamBytes: 900, DownstreamBytes: 900}}).Error)
	scope := func(db *gorm.DB) *gorm.DB { return db.Where("owner_dept_id = ?", 10) }
	trafficScope := func(db *gorm.DB) *gorm.DB { return db.Where("traffic.owner_dept_id = ?", 10) }
	result, err := NewAssetSummaryService(db, time.UTC).Summary(context.Background(), now, scope, scope, trafficScope)
	require.NoError(t, err)
	require.Equal(t, OnlineRateSummary{Total: 2, Online: 1, Offline: 1, Rate: .5}, result.Devices)
	require.Equal(t, OnlineRateSummary{Total: 2, Online: 1, Offline: 1, Rate: .5}, result.Channels)
	require.EqualValues(t, 100, result.Traffic.UpstreamBytes)
	require.EqualValues(t, 200, result.Traffic.DownstreamBytes)
}
