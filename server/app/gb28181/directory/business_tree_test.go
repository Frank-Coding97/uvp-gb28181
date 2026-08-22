package directory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestBuildBusinessTreeGroupsPhysicalDevicesWithoutDuplication(t *testing.T) {
	db := newPlacementDB(t)
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
	require.Len(t, tree, 2)
	require.Equal(t, "重点场所", tree[0].Name)
	require.Equal(t, 1, tree[0].Count)
	require.Len(t, tree[0].Children, 1)
	require.Equal(t, "门岗", tree[0].Children[0].Name)
	require.Equal(t, 1, tree[0].Children[0].Count)
	require.Equal(t, "未归属业务组织", tree[1].Name)
	require.Equal(t, 1, tree[1].Count)
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
