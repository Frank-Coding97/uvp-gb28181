package bootstrap

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	appmodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/openapi/contracts"
	openapimodels "uvplatform.com/uvp-gb28181/app/openapi/models"
)

func TestEnsureCoreCatalogIsIdempotentAndKeepsSysAPIAsMetadata(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&appmodels.SysApi{}, &openapimodels.CapabilityGroup{}, &openapimodels.Capability{}, &openapimodels.Operation{}))
	require.NoError(t, db.Create(&appmodels.SysApi{BaseModel: appmodels.BaseModel{ID: 42}, Title: "设备列表", Path: "/api/gb28181/device-mgmt/devices", Method: "GET", ApiGroup: "设备管理"}).Error)
	require.NoError(t, db.Create(&openapimodels.CapabilityGroup{Code: "playback", Name: "多屏播放", Sort: 20, Status: openapimodels.CatalogStatusDraft, RowVersion: 1}).Error)

	first, err := EnsureCoreCatalog(context.Background(), db, 7)
	require.NoError(t, err)
	require.Equal(t, len(CoreCatalogScopes()), first.OperationsCreated)
	require.Equal(t, len(CoreCatalogScopes()), first.CapabilitiesCreated)
	require.Equal(t, len(coreGroups), first.GroupsCreated)
	second, err := EnsureCoreCatalog(context.Background(), db, 8)
	require.NoError(t, err)
	require.Zero(t, second.OperationsCreated)
	var groups int64
	require.NoError(t, db.Model(&openapimodels.CapabilityGroup{}).Count(&groups).Error)
	require.Equal(t, int64(len(coreGroups)+1), groups)
	var retired openapimodels.CapabilityGroup
	require.NoError(t, db.Where("code = ?", "playback").First(&retired).Error)
	require.Equal(t, openapimodels.CatalogStatusDisabled, retired.Status)
	var capability openapimodels.Capability
	require.NoError(t, db.Where("scope = ?", "device:list").First(&capability).Error)
	require.NotNil(t, capability.SysAPIID)
	require.Equal(t, int64(42), *capability.SysAPIID)
	// The bootstrap only creates missing rows. It must not overwrite an edited
	// description or rebind a stable scope on a later startup.
	require.NoError(t, db.Model(&openapimodels.Capability{}).Where("id = ?", capability.ID).Update("description", "custom").Error)
	_, err = EnsureCoreCatalog(context.Background(), db, 9)
	require.NoError(t, err)
	require.NoError(t, db.First(&capability, capability.ID).Error)
	require.Equal(t, "custom", capability.Description)
}

func TestEnsureCoreCatalogRetiresRemovedPlaybackCapability(t *testing.T) {
	db := newPublishTestDB(t)
	legacy := openapimodels.CapabilityGroup{Code: "playback", Name: "多屏播放", Sort: 20, Status: openapimodels.CatalogStatusDraft, RowVersion: 1}
	require.NoError(t, db.Create(&legacy).Error)
	capability := openapimodels.Capability{
		GroupID: legacy.ID, Code: "play.live.apply", Scope: "play:live:apply", Name: "实时点播授权",
		ResourceType: "channel", RiskLevel: "media", Status: openapimodels.CatalogStatusDraft, RowVersion: 1,
	}
	require.NoError(t, db.Create(&capability).Error)

	_, err := EnsureCoreCatalog(context.Background(), db, SystemActorID)
	require.NoError(t, err)
	require.NoError(t, db.First(&capability, capability.ID).Error)
	var target openapimodels.CapabilityGroup
	require.NoError(t, db.Where("code = ?", "device-management").First(&target).Error)
	require.Equal(t, legacy.ID, capability.GroupID)
	require.Equal(t, openapimodels.CatalogStatusDisabled, capability.Status)
	var retired openapimodels.CapabilityGroup
	require.NoError(t, db.Where("code = ?", "playback").First(&retired).Error)
	require.Equal(t, openapimodels.CatalogStatusDisabled, retired.Status)
}

// The seeded rows are a projection of the code-owned operation registry. This
// fixture covers the whole surface so adding a capability cannot silently omit
// it from the editable catalog or change its adapter/risk/idempotency metadata.
func TestEnsureCoreCatalogSeedsTheWholeCodeOwnedSurface(t *testing.T) {
	db := newPublishTestDB(t)
	_, err := EnsureCoreCatalog(context.Background(), db, SystemActorID)
	require.NoError(t, err)

	expected := make(map[string]contracts.Operation)
	for _, operation := range contracts.Operations() {
		expected[operation.Scope] = operation
	}
	require.Contains(t, expected, "device:firmware-upgrade:execute")
	require.Contains(t, expected, "device:firmware-upgrade:read")
	require.Len(t, expected, len(CoreCatalogScopes()))

	var operations []openapimodels.Operation
	require.NoError(t, db.Find(&operations).Error)
	require.Len(t, operations, len(expected))
	for _, operation := range operations {
		var capability openapimodels.Capability
		require.NoError(t, db.Where("id = ?", operation.CapabilityID).First(&capability).Error)
		want, ok := expected[capability.Scope]
		require.True(t, ok, "意外的 scope %s", capability.Scope)
		require.Equal(t, want.AdapterKey, operation.AdapterKey, capability.Scope)
		require.Equal(t, want.RiskLevel, operation.RiskLevel, capability.Scope)
		require.Equal(t, want.RiskLevel, capability.RiskLevel, capability.Scope)
		require.Equal(t, want.IdempotencyMode, operation.IdempotencyMode, capability.Scope)
		require.Equal(t, want.Method, operation.Method, capability.Scope)
		require.Equal(t, want.ExternalPath, operation.ExternalPath, capability.Scope)
		delete(expected, capability.Scope)
	}
	require.Empty(t, expected, "有 scope 没被播种")
}

func TestEnsureCoreCatalogPreservesExistingPresetDeleteIdentity(t *testing.T) {
	db := newPublishTestDB(t)
	group := openapimodels.CapabilityGroup{Code: "device-control", Name: "设备控制", Status: openapimodels.CatalogStatusActive, RowVersion: 1}
	require.NoError(t, db.Create(&group).Error)
	capability := openapimodels.Capability{GroupID: group.ID, Code: "ptz.preset.delete", Scope: "ptz:preset:delete", Name: "删除预置位", Status: openapimodels.CatalogStatusActive, RowVersion: 1}
	require.NoError(t, db.Create(&capability).Error)
	operation := openapimodels.Operation{CapabilityID: capability.ID, Code: "ptz.preset.delete", Name: capability.Name, Method: "DELETE", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}", AdapterKey: "delegated.ptz.preset-delete.v1", AdapterContractVersion: "v1", Status: openapimodels.CatalogStatusActive, RequestSchema: "{}", ResponseSchema: "{}"}
	require.NoError(t, db.Create(&operation).Error)
	result, err := EnsureCoreCatalog(context.Background(), db, SystemActorID)
	require.NoError(t, err, "existing stable operation code must be reused without reserving its route again")
	require.Equal(t, len(CoreCatalogScopes())-1, result.OperationsCreated)
	require.NoError(t, db.First(&operation, operation.ID).Error)
	require.Equal(t, "ptz.preset.delete", operation.Code)
}
