package client

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"

	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	catalogstore "uvplatform.cn/uvp-gb28181/app/openapi/catalog/store"
)

var ErrCapabilityDrift = errors.New("OpenAPI capability registry drift")

// ErrCapabilityRuntimeUnavailable means a persisted active pointer exists but
// its runtime projection is not ready. Falling back to the legacy static list
// in that state would authorize a scope that the published catalog has not
// confirmed, so callers must fail closed.
var ErrCapabilityRuntimeUnavailable = errors.New("OpenAPI capability runtime unavailable")

// Capability is the external contract metadata for one published scope.
// Internal title, method and group are read from sys_api at catalog build time;
// the stable scope and public path remain explicitly owned by this registry.
type Capability struct {
	Scope               string `json:"scope"`
	Name                string `json:"name"`
	Method              string `json:"method"`
	ExternalPath        string `json:"externalPath"`
	ResourceType        string `json:"resourceType"`
	Risk                string `json:"risk"`
	IdempotencyRequired bool   `json:"idempotencyRequired"`
}

type CapabilityGroup struct {
	Code         string       `json:"code"`
	Name         string       `json:"name"`
	Capabilities []Capability `json:"capabilities"`
}

type capabilityDefinition struct {
	scope        string
	internalPath string
	method       string
	externalPath string
	resourceType string
	risk         string
	idempotent   bool
	groupCode    string
}

var capabilityDefinitions = []capabilityDefinition{
	{scope: "device:list", internalPath: "/api/gb28181/device-mgmt/devices", method: "GET", externalPath: "/openapi/v1/devices", resourceType: "device", risk: "read", groupCode: "device-management"},
	{scope: "device:detail", internalPath: "/api/gb28181/device-mgmt/device/:id", method: "GET", externalPath: "/openapi/v1/devices/{deviceId}", resourceType: "device", risk: "read", groupCode: "device-management"},
	{scope: "device:status", internalPath: "/api/gb28181/device-mgmt/device/:id/status-events", method: "GET", externalPath: "/openapi/v1/devices/{deviceId}/status", resourceType: "device", risk: "read", groupCode: "device-management"},
	{scope: "channel:list", internalPath: "/api/gb28181/device-mgmt/channels", method: "GET", externalPath: "/openapi/v1/devices/{deviceId}/channels", resourceType: "channel", risk: "read", groupCode: "device-management"},
	{scope: "channel:detail", internalPath: "/api/gb28181/device-mgmt/channel/:id", method: "GET", externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}", resourceType: "channel", risk: "read", groupCode: "device-management"},
	{scope: "channel:status", internalPath: "/api/gb28181/device-mgmt/channel/:id/device-status", method: "GET", externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/status", resourceType: "channel", risk: "read", groupCode: "device-management"},
	{scope: "play:live:apply", internalPath: "/api/gb28181/play/:deviceId/:channelId/authorization", method: "POST", externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/live-authorizations", resourceType: "channel", risk: "media", groupCode: "playback"},
	{scope: "ptz:preset:list", internalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/presets", method: "GET", externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets", resourceType: "channel", risk: "read", groupCode: "device-control"},
	{scope: "ptz:preset:save", internalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/presets", method: "POST", externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets", resourceType: "channel", risk: "control", idempotent: true, groupCode: "device-control"},
	{scope: "ptz:preset:call", internalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId/call", method: "POST", externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}/call", resourceType: "channel", risk: "control", idempotent: true, groupCode: "device-control"},
	{scope: "ptz:preset:delete", internalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId", method: "DELETE", externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}", resourceType: "channel", risk: "control", idempotent: true, groupCode: "device-control"},
	{scope: "ptz:operation:read", internalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/operations/:operationId", method: "GET", externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/operations/{operationId}", resourceType: "channel", risk: "read", groupCode: "device-control"},
}

var capabilityGroupNames = map[string]string{
	"device-management": "设备管理",
	"playback":          "多屏播放",
	"device-control":    "设备控制",
}

func capabilityScopeMap() map[string]struct{} {
	scopes := make(map[string]struct{}, len(capabilityDefinitions))
	for _, definition := range capabilityDefinitions {
		scopes[definition.scope] = struct{}{}
	}
	return scopes
}

func CapabilityCatalog(ctx context.Context, db *gorm.DB) ([]CapabilityGroup, error) {
	if db == nil {
		return nil, ErrCapabilityDrift
	}
	if snapshot, active, err := activeReleaseSnapshot(ctx, db); err != nil {
		return nil, err
	} else if active {
		return capabilityCatalogFromSnapshot(snapshot)
	}
	return staticCapabilityCatalog(ctx, db)
}

// SupportedScopesFromDB returns the scopes from the immutable active release.
// Only an entirely un-migrated installation falls back to the compatibility
// registry. A migrated runtime without an active release fails closed.
func SupportedScopesFromDB(ctx context.Context, db *gorm.DB) ([]string, error) {
	if db == nil {
		return nil, ErrCapabilityDrift
	}
	snapshot, active, err := activeReleaseSnapshot(ctx, db)
	if err != nil {
		return nil, err
	}
	if !active {
		return SupportedScopes(), nil
	}
	result := make([]string, 0, len(snapshot.Items))
	seen := make(map[string]struct{}, len(snapshot.Items))
	for _, row := range snapshot.Items {
		if strings.TrimSpace(row.Scope) == "" {
			continue
		}
		if _, ok := seen[row.Scope]; ok {
			continue
		}
		seen[row.Scope] = struct{}{}
		result = append(result, row.Scope)
	}
	sort.Strings(result)
	return result, nil
}

// ScopePublished reports whether a scope is part of the current release. It is
// intentionally separate from a client's grant row: a historical/orphan grant
// can remain queryable for audit, but it must not authorize a new request.
func ScopePublished(ctx context.Context, db *gorm.DB, scope string) (bool, error) {
	if !validScopeName(scope) {
		return false, nil
	}
	scopes, err := SupportedScopesFromDB(ctx, db)
	if err != nil {
		return false, err
	}
	for _, candidate := range scopes {
		if candidate == scope {
			return true, nil
		}
	}
	return false, nil
}

func staticCapabilityCatalog(ctx context.Context, db *gorm.DB) ([]CapabilityGroup, error) {
	groups := make(map[string]*CapabilityGroup)
	for _, definition := range capabilityDefinitions {
		var rows []appmodels.SysApi
		result := db.WithContext(normalizeCatalogContext(ctx)).Where("path = ? AND method = ? AND deleted_at IS NULL", definition.internalPath, definition.method).Find(&rows)
		if result.Error != nil || len(rows) != 1 {
			return nil, ErrCapabilityDrift
		}
		row := rows[0]
		if strings.TrimSpace(row.Title) == "" || row.ApiGroup != capabilityGroupNames[definition.groupCode] {
			return nil, ErrCapabilityDrift
		}
		group := groups[definition.groupCode]
		if group == nil {
			group = &CapabilityGroup{Code: definition.groupCode, Name: row.ApiGroup, Capabilities: make([]Capability, 0)}
			groups[definition.groupCode] = group
		} else if group.Name != row.ApiGroup {
			return nil, ErrCapabilityDrift
		}
		group.Capabilities = append(group.Capabilities, Capability{
			Scope: definition.scope, Name: row.Title, Method: definition.method,
			ExternalPath: definition.externalPath, ResourceType: definition.resourceType,
			Risk: definition.risk, IdempotencyRequired: definition.idempotent,
		})
	}
	result := make([]CapabilityGroup, 0, len(groups))
	for _, group := range groups {
		sort.Slice(group.Capabilities, func(i, j int) bool { return group.Capabilities[i].Scope < group.Capabilities[j].Scope })
		result = append(result, *group)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Code < result[j].Code })
	return result, nil
}

// activeReleaseSnapshot loads the one durable active release through the
// catalog repository. This is deliberately the only client-side path to a
// published capability set so all release/runtime integrity checks are shared
// with startup hydration and request dispatch.
func activeReleaseSnapshot(ctx context.Context, db *gorm.DB) (*catalogstore.ReleaseSnapshot, bool, error) {
	schema, schemaErr := catalogstore.InspectCatalogSchema(db)
	if schemaErr != nil {
		return nil, false, schemaErr
	}
	if schema == catalogstore.CatalogSchemaAbsent {
		return nil, false, nil
	}
	if schema == catalogstore.CatalogSchemaIncomplete {
		missing := fmt.Errorf("%w: catalog schema is incomplete", catalogstore.ErrCatalogTableMissing)
		return nil, false, fmt.Errorf("%w: %w", ErrCapabilityRuntimeUnavailable, missing)
	}
	snapshot, err := catalogstore.NewRepository(db).LoadActive(normalizeCatalogContext(ctx))
	if err == nil {
		return snapshot, true, nil
	}
	if errors.Is(err, catalogstore.ErrNoActiveRelease) {
		return nil, false, fmt.Errorf("%w: catalog has no active release", ErrCapabilityRuntimeUnavailable)
	}
	if errors.Is(err, catalogstore.ErrCatalogTableMissing) {
		return nil, false, err
	}
	if errors.Is(err, catalogstore.ErrCatalogRuntimeUnavailable) {
		return nil, false, fmt.Errorf("%w: %w", ErrCapabilityRuntimeUnavailable, err)
	}
	return nil, false, err
}

func capabilityCatalogFromSnapshot(snapshot *catalogstore.ReleaseSnapshot) ([]CapabilityGroup, error) {
	if snapshot == nil {
		return nil, ErrCapabilityDrift
	}
	groups := make(map[string]*CapabilityGroup)
	for _, row := range snapshot.Items {
		groupCode := strings.TrimSpace(row.GroupCode)
		if groupCode == "" || strings.TrimSpace(row.Scope) == "" {
			return nil, ErrCapabilityDrift
		}
		group := groups[groupCode]
		if group == nil {
			group = &CapabilityGroup{Code: groupCode, Name: row.GroupName, Capabilities: make([]Capability, 0)}
			groups[groupCode] = group
		} else if group.Name != row.GroupName {
			return nil, ErrCapabilityDrift
		}
		group.Capabilities = append(group.Capabilities, Capability{
			Scope: row.Scope, Name: row.CapabilityName, Method: strings.ToUpper(row.Method),
			ExternalPath: row.ExternalPath, ResourceType: row.ResourceType, Risk: row.RiskLevel,
			IdempotencyRequired: row.IdempotencyMode != "none",
		})
	}
	resultGroups := make([]CapabilityGroup, 0, len(groups))
	for _, group := range groups {
		sort.Slice(group.Capabilities, func(i, j int) bool { return group.Capabilities[i].Scope < group.Capabilities[j].Scope })
		resultGroups = append(resultGroups, *group)
	}
	sort.Slice(resultGroups, func(i, j int) bool { return resultGroups[i].Code < resultGroups[j].Code })
	return resultGroups, nil
}

func normalizeCatalogContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
