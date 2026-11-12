package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCapabilityCatalogModelsHaveStableTablesAndSnapshotFields(t *testing.T) {
	require.Equal(t, "sys_openapi_capability_group", (CapabilityGroup{}).TableName())
	require.Equal(t, "sys_openapi_capability", (Capability{}).TableName())
	require.Equal(t, "sys_openapi_operation", (Operation{}).TableName())
	require.Equal(t, "sys_openapi_release", (Release{}).TableName())
	require.Equal(t, "sys_openapi_release_item", (ReleaseItem{}).TableName())
	require.Equal(t, "sys_openapi_runtime_state", (RuntimeState{}).TableName())

	item := ReleaseItem{
		Scope:                  "device:list",
		GroupCode:              "device",
		GroupName:              "设备",
		CapabilityCode:         "device.list",
		CapabilityName:         "设备列表",
		Method:                 "GET",
		ExternalPath:           "/openapi/v1/devices",
		AdapterKey:             "device.list",
		AdapterContractVersion: "v1",
		ResourceType:           "device",
		RiskLevel:              "read",
		IdempotencyMode:        "none",
		RequestSchema:          `{"type":"object"}`,
		ResponseSchema:         `{"type":"array"}`,
		SysAPIID:               ptrInt64(42),
		SysAPIPath:             "/api/gb28181/device-mgmt/devices",
		SysAPIMethod:           "GET",
		SnapshotJSON:           `{"scope":"device:list","adapterKey":"device.list"}`,
	}
	b, err := json.Marshal(item)
	require.NoError(t, err)
	require.Contains(t, string(b), "device:list")
	require.Contains(t, string(b), "snapshotJson")
}

func ptrInt64(value int64) *int64 { return &value }

func TestCapabilityScopeCannotBeReusedAfterSoftDelete(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&CapabilityGroup{}, &Capability{}, &Operation{}, &Release{}, &ReleaseItem{}, &RuntimeState{}))

	group := CapabilityGroup{Code: "device", Name: "设备", Status: CatalogStatusActive}
	require.NoError(t, db.Create(&group).Error)
	capability := Capability{GroupID: group.ID, Code: "device.list", Scope: "device:list", Name: "设备列表", Status: CatalogStatusActive}
	require.NoError(t, db.Create(&capability).Error)
	deletedAt := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Model(&capability).Update("deleted_at", deletedAt).Error)

	duplicate := Capability{GroupID: group.ID, Code: "device.list.v2", Scope: "device:list", Name: "设备列表 v2", Status: CatalogStatusDraft}
	require.Error(t, db.Create(&duplicate).Error, "scope must remain reserved after soft deletion")
}

func TestCapabilityClientScopeHasNoForeignKeyToCatalog(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&Client{}, &ClientScope{}, &Capability{}))

	var foreignKeys []struct {
		Table string
	}
	require.NoError(t, db.Raw("PRAGMA foreign_key_list(sys_openapi_client_scope)").Scan(&foreignKeys).Error)
	for _, fk := range foreignKeys {
		require.NotEqual(t, "sys_openapi_capability", fk.Table)
	}
}

func TestReleaseSnapshotAllowsMultipleOperationsForOneScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&ReleaseItem{}))
	base := ReleaseItem{
		ReleaseID: 1, Scope: "device:control", GroupCode: "device-control", GroupName: "设备控制",
		CapabilityCode: "device.control", CapabilityName: "设备控制", AdapterKey: "device.control", AdapterContractVersion: "v1",
		ResourceType: "device", RiskLevel: "write", IdempotencyMode: "required", RequestSchema: "{}", ResponseSchema: "{}", SnapshotJSON: "{}",
	}
	first := base
	first.Method = "POST"
	first.ExternalPath = "/openapi/v1/devices/{deviceId}/control"
	require.NoError(t, db.Create(&first).Error)
	second := base
	second.Method = "GET"
	second.ExternalPath = "/openapi/v1/devices/{deviceId}/control/status"
	require.NoError(t, db.Create(&second).Error, "one capability scope may expose multiple operations")
}
