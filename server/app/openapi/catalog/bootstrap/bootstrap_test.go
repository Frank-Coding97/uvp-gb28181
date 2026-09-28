package bootstrap

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/adapters"
	openapimodels "uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestEnsureCoreCatalogIsIdempotentAndKeepsSysAPIAsMetadata(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&appmodels.SysApi{}, &openapimodels.CapabilityGroup{}, &openapimodels.Capability{}, &openapimodels.Operation{}))
	require.NoError(t, db.Create(&appmodels.SysApi{BaseModel: appmodels.BaseModel{ID: 42}, Title: "设备列表", Path: "/api/gb28181/device-mgmt/devices", Method: "GET", ApiGroup: "设备管理"}).Error)

	first, err := EnsureCoreCatalog(context.Background(), db, 7)
	require.NoError(t, err)
	require.Equal(t, len(CoreCatalogScopes()), first.OperationsCreated)
	require.Equal(t, len(CoreCatalogScopes()), first.CapabilitiesCreated)
	require.Equal(t, 3, first.GroupsCreated)
	second, err := EnsureCoreCatalog(context.Background(), db, 8)
	require.NoError(t, err)
	require.Zero(t, second.OperationsCreated)
	var groups int64
	require.NoError(t, db.Model(&openapimodels.CapabilityGroup{}).Count(&groups).Error)
	require.Equal(t, int64(3), groups)
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

// The seeded rows are the published contract. If they disagree with the legacy
// registry that app/openapi/client serves, one platform describes two different
// contracts depending on whether the catalog tables are migrated. The static
// registry is the compatibility source of truth for risk and idempotency, so
// every value is pinned here.
func TestEnsureCoreCatalogSeedsTheWholeCodeOwnedSurface(t *testing.T) {
	db := newPublishTestDB(t)
	_, err := EnsureCoreCatalog(context.Background(), db, SystemActorID)
	require.NoError(t, err)

	expected := map[string]struct {
		risk        string
		idempotency string
		adapterKey  string
	}{
		"device:list":        {risk: "read", idempotency: "none", adapterKey: adapters.DeviceListAdapterKey},
		"device:detail":      {risk: "read", idempotency: "none", adapterKey: adapters.DeviceDetailAdapterKey},
		"device:status":      {risk: "read", idempotency: "none", adapterKey: adapters.DeviceStatusAdapterKey},
		"channel:list":       {risk: "read", idempotency: "none", adapterKey: adapters.ChannelListAdapterKey},
		"channel:detail":     {risk: "read", idempotency: "none", adapterKey: adapters.ChannelDetailAdapterKey},
		"channel:status":     {risk: "read", idempotency: "none", adapterKey: adapters.ChannelStatusAdapterKey},
		"play:live:apply":    {risk: "media", idempotency: "none", adapterKey: adapters.MediaLiveApplyAdapterKey},
		"ptz:preset:list":    {risk: "read", idempotency: "none", adapterKey: adapters.PTZPresetListAdapterKey},
		"ptz:preset:save":    {risk: "control", idempotency: "required", adapterKey: adapters.PTZPresetSaveAdapterKey},
		"ptz:preset:call":    {risk: "control", idempotency: "required", adapterKey: adapters.PTZPresetCallAdapterKey},
		"ptz:preset:delete":  {risk: "control", idempotency: "required", adapterKey: adapters.PTZPresetDeleteAdapterKey},
		"ptz:operation:read": {risk: "read", idempotency: "none", adapterKey: adapters.PTZOperationReadAdapterKey},
	}
	require.Len(t, expected, len(CoreCatalogScopes()))

	var operations []openapimodels.Operation
	require.NoError(t, db.Find(&operations).Error)
	require.Len(t, operations, len(expected))
	for _, operation := range operations {
		var capability openapimodels.Capability
		require.NoError(t, db.Where("id = ?", operation.CapabilityID).First(&capability).Error)
		want, ok := expected[capability.Scope]
		require.True(t, ok, "意外的 scope %s", capability.Scope)
		require.Equal(t, want.adapterKey, operation.AdapterKey, capability.Scope)
		require.Equal(t, want.risk, operation.RiskLevel, capability.Scope)
		require.Equal(t, want.risk, capability.RiskLevel, capability.Scope)
		require.Equal(t, want.idempotency, operation.IdempotencyMode, capability.Scope)
		delete(expected, capability.Scope)
	}
	require.Empty(t, expected, "有 scope 没被播种")
}
