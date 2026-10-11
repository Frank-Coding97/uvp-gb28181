package contracts

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistryHasUniquePublishedContractKeys(t *testing.T) {
	operations := Operations()
	require.NotEmpty(t, operations)
	require.NoError(t, Validate(operations))

	scopes := make(map[string]bool)
	routes := make(map[string]bool)
	for _, operation := range operations {
		scopes[operation.Scope] = true
		routes[operation.Method+" "+operation.ExternalPath] = true
	}
	require.Len(t, scopes, len(operations))
	require.Len(t, routes, len(operations))
	require.Contains(t, scopes, "play:live")
	require.Contains(t, scopes, "channel:capabilities")
	require.Contains(t, scopes, "channel:storage-cards")
	require.NotContains(t, scopes, "play:live:apply")
	for _, operation := range operations {
		if operation.Scope == "play:live" {
			require.Equal(t, RiskControl, operation.RiskLevel, "existing live-play capability risk is part of the compatibility contract")
		}
	}
}

func TestRegistryIncludesScopedTalkAndBroadcastSessionOperations(t *testing.T) {
	byScope := make(map[string]Operation)
	for _, operation := range Operations() {
		byScope[operation.Scope] = operation
	}

	for _, want := range []struct {
		scope, method, path, adapter, risk, idempotency string
		confirmation                                    bool
	}{
		{"device:talk:create", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/talk-sessions", "device.voice-talk-create.v1", RiskMedia, IdempotencyRequired, false},
		{"device:talk:read", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/talk-sessions/:sessionId", "device.voice-talk-read.v1", RiskRead, IdempotencyNone, false},
		{"device:talk:end", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/talk-sessions/:sessionId/end", "device.voice-talk-end.v1", RiskControl, IdempotencyNone, false},
		{"device:broadcast:create", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/broadcast-sessions", "device.voice-broadcast-create.v1", RiskMedia, IdempotencyRequired, false},
		{"device:broadcast:read", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/broadcast-sessions/:sessionId", "device.voice-broadcast-read.v1", RiskRead, IdempotencyNone, false},
		{"device:broadcast:end", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/broadcast-sessions/:sessionId/end", "device.voice-broadcast-end.v1", RiskControl, IdempotencyNone, false},
	} {
		operation, ok := byScope[want.scope]
		require.Truef(t, ok, "missing voice scope %s", want.scope)
		require.Equal(t, want.method, operation.Method, want.scope)
		require.Equal(t, want.path, operation.GinPath, want.scope)
		require.Equal(t, want.adapter, operation.AdapterKey, want.scope)
		require.Equal(t, want.risk, operation.RiskLevel, want.scope)
		require.Equal(t, want.idempotency, operation.IdempotencyMode, want.scope)
		require.Equal(t, want.confirmation, operation.RequiresConfirmation, want.scope)
		require.Equal(t, []string{"2016", "2022"}, operation.ProtocolVersions, want.scope)
		require.Contains(t, operation.StandardRefs, "GB/T 28181-2022 9.12", want.scope)
		require.Contains(t, operation.ResponseSchema, `"sessionId"`, want.scope)
		require.NotContains(t, operation.ResponseSchema, `"url"`, want.scope+" must not publish an upstream media URL")
	}
}

func TestRegistryIncludesScopedTalkAndBroadcastMediaProxyOperations(t *testing.T) {
	byScope := make(map[string]Operation)
	for _, operation := range Operations() {
		byScope[operation.Scope] = operation
	}
	for _, want := range []struct {
		scope, path, adapter string
	}{
		{"device:talk:media", "/openapi/v1/devices/:deviceId/channels/:channelId/talk-sessions/:sessionId/media", "device.voice-talk-media.v1"},
		{"device:broadcast:media", "/openapi/v1/devices/:deviceId/channels/:channelId/broadcast-sessions/:sessionId/media", "device.voice-broadcast-media.v1"},
	} {
		operation, ok := byScope[want.scope]
		require.Truef(t, ok, "missing media scope %s", want.scope)
		require.Equal(t, http.MethodPost, operation.Method)
		require.Equal(t, want.path, operation.GinPath)
		require.Equal(t, want.adapter, operation.AdapterKey)
		require.Equal(t, RiskMedia, operation.RiskLevel)
		require.Equal(t, IdempotencyNone, operation.IdempotencyMode)
		require.Contains(t, operation.RequestSchema, `"sdp"`)
		require.Contains(t, operation.ResponseSchema, `"sdp"`)
		require.NotContains(t, operation.ResponseSchema, `"url"`)
		require.NotContains(t, operation.ResponseSchema, `"location"`)
	}
}

func TestRegistryRequiresOperationDescriptions(t *testing.T) {
	operations := Operations()
	for _, operation := range operations {
		require.NotEmpty(t, operation.Description, operation.Scope)
	}

	operations[0].Description = "  "
	require.Error(t, Validate(operations), "operation descriptions are shown in the authorization workbench")
}

func TestRegistryIncludesTheGB281812022PTZControlSurface(t *testing.T) {
	byScope := make(map[string]Operation)
	for _, operation := range Operations() {
		byScope[operation.Scope] = operation
	}
	for _, want := range []struct {
		scope, method, path, adapter string
		versions                     []string
	}{
		{"ptz:move", "POST", "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/move", "delegated.ptz.move.v1", []string{"2016", "2022"}},
		{"ptz:precise", "POST", "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/precise", "delegated.ptz.precise.v1", []string{"2022"}},
		{"ptz:extended", "POST", "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/extended", "delegated.ptz.extended.v1", []string{"2016", "2022"}},
		{"ptz:wiper", "POST", "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/wiper", "delegated.ptz.wiper.v1", []string{"2016", "2022"}},
		{"ptz:home-position:set", "POST", "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/home-position", "delegated.ptz.home-position-set.v1", []string{"2016", "2022"}},
		{"ptz:cruise:control", "POST", "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/cruise/control", "delegated.ptz.cruise-control.v1", []string{"2016", "2022"}},
		{"ptz:target-track", "POST", "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/target-track", "delegated.ptz.target-track.v1", []string{"2022"}},
	} {
		operation, ok := byScope[want.scope]
		require.Truef(t, ok, "missing PTZ scope %s", want.scope)
		require.Equal(t, want.method, operation.Method, want.scope)
		require.Equal(t, want.path, operation.GinPath, want.scope)
		require.Equal(t, want.adapter, operation.AdapterKey, want.scope)
		require.Equal(t, RiskControl, operation.RiskLevel, want.scope)
		require.Equal(t, IdempotencyRequired, operation.IdempotencyMode, want.scope)
		require.Equal(t, want.versions, operation.ProtocolVersions, want.scope)
		require.Contains(t, operation.StandardRefs, "GB/T 28181-2022 7, 9.3, 附录 A")
	}
}

func TestRegistryIncludesCruiseTrackCreateAndRead(t *testing.T) {
	byScope := make(map[string]Operation)
	for _, operation := range Operations() {
		byScope[operation.Scope] = operation
	}
	for _, want := range []struct {
		scope, method, path, adapter, risk, idempotency string
		versions                                        []string
	}{
		{"ptz:cruise:track:create", "POST", "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/cruise/tracks", "delegated.ptz.cruise-track-create.v1", RiskControl, IdempotencyRequired, []string{"2016", "2022"}},
		{"ptz:cruise:track:read", "GET", "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/cruise/tracks/:trackId", "delegated.ptz.cruise-track-read.v1", RiskRead, IdempotencyNone, []string{"2016", "2022"}},
	} {
		operation, ok := byScope[want.scope]
		require.Truef(t, ok, "missing cruise track scope %s", want.scope)
		require.Equal(t, want.method, operation.Method)
		require.Equal(t, want.path, operation.GinPath)
		require.Equal(t, want.adapter, operation.AdapterKey)
		require.Equal(t, want.risk, operation.RiskLevel)
		require.Equal(t, want.idempotency, operation.IdempotencyMode)
		require.Equal(t, want.versions, operation.ProtocolVersions)
	}
}

func TestRegistryIncludesDeviceEventAndPositionReadOperations(t *testing.T) {
	operations := Operations()
	got := make(map[string]string, 7)
	for _, operation := range operations {
		got[operation.Scope] = operation.Method + " " + operation.ExternalPath
	}
	require.Equal(t, map[string]string{
		"device:subscription:create":   "POST /openapi/v1/devices/{deviceId}/subscriptions",
		"device:subscription:read":     "GET /openapi/v1/devices/{deviceId}/subscriptions/{subscriptionId}",
		"device:subscription:renew":    "POST /openapi/v1/devices/{deviceId}/subscriptions/{subscriptionId}/renew",
		"device:subscription:cancel":   "DELETE /openapi/v1/devices/{deviceId}/subscriptions/{subscriptionId}",
		"device:events:read":           "GET /openapi/v1/devices/{deviceId}/subscriptions/{subscriptionId}/events",
		"device:position:read":         "GET /openapi/v1/devices/{deviceId}/positions",
		"device:position:history:read": "GET /openapi/v1/devices/{deviceId}/positions/history",
	}, map[string]string{
		"device:subscription:create":   got["device:subscription:create"],
		"device:subscription:read":     got["device:subscription:read"],
		"device:subscription:renew":    got["device:subscription:renew"],
		"device:subscription:cancel":   got["device:subscription:cancel"],
		"device:events:read":           got["device:events:read"],
		"device:position:read":         got["device:position:read"],
		"device:position:history:read": got["device:position:history:read"],
	})
}

func TestRegistryIncludesDeviceConfigurationOperations(t *testing.T) {
	got := make(map[string]Operation)
	for _, operation := range Operations() {
		got[operation.Scope] = operation
	}
	want := map[string]struct {
		method string
		path   string
	}{
		"device:config:refresh":        {"POST", "/openapi/v1/devices/{deviceId}/channels/{channelId}/device-configs/refresh"},
		"device:config:read":           {"GET", "/openapi/v1/devices/{deviceId}/channels/{channelId}/device-configs"},
		"device:config:apply":          {"PUT", "/openapi/v1/devices/{deviceId}/channels/{channelId}/device-configs"},
		"channel:video-params:refresh": {"POST", "/openapi/v1/devices/{deviceId}/channels/{channelId}/video-params/refresh"},
		"channel:video-params:read":    {"GET", "/openapi/v1/devices/{deviceId}/channels/{channelId}/video-params"},
		"channel:video-params:apply":   {"PUT", "/openapi/v1/devices/{deviceId}/channels/{channelId}/video-params"},
		"device:config:operation:read": {"GET", "/openapi/v1/devices/{deviceId}/channels/{channelId}/config-operations/{operationId}"},
	}
	require.Len(t, got, len(Operations()))
	for scope, expected := range want {
		operation, ok := got[scope]
		require.Truef(t, ok, "missing configuration scope %s", scope)
		require.Equal(t, expected.method, operation.Method, scope)
		require.Equal(t, expected.path, operation.ExternalPath, scope)
		require.Equal(t, "device.config", operation.GroupCode)
		require.NotEmpty(t, operation.AdapterKey)
	}
}

func TestRegistryIncludesScopedGB281812022FirmwareUpgrade(t *testing.T) {
	byScope := make(map[string]Operation)
	for _, operation := range Operations() {
		byScope[operation.Scope] = operation
	}

	create, ok := byScope["device:firmware-upgrade:execute"]
	require.True(t, ok)
	require.Equal(t, "POST", create.Method)
	require.Equal(t, "/openapi/v1/devices/:deviceId/firmware-upgrades", create.GinPath)
	require.Equal(t, "device.maintenance.firmware-upgrade.v1", create.AdapterKey)
	require.Equal(t, RiskDestructive, create.RiskLevel)
	require.Equal(t, IdempotencyRequired, create.IdempotencyMode)
	require.Equal(t, []string{"2022"}, create.ProtocolVersions)
	require.True(t, create.RequiresConfirmation)
	require.Contains(t, create.StandardRefs, "GB/T 28181-2022 9.13, 附录 A.2.3.1.12、A.2.5.9")
	require.Contains(t, create.RequestSchema, `"firmwareId"`)
	require.NotContains(t, create.RequestSchema, `"fileUrl"`)

	read, ok := byScope["device:firmware-upgrade:read"]
	require.True(t, ok)
	require.Equal(t, "GET", read.Method)
	require.Equal(t, "/openapi/v1/devices/:deviceId/firmware-upgrades/:operationId", read.GinPath)
	require.Equal(t, "device.maintenance.firmware-upgrade-read.v1", read.AdapterKey)
	require.Equal(t, RiskRead, read.RiskLevel)
	require.Equal(t, IdempotencyNone, read.IdempotencyMode)
	require.Equal(t, []string{"2022"}, read.ProtocolVersions)
	require.NotContains(t, read.ResponseSchema, `"fileUrl"`)
}

func TestValidateRejectsIncompleteOrConflictingDefinitions(t *testing.T) {
	valid := Operation{
		GroupCode: "device-management", GroupName: "Device management", Code: "device.list",
		Scope: "device:list", Name: "List devices", Method: "GET",
		GinPath: "/openapi/v1/devices", ExternalPath: "/openapi/v1/devices",
		AdapterKey: "resource.device.list.v1", AdapterVersion: "v1",
		ResourceType: "device", RiskLevel: "read", IdempotencyMode: "none", RequestSchema: "{}", ResponseSchema: "{}",
	}

	tests := []struct {
		name  string
		items []Operation
	}{
		{name: "duplicate scope", items: []Operation{valid, func() Operation {
			next := valid
			next.Code = "device.other"
			next.GinPath = "/openapi/v1/other"
			next.ExternalPath = next.GinPath
			return next
		}()}},
		{name: "duplicate route", items: []Operation{valid, func() Operation { next := valid; next.Scope = "device:other"; next.Code = "device.other"; return next }()}},
		{name: "missing adapter", items: []Operation{func() Operation { next := valid; next.AdapterKey = ""; return next }()}},
		{name: "unknown risk", items: []Operation{func() Operation { next := valid; next.RiskLevel = "surprise"; return next }()}},
		{name: "unknown idempotency", items: []Operation{func() Operation { next := valid; next.IdempotencyMode = "sometimes"; return next }()}},
		{name: "missing gin path", items: []Operation{func() Operation { next := valid; next.GinPath = ""; return next }()}},
		{name: "invalid schema JSON", items: []Operation{func() Operation { next := valid; next.RequestSchema = "not-json"; return next }()}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Error(t, Validate(test.items))
		})
	}
}

func TestOperationsReturnsAnIsolatedCopy(t *testing.T) {
	first := Operations()
	index := -1
	for candidate := range first {
		if first[candidate].Scope == "play:live" {
			index = candidate
			break
		}
	}
	require.NotEqual(t, -1, index)
	first[index].Scope = "corrupted"
	first[index].ProtocolVersions[0] = "corrupted"
	first[index].StandardRefs[0] = "corrupted"
	second := Operations()
	require.NotEqual(t, "corrupted", second[index].Scope)
	require.NotEqual(t, "corrupted", second[index].ProtocolVersions[0])
	require.NotEqual(t, "corrupted", second[index].StandardRefs[0])
}
