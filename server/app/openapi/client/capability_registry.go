package client

import (
	"context"
	"errors"
	"sort"
	"strings"

	"gorm.io/gorm"

	appmodels "uvplatform.cn/uvp-gb28181/app/models"
)

var ErrCapabilityDrift = errors.New("OpenAPI capability registry drift")

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
	groups := make(map[string]*CapabilityGroup)
	for _, definition := range capabilityDefinitions {
		var rows []appmodels.SysApi
		result := db.WithContext(ctx).Where("path = ? AND method = ? AND deleted_at IS NULL", definition.internalPath, definition.method).Find(&rows)
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
