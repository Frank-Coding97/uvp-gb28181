package routes

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	appmodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	"uvplatform.com/uvp-gb28181/app/openapi/catalog/bootstrap"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	catalogstore "uvplatform.com/uvp-gb28181/app/openapi/catalog/store"
	"uvplatform.com/uvp-gb28181/app/openapi/contracts"
	"uvplatform.com/uvp-gb28181/app/openapi/models"
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

func TestCatalogRuntimeRegistersScopedDeviceQueryAdapters(t *testing.T) {
	db := newCatalogRuntimeTestDB(t)
	require.NoError(t, db.AutoMigrate(catalogRuntimeTables(t, true)...))
	runtime, err := InitializeCatalogRuntime(context.Background(), db)
	require.NoError(t, err)

	deviceID := "34020000002000000010"
	channelID := "37011200001310000010"
	for _, operation := range []struct {
		scope string
		path  string
	}{
		{"device:alarms", "/openapi/v1/devices/" + deviceID + "/alarms"},
		{"device:status-events", "/openapi/v1/devices/" + deviceID + "/status-events"},
		{"channel:precise-status", "/openapi/v1/devices/" + deviceID + "/channels/" + channelID + "/ptz/precise-status"},
		{"ptz:home-position:read", "/openapi/v1/devices/" + deviceID + "/channels/" + channelID + "/ptz/home-position"},
		{"ptz:cruise:read", "/openapi/v1/devices/" + deviceID + "/channels/" + channelID + "/ptz/cruise/tracks"},
		{"channel:target-track:read", "/openapi/v1/devices/" + deviceID + "/channels/" + channelID + "/target-track"},
	} {
		_, err := runtime.Dispatch(context.Background(), operation.scope, catalogruntime.Invocation{
			Method: "GET", Path: operation.path,
			Value: adapters.Request{DeviceID: deviceID, ChannelID: channelID},
		})
		require.ErrorIsf(t, err, adapters.ErrNotFound, "scope %s must dispatch to its query adapter", operation.scope)
	}
}

// An installation that published before this binary learned about the PTZ
// delegated scopes is repaired additively on the next startup.
func TestInitializeCatalogRuntimeRepairsPartialRelease(t *testing.T) {
	db := newCatalogRuntimeTestDB(t)
	require.NoError(t, db.AutoMigrate(catalogRuntimeTables(t, true)...))
	seedPartialCatalogRuntimeRelease(t, db)
	partial := make(map[string]struct{}, len(bootstrap.CoreCatalogScopes()))
	for _, scope := range bootstrap.CoreCatalogScopes() {
		partial[scope] = struct{}{}
	}
	for _, operation := range contracts.Operations() {
		if strings.HasPrefix(operation.AdapterKey, "delegated.") {
			delete(partial, operation.Scope)
		}
	}
	read := make([]string, 0, len(partial))
	for scope := range partial {
		read = append(read, scope)
	}
	sort.Strings(read)
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

func TestCatalogPublishFailureReasonClassifiesTimeout(t *testing.T) {
	require.Equal(t, "catalog_publish_timeout", catalogPublishFailureReason(context.DeadlineExceeded))
}

func TestCatalogHydrateReadsSysAPIMetadataInOneBatch(t *testing.T) {
	db := newCatalogRuntimeTestDB(t)
	require.NoError(t, db.AutoMigrate(catalogRuntimeTables(t, true)...))
	_, err := InitializeCatalogRuntime(context.Background(), db)
	require.NoError(t, err)
	queries := 0
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:sys_api_count", func(tx *gorm.DB) {
		if tx.Statement.Table == "sys_api" {
			queries++
		}
	}))
	_, err = InitializeCatalogRuntime(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, 1, queries, "hydration metadata checks must not query once per operation")
}

func TestCatalogSysAPIBatchPreservesOrderMissingAssetsAndFirstMatch(t *testing.T) {
	db := newCatalogRuntimeTestDB(t)
	require.NoError(t, db.AutoMigrate(&appmodels.SysApi{}))
	for _, row := range []appmodels.SysApi{
		{BaseModel: appmodels.BaseModel{ID: 20}, Path: "/internal", Method: "GET"},
		{BaseModel: appmodels.BaseModel{ID: 10}, Path: "/internal", Method: "GET"},
		{BaseModel: appmodels.BaseModel{ID: 30}, Path: "/internal", Method: "POST"},
	} {
		require.NoError(t, db.Create(&row).Error)
	}
	resolver := catalogSysAPIResolver{db: db}
	assets, err := resolver.ResolveBatch(context.Background(), []catalogruntime.SysAPIAsset{
		{Path: "/internal", Method: "POST"},
		{Path: "/missing", Method: "GET"},
		{Path: "/internal", Method: "GET"},
	})
	require.NoError(t, err)
	require.Len(t, assets, 3)
	require.EqualValues(t, 30, assets[0].ID)
	require.True(t, assets[1].Deleted)
	require.Equal(t, "/missing", assets[1].Path)
	require.EqualValues(t, 10, assets[2].ID)
	require.False(t, assets[2].Deleted)
}
