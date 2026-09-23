package bootstrap

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	openapimodels "uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestEnsureCoreReadCatalogIsIdempotentAndKeepsSysAPIAsMetadata(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&appmodels.SysApi{}, &openapimodels.CapabilityGroup{}, &openapimodels.Capability{}, &openapimodels.Operation{}))
	require.NoError(t, db.Create(&appmodels.SysApi{BaseModel: appmodels.BaseModel{ID: 42}, Title: "设备列表", Path: "/api/gb28181/device-mgmt/devices", Method: "GET", ApiGroup: "设备管理"}).Error)

	first, err := EnsureCoreReadCatalog(context.Background(), db, 7)
	require.NoError(t, err)
	require.Equal(t, 6, first.OperationsCreated)
	require.Equal(t, 6, first.CapabilitiesCreated)
	require.Equal(t, 3, first.GroupsCreated)
	second, err := EnsureCoreReadCatalog(context.Background(), db, 8)
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
	_, err = EnsureCoreReadCatalog(context.Background(), db, 9)
	require.NoError(t, err)
	require.NoError(t, db.First(&capability, capability.ID).Error)
	require.Equal(t, "custom", capability.Description)
}
