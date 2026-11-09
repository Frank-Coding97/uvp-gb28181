package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func resourceJSON(t *testing.T, value any) map[string]any {
	t.Helper()
	body, err := json.Marshal(value)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(body, &fields))
	return fields
}

func TestResourceDetailsExposeStoredFieldsAndMatchLists(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()
	at := time.Date(2026, 11, 9, 12, 30, 0, 0, time.UTC)
	deviceValues := map[string]any{
		"firmware": "v3.2", "transport": "TCP", "ip": "192.0.2.10", "port": 5060,
		"keepaliveInterval": 30, "expires": 3600, "reportedVersion": "2022", "effectiveVersion": "2022",
		"effectiveVersionSource": "reported", "protocolOverride": "auto", "subscribeCapability": "subscribed",
		"reportedCharset": "GB2312", "wireCharset": "GB18030",
	}
	channelValues := map[string]any{
		"owner": "园区", "civilCode": "340200", "parentId": "34020000002000000001/34020000002000000002",
		"roomType": 2, "supplyLightType": 3, "directionType": 4, "resolution": "1920*1080", "positionType": 5, "useType": 2,
		"photoelectricImagingType": "1/2", "capturePositionType": "4", "streamNumberList": "0/1",
		"ipAddress": "192.0.2.11", "port": 6000, "longitude": 117.25, "latitude": 31.75, "positionSource": "manual",
		"statusEvent": "VLOST", "streamId": "live-001", "onDemandLive": false, "streamTransport": "TCP-Active",
		"audioEnabled": false, "recordingMode": "continuous", "cloudRecordingEnabled": true,
		"cloudRecordingState": "recording", "cloudRecordingError": "storage offline",
	}
	deviceTimes := []string{"registerTime", "registerExpireAt", "keepaliveTime", "offlineAt", "effectiveVersionAt", "subscribeLastTest", "subscribeExpiresAt", "reportedCharsetAt", "createdAt", "updatedAt"}
	channelTimes := []string{"positionUpdatedAt", "cloudRecordingUpdatedAt", "snapshotAt", "createdAt", "updatedAt"}
	deviceUpdates := map[string]any{"password": "must-not-leak"}
	channelUpdates := map[string]any{"snapshot_url": "/uploads/internal-image.jpg"}
	for key, value := range deviceValues {
		deviceUpdates[db.NamingStrategy.ColumnName("", key)] = value
	}
	for key, value := range channelValues {
		channelUpdates[db.NamingStrategy.ColumnName("", key)] = value
	}
	for _, key := range deviceTimes {
		deviceUpdates[db.NamingStrategy.ColumnName("", key)] = at
		deviceValues[key] = at.Format(time.RFC3339)
	}
	for _, key := range channelTimes {
		channelUpdates[db.NamingStrategy.ColumnName("", key)] = at
		channelValues[key] = at.Format(time.RFC3339)
	}
	require.NoError(t, db.Table("gb_device").Where("device_id = ?", resourceDevice).Updates(deviceUpdates).Error)
	require.NoError(t, db.Table("gb_channel").Where("device_id = ? AND channel_id = ?", resourceDevice, resourceChannel).Updates(channelUpdates).Error)
	device, err := svc.GetDevice(ctx, resourceOwnerDept, resourceDevice)
	require.NoError(t, err)
	deviceFields := resourceJSON(t, device)
	for key, value := range deviceValues {
		require.Contains(t, deviceFields, key)
		require.EqualValues(t, value, deviceFields[key], key)
	}
	require.NotContains(t, deviceFields, "password")
	require.NotContains(t, deviceFields, "ownerDeptId")
	channel, err := svc.GetChannel(ctx, resourceOwnerDept, resourceDevice, resourceChannel)
	require.NoError(t, err)
	channelFields := resourceJSON(t, channel)
	for key, value := range channelValues {
		require.Contains(t, channelFields, key)
		require.EqualValues(t, value, channelFields[key], key)
	}
	require.NotContains(t, channelFields, "snapshotUrl")
	require.NotContains(t, channelFields, "ownerDeptId")
	devices, err := svc.ListDevices(ctx, resourceOwnerDept, DeviceListOptions{})
	require.NoError(t, err)
	require.Equal(t, deviceFields, resourceJSON(t, devices.Items[0]))
	channels, err := svc.ListChannels(ctx, resourceOwnerDept, resourceDevice, ChannelListOptions{})
	require.NoError(t, err)
	require.Equal(t, channelFields, resourceJSON(t, channels.Items[0]))
}

func TestResourceUnknownTimesAreExplicitNull(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()
	for _, table := range []string{"gb_device", "gb_channel"} {
		require.NoError(t, db.Table(table).Where("device_id = ?", resourceDevice).Updates(map[string]any{"created_at": nil, "updated_at": time.Time{}}).Error)
	}
	device, err := svc.GetDevice(ctx, resourceOwnerDept, resourceDevice)
	require.NoError(t, err)
	fields := resourceJSON(t, device)
	for _, key := range []string{"registerTime", "registerExpireAt", "keepaliveTime", "offlineAt", "effectiveVersionAt", "subscribeLastTest", "subscribeExpiresAt", "reportedCharsetAt", "createdAt", "updatedAt"} {
		require.Contains(t, fields, key)
		require.Nil(t, fields[key], key)
	}
	channel, err := svc.GetChannel(ctx, resourceOwnerDept, resourceDevice, resourceChannel)
	require.NoError(t, err)
	fields = resourceJSON(t, channel)
	for _, key := range []string{"positionUpdatedAt", "cloudRecordingUpdatedAt", "snapshotAt", "createdAt", "updatedAt"} {
		require.Contains(t, fields, key)
		require.Nil(t, fields[key], key)
	}
}

func TestDeviceChannelCountsUseVisibleUniqueUndeletedChannels(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()
	at := time.Now()
	rows := []gbmodels.GbChannel{
		{DeviceID: resourceDevice, ChannelID: "37011200001310000041", OwnerDeptID: 10, Status: 0},
		{DeviceID: resourceDevice, ChannelID: "37011200001310000042", OwnerDeptID: 10, Status: 2},
		{DeviceID: resourceDevice, ChannelID: "37011200001310000043", OwnerDeptID: 10, Status: 1, DeletedAt: &at},
		{DeviceID: resourceDevice, ChannelID: "37011200001310000044", OwnerDeptID: 20, Status: 1},
	}
	require.NoError(t, db.Create(&rows).Error)
	require.NoError(t, db.Table("gb_channel").Where("channel_id = ?", rows[1].ChannelID).Update("status", nil).Error)
	device, err := svc.GetDevice(ctx, resourceOwnerDept, resourceDevice)
	require.NoError(t, err)
	fields := resourceJSON(t, device)
	require.EqualValues(t, 4, fields["channelCount"])
	require.EqualValues(t, 2, fields["onlineChannelCount"])
	channels, err := svc.ListChannels(ctx, resourceOwnerDept, resourceDevice, ChannelListOptions{})
	require.NoError(t, err)
	require.EqualValues(t, 4, channels.Total)
	online, err := svc.ListChannels(ctx, resourceOwnerDept, resourceDevice, ChannelListOptions{Status: "online"})
	require.NoError(t, err)
	require.EqualValues(t, 2, online.Total)
	_, err = svc.GetDevice(ctx, resourceOwnerDept, resourceOther)
	require.ErrorIs(t, err, ErrResourceNotFound)
	children, err := svc.ListDevicesInScope(ctx, DepartmentScope{OwnerDeptID: 10, DataScope: DataScopeDepartmentAndChildren}, DeviceListOptions{})
	require.NoError(t, err)
	require.EqualValues(t, 1, resourceJSON(t, children.Items[1])["channelCount"])
}

func TestDeviceListChannelCountsDoNotMultiplyDatabaseQueries(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()
	queries := 0
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("count-resource-queries", func(tx *gorm.DB) {
		if !tx.DryRun {
			queries++
		}
	}))
	_, err := svc.ListDevices(ctx, resourceOwnerDept, DeviceListOptions{})
	require.NoError(t, err)
	one := queries
	for i := 0; i < 12; i++ {
		require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: fmt.Sprintf("340200000020000001%02d", i), OwnerDeptID: 10}).Error)
	}
	queries = 0
	page, err := svc.ListDevices(ctx, resourceOwnerDept, DeviceListOptions{})
	require.NoError(t, err)
	require.Len(t, page.Items, 13)
	require.Equal(t, one, queries)
	require.LessOrEqual(t, queries, 3)
	require.EqualValues(t, 0, resourceJSON(t, page.Items[1])["channelCount"])
}
