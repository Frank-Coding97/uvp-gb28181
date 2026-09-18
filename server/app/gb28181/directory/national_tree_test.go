package directory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestBuildNationalTreeReadableHierarchyAndUnknown(t *testing.T) {
	db := newPlacementDB(t)
	devices := []gbmodels.GbDevice{
		{DeviceID: "37011200001180000001", Name: "D1", OwnerDeptID: 10, Status: gbmodels.DeviceStatusOnline},
		{DeviceID: "37011200001180000002", Name: "D2", OwnerDeptID: 10},
		{DeviceID: "invalid", Name: "D3", OwnerDeptID: 10},
		{DeviceID: "37011200001180000009", Name: "hidden", OwnerDeptID: 20},
	}
	require.NoError(t, db.Create(&devices).Error)
	tree, err := BuildNationalTree(context.Background(), db, 10, testCivilLookup())
	require.NoError(t, err)
	require.Len(t, tree, 2)
	require.Equal(t, "山东省", tree[0].Name)
	require.Equal(t, 2, tree[0].Count)
	require.Equal(t, 1, tree[0].OnlineCount)
	require.Equal(t, "济南市", tree[0].Children[0].Name)
	require.Equal(t, "历城区", tree[0].Children[0].Children[0].Name)
	require.Equal(t, "national:unknown", tree[1].Key)
	require.Equal(t, 1, tree[1].Count)
	require.True(t, tree[1].ReadOnly)
}

func TestBuildNationalTreeEmpty(t *testing.T) {
	db := newPlacementDB(t)
	tree, err := BuildNationalTree(context.Background(), db, 10, testCivilLookup())
	require.NoError(t, err)
	require.Empty(t, tree)
}

func TestBuildNationalTreeKeepsReportedOrganizations(t *testing.T) {
	db := newPlacementDB(t)
	device := gbmodels.GbDevice{DeviceID: "37011200001180000001", OwnerDeptID: 10}
	require.NoError(t, db.Create(&device).Error)
	area := gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeCivilCode, Path: "/", Name: "行政区 370112", CivilCode: "370112", Source: gbmodels.NodeSourceCatalog}
	require.NoError(t, db.Create(&area).Error)
	org := gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeBizGroup, ParentID: &area.ID, Path: "/", Name: "园区 A", Source: gbmodels.NodeSourceCatalog}
	require.NoError(t, db.Create(&org).Error)
	deviceNode := gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeDevice, ParentID: &org.ID, Path: "/", DeviceID: &device.ID, Source: gbmodels.NodeSourceCatalog}
	require.NoError(t, db.Create(&deviceNode).Error)

	tree, err := BuildNationalTree(context.Background(), db, 10, testCivilLookup())
	require.NoError(t, err)
	county := tree[0].Children[0].Children[0]
	require.Len(t, county.Children, 1)
	require.Equal(t, "园区 A", county.Children[0].Name)
	require.Equal(t, "organization", county.Children[0].Type)
	require.Equal(t, 1, county.Children[0].Count)
}
