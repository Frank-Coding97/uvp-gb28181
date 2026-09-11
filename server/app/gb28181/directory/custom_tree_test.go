package directory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestBuildCustomTreeCountsOnlineDevicesAcrossDescendants(t *testing.T) {
	db := newCustomServiceDB(t)
	svc := NewCustomGroupService(db)
	root, err := svc.Create(context.Background(), 10, 1, 0, "园区")
	require.NoError(t, err)
	child, err := svc.Create(context.Background(), 10, 1, root.ID, "入口")
	require.NoError(t, err)

	devices := []gbmodels.GbDevice{
		{DeviceID: "online-root", OwnerDeptID: 10, Status: gbmodels.DeviceStatusOnline},
		{DeviceID: "online-child", OwnerDeptID: 10, Status: gbmodels.DeviceStatusOnline},
		{DeviceID: "offline-child", OwnerDeptID: 10},
		{DeviceID: "online-ungrouped", OwnerDeptID: 10, Status: gbmodels.DeviceStatusOnline},
	}
	require.NoError(t, db.Create(&devices).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbCustomGroupDevice{
		{GroupID: root.ID, DeviceID: devices[0].ID},
		{GroupID: child.ID, DeviceID: devices[1].ID},
		{GroupID: child.ID, DeviceID: devices[2].ID},
	}).Error)

	tree, err := BuildCustomTree(context.Background(), db, 10)
	require.NoError(t, err)
	require.Len(t, tree, 2)
	require.Equal(t, 3, tree[0].Count)
	require.Equal(t, 2, tree[0].OnlineCount)
	require.Len(t, tree[0].Children, 1)
	require.Equal(t, 2, tree[0].Children[0].Count)
	require.Equal(t, 1, tree[0].Children[0].OnlineCount)
	require.Equal(t, "ungrouped", tree[1].Type)
	require.Equal(t, 1, tree[1].Count)
	require.Equal(t, 1, tree[1].OnlineCount)
}
