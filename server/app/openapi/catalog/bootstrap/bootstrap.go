// Package bootstrap contains idempotent seed operations for the editable
// OpenAPI capability catalog. It only creates source rows; publishing a
// release remains an explicit catalog-management action.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/adapters"
	openapimodels "uvplatform.cn/uvp-gb28181/app/openapi/models"
)

var (
	// ErrBootstrapConflict means a stable catalog identity is already reserved
	// by a soft-deleted row or by a different operation route.
	ErrBootstrapConflict = errors.New("OpenAPI catalog bootstrap identity conflict")
	// ErrBootstrapUnavailable means the bootstrap function was called without a
	// usable database handle.
	ErrBootstrapUnavailable = errors.New("OpenAPI catalog bootstrap database unavailable")
)

// BootstrapResult reports rows created by EnsureCoreReadCatalog. Existing
// rows are deliberately not counted and are never updated by this function.
type BootstrapResult struct {
	GroupsCreated       int
	CapabilitiesCreated int
	OperationsCreated   int
}

type groupDefinition struct {
	code string
	name string
	sort int
}

type readDefinition struct {
	groupCode    string
	code         string
	scope        string
	name         string
	internalPath string
	method       string
	externalPath string
	adapterKey   string
	resourceType string
}

var coreGroups = []groupDefinition{
	{code: "device-management", name: "设备管理", sort: 10},
	{code: "playback", name: "多屏播放", sort: 20},
	{code: "device-control", name: "设备控制", sort: 30},
}

// The first bootstrap only seeds the stable device/channel read surface. The
// remaining groups are present so later playback and control capabilities can
// be added through the catalog management flow without changing group IDs.
var coreReadDefinitions = []readDefinition{
	{
		groupCode: "device-management", code: "device.list", scope: "device:list", name: "设备列表",
		internalPath: "/api/gb28181/device-mgmt/devices", method: "GET", externalPath: "/openapi/v1/devices",
		adapterKey: adapters.DeviceListAdapterKey, resourceType: "device",
	},
	{
		groupCode: "device-management", code: "device.detail", scope: "device:detail", name: "设备详情",
		internalPath: "/api/gb28181/device-mgmt/device/:id", method: "GET", externalPath: "/openapi/v1/devices/{deviceId}",
		adapterKey: adapters.DeviceDetailAdapterKey, resourceType: "device",
	},
	{
		groupCode: "device-management", code: "device.status", scope: "device:status", name: "设备状态",
		internalPath: "/api/gb28181/device-mgmt/device/:id/status-events", method: "GET", externalPath: "/openapi/v1/devices/{deviceId}/status",
		adapterKey: adapters.DeviceStatusAdapterKey, resourceType: "device",
	},
	{
		groupCode: "device-management", code: "channel.list", scope: "channel:list", name: "通道列表",
		internalPath: "/api/gb28181/device-mgmt/channels", method: "GET", externalPath: "/openapi/v1/devices/{deviceId}/channels",
		adapterKey: adapters.ChannelListAdapterKey, resourceType: "channel",
	},
	{
		groupCode: "device-management", code: "channel.detail", scope: "channel:detail", name: "通道详情",
		internalPath: "/api/gb28181/device-mgmt/channel/:id", method: "GET", externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}",
		adapterKey: adapters.ChannelDetailAdapterKey, resourceType: "channel",
	},
	{
		groupCode: "device-management", code: "channel.status", scope: "channel:status", name: "通道状态",
		internalPath: "/api/gb28181/device-mgmt/channel/:id/device-status", method: "GET", externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/status",
		adapterKey: adapters.ChannelStatusAdapterKey, resourceType: "channel",
	},
}

// EnsureCoreReadCatalog creates the initial editable catalog rows. It is
// safe to call during every startup: rows are matched by their stable code,
// scope, and route, and existing rows are left untouched. A matching sys_api
// row is copied as metadata only; sys_api is never used as the external scope
// or as the publication trigger.
func EnsureCoreReadCatalog(ctx context.Context, db *gorm.DB, actorID int64) (BootstrapResult, error) {
	var result BootstrapResult
	if db == nil {
		return result, ErrBootstrapUnavailable
	}
	if actorID < 0 {
		return result, fmt.Errorf("%w: actor id must not be negative", ErrBootstrapConflict)
	}
	ctx = normalizeContext(ctx)
	createdBy := uint(actorID)

	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		groups := make(map[string]openapimodels.CapabilityGroup, len(coreGroups))
		for _, definition := range coreGroups {
			group, created, err := ensureGroup(ctx, tx, definition, createdBy)
			if err != nil {
				return err
			}
			groups[definition.code] = group
			if created {
				result.GroupsCreated++
			}
		}

		for _, definition := range coreReadDefinitions {
			group, ok := groups[definition.groupCode]
			if !ok || group.ID == 0 {
				return fmt.Errorf("%w: group %q is missing", ErrBootstrapConflict, definition.groupCode)
			}
			sysAPI, err := findSysAPI(ctx, tx, definition.internalPath, definition.method)
			if err != nil {
				return err
			}
			capability, created, err := ensureCapability(ctx, tx, group, definition, sysAPI, createdBy)
			if err != nil {
				return err
			}
			if created {
				result.CapabilitiesCreated++
			}

			created, err = ensureOperation(ctx, tx, capability, definition, sysAPI, createdBy)
			if err != nil {
				return err
			}
			if created {
				result.OperationsCreated++
			}
		}
		return nil
	})
	if err != nil {
		return BootstrapResult{}, err
	}
	return result, nil
}

func ensureGroup(ctx context.Context, tx *gorm.DB, definition groupDefinition, createdBy uint) (openapimodels.CapabilityGroup, bool, error) {
	var group openapimodels.CapabilityGroup
	query := tx.WithContext(ctx).Unscoped().Where("code = ?", definition.code).First(&group)
	if query.Error == nil {
		if group.DeletedAt != nil {
			return openapimodels.CapabilityGroup{}, false, fmt.Errorf("%w: group %q is soft deleted", ErrBootstrapConflict, definition.code)
		}
		return group, false, nil
	}
	if !errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return openapimodels.CapabilityGroup{}, false, query.Error
	}

	group = openapimodels.CapabilityGroup{
		Code: definition.code, Name: definition.name, Sort: definition.sort,
		Status: openapimodels.CatalogStatusDraft, RowVersion: 1,
		CreatedBy: createdBy, UpdatedBy: createdBy,
	}
	if err := tx.WithContext(ctx).Create(&group).Error; err != nil {
		return openapimodels.CapabilityGroup{}, false, err
	}
	return group, true, nil
}

func ensureCapability(ctx context.Context, tx *gorm.DB, group openapimodels.CapabilityGroup, definition readDefinition, sysAPI *appmodels.SysApi, createdBy uint) (openapimodels.Capability, bool, error) {
	var capability openapimodels.Capability
	query := tx.WithContext(ctx).Unscoped().Where("scope = ?", definition.scope).First(&capability)
	if query.Error == nil {
		if capability.DeletedAt != nil {
			return openapimodels.Capability{}, false, fmt.Errorf("%w: capability scope %q is soft deleted", ErrBootstrapConflict, definition.scope)
		}
		return capability, false, nil
	}
	if !errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return openapimodels.Capability{}, false, query.Error
	}

	name := definition.name
	if sysAPI != nil && strings.TrimSpace(sysAPI.Title) != "" {
		name = sysAPI.Title
	}
	capability = openapimodels.Capability{
		GroupID: group.ID, Code: definition.code, Scope: definition.scope, Name: name,
		ResourceType: definition.resourceType, RiskLevel: "read", Status: openapimodels.CatalogStatusDraft,
		SysAPIID: sysAPIID(sysAPI), SysAPIPath: definition.internalPath, SysAPIMethod: definition.method,
		RowVersion: 1, CreatedBy: createdBy, UpdatedBy: createdBy,
	}
	if err := tx.WithContext(ctx).Create(&capability).Error; err != nil {
		return openapimodels.Capability{}, false, err
	}
	return capability, true, nil
}

func ensureOperation(ctx context.Context, tx *gorm.DB, capability openapimodels.Capability, definition readDefinition, sysAPI *appmodels.SysApi, createdBy uint) (bool, error) {
	var operation openapimodels.Operation
	query := tx.WithContext(ctx).Unscoped().Where("capability_id = ? AND code = ?", capability.ID, definition.code).First(&operation)
	if query.Error == nil {
		if operation.DeletedAt != nil {
			return false, fmt.Errorf("%w: operation %q is soft deleted", ErrBootstrapConflict, definition.code)
		}
		return false, nil
	}
	if !errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return false, query.Error
	}

	// The external route is globally stable. Detect an existing route owned by
	// another operation before relying on the database unique index.
	var routeOwner openapimodels.Operation
	query = tx.WithContext(ctx).Unscoped().Where("method = ? AND external_path = ?", definition.method, definition.externalPath).First(&routeOwner)
	if query.Error == nil {
		if routeOwner.DeletedAt != nil || routeOwner.CapabilityID != capability.ID || routeOwner.Code != definition.code {
			return false, fmt.Errorf("%w: route %s %s is already reserved", ErrBootstrapConflict, definition.method, definition.externalPath)
		}
		return false, nil
	}
	if !errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return false, query.Error
	}

	operation = openapimodels.Operation{
		CapabilityID: capability.ID, Code: definition.code, Name: capability.Name,
		Method: definition.method, ExternalPath: definition.externalPath,
		AdapterKey: definition.adapterKey, AdapterContractVersion: adapters.ResourceAdapterContractVersion,
		ResourceType: definition.resourceType, RiskLevel: "read", IdempotencyMode: "none",
		RequestSchema: "{}", ResponseSchema: "{}", SysAPIID: sysAPIID(sysAPI),
		SysAPIPath: definition.internalPath, SysAPIMethod: definition.method,
		Status: openapimodels.CatalogStatusDraft, CreatedBy: createdBy, UpdatedBy: createdBy,
	}
	if err := tx.WithContext(ctx).Create(&operation).Error; err != nil {
		return false, err
	}
	return true, nil
}

func findSysAPI(ctx context.Context, tx *gorm.DB, path, method string) (*appmodels.SysApi, error) {
	var row appmodels.SysApi
	query := tx.WithContext(ctx).
		Where("path = ? AND method = ? AND deleted_at IS NULL", path, method).
		Order("id ASC").First(&row)
	if errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if query.Error != nil {
		return nil, query.Error
	}
	return &row, nil
}

func sysAPIID(row *appmodels.SysApi) *int64 {
	if row == nil || row.ID == 0 {
		return nil
	}
	id := int64(row.ID)
	return &id
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
