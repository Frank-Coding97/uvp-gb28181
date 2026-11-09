package resource

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestManagementFiltersStatisticsAndOrderingShareVisibility(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()
	scope := DepartmentScope{OwnerDeptID: resourceOwnerDept, DataScope: DataScopeDepartmentAndChildren}
	require.NoError(t, db.Table("gb_device").Where("device_id = ?", resourceDevice).Updates(map[string]any{"transport": "TCP", "effective_version": "2022"}).Error)
	options := DeviceListOptions{Manufacturer: "厂商A", Model: "型号A", Transport: "TCP", ProtocolVersion: "2022", Sort: "name", Order: "desc"}
	page, err := svc.ListDevicesInScope(ctx, scope, options)
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	stats, err := svc.GetDeviceStatisticsInScope(ctx, scope, options)
	require.NoError(t, err)
	require.Equal(t, DeviceStatistics{Total: 1, Online: 1, ChannelCount: 2, OnlineChannelCount: 2}, stats)
	zero, no := int8(0), false
	require.NoError(t, db.Table("gb_channel").Where("device_id = ? AND channel_id = ?", resourceDevice, resourceChannel).Updates(map[string]any{"ptz_type": 0, "audio_enabled": false, "stream_transport": "UDP", "status": nil}).Error)
	filter := ChannelListOptions{PTZType: &zero, AudioEnabled: &no, StreamTransport: "UDP", Manufacturer: "厂商A", Model: "型号A"}
	channels, err := svc.ListChannelsInScope(ctx, scope, resourceDevice, filter)
	require.NoError(t, err)
	require.Equal(t, int64(1), channels.Total)
	channelStats, err := svc.GetChannelStatisticsInScope(ctx, scope, resourceDevice, filter)
	require.NoError(t, err)
	require.Equal(t, ChannelStatistics{Total: 1, Unknown: 1}, channelStats)
	_, err = svc.GetChannelStatisticsInScope(ctx, scope, resourceOther, ChannelListOptions{})
	require.ErrorIs(t, err, ErrResourceNotFound)
	_, err = svc.ListDevicesInScope(ctx, scope, DeviceListOptions{Sort: "name; drop table gb_device"})
	require.ErrorIs(t, err, ErrInvalidListOptions)
	_, err = svc.ListChannelsInScope(ctx, scope, resourceDevice, ChannelListOptions{Order: "random"})
	require.ErrorIs(t, err, ErrInvalidListOptions)
}

func TestManagementUpdatesAreScopedValidatedAndNaturallyIdempotent(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()
	scope := DepartmentScope{OwnerDeptID: resourceOwnerDept, DataScope: DataScopeDepartment}
	alias, override := "新别名", "2022"
	device, err := svc.UpdateDeviceInScope(ctx, scope, resourceDevice, DeviceUpdate{Alias: &alias, ProtocolOverride: &override})
	require.NoError(t, err)
	require.Equal(t, alias, device.Alias)
	require.Equal(t, "2022", device.EffectiveVersion)
	require.Equal(t, "override", device.EffectiveVersionSource)
	updatedAt := *device.UpdatedAt
	device, err = svc.UpdateDeviceInScope(ctx, scope, resourceDevice, DeviceUpdate{Alias: &alias, ProtocolOverride: &override})
	require.NoError(t, err)
	require.Equal(t, updatedAt, *device.UpdatedAt)
	_, err = svc.UpdateDeviceInScope(ctx, scope, resourceOther, DeviceUpdate{Alias: &alias})
	require.ErrorIs(t, err, ErrResourceNotFound)
	_, err = svc.UpdateDeviceInScope(ctx, scope, resourceDevice, DeviceUpdate{})
	require.ErrorIs(t, err, ErrInvalidUpdate)
	badOverride := "2024"
	_, err = svc.UpdateDeviceInScope(ctx, scope, resourceDevice, DeviceUpdate{Alias: &alias, ProtocolOverride: &badOverride})
	require.ErrorIs(t, err, ErrInvalidUpdate)
	no, zero, lng, lat, transport := false, int8(0), 117.25, 31.75, "TCP-Active"
	channel, err := svc.UpdateChannelInScope(ctx, scope, resourceDevice, resourceChannel, ChannelUpdate{Alias: &alias, PTZType: &zero, AudioEnabled: &no, OnDemandLive: &no, Longitude: &lng, Latitude: &lat, StreamTransport: &transport})
	require.NoError(t, err)
	require.Equal(t, "manual", channel.PositionSource)
	require.Equal(t, int8(0), channel.PTZType)
	require.False(t, channel.AudioEnabled)
	first := *channel.UpdatedAt
	channel, err = svc.UpdateChannelInScope(ctx, scope, resourceDevice, resourceChannel, ChannelUpdate{Alias: &alias, PTZType: &zero, AudioEnabled: &no, OnDemandLive: &no, Longitude: &lng, Latitude: &lat, StreamTransport: &transport})
	require.NoError(t, err)
	require.True(t, first.Equal(*channel.UpdatedAt))
	_, err = svc.UpdateChannelInScope(ctx, scope, resourceDevice, resourceChannel, ChannelUpdate{Longitude: &lng})
	require.ErrorIs(t, err, ErrInvalidUpdate)
	clear := 0.0
	channel, err = svc.UpdateChannelInScope(ctx, scope, resourceDevice, resourceChannel, ChannelUpdate{Longitude: &clear, Latitude: &clear})
	require.NoError(t, err)
	require.Empty(t, channel.PositionSource)
	require.Nil(t, channel.PositionUpdatedAt)
	_, err = svc.UpdateChannelInScope(ctx, scope, resourceDevice, resourceBadOwner, ChannelUpdate{Alias: &alias})
	require.ErrorIs(t, err, ErrResourceNotFound)
	_, err = svc.UpdateChannelInScope(ctx, scope, resourceDevice, resourceDuplicate, ChannelUpdate{Alias: &alias})
	require.ErrorIs(t, err, ErrResourceNotFound)
	var rows []gbmodels.GbChannel
	require.NoError(t, db.Where("device_id = ? AND channel_id = ?", resourceDevice, resourceDuplicate).Find(&rows).Error)
	for _, row := range rows {
		require.NotEqual(t, alias, row.Alias)
	}
	require.WithinDuration(t, time.Now(), *channel.UpdatedAt, time.Second*5)
}

func TestManagementStatisticsIgnorePaginationAndReturnZeroWhenFilterMisses(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()
	scope := DepartmentScope{OwnerDeptID: resourceOwnerDept, DataScope: DataScopeDepartmentAndChildren}
	require.NoError(t, db.Table("gb_device").Where("device_id = ?", resourceDevice).Update("name", "Alpha").Error)
	require.NoError(t, db.Table("gb_device").Where("device_id = ?", resourceChild).Updates(map[string]any{"name": "Zulu", "status": 0}).Error)
	page, err := svc.ListDevicesInScope(ctx, scope, DeviceListOptions{PageSize: 1, Sort: "name", Order: "desc"})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, resourceChild, page.Items[0].DeviceID)
	stats, err := svc.GetDeviceStatisticsInScope(ctx, scope, DeviceListOptions{Page: 999, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, DeviceStatistics{Total: 2, Online: 1, Offline: 1, ChannelCount: 3, OnlineChannelCount: 3}, stats)
	stats, err = svc.GetDeviceStatisticsInScope(ctx, scope, DeviceListOptions{Manufacturer: "not-present"})
	require.NoError(t, err)
	require.Equal(t, DeviceStatistics{}, stats)
	require.NoError(t, db.Table("gb_channel").Where("device_id = ? AND channel_id = ?", resourceDevice, resourceChannel).Updates(map[string]any{"stream_id": "stream", "cloud_recording_enabled": true, "status": 4}).Error)
	channels, err := svc.GetChannelStatisticsInScope(ctx, scope, resourceDevice, ChannelListOptions{})
	require.NoError(t, err)
	require.Equal(t, ChannelStatistics{Total: 2, Online: 1, Unknown: 1, Playing: 1, CloudRecordingEnabled: 1}, channels)
	channels, err = svc.GetChannelStatisticsInScope(ctx, scope, resourceDevice, ChannelListOptions{Model: "not-present"})
	require.NoError(t, err)
	require.Equal(t, ChannelStatistics{}, channels)
}

func TestManagementUpdateRollsBackWhenDatabaseUpdateFails(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()
	scope := DepartmentScope{OwnerDeptID: resourceOwnerDept, DataScope: DataScopeDepartment}
	before, err := svc.GetDeviceInScope(ctx, scope, resourceDevice)
	require.NoError(t, err)
	failure := errors.New("forced persistence failure after update")
	require.NoError(t, db.Callback().Update().After("gorm:update").Register("resource:test_rollback", func(tx *gorm.DB) { tx.AddError(failure) }))
	alias, version := "must-roll-back", "2022"
	_, err = svc.UpdateDeviceInScope(ctx, scope, resourceDevice, DeviceUpdate{Alias: &alias, ProtocolOverride: &version})
	require.ErrorIs(t, err, failure)
	require.NoError(t, db.Callback().Update().Remove("resource:test_rollback"))
	after, err := svc.GetDeviceInScope(ctx, scope, resourceDevice)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
