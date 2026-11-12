package bootstrap

import (
	"context"

	"gorm.io/gorm"
	appmodels "uvplatform.com/uvp-gb28181/app/models"
	openapimodels "uvplatform.com/uvp-gb28181/app/openapi/models"
)

type operationSeedKey struct {
	capabilityID int64
	code         string
}
type routeSeedKey struct{ method, path string }

// Read identities once within the seed transaction. Unscoped catalog rows
// retain soft-delete reservations; sys_api remains optional active metadata.
type seedLookup struct {
	groups       map[string]openapimodels.CapabilityGroup
	capabilities map[string]openapimodels.Capability
	operations   map[operationSeedKey]openapimodels.Operation
	routes       map[routeSeedKey]openapimodels.Operation
	apis         map[routeSeedKey]appmodels.SysApi
}

func loadSeedLookup(ctx context.Context, tx *gorm.DB) (*seedLookup, error) {
	var groups []openapimodels.CapabilityGroup
	var capabilities []openapimodels.Capability
	var operations []openapimodels.Operation
	var apis []appmodels.SysApi
	for _, rows := range []any{&groups, &capabilities, &operations} {
		if err := tx.WithContext(ctx).Unscoped().Order("id ASC").Find(rows).Error; err != nil {
			return nil, err
		}
	}
	paths := make([]string, 0)
	for _, definition := range coreCatalogDefinitions() {
		paths = append(paths, definition.internalPath)
	}
	if err := tx.WithContext(ctx).Where("path IN ? AND deleted_at IS NULL", paths).Order("id ASC").Find(&apis).Error; err != nil {
		return nil, err
	}
	lookup := &seedLookup{
		groups: make(map[string]openapimodels.CapabilityGroup), capabilities: make(map[string]openapimodels.Capability),
		operations: make(map[operationSeedKey]openapimodels.Operation), routes: make(map[routeSeedKey]openapimodels.Operation), apis: make(map[routeSeedKey]appmodels.SysApi),
	}
	for _, row := range groups {
		lookup.groups[row.Code] = row
	}
	for _, row := range capabilities {
		lookup.capabilities[row.Scope] = row
	}
	for _, row := range operations {
		lookup.operations[operationSeedKey{row.CapabilityID, row.Code}] = row
		lookup.routes[routeSeedKey{row.Method, row.ExternalPath}] = row
	}
	for _, row := range apis {
		key := routeSeedKey{row.Method, row.Path}
		if _, ok := lookup.apis[key]; !ok {
			lookup.apis[key] = row
		}
	}
	return lookup, nil
}
