// Package bootstrap owns the code-owned half of the OpenAPI capability
// catalog lifecycle: it creates the editable source rows for the whole
// external scope surface (this file) and, for an installation whose active
// release does not cover that surface yet, the publication that closes the gap
// (publish.go).
//
// The surface is exactly the 12 scopes the legacy registry in
// app/openapi/client exposes. Six are dispatched by the catalog runtime, five by
// the dedicated PTZ gateway plane; both kinds have to be published all
// the same, because client.ScopePublished reads the release and nothing else.
//
// Publishing stays deliberate: startup repairs additively, with the sole
// pre-release exception of explicitly retired scopes, and `-publish-catalog`
// remains the explicit operator override for all other contract changes.
// An installation whose release already covers the surface is never re-seeded
// or re-versioned.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"

	appmodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	openapimodels "uvplatform.com/uvp-gb28181/app/openapi/models"
)

var (
	// ErrBootstrapConflict means a stable catalog identity is already reserved
	// by a soft-deleted row or by a different operation route.
	ErrBootstrapConflict = errors.New("OpenAPI catalog bootstrap identity conflict")
	// ErrBootstrapUnavailable means the bootstrap function was called without a
	// usable database handle.
	ErrBootstrapUnavailable = errors.New("OpenAPI catalog bootstrap database unavailable")
)

// BootstrapResult reports rows created by EnsureCoreCatalog. Existing rows are
// deliberately not counted and are never updated by this function.
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

// readDefinition describes one code-owned catalog row triple (group,
// capability, operation). risk and idempotencyMode are part of the published
// contract: the capability catalog reader derives IdempotencyRequired from
// idempotencyMode, and the legacy static registry in app/openapi/client must
// agree with it value for value or the two readers describe different
// surfaces.
type readDefinition struct {
	groupCode       string
	code            string
	scope           string
	name            string
	internalPath    string
	method          string
	externalPath    string
	adapterKey      string
	resourceType    string
	risk            string
	idempotencyMode string
}

var coreGroups = []groupDefinition{
	{code: "device-management", name: "设备管理", sort: 10},
	{code: "device-control", name: "设备控制", sort: 20},
}

// coreReadDefinitions is the device/channel read surface. It is dispatched by
// the catalog runtime: every adapter key here is registered by
// adapters.NewDeviceChannelAdapterRegistrations and reached through
// CatalogRuntime.Dispatch.
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
	// channel.status is seeded into device-control, matching both
	// sys_api.api_group for this route (「设备控制」) and the legacy registry in
	// app/openapi/client. A published release must not regroup a capability
	// differently from the compatibility path it replaces.
	{
		groupCode: "device-control", code: "channel.status", scope: "channel:status", name: "通道状态",
		internalPath: "/api/gb28181/device-mgmt/channel/:id/device-status", method: "GET", externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/status",
		adapterKey: adapters.ChannelStatusAdapterKey, resourceType: "channel",
	},
}

// coreDelegatedDefinitions is the PTZ surface. It is *not* dispatched by
// the catalog runtime: handlePTZ consumes these scopes before the metadata
// dispatcher, and its executable behaviour is PTZDispatcher. It is seeded
// anyway because the active release
// is what client.ScopePublished consults — a scope missing from the release can
// never be granted, whatever its dispatch plane is.
//
// risk and idempotencyMode mirror the legacy registry in app/openapi/client
// exactly (ptz reads = read, ptz writes = control; the three preset writes
// require an Idempotency-Key). A published release that disagrees
// with the compatibility path would make the same platform describe two
// different contracts depending on whether the catalog tables are migrated.
var coreDelegatedDefinitions = []readDefinition{
	{
		groupCode: "device-control", code: "play.live", scope: "play:live", name: "发起实时点播",
		internalPath: "/api/gb28181/play/:deviceId/:channelId", method: "POST",
		externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/live",
		adapterKey:   adapters.PlayLiveAdapterKey, resourceType: "channel", risk: "control",
	},
	{
		groupCode: "device-control", code: "ptz.preset.list", scope: "ptz:preset:list", name: "预置位列表",
		internalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/presets", method: "GET",
		externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets",
		adapterKey:   adapters.PTZPresetListAdapterKey, resourceType: "channel", risk: "read",
	},
	{
		groupCode: "device-control", code: "ptz.preset.save", scope: "ptz:preset:save", name: "保存预置位",
		internalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/presets", method: "POST",
		externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets",
		adapterKey:   adapters.PTZPresetSaveAdapterKey, resourceType: "channel", risk: "control", idempotencyMode: "required",
	},
	{
		groupCode: "device-control", code: "ptz.preset.call", scope: "ptz:preset:call", name: "调用预置位",
		internalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId/call", method: "POST",
		externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}/call",
		adapterKey:   adapters.PTZPresetCallAdapterKey, resourceType: "channel", risk: "control", idempotencyMode: "required",
	},
	{
		groupCode: "device-control", code: "ptz.preset.delete", scope: "ptz:preset:delete", name: "删除预置位",
		internalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId", method: "DELETE",
		externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}",
		adapterKey:   adapters.PTZPresetDeleteAdapterKey, resourceType: "channel", risk: "control", idempotencyMode: "required",
	},
	{
		groupCode: "device-control", code: "ptz.operation.read", scope: "ptz:operation:read", name: "云台操作结果",
		internalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/operations/:operationId", method: "GET",
		externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/operations/{operationId}",
		adapterKey:   adapters.PTZOperationReadAdapterKey, resourceType: "channel", risk: "read",
	},
}

// coreCatalogDefinitions is the complete code-owned catalog surface, which is
// also the complete external scope surface. It is the single list that
// publication, the "is this release still complete" check and the tests all
// agree on.
func coreCatalogDefinitions() []readDefinition {
	definitions := make([]readDefinition, 0, len(coreReadDefinitions)+len(coreDelegatedDefinitions))
	definitions = append(definitions, coreReadDefinitions...)
	definitions = append(definitions, coreDelegatedDefinitions...)
	return definitions
}

// CoreCatalogScopes returns every scope this binary is able to serve, sorted so
// callers can compare it with a scope list read back from the database. Callers
// use it to detect an active release that predates part of the surface.
func CoreCatalogScopes() []string {
	definitions := coreCatalogDefinitions()
	scopes := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		scopes = append(scopes, definition.scope)
	}
	sort.Strings(scopes)
	return scopes
}

// EnsureCoreCatalog creates the initial editable catalog rows. It is safe to
// call during every startup: rows are matched by their stable code, scope, and
// route, and existing rows are left untouched. A matching sys_api row is copied
// as metadata only; sys_api is never used as the external scope or as the
// publication trigger.
func EnsureCoreCatalog(ctx context.Context, db *gorm.DB, actorID int64) (BootstrapResult, error) {
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
		if err := retireRemovedPlaybackCapability(ctx, tx, createdBy); err != nil {
			return err
		}
		if err := disableRetiredPlaybackGroup(ctx, tx, createdBy); err != nil {
			return err
		}

		for _, definition := range coreCatalogDefinitions() {
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

// disableRetiredPlaybackGroup retires the former playback category when it is
// empty. No playback capability is seeded or migrated anymore.
func disableRetiredPlaybackGroup(ctx context.Context, tx *gorm.DB, updatedBy uint) error {
	var group openapimodels.CapabilityGroup
	query := tx.WithContext(ctx).Where("code = ? AND deleted_at IS NULL", "playback").First(&group)
	if errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return nil
	}
	if query.Error != nil {
		return query.Error
	}
	var count int64
	if err := tx.WithContext(ctx).Model(&openapimodels.Capability{}).
		Where("group_id = ? AND deleted_at IS NULL AND status <> ?", group.ID, openapimodels.CatalogStatusDisabled).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 || group.Status == openapimodels.CatalogStatusDisabled {
		return nil
	}
	return tx.WithContext(ctx).Model(&group).Updates(map[string]any{
		"status":     openapimodels.CatalogStatusDisabled,
		"updated_by": updatedBy,
	}).Error
}

// retireRemovedPlaybackCapability keeps old catalog identities/audit rows
// queryable while making the removed external operation unavailable to new
// releases and grants.
func retireRemovedPlaybackCapability(ctx context.Context, tx *gorm.DB, updatedBy uint) error {
	var capability openapimodels.Capability
	query := tx.WithContext(ctx).
		Where("scope IN ? AND deleted_at IS NULL", []string{"play:live:apply", "__retired_play_live_apply__"}).
		Order("id ASC").First(&capability)
	if errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return nil
	}
	if query.Error != nil {
		return query.Error
	}
	if capability.Status != openapimodels.CatalogStatusDisabled {
		if err := tx.WithContext(ctx).Model(&capability).Updates(map[string]any{
			"status": openapimodels.CatalogStatusDisabled, "updated_by": updatedBy,
		}).Error; err != nil {
			return err
		}
	}
	return tx.WithContext(ctx).Model(&openapimodels.Operation{}).
		Where("capability_id = ? AND deleted_at IS NULL", capability.ID).
		Updates(map[string]any{"status": openapimodels.CatalogStatusDisabled, "updated_by": updatedBy}).Error
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
		ResourceType: definition.resourceType, RiskLevel: definitionRisk(definition), Status: openapimodels.CatalogStatusDraft,
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
		AdapterKey: definition.adapterKey, AdapterContractVersion: adapterContractVersion(definition),
		ResourceType: definition.resourceType, RiskLevel: definitionRisk(definition), IdempotencyMode: definitionIdempotencyMode(definition),
		RequestSchema: "{}", ResponseSchema: "{}", SysAPIID: sysAPIID(sysAPI),
		SysAPIPath: definition.internalPath, SysAPIMethod: definition.method,
		Status: openapimodels.CatalogStatusDraft, CreatedBy: createdBy, UpdatedBy: createdBy,
	}
	if err := tx.WithContext(ctx).Create(&operation).Error; err != nil {
		return false, err
	}
	return true, nil
}

// definitionRisk defaults to "read", which is what an unset column resolves to
// as well. A definition that omits it therefore cannot introduce a value the
// legacy static registry would reject.
func definitionRisk(definition readDefinition) string {
	if risk := strings.TrimSpace(definition.risk); risk != "" {
		return risk
	}
	return "read"
}

// definitionIdempotencyMode defaults to "none" for the same reason: it is the
// column default, so an omitted mode is never a silent contract change.
func definitionIdempotencyMode(definition readDefinition) string {
	if mode := strings.TrimSpace(definition.idempotencyMode); mode != "" {
		return mode
	}
	return "none"
}

// adapterContractVersion keeps the two planes' contract versions separate in
// the source while storing what catalog.store normalizes an unset column to.
// The delegated keys carry their own version constant precisely so a future
// bump of either plane is a visible diff in the release item.
func adapterContractVersion(definition readDefinition) string {
	if strings.HasPrefix(definition.adapterKey, "delegated.") {
		return adapters.DelegatedPlaneContractVersion
	}
	return adapters.ResourceAdapterContractVersion
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
