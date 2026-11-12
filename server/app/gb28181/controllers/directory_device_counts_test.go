package controllers

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbdirectory "uvplatform.com/uvp-gb28181/app/gb28181/directory"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// deviceLeaf 造一个设备叶节点，channelCount 故意留零值 —— 构建层就是这个状态。
func deviceLeaf(deviceID string) gbdirectory.DirectoryNodeVO {
	return gbdirectory.DirectoryNodeVO{
		Key: "national:device:x", Name: deviceID, Type: "device", ReadOnly: true,
		Device: &gbdirectory.DirectoryDeviceVO{DeviceID: deviceID, Name: deviceID},
	}
}

// TestFillDirectoryDeviceChannelCountsFillsDeviceLeaves 钉住「设备叶节点必须带上
// 真实通道数」。
//
// 多屏取流页把设备节点的 count 当通道数用（channelCount），再据此决定渲染不渲染
// 展开箭头。构建层从来不填设备节点的 Count，于是任何设备（含底下挂着一堆通道的
// 级联平台）都没有箭头、点不开，通道根本选不到 —— 这条用例守的就是这个零值。
func TestFillDirectoryDeviceChannelCountsFillsDeviceLeaves(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbChannel{}))
	require.NoError(t, db.Create(&[]gbmodels.GbChannel{
		{ChannelID: "34020000001320000001", DeviceID: "34020000002000000001", Name: "通道1", Status: gbmodels.ChannelStatusOnline},
		{ChannelID: "34020000001320000002", DeviceID: "34020000002000000001", Name: "通道2", Status: gbmodels.ChannelStatusOnline},
		{ChannelID: "34020000001320000003", DeviceID: "34020000002000000001", Name: "通道3", Status: gbmodels.ChannelStatusOffline},
		{ChannelID: "37011200001130000001", DeviceID: "37011200001180000001", Name: "别的通道", Status: gbmodels.ChannelStatusOnline},
	}).Error)

	platform := deviceLeaf("34020000002000000001")
	empty := deviceLeaf("34020000001180000009")
	tree := []gbdirectory.DirectoryNodeVO{{
		Key: "national:area:3701", Name: "历城区", Type: "area", Count: 2, OnlineCount: 1,
		Children: []gbdirectory.DirectoryNodeVO{platform, empty},
	}}

	require.NoError(t, fillDirectoryDeviceChannelCounts(context.Background(), db, tree))

	// 平台节点必须拿到 3 个通道（2 在线），否则前端不渲染展开箭头。
	require.Equal(t, 3, tree[0].Children[0].Count)
	require.Equal(t, 2, tree[0].Children[0].OnlineCount)
	// 真没通道的设备仍是 0，不能被误抬成"可展开"（会给出点了展开是空壳的假箭头）。
	require.Equal(t, 0, tree[0].Children[1].Count)
	// 上级行政区节点的口径是**设备数**，不能被这里改成通道数。
	require.Equal(t, 2, tree[0].Count)
	require.Equal(t, 1, tree[0].OnlineCount)
}

// TestDirectoryTreeDeviceLeavesCarryChannelCounts 是接线守卫：走真实 handler，
// 确认回填确实被调用。用 department 视图是为了避开行政区划视图对 sys_civil_code 的依赖。
func TestDirectoryTreeDeviceLeavesCarryChannelCounts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	require.NoError(t, db.Create(&gbmodels.GbDevice{
		DeviceID: "34020000002000000001", Name: "平台", OwnerDeptID: 10,
		Status: gbmodels.DeviceStatusOnline,
	}).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbChannel{
		{ChannelID: "34020000001320000001", DeviceID: "34020000002000000001", Name: "通道1", Status: gbmodels.ChannelStatusOnline, OwnerDeptID: 10},
		{ChannelID: "34020000001320000002", DeviceID: "34020000002000000001", Name: "通道2", Status: gbmodels.ChannelStatusOffline, OwnerDeptID: 10},
	}).Error)

	tree := gbdirectory.BuildDepartmentTree(nil, []gbmodels.GbDevice{{
		DeviceID: "34020000002000000001", Name: "平台", OwnerDeptID: 10,
		Status: gbmodels.DeviceStatusOnline,
	}})
	require.NotEmpty(t, tree)
	require.NoError(t, fillDirectoryDeviceChannelCounts(context.Background(), db, tree))

	var found bool
	var walk func([]gbdirectory.DirectoryNodeVO)
	walk = func(nodes []gbdirectory.DirectoryNodeVO) {
		for i := range nodes {
			if nodes[i].Device != nil && nodes[i].Device.DeviceID == "34020000002000000001" {
				found = true
				require.Equal(t, 2, nodes[i].Count, "设备叶节点必须带上通道数")
			}
			walk(nodes[i].Children)
		}
	}
	walk(tree)
	require.True(t, found, "目录树里应当能找平台设备节点")
}
