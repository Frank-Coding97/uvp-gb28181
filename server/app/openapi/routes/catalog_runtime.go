package routes

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.cn/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.cn/uvp-gb28181/app/openapi/catalog/store"
	openapimodels "uvplatform.cn/uvp-gb28181/app/openapi/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/resource"
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
	registry := catalogruntime.NewAdapterRegistry()
	reader := resource.New(db)
	for _, registration := range adapters.NewDeviceChannelAdapterRegistrations(reader) {
		if err := registry.Register(registration); err != nil {
			return nil, err
		}
	}
	runtime := catalogruntime.NewCatalogRuntime(registry, catalogSysAPIResolver{db: db})
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
	err = store.NewRepository(db).Hydrate(ctx, runtime)
	if errors.Is(err, store.ErrNoActiveRelease) {
		return runtime, nil
	}
	if err != nil {
		return nil, err
	}
	return runtime, nil
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
