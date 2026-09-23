package routes

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/openapi/catalog/store"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestInitializeCatalogRuntimeKeepsLegacyDeploymentCompatible(t *testing.T) {
	db := newCatalogRuntimeTestDB(t)

	runtime, err := InitializeCatalogRuntime(context.Background(), db)
	require.NoError(t, err)
	require.NotNil(t, runtime)
	require.Nil(t, runtime.Active())
}

func TestInitializeCatalogRuntimeKeepsCatalogMigrationWithoutActiveReleaseCompatible(t *testing.T) {
	db := newCatalogRuntimeTestDB(t)
	require.NoError(t, db.AutoMigrate(catalogRuntimeTables(t, true)...))

	runtime, err := InitializeCatalogRuntime(context.Background(), db)
	require.NoError(t, err)
	require.NotNil(t, runtime)
	require.Nil(t, runtime.Active())
}

func TestInitializeCatalogRuntimeHydratesPublishedRelease(t *testing.T) {
	db := newCatalogRuntimeTestDB(t)
	require.NoError(t, db.AutoMigrate(catalogRuntimeTables(t, true)...))
	seedCatalogRuntimeRelease(t, db)

	runtime, err := InitializeCatalogRuntime(context.Background(), db)
	require.NoError(t, err)
	require.NotNil(t, runtime)
	require.NotNil(t, runtime.Active())
	operation, err := runtime.Resolve("GET", "/openapi/v1/devices")
	require.NoError(t, err)
	require.Equal(t, "device:list", operation.Scope)
}

func TestInitializeCatalogRuntimeFailsClosedWhenActiveReleaseIsInvalid(t *testing.T) {
	db := newCatalogRuntimeTestDB(t)
	require.NoError(t, db.AutoMigrate(catalogRuntimeTables(t, true)...))
	seedCatalogRuntimeRelease(t, db)
	require.NoError(t, db.Model(&models.RuntimeState{}).Where("id = ?", 1).Update("status", models.RuntimeCatalogUnavailable).Error)

	runtime, err := InitializeCatalogRuntime(context.Background(), db)
	require.ErrorIs(t, err, store.ErrCatalogRuntimeUnavailable)
	require.Nil(t, runtime)
}

func TestInitializeCatalogRuntimeFailsClosedWhenActivePointerHasIncompleteSchema(t *testing.T) {
	db := newCatalogRuntimeTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.RuntimeState{}))
	activeRelease := int64(1)
	require.NoError(t, db.Create(&models.RuntimeState{ID: 1, ActiveRelease: &activeRelease, ActiveVersion: 1, Status: models.RuntimeCatalogReady}).Error)

	runtime, err := InitializeCatalogRuntime(context.Background(), db)
	require.ErrorIs(t, err, store.ErrCatalogTableMissing)
	require.Nil(t, runtime)
}

func newCatalogRuntimeTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	return db
}

func catalogRuntimeTables(_ *testing.T, withClientScope bool) []any {
	tables := []any{
		&models.CapabilityGroup{},
		&models.Capability{},
		&models.Operation{},
		&models.Release{},
		&models.ReleaseItem{},
		&models.RuntimeState{},
	}
	if withClientScope {
		tables = append(tables, &models.ClientScope{})
	}
	return tables
}

func seedCatalogRuntimeRelease(t *testing.T, db *gorm.DB) {
	group := models.CapabilityGroup{Code: "device-management", Name: "设备管理", Status: models.CatalogStatusActive}
	require.NoError(t, db.Create(&group).Error)
	capability := models.Capability{
		GroupID: group.ID, Code: "device.list", Scope: "device:list", Name: "设备列表",
		ResourceType: "device", RiskLevel: "read", Status: models.CatalogStatusActive,
	}
	require.NoError(t, db.Create(&capability).Error)
	require.NoError(t, db.Create(&models.Operation{
		CapabilityID: capability.ID, Code: "list", Name: "设备列表", Method: "GET",
		ExternalPath: "/openapi/v1/devices", AdapterKey: "resource.device.list.v1",
		AdapterContractVersion: "v1", ResourceType: "device", RiskLevel: "read",
		IdempotencyMode: "none", RequestSchema: `{}`, ResponseSchema: `{}`,
		Status: models.CatalogStatusActive,
	}).Error)

	repo := store.NewRepository(db)
	draft, err := repo.BuildDraft(context.Background())
	require.NoError(t, err)
	_, err = repo.Publish(context.Background(), draft, store.PublishOptions{Version: 1, Name: "initial"})
	require.NoError(t, err)
}
