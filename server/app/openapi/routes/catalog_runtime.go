package routes

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/adapters"
	"uvplatform.cn/uvp-gb28181/app/openapi/catalog/bootstrap"
	catalogruntime "uvplatform.cn/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.cn/uvp-gb28181/app/openapi/catalog/store"
	openapimodels "uvplatform.cn/uvp-gb28181/app/openapi/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/resource"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

var catalogRuntimeSchemaModels = []any{
	&openapimodels.CapabilityGroup{},
	&openapimodels.Capability{},
	&openapimodels.Operation{},
	&openapimodels.Release{},
	&openapimodels.ReleaseItem{},
	&openapimodels.RuntimeState{},
}

// InitializeCatalogRuntime restores the immutable active OpenAPI release into
// a process-local runtime. The editable catalog is never used as a request
// dispatch source. During rolling migration, an installation may not have the
// catalog tables yet, or may have the tables before its first publication;
// both states deliberately return an empty runtime and no error. Once an
// active release exists, every hydration error is returned so startup cannot
// silently expose a different contract than the persisted release.
func InitializeCatalogRuntime(ctx context.Context, db *gorm.DB) (*catalogruntime.CatalogRuntime, error) {
	if db == nil {
		return nil, store.ErrCatalogRepositoryUnavailable
	}
	runtime, err := newCatalogRuntime(db)
	if err != nil {
		return nil, err
	}
	ctx = normalizeCatalogRuntimeContext(ctx)
	installed, err := catalogRuntimeSchemaInstalled(db)
	if err != nil {
		return nil, err
	}
	if !installed {
		active, err := catalogRuntimeActivePointer(ctx, db)
		if err != nil {
			return nil, err
		}
		if !active {
			return runtime, nil
		}
		return nil, fmt.Errorf("%w: active release cannot be hydrated while catalog schema is incomplete", store.ErrCatalogTableMissing)
	}
	// A migrated catalog that was never published is a dead end for every read
	// path (capability catalog, grantable scopes, gateway readiness). Publish
	// the initial release before hydrating so the installation becomes
	// self-consistent; an installation whose release already covers the
	// code-owned surface is untouched.
	outcome, err := bootstrap.EnsurePublishedCatalog(ctx, db, bootstrap.SystemActorID, runtime)
	if err != nil {
		// The caller collapses this into auth.ErrUnavailable, so the catalog
		// reason has to be recorded here or it is lost. `reason` is a code:
		// the sanitize core keeps error class/type only.
		app.Log(ctx).Error("OpenAPI 能力目录首次发布失败",
			zap.String("event", "startup.failed"),
			zap.String("phase", "openapi_catalog_publish"),
			zap.String("reason", catalogPublishFailureReason(err)),
			logging.Error(err))
		return nil, err
	}
	// A gap that the additive repair refused to close is not fatal — the
	// release is unchanged, so continuing exposes exactly the contract that was
	// already published — but it is not silent either: without this line the
	// operator only sees a capability that grants fine and then answers 403.
	if len(outcome.CoreScopesMissing) > 0 {
		app.Log(ctx).Error("OpenAPI 能力目录缺少本该由本进程提供的 scope",
			zap.String("event", "startup.failed"),
			zap.String("phase", "openapi_catalog_publish"),
			zap.String("reason", "catalog_core_scope_missing"),
			zap.Int("count", len(outcome.CoreScopesMissing)),
			zap.Strings("items", outcome.CoreScopesMissing))
	}
	err = store.NewRepository(db).Hydrate(ctx, runtime)
	if errors.Is(err, store.ErrNoActiveRelease) {
		return runtime, nil
	}
	if err != nil {
		return nil, err
	}
	return runtime, nil
}

// PublishCatalog publishes the current editable catalog as an operator action.
// It is the entry point behind `-publish-catalog`, so a deployment can repair
// or deliberately change its capability surface without restarting the
// service. Unlike the startup path it may shrink the surface; a catalog whose
// publishable surface is unchanged is still left alone.
func PublishCatalog(ctx context.Context, db *gorm.DB) (bootstrap.PublishOutcome, error) {
	if db == nil {
		return bootstrap.PublishOutcome{}, store.ErrCatalogRepositoryUnavailable
	}
	installed, err := catalogRuntimeSchemaInstalled(db)
	if err != nil {
		return bootstrap.PublishOutcome{}, err
	}
	if !installed {
		return bootstrap.PublishOutcome{}, fmt.Errorf("%w: run -migrate-up before publishing the capability catalog", store.ErrCatalogTableMissing)
	}
	runtime, err := newCatalogRuntime(db)
	if err != nil {
		return bootstrap.PublishOutcome{}, err
	}
	return bootstrap.PublishCurrentCatalog(normalizeCatalogRuntimeContext(ctx), db, bootstrap.SystemActorID, runtime)
}

// catalogPublishFailureReason names the failure class without printing the
// error text. The log keeps class/type only, so a code here is the difference
// between "publish failed" and "publish failed because the schema is half
// migrated".
func catalogPublishFailureReason(err error) string {
	switch {
	case errors.Is(err, store.ErrCatalogTableMissing):
		return "catalog_schema_incomplete"
	case errors.Is(err, bootstrap.ErrBootstrapConflict):
		return "catalog_seed_conflict"
	case errors.Is(err, bootstrap.ErrBootstrapDraftEmpty):
		return "catalog_draft_empty"
	case errors.Is(err, catalogruntime.ErrDraftInvalid):
		return "catalog_draft_invalid"
	default:
		return "catalog_publish_failed"
	}
}

// newCatalogRuntime builds the process-local runtime around the code-owned
// adapter registry. The registry is the only source of executable behaviour:
// catalog rows may select a registered key, they can never install one.
//
// Both planes have to be registered or publication would fail. The
// device/channel adapters carry the read surface; the delegated-plane
// declarations carry the ptz:* scopes, which the gateway dispatches through
// PTZDispatcher rather than through CatalogRuntime.Dispatch.
func newCatalogRuntime(db *gorm.DB) (*catalogruntime.CatalogRuntime, error) {
	registry := catalogruntime.NewAdapterRegistry()
	reader := resource.New(db)
	registrations := adapters.NewDeviceChannelAdapterRegistrations(reader)
	registrations = append(registrations, adapters.NewDelegatedPlaneRegistrations()...)
	for _, registration := range registrations {
		if err := registry.Register(registration); err != nil {
			return nil, err
		}
	}
	return catalogruntime.NewCatalogRuntime(registry, catalogSysAPIResolver{db: db}), nil
}

func catalogRuntimeSchemaInstalled(db *gorm.DB) (bool, error) {
	if db == nil {
		return false, store.ErrCatalogRepositoryUnavailable
	}
	for _, model := range catalogRuntimeSchemaModels {
		if !db.Migrator().HasTable(model) {
			return false, nil
		}
	}
	return true, nil
}

func catalogRuntimeActivePointer(ctx context.Context, db *gorm.DB) (bool, error) {
	if db == nil || !db.Migrator().HasTable(&openapimodels.RuntimeState{}) {
		return false, nil
	}
	var state openapimodels.RuntimeState
	result := db.WithContext(ctx).Where("id = ?", 1).First(&state)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if result.Error != nil {
		return false, result.Error
	}
	return state.ActiveRelease != nil && *state.ActiveRelease > 0, nil
}

type catalogSysAPIResolver struct {
	db *gorm.DB
}

func (resolver catalogSysAPIResolver) Resolve(ctx context.Context, path, method string) (catalogruntime.SysAPIAsset, error) {
	asset := catalogruntime.SysAPIAsset{Path: path, Method: method}
	if resolver.db == nil {
		return asset, gorm.ErrInvalidDB
	}
	var row appmodels.SysApi
	result := resolver.db.WithContext(normalizeCatalogRuntimeContext(ctx)).
		Where("path = ? AND method = ? AND deleted_at IS NULL", path, method).
		Order("id ASC").First(&row)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		asset.Deleted = true
		return asset, nil
	}
	if result.Error != nil {
		return catalogruntime.SysAPIAsset{}, result.Error
	}
	asset.ID = row.ID
	asset.Path = row.Path
	asset.Method = row.Method
	return asset, nil
}

func normalizeCatalogRuntimeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
