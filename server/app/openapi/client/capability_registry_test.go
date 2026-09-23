package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	catalogstore "uvplatform.cn/uvp-gb28181/app/openapi/catalog/store"
	openapimodels "uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestCapabilityCatalogReusesSysAPITitlesAndGroups(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&appmodels.SysApi{}))
	rows := make([]appmodels.SysApi, 0, len(capabilityDefinitions))
	for index, definition := range capabilityDefinitions {
		rows = append(rows, appmodels.SysApi{
			BaseModel: appmodels.BaseModel{ID: uint(index + 1)},
			Title:     "标题 " + definition.scope,
			Path:      definition.internalPath,
			Method:    definition.method,
			ApiGroup:  capabilityGroupNames[definition.groupCode],
		})
	}
	require.NoError(t, db.Create(&rows).Error)

	catalog, err := CapabilityCatalog(context.Background(), db)
	require.NoError(t, err)
	require.Len(t, catalog, 3)
	require.Equal(t, "device-control", catalog[0].Code)
	require.Equal(t, "设备控制", catalog[0].Name)
	require.Equal(t, "ptz:operation:read", catalog[0].Capabilities[0].Scope)
	require.Equal(t, "device-management", catalog[1].Code)
	require.Equal(t, "设备管理", catalog[1].Name)
	require.Equal(t, "channel:detail", catalog[1].Capabilities[0].Scope)
	require.Equal(t, "标题 channel:detail", catalog[1].Capabilities[0].Name)
	require.Equal(t, "/openapi/v1/devices/{deviceId}/channels/{channelId}", catalog[1].Capabilities[0].ExternalPath)
	require.Equal(t, "playback", catalog[2].Code)
}

func TestCapabilityCatalogFailsClosedWhenInternalAssetDrifts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&appmodels.SysApi{}))
	definition := capabilityDefinitions[0]
	require.NoError(t, db.Create(&appmodels.SysApi{BaseModel: appmodels.BaseModel{ID: 1}, Title: "设备列表", Path: definition.internalPath, Method: definition.method, ApiGroup: "错误分组"}).Error)
	_, err = CapabilityCatalog(context.Background(), db)
	require.ErrorIs(t, err, ErrCapabilityDrift)
}

func TestCapabilityCatalogUsesActiveReleaseSnapshotWhenAvailable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&openapimodels.Release{}, &openapimodels.ReleaseItem{}, &openapimodels.RuntimeState{}))
	now := time.Now().UTC()
	release := openapimodels.Release{Version: 7, Name: "device-read", Status: openapimodels.ReleaseStatusPublished, SnapshotHash: "snapshot", ItemCount: 2, PublishedAt: &now, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&release).Error)
	items := []openapimodels.ReleaseItem{
		{ReleaseID: release.ID, GroupCode: "device-management", GroupName: "设备管理", CapabilityCode: "device.list", CapabilityName: "设备列表快照", Scope: "device:list", Method: "GET", ExternalPath: "/openapi/v1/devices", AdapterKey: "resource.device.list.v1", AdapterContractVersion: "v1", ResourceType: "device", RiskLevel: "read", IdempotencyMode: "none", RequestSchema: "{}", ResponseSchema: "{}", CreatedAt: now},
		{ReleaseID: release.ID, GroupCode: "device-management", GroupName: "设备管理", CapabilityCode: "channel.list", CapabilityName: "通道列表快照", Scope: "channel:list", Method: "GET", ExternalPath: "/openapi/v1/devices/{deviceId}/channels", AdapterKey: "resource.channel.list.v1", AdapterContractVersion: "v1", ResourceType: "channel", RiskLevel: "read", IdempotencyMode: "none", RequestSchema: "{}", ResponseSchema: "{}", CreatedAt: now},
	}
	for index := range items {
		items[index].SnapshotJSON = testReleaseItemSnapshotJSON(items[index])
	}
	release.SnapshotHash = testReleaseItemsHash(items)
	release.UpdatedAt = now
	require.NoError(t, db.Model(&release).Updates(map[string]any{"snapshot_hash": release.SnapshotHash}).Error)
	require.NoError(t, db.Create(&items).Error)
	require.NoError(t, db.Create(&openapimodels.RuntimeState{ID: 1, ActiveRelease: &release.ID, ActiveVersion: release.Version, SnapshotHash: release.SnapshotHash, Status: openapimodels.RuntimeCatalogReady, RuntimeEpoch: 1, LoadedAt: &now, UpdatedAt: now}).Error)

	catalog, err := CapabilityCatalog(context.Background(), db)
	require.NoError(t, err)
	require.Len(t, catalog, 1)
	require.Equal(t, "设备管理", catalog[0].Name)
	require.Equal(t, []string{"channel:list", "device:list"}, []string{catalog[0].Capabilities[0].Scope, catalog[0].Capabilities[1].Scope})

	scopes, err := SupportedScopesFromDB(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, []string{"channel:list", "device:list"}, scopes)
}

func TestSupportedScopesFromDBFallsBackBeforeFirstRelease(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	scopes, err := SupportedScopesFromDB(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, SupportedScopes(), scopes)
}

func TestSupportedScopesFromDBFailsClosedWhenCatalogSchemaHasNoRelease(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&openapimodels.CapabilityGroup{}, &openapimodels.Capability{}, &openapimodels.Operation{},
		&openapimodels.Release{}, &openapimodels.ReleaseItem{}, &openapimodels.RuntimeState{},
	))
	_, err = SupportedScopesFromDB(context.Background(), db)
	require.ErrorIs(t, err, ErrCapabilityRuntimeUnavailable)
	_, err = CapabilityCatalog(context.Background(), db)
	require.ErrorIs(t, err, ErrCapabilityRuntimeUnavailable)
}

func TestCapabilityReadersRejectPartialCatalogSchemaInsteadOfFallingBack(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&openapimodels.RuntimeState{}))

	_, err = CapabilityCatalog(context.Background(), db)
	require.ErrorIs(t, err, catalogstore.ErrCatalogTableMissing)
	_, err = SupportedScopesFromDB(context.Background(), db)
	require.ErrorIs(t, err, catalogstore.ErrCatalogTableMissing)
}

func TestCapabilityCatalogFailsClosedWhenPersistedRuntimeIsUnavailable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&openapimodels.RuntimeState{}))
	active := int64(9)
	require.NoError(t, db.Create(&openapimodels.RuntimeState{ID: 1, ActiveRelease: &active, Status: openapimodels.RuntimeCatalogUnavailable, UpdatedAt: time.Now().UTC()}).Error)
	_, err = SupportedScopesFromDB(context.Background(), db)
	require.ErrorIs(t, err, ErrCapabilityRuntimeUnavailable)
}

func TestActiveReleaseIntegrityFailuresFailClosedForCapabilityReaders(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, db *gorm.DB)
	}{
		{
			name: "runtime active version drift",
			mutate: func(t *testing.T, db *gorm.DB) {
				var state openapimodels.RuntimeState
				require.NoError(t, db.First(&state, 1).Error)
				require.NoError(t, db.Model(&state).Update("active_version", state.ActiveVersion+1).Error)
			},
		},
		{
			name: "runtime snapshot hash drift",
			mutate: func(t *testing.T, db *gorm.DB) {
				require.NoError(t, db.Model(&openapimodels.RuntimeState{}).Where("id = ?", 1).Update("snapshot_hash", "different").Error)
			},
		},
		{
			name: "release status drift",
			mutate: func(t *testing.T, db *gorm.DB) {
				require.NoError(t, db.Model(&openapimodels.Release{}).Where("id = ?", 1).Update("status", openapimodels.ReleaseStatusSuperseded).Error)
			},
		},
		{
			name: "release item count drift",
			mutate: func(t *testing.T, db *gorm.DB) {
				require.NoError(t, db.Model(&openapimodels.Release{}).Where("id = ?", 1).Update("item_count", 1).Error)
			},
		},
		{
			name: "release snapshot hash tampered",
			mutate: func(t *testing.T, db *gorm.DB) {
				require.NoError(t, db.Model(&openapimodels.Release{}).Where("id = ?", 1).Update("snapshot_hash", "different").Error)
			},
		},
		{
			name: "release item snapshot tampered",
			mutate: func(t *testing.T, db *gorm.DB) {
				var item openapimodels.ReleaseItem
				require.NoError(t, db.Order("id").First(&item).Error)
				require.NoError(t, db.Model(&item).Update("snapshot_json", `{"scope":"tampered"}`).Error)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newActiveCapabilityCatalogDB(t)
			test.mutate(t, db)

			_, err := CapabilityCatalog(context.Background(), db)
			require.ErrorIs(t, err, catalogstore.ErrCatalogDependency)
			_, err = SupportedScopesFromDB(context.Background(), db)
			require.ErrorIs(t, err, catalogstore.ErrCatalogDependency)
		})
	}
}

func newActiveCapabilityCatalogDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&openapimodels.Release{}, &openapimodels.ReleaseItem{}, &openapimodels.RuntimeState{}))
	now := time.Now().UTC()
	release := openapimodels.Release{Version: 7, Name: "device-read", Status: openapimodels.ReleaseStatusPublished, ItemCount: 2, PublishedAt: &now, CreatedAt: now, UpdatedAt: now}
	items := []openapimodels.ReleaseItem{
		{GroupCode: "device-management", GroupName: "设备管理", CapabilityCode: "device.list", CapabilityName: "设备列表快照", Scope: "device:list", Method: "GET", ExternalPath: "/openapi/v1/devices", AdapterKey: "resource.device.list.v1", AdapterContractVersion: "v1", ResourceType: "device", RiskLevel: "read", IdempotencyMode: "none", RequestSchema: "{}", ResponseSchema: "{}", CreatedAt: now},
		{GroupCode: "device-management", GroupName: "设备管理", CapabilityCode: "channel.list", CapabilityName: "通道列表快照", Scope: "channel:list", Method: "GET", ExternalPath: "/openapi/v1/devices/{deviceId}/channels", AdapterKey: "resource.channel.list.v1", AdapterContractVersion: "v1", ResourceType: "channel", RiskLevel: "read", IdempotencyMode: "none", RequestSchema: "{}", ResponseSchema: "{}", CreatedAt: now},
	}
	for index := range items {
		items[index].SnapshotJSON = testReleaseItemSnapshotJSON(items[index])
	}
	release.SnapshotHash = testReleaseItemsHash(items)
	require.NoError(t, db.Create(&release).Error)
	for index := range items {
		items[index].ReleaseID = release.ID
	}
	require.NoError(t, db.Create(&items).Error)
	require.NoError(t, db.Create(&openapimodels.RuntimeState{ID: 1, ActiveRelease: &release.ID, ActiveVersion: release.Version, SnapshotHash: release.SnapshotHash, Status: openapimodels.RuntimeCatalogReady, RuntimeEpoch: 1, LoadedAt: &now, UpdatedAt: now}).Error)
	return db
}

type testReleaseItemSnapshot struct {
	CapabilityID           int64  `json:"capabilityId"`
	OperationID            int64  `json:"operationId"`
	GroupCode              string `json:"groupCode"`
	GroupName              string `json:"groupName"`
	CapabilityCode         string `json:"capabilityCode"`
	CapabilityName         string `json:"capabilityName"`
	OperationName          string `json:"operationName"`
	Scope                  string `json:"scope"`
	Method                 string `json:"method"`
	ExternalPath           string `json:"externalPath"`
	AdapterKey             string `json:"adapterKey"`
	AdapterContractVersion string `json:"adapterContractVersion"`
	ResourceType           string `json:"resourceType"`
	RiskLevel              string `json:"riskLevel"`
	IdempotencyMode        string `json:"idempotencyMode"`
	RequestSchema          string `json:"requestSchema"`
	ResponseSchema         string `json:"responseSchema"`
	SysAPIID               int64  `json:"sysApiId,omitempty"`
	SysAPIPath             string `json:"sysApiPath"`
	SysAPIMethod           string `json:"sysApiMethod"`
	Sort                   int    `json:"sort"`
}

func testReleaseItemSnapshotJSON(item openapimodels.ReleaseItem) string {
	raw, err := json.Marshal(testReleaseItemSnapshot{
		CapabilityID: pointerValueForTest(item.CapabilityID), OperationID: pointerValueForTest(item.OperationID),
		GroupCode: item.GroupCode, GroupName: item.GroupName, CapabilityCode: item.CapabilityCode,
		CapabilityName: item.CapabilityName, OperationName: item.CapabilityName, Scope: item.Scope,
		Method: item.Method, ExternalPath: item.ExternalPath, AdapterKey: item.AdapterKey,
		AdapterContractVersion: item.AdapterContractVersion, ResourceType: item.ResourceType,
		RiskLevel: item.RiskLevel, IdempotencyMode: item.IdempotencyMode, RequestSchema: item.RequestSchema,
		ResponseSchema: item.ResponseSchema, SysAPIID: pointerValueForTest(item.SysAPIID), SysAPIPath: item.SysAPIPath,
		SysAPIMethod: item.SysAPIMethod, Sort: item.Sort,
	})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func testReleaseItemsHash(items []openapimodels.ReleaseItem) string {
	canonical := make([]string, 0, len(items))
	for _, item := range items {
		canonical = append(canonical, item.SnapshotJSON)
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		panic(err)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func pointerValueForTest(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
