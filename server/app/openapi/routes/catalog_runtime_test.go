package routes

import (
	"context"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/catalog/bootstrap"
	catalogstore "uvplatform.cn/uvp-gb28181/app/openapi/catalog/store"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestInitializeCatalogRuntimeKeepsLegacyDeploymentCompatible(t *testing.T) {
	db := newCatalogRuntimeTestDB(t)

	runtime, err := InitializeCatalogRuntime(context.Background(), db)
	require.NoError(t, err)
	require.NotNil(t, runtime)
	require.Nil(t, runtime.Active())
}

func TestInitializeCatalogRuntimePublishesInitialReleaseOnMigratedCatalog(t *testing.T) {
	db := newCatalogRuntimeTestDB(t)
	require.NoError(t, db.AutoMigrate(catalogRuntimeTables(t, true)...))

	runtime, err := InitializeCatalogRuntime(context.Background(), db)
	require.NoError(t, err)
	require.NotNil(t, runtime)
	require.NotNil(t, runtime.Active(), "已迁移但从未发布过的 catalog 必须自举出一个 active release")
	operation, err := runtime.Resolve("GET", "/openapi/v1/devices")
	require.NoError(t, err)
	require.Equal(t, "device:list", operation.Scope)
	_, ready := runtime.ReadyIdentity()
	require.True(t, ready, "自举发布必须绑上持久化 identity,否则网关最后一道门仍会 fail closed")

	// The release has to carry the PTZ delegated scopes too. They are not
	// dispatched through CatalogRuntime.Dispatch, but client.ScopePublished
	// reads this release.
	require.Equal(t, bootstrap.CoreCatalogScopes(), activeReleaseScopes(t, db))

	// 每次重启都会走这段。第二次只能读,不能再发一版。
	second, err := InitializeCatalogRuntime(context.Background(), db)
	require.NoError(t, err)
	require.NotNil(t, second.Active())
	var releases int64
	require.NoError(t, db.Model(&models.Release{}).Count(&releases).Error)
	require.Equal(t, int64(1), releases)
	var state models.RuntimeState
	require.NoError(t, db.First(&state, 1).Error)
	require.NotNil(t, state.ActiveRelease)
	require.EqualValues(t, 1, state.RuntimeEpoch)
}

func TestPublishCatalogIsIdempotentAndRequiresMigratedSchema(t *testing.T) {
	legacy := newCatalogRuntimeTestDB(t)
	_, err := PublishCatalog(context.Background(), legacy)
	require.ErrorIs(t, err, catalogstore.ErrCatalogTableMissing, "-publish-catalog 在未迁移的库上必须明确报错")

	db := newCatalogRuntimeTestDB(t)
	require.NoError(t, db.AutoMigrate(catalogRuntimeTables(t, true)...))
	first, err := PublishCatalog(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, bootstrap.PublishActionPublished, first.Action)
	second, err := PublishCatalog(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, bootstrap.PublishActionExisting, second.Action)
	require.Equal(t, first.ReleaseID, second.ReleaseID)
	var releases int64
	require.NoError(t, db.Model(&models.Release{}).Count(&releases).Error)
	require.Equal(t, int64(1), releases)
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
	// A release that already covers the code-owned surface is not revised.
	var releases int64
	require.NoError(t, db.Model(&models.Release{}).Count(&releases).Error)
	require.Equal(t, int64(1), releases, "已经完整的 release 不该被启动链路再发一版")
}

// An installation that published before this binary learned about the PTZ
// delegated scopes is repaired additively on the next startup.
func TestInitializeCatalogRuntimeRepairsPartialRelease(t *testing.T) {
	db := newCatalogRuntimeTestDB(t)
	require.NoError(t, db.AutoMigrate(catalogRuntimeTables(t, true)...))
	seedPartialCatalogRuntimeRelease(t, db)
	read := make([]string, 0, len(bootstrap.CoreCatalogScopes()))
	for _, scope := range bootstrap.CoreCatalogScopes() {
		if !strings.Contains(scope, "play:") && !strings.Contains(scope, "ptz:") {
			read = append(read, scope)
		}
	}
	require.Equal(t, read, activeReleaseScopes(t, db))

	runtime, err := InitializeCatalogRuntime(context.Background(), db)
	require.NoError(t, err)
	require.NotNil(t, runtime.Active())
	require.Equal(t, bootstrap.CoreCatalogScopes(), activeReleaseScopes(t, db))
	operation, err := runtime.Resolve("GET", "/openapi/v1/devices/34020000002000000001/channels/34020000001320000001/ptz/presets")
	require.NoError(t, err, "补齐后的 release 必须含 PTZ delegated scope")
	require.Equal(t, "ptz:preset:list", operation.Scope)
}

func TestInitializeCatalogRuntimeFailsClosedWhenActiveReleaseIsInvalid(t *testing.T) {
	db := newCatalogRuntimeTestDB(t)
	require.NoError(t, db.AutoMigrate(catalogRuntimeTables(t, true)...))
	seedCatalogRuntimeRelease(t, db)
	require.NoError(t, db.Model(&models.RuntimeState{}).Where("id = ?", 1).Update("status", models.RuntimeCatalogUnavailable).Error)

	runtime, err := InitializeCatalogRuntime(context.Background(), db)
	require.ErrorIs(t, err, catalogstore.ErrCatalogRuntimeUnavailable)
	require.Nil(t, runtime)
}

func TestInitializeCatalogRuntimeFailsClosedWhenActivePointerHasIncompleteSchema(t *testing.T) {
	db := newCatalogRuntimeTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.RuntimeState{}))
	activeRelease := int64(1)
	require.NoError(t, db.Create(&models.RuntimeState{ID: 1, ActiveRelease: &activeRelease, ActiveVersion: 1, Status: models.RuntimeCatalogReady}).Error)

	runtime, err := InitializeCatalogRuntime(context.Background(), db)
	require.ErrorIs(t, err, catalogstore.ErrCatalogTableMissing)
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
		&appmodels.SysApi{},
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

// activeReleaseScopes reads the scope set of the release the runtime pointer
// currently selects. Reading every release item would silently mix a superseded
// release into the comparison.
func activeReleaseScopes(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var state models.RuntimeState
	require.NoError(t, db.First(&state, 1).Error)
	require.NotNil(t, state.ActiveRelease)
	var scopes []string
	require.NoError(t, db.Model(&models.ReleaseItem{}).
		Where("release_id = ?", *state.ActiveRelease).Order("scope").Pluck("scope", &scopes).Error)
	return scopes
}

// seedCatalogRuntimeRelease publishes the complete code-owned surface the way
// an operator would, so the fixture is a realistic "already published"
// installation rather than a hand-written partial release.
func seedCatalogRuntimeRelease(t *testing.T, db *gorm.DB) {
	t.Helper()
	outcome, err := PublishCatalog(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, bootstrap.PublishActionPublished, outcome.Action)
}

// seedPartialCatalogRuntimeRelease publishes only the device/channel read
// surface, which is what an older binary's release looked like.
func seedPartialCatalogRuntimeRelease(t *testing.T, db *gorm.DB) {
	t.Helper()
	_, err := bootstrap.EnsureCoreCatalog(context.Background(), db, bootstrap.SystemActorID)
	require.NoError(t, err)
	var ids []int64
	require.NoError(t, db.Unscoped().Model(&models.Operation{}).
		Where("adapter_key LIKE ?", "delegated.%").Pluck("capability_id", &ids).Error)
	require.NotEmpty(t, ids)
	require.NoError(t, db.Unscoped().Where("capability_id IN ?", ids).Delete(&models.Operation{}).Error)
	require.NoError(t, db.Unscoped().Where("id IN ?", ids).Delete(&models.Capability{}).Error)

	repository := catalogstore.NewRepository(db)
	draft, err := repository.BuildDraft(context.Background())
	require.NoError(t, err)
	_, err = repository.Publish(context.Background(), draft, catalogstore.PublishOptions{Name: "legacy"})
	require.NoError(t, err)
}
