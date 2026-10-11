package directory

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// newBusinessDB 业务组织视图专用测试库。
//
// 比 newPlacementDB 多建 gb_channel / gb_alarm_resource：业务树的「叶」是国标目录项里的
// 资源类节点(设备/摄像机/报警设备)，摄像机在平台里是 channel 节点、自己不带 device_id，
// 必须靠 gb_channel 反查物理设备，所以这两张表是统计的必要条件。
func newBusinessDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{},
		&gbmodels.GbCatalogNode{},
		&gbmodels.GbChannel{},
		&gbmodels.GbAlarmResource{},
	))
	return db
}

func TestBuildBusinessTreeGroupsPhysicalDevicesWithoutDuplication(t *testing.T) {
	db := newBusinessDB(t)
	devices := []gbmodels.GbDevice{
		{DeviceID: "34020000001170000001", Name: "NVR", OwnerDeptID: 10, Status: gbmodels.DeviceStatusOnline},
		{DeviceID: "34020000001170000002", Name: "NVR2", OwnerDeptID: 10},
	}
	require.NoError(t, db.Create(&devices).Error)
	group := gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeBizGroup, Name: "重点场所", Code: "34020000002150000001", Path: "/", Source: gbmodels.NodeSourceCatalog}
	virtual := gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeVirtualOrg, Name: "门岗", Code: "34020000002160000001", Path: "/", Source: gbmodels.NodeSourceCatalog}
	require.NoError(t, db.Create(&group).Error)
	virtual.ParentID = &group.ID
	require.NoError(t, db.Create(&virtual).Error)
	deviceNode := gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeDevice, Name: "NVR", Path: "/", DeviceID: &devices[0].ID, ParentID: &virtual.ID, Source: gbmodels.NodeSourceCatalog}
	otherNode := gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeDevice, Name: "NVR2", Path: "/", DeviceID: &devices[1].ID, Source: gbmodels.NodeSourceCatalog}
	require.NoError(t, db.Create(&deviceNode).Error)
	require.NoError(t, db.Create(&otherNode).Error)

	tree, err := BuildBusinessTree(context.Background(), db, 10)
	require.NoError(t, err)
	// ⛔ 2026-10-11 产品决策：没上报业务分组的设备(这里的 NVR2)不出现在业务组织视图，
	// 平台不再补「未归属业务组织」兜底节点 ⇒ 只剩挂到虚拟组织下的 NVR。
	require.Len(t, tree, 1)
	require.Equal(t, "重点场所", tree[0].Name)
	require.Equal(t, 1, tree[0].Count)
	require.Len(t, tree[0].Children, 1)
	require.Equal(t, "门岗", tree[0].Children[0].Name)
	require.Equal(t, 1, tree[0].Children[0].Count)
}

// ⛔⛔ 回归锁：国标附录 J 图 J.2 的业务树叶子是摄像机(132 段)，平台把它存成 channel。
// 只统计 node_type='device' 会让业务组织视图恒为 0/0 —— 这是 2026-10-11 实测到的线上现象。
func TestBuildBusinessTreeCountsCameraChannelAsLeaf(t *testing.T) {
	db := newBusinessDB(t)
	device := gbmodels.GbDevice{DeviceID: "37010301021180000007", Name: "UVP-Sim", OwnerDeptID: 4, Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(&device).Error)
	channel := gbmodels.GbChannel{ChannelID: "34020000001320000020", DeviceID: device.DeviceID, Name: "前置摄像头", OwnerDeptID: 4}
	require.NoError(t, db.Create(&channel).Error)

	group := gbmodels.GbCatalogNode{OwnerDeptID: 4, NodeType: gbmodels.NodeTypeBizGroup, Name: "重点场所", Code: "34020000002150000001", Path: "/", Source: gbmodels.NodeSourceCatalog}
	require.NoError(t, db.Create(&group).Error)
	virtual := gbmodels.GbCatalogNode{OwnerDeptID: 4, NodeType: gbmodels.NodeTypeVirtualOrg, Name: "门岗", Code: "34020000002160000001", Path: "/", ParentID: &group.ID, Source: gbmodels.NodeSourceCatalog}
	require.NoError(t, db.Create(&virtual).Error)
	// 通道节点本身不带 device_id(开发库实测全为 NULL)，只能靠 channel_id 反查所属设备。
	camera := gbmodels.GbCatalogNode{OwnerDeptID: 4, NodeType: gbmodels.NodeTypeChannel, Name: "前置摄像头", Code: "34020000001320000020", ChannelID: &channel.ID, ParentID: &virtual.ID, Path: "/", Source: gbmodels.NodeSourceCatalog}
	require.NoError(t, db.Create(&camera).Error)

	tree, err := BuildBusinessTree(context.Background(), db, 4)
	require.NoError(t, err)
	require.Len(t, tree, 1, "已归属的摄像机应当只落在业务分组这一层，树上不应多出别的节点")
	require.Equal(t, "重点场所", tree[0].Name)
	require.Equal(t, 1, tree[0].Count)
	require.Equal(t, 1, tree[0].OnlineCount)
	require.Len(t, tree[0].Children, 1)
	require.Equal(t, 1, tree[0].Children[0].Count)
}

// ⛔ 开发库真实形态：catalog/pipeline.go 的 resolveBusinessParent 在 device 分支建节点时
// 不回填 device_id，实测 gb_catalog_node.device_id 54 行全为 NULL。
// 若只认 device_id，计数依旧是 0。
func TestBuildBusinessTreeResolvesDeviceNodeByCodeWhenDeviceIDMissing(t *testing.T) {
	db := newBusinessDB(t)
	device := gbmodels.GbDevice{DeviceID: "37010301021180000007", Name: "UVP-Sim", OwnerDeptID: 4}
	require.NoError(t, db.Create(&device).Error)
	group := gbmodels.GbCatalogNode{OwnerDeptID: 4, NodeType: gbmodels.NodeTypeBizGroup, Name: "重点场所", Code: "34020000002150000001", Path: "/", Source: gbmodels.NodeSourceCatalog}
	require.NoError(t, db.Create(&group).Error)
	deviceNode := gbmodels.GbCatalogNode{OwnerDeptID: 4, NodeType: gbmodels.NodeTypeDevice, Name: "UVP-Sim", Code: device.DeviceID, ParentID: &group.ID, Path: "/", Source: gbmodels.NodeSourceCatalog}
	require.NoError(t, db.Create(&deviceNode).Error)
	require.Nil(t, deviceNode.DeviceID, "用例前提就是这个节点没有 device_id")

	tree, err := BuildBusinessTree(context.Background(), db, 4)
	require.NoError(t, err)
	require.Len(t, tree, 1)
	require.Equal(t, 1, tree[0].Count)
}

// ⛔⛔ 契约变更锁（2026-10-11 产品决策，老板拍板）：叶没挂在任何业务分组/虚拟组织下时
// **不生成任何节点** —— 业务组织视图是国标附录 J 的「设备上报了什么就展示什么」，
// 平台不补「未归属业务组织」兜底（该节点不是国标概念；且旧实现按部门各生成一个，
// 超管可见多部门时会平铺出 N 个同名节点，开发库实测 4 个）。
// ⚠️ 本用例旧名 TestBuildBusinessTreeKeepsUnattachedChannelInUnknownBucket，
// 断言的正是相反契约；按产品决策**有意翻转**，不是修 bug。没归属的设备去综合目录/设备管理查。
func TestBuildBusinessTreeHidesUnattachedLeafWithoutBusinessParent(t *testing.T) {
	db := newBusinessDB(t)
	device := gbmodels.GbDevice{DeviceID: "37010301021180000007", Name: "UVP-Sim", OwnerDeptID: 4}
	require.NoError(t, db.Create(&device).Error)
	channel := gbmodels.GbChannel{ChannelID: device.DeviceID, DeviceID: device.DeviceID, Name: "UVP-Sim", OwnerDeptID: 4}
	require.NoError(t, db.Create(&channel).Error)
	area := gbmodels.GbCatalogNode{OwnerDeptID: 4, NodeType: gbmodels.NodeTypeCivilCode, Name: "行政区 370103", Code: "370103", Path: "/", Source: gbmodels.NodeSourceCatalog}
	require.NoError(t, db.Create(&area).Error)
	self := gbmodels.GbCatalogNode{OwnerDeptID: 4, NodeType: gbmodels.NodeTypeChannel, Name: "UVP-Sim", Code: device.DeviceID, ChannelID: &channel.ID, ParentID: &area.ID, Path: "/", Source: gbmodels.NodeSourceCatalog}
	require.NoError(t, db.Create(&self).Error)

	tree, err := BuildBusinessTree(context.Background(), db, 4)
	require.NoError(t, err)
	require.Empty(t, tree, "设备没上报业务分组 ⇒ 业务组织视图里就不该出现任何节点")
}

// 同一台设备被业务分组与虚拟组织两级同时引用：两级都要计入，且按物理设备去重。
func TestBuildBusinessTreeDeduplicatesPhysicalDeviceAcrossNestedContainers(t *testing.T) {
	db := newBusinessDB(t)
	device := gbmodels.GbDevice{DeviceID: "37010301021180000007", OwnerDeptID: 4, Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(&device).Error)
	channels := []gbmodels.GbChannel{
		{ChannelID: "34020000001320000020", DeviceID: device.DeviceID, OwnerDeptID: 4},
		{ChannelID: "34020000001320000010", DeviceID: device.DeviceID, OwnerDeptID: 4},
	}
	require.NoError(t, db.Create(&channels).Error)
	group := gbmodels.GbCatalogNode{OwnerDeptID: 4, NodeType: gbmodels.NodeTypeBizGroup, Name: "重点场所", Code: "34020000002150000001", Path: "/"}
	require.NoError(t, db.Create(&group).Error)
	virtual := gbmodels.GbCatalogNode{OwnerDeptID: 4, NodeType: gbmodels.NodeTypeVirtualOrg, Name: "门岗", Code: "34020000002160000001", Path: "/", ParentID: &group.ID}
	require.NoError(t, db.Create(&virtual).Error)
	for i := range channels {
		node := gbmodels.GbCatalogNode{OwnerDeptID: 4, NodeType: gbmodels.NodeTypeChannel, Name: channels[i].Name, Code: channels[i].ChannelID, ChannelID: &channels[i].ID, ParentID: &virtual.ID, Path: "/"}
		require.NoError(t, db.Create(&node).Error)
	}

	tree, err := BuildBusinessTree(context.Background(), db, 4)
	require.NoError(t, err)
	require.Len(t, tree, 1)
	// 两个摄像机同属一台物理设备 ⇒ 业务分组与虚拟组织两级都应该是 1，不是 2。
	require.Equal(t, 1, tree[0].Count)
	require.Equal(t, 1, tree[0].OnlineCount)
	require.Len(t, tree[0].Children, 1)
	require.Equal(t, 1, tree[0].Children[0].Count)
}

func TestBuildAdministrativeTreeUsesAdministrativeKeys(t *testing.T) {
	db := newPlacementDB(t)
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "37011200001180000001", OwnerDeptID: 10}).Error)
	tree, err := BuildAdministrativeTree(context.Background(), db, 10, testCivilLookup())
	require.NoError(t, err)
	require.Len(t, tree, 1)
	require.Equal(t, "administrative:area:370000", tree[0].Key)
	require.Equal(t, "administrative:area:370112", tree[0].Children[0].Children[0].Key)
}
