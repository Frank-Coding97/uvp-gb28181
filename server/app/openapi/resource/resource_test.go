package resource

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	appmodels "uvplatform.cn/uvp-gb28181/app/models"
)

const (
	resourceOwnerDept = uint(10)
	resourceDevice    = "34020000002000000010"
	resourceChild     = "34020000002000000011"
	resourceOther     = "34020000002000000020"
	resourceShared    = "34020000002000000030"
	resourceNoOwner   = "34020000002000000000"
	resourceInvalid   = "34020000002000000040"
	resourceDeleted   = "34020000002000000050"
	resourceChannel   = "37011200001310000010"
	resourceChildChannel = "37011200001310000011"
	resourceWrongRoot = "37011200001310000011"
	resourceBadOwner  = "37011200001310000012"
	resourceDuplicate = "37011200001310000013"
)

func newResourceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &appmodels.SysDepartment{}))
	t.Cleanup(func() { _ = raw.Close() })
	seedResourceRows(t, db)
	return db
}

func seedResourceRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	active := int8(1)
	inactive := int8(0)
	parent := resourceOwnerDept
	departments := []appmodels.SysDepartment{
		{BaseModel: appmodels.BaseModel{ID: 10}, Name: "A", Status: &active},
		{BaseModel: appmodels.BaseModel{ID: 11}, ParentID: &parent, Name: "A1", Status: &active},
		{BaseModel: appmodels.BaseModel{ID: 20}, Name: "B", Status: &active},
		{BaseModel: appmodels.BaseModel{ID: 30}, Name: "C", Status: &active},
		{BaseModel: appmodels.BaseModel{ID: 40}, Name: "停用", Status: &inactive},
		{BaseModel: appmodels.BaseModel{ID: 50, DeletedAt: gorm.DeletedAt{Time: time.Now(), Valid: true}}, Name: "已删除", Status: &active},
		{BaseModel: appmodels.BaseModel{ID: 60}, Name: "未知状态", Status: nil},
	}
	for i := range departments {
		require.NoError(t, db.Create(&departments[i]).Error)
	}
	devices := []gbmodels.GbDevice{
		{DeviceID: resourceDevice, Name: "本部门设备", Alias: "主设备", Manufacturer: "厂商A", Model: "型号A", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 10},
		{DeviceID: resourceChild, Name: "子部门设备", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 11},
		{DeviceID: resourceOther, Name: "其他部门设备", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 20},
		{DeviceID: resourceShared, Name: "共享来源设备", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 30},
		{DeviceID: resourceNoOwner, Name: "无归属设备", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 0},
		{DeviceID: resourceInvalid, Name: "无效部门设备", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 40},
		{DeviceID: resourceDeleted, Name: "已删除设备", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 10, BaseModel: appmodels.BaseModel{DeletedAt: gorm.DeletedAt{Time: time.Now(), Valid: true}}},
	}
	for i := range devices {
		require.NoError(t, db.Create(&devices[i]).Error)
	}
	channels := []gbmodels.GbChannel{
		{DeviceID: resourceDevice, ChannelID: resourceChannel, Name: "正常通道", Alias: "通道别名", Manufacturer: "厂商A", Model: "型号A", PTZType: 1, Status: gbmodels.ChannelStatusOnline, OwnerDeptID: 10},
		{DeviceID: resourceChild, ChannelID: resourceChildChannel, Name: "子部门通道", Status: gbmodels.ChannelStatusOnline, OwnerDeptID: 11},
		{DeviceID: resourceDevice, ChannelID: resourceWrongRoot, Name: "正常但请求错根", Status: gbmodels.ChannelStatusOnline, OwnerDeptID: 10},
		{DeviceID: resourceDevice, ChannelID: resourceBadOwner, Name: "归属副本错误", Status: gbmodels.ChannelStatusOnline, OwnerDeptID: 20},
		{DeviceID: resourceDevice, ChannelID: resourceDuplicate, Name: "重复链一", Status: gbmodels.ChannelStatusOnline, OwnerDeptID: 10},
		{DeviceID: resourceDevice, ChannelID: resourceDuplicate, Name: "重复链二", Status: gbmodels.ChannelStatusOnline, OwnerDeptID: 10},
		{DeviceID: resourceOther, ChannelID: "37011200001310000020", Name: "其他部门通道", Status: gbmodels.ChannelStatusOnline, OwnerDeptID: 20},
	}
	for i := range channels {
		require.NoError(t, db.Create(&channels[i]).Error)
	}
}

func TestOpenAPIResourceDepartmentScopeIncludesActiveDescendants(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()
	scope := DepartmentScope{OwnerDeptID: resourceOwnerDept, DataScope: DataScopeDepartmentAndChildren}

	ids, err := ResolveDepartmentIDs(ctx, db, scope)
	require.NoError(t, err)
	assert.Equal(t, []uint{resourceOwnerDept, 11}, ids)

	devices, err := svc.ListDevicesInScope(ctx, scope, DeviceListOptions{})
	require.NoError(t, err)
	assert.Equal(t, int64(2), devices.Total)
	assert.Equal(t, resourceDevice, devices.Items[0].DeviceID)
	assert.Equal(t, resourceChild, devices.Items[1].DeviceID)

	child, err := svc.GetDeviceInScope(ctx, scope, resourceChild)
	require.NoError(t, err)
	assert.Equal(t, resourceChild, child.DeviceID)
	childStatus, err := svc.GetDeviceStatusInScope(ctx, scope, resourceChild)
	require.NoError(t, err)
	assert.Equal(t, "online", childStatus.Status)

	channels, err := svc.ListChannelsInScope(ctx, scope, resourceChild, ChannelListOptions{})
	require.NoError(t, err)
	require.Equal(t, int64(1), channels.Total)
	assert.Equal(t, resourceChildChannel, channels.Items[0].ChannelID)
	channel, err := svc.GetChannelInScope(ctx, scope, resourceChild, resourceChildChannel)
	require.NoError(t, err)
	assert.Equal(t, resourceChildChannel, channel.ChannelID)
	channelStatus, err := svc.GetChannelStatusInScope(ctx, scope, resourceChild, resourceChildChannel)
	require.NoError(t, err)
	assert.Equal(t, "online", channelStatus.Status)

	_, err = svc.GetDeviceInScope(ctx, DepartmentScope{OwnerDeptID: resourceOwnerDept, DataScope: 2}, resourceDevice)
	assert.ErrorIs(t, err, ErrInvalidDepartmentScope)
	_, err = svc.GetDeviceInScope(ctx, DepartmentScope{OwnerDeptID: 40, DataScope: DataScopeDepartmentAndChildren}, resourceInvalid)
	assert.ErrorIs(t, err, ErrResourceNotFound)
}

func TestOpenAPIResourceMetadataUsesExactOwnerAndValidDepartment(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()

	devices, err := svc.ListDevices(ctx, resourceOwnerDept, DeviceListOptions{})
	require.NoError(t, err)
	require.Equal(t, int64(1), devices.Total)
	require.Len(t, devices.Items, 1)
	assert.Equal(t, resourceDevice, devices.Items[0].DeviceID)

	device, err := svc.GetDevice(ctx, resourceOwnerDept, resourceDevice)
	require.NoError(t, err)
	assert.Equal(t, "online", device.Status)

	status, err := svc.GetDeviceStatus(ctx, resourceOwnerDept, resourceDevice)
	require.NoError(t, err)
	assert.Equal(t, DeviceStatus{DeviceID: resourceDevice, Status: "online"}, status)

	channels, err := svc.ListChannels(ctx, resourceOwnerDept, resourceDevice, ChannelListOptions{})
	require.NoError(t, err)
	assert.Equal(t, int64(2), channels.Total, "malformed owner copy and ambiguous chain are excluded")
	assert.Equal(t, resourceChannel, channels.Items[0].ChannelID)
	assert.Equal(t, resourceWrongRoot, channels.Items[1].ChannelID)

	channel, err := svc.GetChannel(ctx, resourceOwnerDept, resourceDevice, resourceChannel)
	require.NoError(t, err)
	assert.Equal(t, resourceDevice, channel.DeviceID)
	channelStatus, err := svc.GetChannelStatus(ctx, resourceOwnerDept, resourceDevice, resourceChannel)
	require.NoError(t, err)
	assert.Equal(t, ChannelStatus{DeviceID: resourceDevice, ChannelID: resourceChannel, Status: "online"}, channelStatus)

	for _, deptID := range []uint{0, 40, 50, 60, 999} {
		_, err := svc.ListDevices(ctx, deptID, DeviceListOptions{})
		assert.ErrorIs(t, err, ErrResourceNotFound, "invalid owner department %d", deptID)
	}
}

func TestOpenAPIResourceRejectsInvalidAndSharedDevices(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()

	for _, deviceID := range []string{resourceChild, resourceOther, resourceShared, resourceNoOwner, resourceInvalid, resourceDeleted, "34020000002000009999"} {
		_, err := svc.GetDevice(ctx, resourceOwnerDept, deviceID)
		assert.ErrorIs(t, err, ErrResourceNotFound, "device %s must not be visible", deviceID)
	}
}

func TestOpenAPIResourceRejectsAmbiguousChannelRootAndOwnerMismatch(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()

	_, err := svc.GetChannel(ctx, resourceOwnerDept, resourceOther, resourceChannel)
	assert.ErrorIs(t, err, ErrResourceNotFound)
	_, err = svc.GetChannel(ctx, resourceOwnerDept, resourceDevice, resourceBadOwner)
	assert.ErrorIs(t, err, ErrResourceNotFound)
	_, err = svc.GetChannel(ctx, resourceOwnerDept, resourceDevice, resourceDuplicate)
	assert.ErrorIs(t, err, ErrResourceNotFound)
	_, err = svc.GetChannel(ctx, resourceOwnerDept, resourceDevice, "37011200001310000099")
	assert.ErrorIs(t, err, ErrResourceNotFound)
	_, err = svc.ListChannels(ctx, resourceOwnerDept, resourceOther, ChannelListOptions{})
	assert.ErrorIs(t, err, ErrResourceNotFound)
}

func TestOpenAPIResourceListPaginationAndStatusWhitelist(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()

	page, err := svc.ListDevices(ctx, resourceOwnerDept, DeviceListOptions{Page: 1, PageSize: 20, Keyword: "本部门", Status: "online"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total)
	assert.Equal(t, 1, page.Page)
	assert.Equal(t, 20, page.PageSize)

	_, err = svc.ListDevices(ctx, resourceOwnerDept, DeviceListOptions{Page: -1})
	assert.ErrorIs(t, err, ErrInvalidListOptions)
	_, err = svc.ListDevices(ctx, resourceOwnerDept, DeviceListOptions{PageSize: 101})
	assert.ErrorIs(t, err, ErrInvalidListOptions)
	_, err = svc.ListDevices(ctx, resourceOwnerDept, DeviceListOptions{Status: "running"})
	assert.ErrorIs(t, err, ErrInvalidListOptions)
	_, err = svc.ListDevices(ctx, resourceOwnerDept, DeviceListOptions{Keyword: string(make([]rune, 101))})
	assert.ErrorIs(t, err, ErrInvalidListOptions)
}

func TestOpenAPIResourcePreservesUnknownStatusAndRejectsOffsetOverflow(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()

	require.NoError(t, db.Exec("UPDATE gb_device SET status = NULL WHERE device_id = ?", resourceDevice).Error)
	require.NoError(t, db.Exec("UPDATE gb_channel SET status = NULL WHERE device_id = ? AND channel_id = ?", resourceDevice, resourceChannel).Error)

	device, err := svc.GetDevice(ctx, resourceOwnerDept, resourceDevice)
	require.NoError(t, err)
	assert.Equal(t, "unknown", device.Status)
	devices, err := svc.ListDevices(ctx, resourceOwnerDept, DeviceListOptions{Status: "unknown"})
	require.NoError(t, err)
	require.Len(t, devices.Items, 1)
	assert.Equal(t, resourceDevice, devices.Items[0].DeviceID)
	assert.Equal(t, "unknown", devices.Items[0].Status)

	channel, err := svc.GetChannel(ctx, resourceOwnerDept, resourceDevice, resourceChannel)
	require.NoError(t, err)
	assert.Equal(t, "unknown", channel.Status)
	channels, err := svc.ListChannels(ctx, resourceOwnerDept, resourceDevice, ChannelListOptions{Status: "unknown"})
	require.NoError(t, err)
	require.Len(t, channels.Items, 1)
	assert.Equal(t, resourceChannel, channels.Items[0].ChannelID)
	assert.Equal(t, "unknown", channels.Items[0].Status)

	maxInt := int(^uint(0) >> 1)
	_, err = svc.ListDevices(ctx, resourceOwnerDept, DeviceListOptions{Page: maxInt, PageSize: 100})
	assert.ErrorIs(t, err, ErrInvalidListOptions)
	_, err = svc.ListChannels(ctx, resourceOwnerDept, resourceDevice, ChannelListOptions{Page: maxInt, PageSize: 100})
	assert.ErrorIs(t, err, ErrInvalidListOptions)
}

func TestOpenAPIResourceSQLRechecksCurrentOwner(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()

	var device gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", resourceDevice).First(&device).Error)
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("id = ?", device.ID).Update("owner_dept_id", 20).Error)
	_, err := svc.GetDevice(ctx, resourceOwnerDept, resourceDevice)
	assert.ErrorIs(t, err, ErrResourceNotFound)

	var channel gbmodels.GbChannel
	require.NoError(t, db.Where("device_id = ? AND channel_id = ?", resourceDevice, resourceChannel).First(&channel).Error)
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("id = ?", channel.ID).Update("owner_dept_id", 20).Error)
	_, err = svc.GetChannel(ctx, resourceOwnerDept, resourceDevice, resourceChannel)
	assert.ErrorIs(t, err, ErrResourceNotFound)
}

func TestOpenAPIResourceDTOWhitelist(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()

	device, err := svc.GetDevice(ctx, resourceOwnerDept, resourceDevice)
	require.NoError(t, err)
	channel, err := svc.GetChannel(ctx, resourceOwnerDept, resourceDevice, resourceChannel)
	require.NoError(t, err)
	deviceJSON, err := json.Marshal(device)
	require.NoError(t, err)
	channelJSON, err := json.Marshal(channel)
	require.NoError(t, err)
	assert.JSONEq(t, `{"deviceId":"34020000002000000010","name":"本部门设备","alias":"主设备","manufacturer":"厂商A","model":"型号A","status":"online"}`, string(deviceJSON))
	assert.JSONEq(t, `{"deviceId":"34020000002000000010","channelId":"37011200001310000010","name":"正常通道","alias":"通道别名","manufacturer":"厂商A","model":"型号A","status":"online","ptzType":1}`, string(channelJSON))
}
