package contracts

import "encoding/json"

// TargetParameters are bound from GET query fields or POST JSON fields.
var targetParameters = map[string][]string{
	"device:detail":                       {"deviceId"},
	"device:status":                       {"deviceId"},
	"channel:list":                        {"deviceId"},
	"channel:detail":                      {"deviceId", "channelId"},
	"channel:status":                      {"deviceId", "channelId"},
	"channel:capabilities":                {"deviceId", "channelId"},
	"channel:storage-cards":               {"deviceId", "channelId"},
	"play:live":                           {"deviceId", "channelId"},
	"play:live:stop":                      {"deviceId", "channelId"},
	"ptz:preset:list":                     {"deviceId", "channelId"},
	"ptz:preset:save":                     {"deviceId", "channelId"},
	"ptz:preset:call":                     {"deviceId", "channelId", "presetId"},
	"ptz:preset:delete":                   {"deviceId", "channelId", "presetId"},
	"ptz:operation:read":                  {"deviceId", "channelId", "operationId"},
	"device:alarms":                       {"deviceId"},
	"device:status-events":                {"deviceId"},
	"device:subscription:create":          {"deviceId"},
	"device:subscription:list":            {"deviceId"},
	"device:subscription:read":            {"deviceId", "subscriptionId"},
	"device:subscription:cancel":          {"deviceId", "subscriptionId"},
	"device:subscription:renew":           {"deviceId", "subscriptionId"},
	"device:events:read":                  {"deviceId", "subscriptionId"},
	"device:position:read":                {"deviceId"},
	"device:position:history:read":        {"deviceId"},
	"device:config:refresh":               {"deviceId", "channelId"},
	"device:config:read":                  {"deviceId", "channelId"},
	"device:config:apply":                 {"deviceId", "channelId"},
	"channel:video-params:refresh":        {"deviceId", "channelId"},
	"channel:video-params:read":           {"deviceId", "channelId"},
	"channel:video-params:apply":          {"deviceId", "channelId"},
	"device:config:operation:read":        {"deviceId", "channelId", "operationId"},
	"channel:precise-status":              {"deviceId", "channelId"},
	"ptz:cruise:read":                     {"deviceId", "channelId"},
	"ptz:cruise:track:create":             {"deviceId", "channelId"},
	"ptz:cruise:track:read":               {"deviceId", "channelId", "trackId"},
	"channel:target-track:read":           {"deviceId", "channelId"},
	"ptz:move":                            {"deviceId", "channelId"},
	"ptz:precise":                         {"deviceId", "channelId"},
	"ptz:extended":                        {"deviceId", "channelId"},
	"ptz:wiper":                           {"deviceId", "channelId"},
	"ptz:home-position:read":              {"deviceId", "channelId"},
	"ptz:home-position:set":               {"deviceId", "channelId"},
	"ptz:cruise:control":                  {"deviceId", "channelId"},
	"ptz:target-track":                    {"deviceId", "channelId"},
	"device:recording:start":              {"deviceId", "channelId"},
	"device:recording:stop":               {"deviceId", "channelId"},
	"device:alarm:guard:set":              {"deviceId", "channelId"},
	"device:alarm:guard:reset":            {"deviceId", "channelId"},
	"device:alarm:reset":                  {"deviceId", "channelId"},
	"device:maintenance:reboot":           {"deviceId"},
	"channel:storage-card:format":         {"deviceId", "channelId"},
	"device:maintenance:operation:list":   {"deviceId"},
	"device:maintenance:operation:read":   {"deviceId", "operationId"},
	"device:catalog:refresh":              {"deviceId"},
	"device:catalog:refresh:read":         {"deviceId", "operationId"},
	"device:talk:create":                  {"deviceId", "channelId"},
	"device:talk:read":                    {"deviceId", "channelId", "sessionId"},
	"device:talk:end":                     {"deviceId", "channelId", "sessionId"},
	"device:broadcast:create":             {"deviceId", "channelId"},
	"device:broadcast:read":               {"deviceId", "channelId", "sessionId"},
	"device:broadcast:end":                {"deviceId", "channelId", "sessionId"},
	"device:talk:media":                   {"deviceId", "channelId", "sessionId"},
	"device:broadcast:media":              {"deviceId", "channelId", "sessionId"},
	"device:firmware-upgrade:execute":     {"deviceId"},
	"device:firmware-upgrade:list":        {"deviceId"},
	"device:firmware-upgrade:read":        {"deviceId", "operationId"},
	"channel:mounts:read":                 {"deviceId", "channelId"},
	"channel:timeline:read":               {"deviceId", "channelId"},
	"device:record-query:options":         {"deviceId", "channelId"},
	"device:record-query":                 {"deviceId", "channelId"},
	"device:playback-create":              {"deviceId", "channelId"},
	"device:playback-read":                {"deviceId", "channelId", "sessionId"},
	"device:playback-action":              {"deviceId", "channelId", "sessionId"},
	"device:playback-stop":                {"deviceId", "channelId", "sessionId"},
	"device:download-create":              {"deviceId", "channelId"},
	"device:download-read":                {"deviceId", "channelId", "sessionId"},
	"device:download-cancel":              {"deviceId", "channelId", "sessionId"},
	"channel:snapshot:create":             {"deviceId", "channelId"},
	"channel:snapshot:read":               {"deviceId", "channelId", "sessionId"},
	"channel:snapshot:image:read":         {"deviceId", "channelId", "sessionId", "fileName"},
	"channel:snapshot:library:read":       {"deviceId", "channelId"},
	"channel:snapshot:library:delete":     {"deviceId", "channelId"},
	"channel:snapshot:library:image:read": {"deviceId", "channelId", "snapshotId"},
}

var internalMethodDefaults = map[string]string{
	"play:live:stop":                  "DELETE",
	"ptz:preset:delete":               "DELETE",
	"device:subscription:cancel":      "DELETE",
	"device:config:apply":             "PUT",
	"channel:video-params:apply":      "PUT",
	"device:download-cancel":          "DELETE",
	"channel:snapshot:library:delete": "DELETE",
}

// targetSchema mirrors the scalar target models in the published OpenAPI contract.
func targetSchema(name string) map[string]any {
	switch name {
	case "deviceId", "channelId":
		return map[string]any{"type": "string", "pattern": "^[0-9]{20}$"}
	case "presetId":
		return map[string]any{"type": "string", "pattern": "^[0-9]{1,3}$"}
	case "trackId":
		return map[string]any{"type": "integer", "minimum": 0, "maximum": 255}
	case "snapshotId", "alarmId", "fileId":
		return map[string]any{"type": "string", "pattern": "^[1-9][0-9]{0,19}$"}
	case "subscriptionId":
		return map[string]any{"type": "string", "pattern": "^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"}
	case "fileName":
		return map[string]any{"type": "string", "pattern": "^[A-Za-z0-9._-]{1,64}$"}
	default:
		return map[string]any{"type": "string", "minLength": 1, "maxLength": 64}
	}
}

func publicOperation(operation Operation) Operation {
	operation.TargetParameters = append([]string(nil), targetParameters[operation.Scope]...)
	operation.BusinessRequestSchema = operation.RequestSchema
	operation.BusinessQueryAllowlist = append([]string(nil), operation.QueryAllowlist...)
	if operation.SysAPIMethod == "" {
		operation.SysAPIMethod = internalMethodDefaults[operation.Scope]
	}
	if operation.Method == "GET" {
		operation.QueryAllowlist = append(append([]string(nil), operation.QueryAllowlist...), operation.TargetParameters...)
		return operation
	}
	var schema map[string]any
	_ = json.Unmarshal([]byte(operation.RequestSchema), &schema)
	if schema == nil {
		schema = make(map[string]any)
	}
	schema["type"] = "object"
	if schema["additionalProperties"] == nil {
		schema["additionalProperties"] = false
	}
	properties, _ := schema["properties"].(map[string]any)
	if properties == nil {
		properties = make(map[string]any)
	}
	required, _ := schema["required"].([]any)
	for _, name := range operation.TargetParameters {
		properties[name] = targetSchema(name)
		if name == "sessionId" && (operation.Scope == "device:playback-action" || operation.Scope == "device:playback-stop" || operation.Scope == "device:download-cancel") {
			properties[name] = map[string]any{"type": "string", "format": "uuid"}
		}
		required = append(required, name)
	}
	if minimum, ok := schema["minProperties"].(float64); ok {
		schema["minProperties"] = minimum + float64(len(operation.TargetParameters))
	}
	if operation.Scope == "device:record-query" {
		properties["cursor"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 256}
		properties["pageSize"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "default": 20}
		required = []any{"deviceId", "channelId"}
		schema["oneOf"] = []any{
			map[string]any{"required": []string{"startTime", "endTime"}},
			map[string]any{"required": []string{"cursor"}},
		}
		operation.QueryAllowlist = nil
	}
	schema["properties"] = properties
	schema["required"] = required
	encoded, _ := json.Marshal(schema)
	operation.RequestSchema = string(encoded)
	return operation
}
