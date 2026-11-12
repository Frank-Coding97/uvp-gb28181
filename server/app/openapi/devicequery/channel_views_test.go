package devicequery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

// seededView closes the shared fixture with the two catalog tables the mount
// view needs. The shared helper stays untouched so unrelated tests keep the
// exact schema they already assert against.
func seededView(t *testing.T) (*Service, resource.DepartmentScope, uint, uint) {
	t.Helper()
	db, scope, deviceID, channelID, _ := newDeviceQueryDB(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbChannelMount{}, &gbmodels.GbCatalogNode{}))
	return New(db), scope, deviceID, channelID
}

func TestDeviceMaintenanceOperationsUseScopedDeviceAndNormalizeStaleQueue(t *testing.T) {
	service, scope, deviceID, channelID := seededView(t)
	db := service.db
	now := time.Now().UTC().Truncate(time.Second)
	expired := now.Add(-2 * time.Minute)
	cableBoot := "op-teleboot"
	formatSD := "op-format-sd"
	ptzMove := "op-ptz-move"
	staleQueue := "op-stale"
	for _, operation := range []gbmodels.GbPTZOperation{
		{
			OperationID: cableBoot, IdempotencyKey: "k1", DeviceID: deviceID, DeviceCode: queryRootDevice,
			ChannelID: channelID, ChannelCode: queryRootChannel, CmdType: "devicecontrol", Action: "teleboot",
			Status: gbmodels.PTZOperationAccepted, SIPStatus: 200, SN: 1, CreatedAt: now.Add(-3 * time.Minute),
		},
		{
			OperationID: formatSD, IdempotencyKey: "k2", DeviceID: deviceID, DeviceCode: queryRootDevice,
			ChannelID: channelID, ChannelCode: queryRootChannel, CmdType: "devicecontrol", Action: "format_sd",
			Status: gbmodels.PTZOperationRejected, SIPStatus: 400, SN: 2, CreatedAt: now.Add(-time.Minute),
			ErrorMessage: "device refused", DeviceError: "card busy",
		},
		{
			// Outside the platform maintenance whitelist: never exposed here.
			OperationID: ptzMove, IdempotencyKey: "k3", DeviceID: deviceID, DeviceCode: queryRootDevice,
			ChannelID: channelID, ChannelCode: queryRootChannel, CmdType: "devicecontrol", Action: "ptz_move",
			Status: gbmodels.PTZOperationAccepted, SN: 3, CreatedAt: now,
		},
		{
			// A crash after durable creation leaves a queued row with an expired
			// deadline. It must surface as unknown, never as still-queued.
			OperationID: staleQueue, IdempotencyKey: "k4", DeviceID: deviceID, DeviceCode: queryRootDevice,
			ChannelID: channelID, ChannelCode: queryRootChannel, CmdType: "devicecontrol", Action: "teleboot",
			Status: gbmodels.PTZOperationQueued, SN: 4, CreatedAt: now.Add(-5 * time.Minute), QueueDeadlineAt: &expired,
		},
	} {
		require.NoError(t, db.Create(&operation).Error)
	}
	// GORM owns CreatedAt, so the ordering fixture is applied afterwards: the
	// list contract sorts on created_at, not on insertion order.
	for operationID, createdAt := range map[string]time.Time{
		cableBoot: now.Add(-3 * time.Minute), formatSD: now.Add(-time.Minute),
		ptzMove: now, staleQueue: now.Add(-5 * time.Minute),
	} {
		require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).
			Where("operation_id = ?", operationID).Update("created_at", createdAt).Error)
	}

	page, err := service.ListDeviceMaintenanceOperationsInScope(context.Background(), scope, queryRootDevice, resource.DeviceListOptions{})
	require.NoError(t, err)
	assert.Equal(t, int64(3), page.Total, "ptz_move must stay out of the maintenance view")
	assert.Equal(t, 1, page.Page)
	require.Len(t, page.Items, 3)
	assert.Equal(t, formatSD, page.Items[0].OperationID, "newest first")
	assert.Equal(t, string(gbmodels.PTZOperationRejected), page.Items[0].Status)
	assert.Equal(t, "device refused", page.Items[0].ErrorMessage)
	assert.Equal(t, "card busy", page.Items[0].DeviceError)
	assert.Equal(t, cableBoot, page.Items[1].OperationID)
	assert.Equal(t, staleQueue, page.Items[2].OperationID)
	assert.Equal(t, "unknown", page.Items[2].Status, "an expired queue deadline is unknown, never still queued")

	paged, err := service.ListDeviceMaintenanceOperationsInScope(context.Background(), scope, queryRootDevice, resource.DeviceListOptions{Page: 2, PageSize: 2})
	require.NoError(t, err)
	require.Len(t, paged.Items, 1)
	assert.Equal(t, staleQueue, paged.Items[0].OperationID)

	_, err = service.ListDeviceMaintenanceOperationsInScope(context.Background(), scope, queryOtherDevice, resource.DeviceListOptions{})
	assert.True(t, errors.Is(err, resource.ErrResourceNotFound), "another department's device must stay invisible")

	_, err = service.ListDeviceMaintenanceOperationsInScope(context.Background(), scope, queryRootDevice, resource.DeviceListOptions{PageSize: maxPageSize + 1})
	assert.True(t, errors.Is(err, resource.ErrInvalidListOptions))
}

func TestChannelMountsResolveParentNodeInsideScope(t *testing.T) {
	service, scope, _, channelID := seededView(t)
	db := service.db
	primary := uint(101)
	secondary := uint(102)
	for _, node := range []gbmodels.GbCatalogNode{
		{ID: primary, OwnerDeptID: queryRootDept, NodeType: gbmodels.NodeTypeBizGroup, Path: "/a/b", Name: "主视图"},
		{ID: secondary, OwnerDeptID: queryRootDept, NodeType: gbmodels.NodeTypeVirtualOrg, Path: "/a/c", Name: "副视图"},
	} {
		require.NoError(t, db.Create(&node).Error)
	}
	for _, mount := range []gbmodels.GbChannelMount{
		{OwnerDeptID: queryRootDept, ChannelID: channelID, ParentNodeID: secondary, DisplayName: "副", MountSource: gbmodels.MountSourceManual, SortOrder: 2},
		{OwnerDeptID: queryRootDept, ChannelID: channelID, ParentNodeID: primary, DisplayName: "主", IsPrimary: true, MountSource: gbmodels.MountSourceCatalog, SortOrder: 1},
	} {
		require.NoError(t, db.Create(&mount).Error)
	}

	list, err := service.ListChannelMountsInScope(context.Background(), scope, queryRootDevice, queryRootChannel)
	require.NoError(t, err)
	assert.Equal(t, queryRootChannel, list.ChannelID)
	require.Len(t, list.Items, 2)
	assert.Equal(t, primary, list.Items[0].ParentNodeID, "primary mount comes first")
	assert.True(t, list.Items[0].IsPrimary)
	assert.Equal(t, "主视图", list.Items[0].ParentName)
	assert.Equal(t, "/a/b", list.Items[0].ParentPath)
	assert.Equal(t, "manual", list.Items[1].MountSource)

	_, err = service.ListChannelMountsInScope(context.Background(), scope, queryOtherDevice, queryOtherChannel)
	assert.True(t, errors.Is(err, resource.ErrResourceNotFound))
}

func TestChannelTimelineLabelsPlaceholderGrid(t *testing.T) {
	service, scope, _, channelID := seededView(t)
	require.NoError(t, service.db.Model(&gbmodels.GbChannel{}).
		Where("id = ?", channelID).Update("status", gbmodels.ChannelStatusOnline).Error)

	timeline, err := service.GetChannelTimelineInScope(context.Background(), scope, queryRootDevice, queryRootChannel)
	require.NoError(t, err)
	assert.Equal(t, queryRootChannel, timeline.ChannelID)
	assert.Equal(t, "24h", timeline.Range)
	assert.Equal(t, 30, timeline.SlotMinutes)
	assert.True(t, timeline.Simplified, "a repeated grid must never be presented as measured uptime")
	require.Len(t, timeline.Slots, timelineSlotCount)
	assert.Equal(t, "online", timeline.Slots[0].Status)
	assert.Equal(t, timeline.Slots[0].End, timeline.Slots[1].Start, "slots are contiguous")
	assert.True(t, timeline.Slots[len(timeline.Slots)-1].End.After(timeline.Slots[0].End))
}
