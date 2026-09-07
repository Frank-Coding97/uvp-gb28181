package directory

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newPlacementDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbCatalogNode{}))
	return db
}

func TestResolveNationalPlacementsPriorityAndFallback(t *testing.T) {
	db := newPlacementDB(t)
	devices := []gbmodels.GbDevice{
		{DeviceID: "37010000001180000001", OwnerDeptID: 10},
		{DeviceID: "37011200001180000002", OwnerDeptID: 10},
		{DeviceID: "not-standard", OwnerDeptID: 10},
	}
	require.NoError(t, db.Create(&devices).Error)
	area := gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeCivilCode, Path: "/", Name: "历城区", Code: "370112", CivilCode: "370112", Source: gbmodels.NodeSourceCatalog}
	require.NoError(t, db.Create(&area).Error)
	deviceNode := gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeDevice, ParentID: &area.ID, Path: fmt.Sprintf("/%d/", area.ID), DeviceID: &devices[0].ID, Source: gbmodels.NodeSourceCatalog}
	require.NoError(t, db.Create(&deviceNode).Error)
	invalidArea := gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeCivilCode, Path: "/", Name: "未知", Code: "999999", CivilCode: "999999"}
	require.NoError(t, db.Create(&invalidArea).Error)
	invalidNode := gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeDevice, ParentID: &invalidArea.ID, Path: fmt.Sprintf("/%d/", invalidArea.ID), DeviceID: &devices[1].ID}
	require.NoError(t, db.Create(&invalidNode).Error)

	got, err := ResolveNationalPlacements(context.Background(), db, 10, testCivilLookup())
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, "370112", got[devices[0].ID].ResolvedCode)
	require.Equal(t, PlacementCatalog, got[devices[0].ID].Source)
	require.Equal(t, "370112", got[devices[1].ID].ResolvedCode)
	require.Equal(t, PlacementDeviceID, got[devices[1].ID].Source)
	require.Equal(t, "999999", got[devices[1].ID].RawCode)
	require.Equal(t, "national:unknown", got[devices[2].ID].Key)
	require.NotEmpty(t, got[devices[2].ID].Reason)
}

func TestResolveNationalPlacementsIsBatchOriented(t *testing.T) {
	db := newPlacementDB(t)
	devices := make([]gbmodels.GbDevice, 1000)
	for i := range devices {
		devices[i] = gbmodels.GbDevice{DeviceID: fmt.Sprintf("370112%014d", i), OwnerDeptID: 10}
	}
	require.NoError(t, db.CreateInBatches(&devices, 200).Error)
	got, err := ResolveNationalPlacements(context.Background(), db, 10, testCivilLookup())
	require.NoError(t, err)
	require.Len(t, got, 1000)
}

func testCivilLookup() fakeCivilCodes {
	return fakeCivilCodes{
		"370000": {Code: "370000", ShortName: "山东省", Level: 1},
		"370100": {Code: "370100", ShortName: "济南市", ParentCode: "370000", Level: 2},
		"370112": {Code: "370112", ShortName: "历城区", ParentCode: "370100", Level: 3},
	}
}
