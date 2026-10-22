// Package contracts owns the code-side OpenAPI operation surface. Database
// catalog rows may publish these operations, but they cannot add routes or
// executable adapters.
package contracts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Operation is the stable metadata shared by public routing, capability
// bootstrap, client compatibility, and contract tests.
type Operation struct {
	GroupCode            string
	GroupName            string
	Code                 string
	Scope                string
	Name                 string
	Description          string
	InternalPath         string
	SysAPIGroup          string
	SysAPIMethod         string
	Method               string
	GinPath              string
	ExternalPath         string
	AdapterKey           string
	AdapterVersion       string
	ResourceType         string
	RiskLevel            string
	IdempotencyMode      string
	ProtocolVersions     []string
	StandardRefs         []string
	RequiresConfirmation bool
	RequestSchema        string
	ResponseSchema       string
	QueryAllowlist       []string
}

const (
	RiskRead        = "read"
	RiskMedia       = "media"
	RiskControl     = "control"
	RiskDestructive = "destructive"

	IdempotencyNone     = "none"
	IdempotencyOptional = "optional"
	IdempotencyRequired = "required"
)

var operationDefinitions = []Operation{
	{
		GroupCode: "device-management", GroupName: "设备管理", Code: "device.list", Scope: "device:list", Name: "设备列表", Description: "分页查询客户端部门范围内可见的设备。",
		InternalPath: "/api/gb28181/device-mgmt/devices", Method: http.MethodGet, GinPath: "/openapi/v1/devices", ExternalPath: "/openapi/v1/devices",
		AdapterKey: "resource.device.list.v1", AdapterVersion: "v1", ResourceType: "device", RiskLevel: RiskRead, IdempotencyMode: IdempotencyNone,
		QueryAllowlist: []string{"keyword", "page", "pageSize", "status"}, RequestSchema: "{}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-management", GroupName: "设备管理", Code: "device.detail", Scope: "device:detail", Name: "设备详情", Description: "读取一台客户端可见设备的基本资料。",
		InternalPath: "/api/gb28181/device-mgmt/device/:id", Method: http.MethodGet, GinPath: "/openapi/v1/devices/:deviceId", ExternalPath: "/openapi/v1/devices/{deviceId}",
		AdapterKey: "resource.device.detail.v1", AdapterVersion: "v1", ResourceType: "device", RiskLevel: RiskRead, IdempotencyMode: IdempotencyNone,
		RequestSchema: "{}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-management", GroupName: "设备管理", Code: "device.status", Scope: "device:status", Name: "设备状态", Description: "读取设备最近一次在线状态。",
		InternalPath: "/api/gb28181/device-mgmt/device/:id/status-events", Method: http.MethodGet, GinPath: "/openapi/v1/devices/:deviceId/status", ExternalPath: "/openapi/v1/devices/{deviceId}/status",
		AdapterKey: "resource.device.status.v1", AdapterVersion: "v1", ResourceType: "device", RiskLevel: RiskRead, IdempotencyMode: IdempotencyNone,
		RequestSchema: "{}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-management", GroupName: "设备管理", Code: "channel.list", Scope: "channel:list", Name: "通道列表", Description: "分页查询指定设备下客户端可见的通道。",
		InternalPath: "/api/gb28181/device-mgmt/channels", Method: http.MethodGet, GinPath: "/openapi/v1/devices/:deviceId/channels", ExternalPath: "/openapi/v1/devices/{deviceId}/channels",
		AdapterKey: "resource.channel.list.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskRead, IdempotencyMode: IdempotencyNone,
		QueryAllowlist: []string{"keyword", "page", "pageSize", "status"}, RequestSchema: "{}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-management", GroupName: "设备管理", Code: "channel.detail", Scope: "channel:detail", Name: "通道详情", Description: "读取指定设备下一个通道的基本资料。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id", Method: http.MethodGet, GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}",
		AdapterKey: "resource.channel.detail.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskRead, IdempotencyMode: IdempotencyNone,
		RequestSchema: "{}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "play.live", Scope: "play:live", Name: "发起实时点播", Description: "为可见通道创建或复用实时媒体会话。",
		InternalPath: "/api/gb28181/play/:deviceId/:channelId", SysAPIGroup: "设备管理", Method: http.MethodPost,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/live", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/live",
		AdapterKey: "delegated.play.live.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskControl, IdempotencyMode: IdempotencyNone,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"GB/T 28181-2016 9.2", "GB/T 28181-2022 9.2"},
		RequestSchema: "{}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "play.live.stop", Scope: "play:live:stop", Name: "结束实时点播", Description: "结束本客户端为该通道发起的实时媒体流；平台直播流按通道共享，停流会中断该通道的其他观看者，客户端从未发起过的通道返回未找到。",
		InternalPath: "/api/gb28181/play/:streamId", Method: http.MethodDelete,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/live", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/live",
		AdapterKey: "delegated.play.live-stop.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskMedia, IdempotencyMode: IdempotencyNone,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"GB/T 28181-2016 9.2", "GB/T 28181-2022 9.2"},
		RequestSchema:  "{}",
		ResponseSchema: `{"type":"object","required":["released"],"additionalProperties":false,"properties":{"released":{"type":"boolean"},"streamId":{"type":"string"}}}`,
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "channel.status", Scope: "channel:status", Name: "通道状态", Description: "读取指定通道最近一次在线状态。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/device-status", Method: http.MethodGet, GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/status", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/status",
		AdapterKey: "resource.channel.status.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskRead, IdempotencyMode: IdempotencyNone,
		RequestSchema: "{}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "channel.capabilities", Scope: "channel:capabilities", Name: "通道控制能力", Description: "读取平台已解析的控制能力；unknown 不表示设备支持。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/control-capabilities", Method: http.MethodGet,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/control-capabilities", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/control-capabilities",
		AdapterKey: "resource.channel.capabilities.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskRead, IdempotencyMode: IdempotencyNone,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"平台基于设备上报能力的三态解析"},
		RequestSchema: "{}", ResponseSchema: "{\"type\":\"object\",\"required\":[\"basicPtz\",\"targetTrack\"]}",
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "channel.storage-cards", Scope: "channel:storage-cards", Name: "存储卡状态", Description: "读取平台缓存的存储卡状态，不会主动向设备查询。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/storage-cards", Method: http.MethodGet,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/storage-cards", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/storage-cards",
		AdapterKey: "resource.channel.storage-cards.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskRead, IdempotencyMode: IdempotencyNone,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"平台缓存的设备存储状态；结果带 freshness/observedAt"}, RequestSchema: "{}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-management", GroupName: "设备管理", Code: "device.alarms", Scope: "device:alarms", Name: "设备告警记录", Description: "分页读取客户端可见设备的已持久化告警记录。",
		InternalPath: "/api/gb28181/device-mgmt/device/:id/alarms", Method: http.MethodGet,
		GinPath: "/openapi/v1/devices/:deviceId/alarms", ExternalPath: "/openapi/v1/devices/{deviceId}/alarms",
		AdapterKey: "devicequery.device.alarms.v1", AdapterVersion: "v1", ResourceType: "device", RiskLevel: RiskRead, IdempotencyMode: IdempotencyNone,
		QueryAllowlist: []string{"page", "pageSize"}, RequestSchema: "{}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-management", GroupName: "设备管理", Code: "device.status-events", Scope: "device:status-events", Name: "设备状态事件", Description: "按不透明游标续读设备状态事件；结果来自平台事件记录。",
		InternalPath: "/api/gb28181/device-mgmt/device/:id/status-events", Method: http.MethodGet,
		GinPath: "/openapi/v1/devices/:deviceId/status-events", ExternalPath: "/openapi/v1/devices/{deviceId}/status-events",
		AdapterKey: "devicequery.device.status-events.v1", AdapterVersion: "v1", ResourceType: "device", RiskLevel: RiskRead, IdempotencyMode: IdempotencyNone,
		QueryAllowlist: []string{"cursor", "pageSize"}, RequestSchema: "{}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "channel.precise-status", Scope: "channel:precise-status", Name: "精准云台状态", Description: "读取平台采集的精准云台状态快照和观测时间。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/precise-status", Method: http.MethodGet,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/precise-status", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/precise-status",
		AdapterKey: "devicequery.channel.precise-status.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskRead, IdempotencyMode: IdempotencyNone,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"平台缓存的设备状态；结果带 freshness/observedAt"}, RequestSchema: "{}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "ptz.home-position.read", Scope: "ptz:home-position:read", Name: "看守位状态", Description: "读取看守位缓存及平台确认来源，不会主动下发查询命令。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/home-position", Method: http.MethodGet,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/home-position", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/home-position",
		AdapterKey: "devicequery.ptz.home-position.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskRead, IdempotencyMode: IdempotencyNone,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"平台缓存的看守位设置与核验状态"}, RequestSchema: "{}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "ptz.cruise.tracks.read", Scope: "ptz:cruise:read", Name: "巡航轨迹列表", Description: "读取已缓存的巡航轨迹及数据来源和观测时间。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/cruise/tracks", Method: http.MethodGet,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/cruise/tracks", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/cruise/tracks",
		AdapterKey: "devicequery.ptz.cruise-tracks.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskRead, IdempotencyMode: IdempotencyNone,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"平台缓存的巡航轨迹"}, RequestSchema: "{}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "channel.target-track.read", Scope: "channel:target-track:read", Name: "目标跟踪意图", Description: "读取平台最近一次目标跟踪下发意图；设备确认状态始终如实返回。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/target-track", Method: http.MethodGet,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/target-track", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/target-track",
		AdapterKey: "devicequery.channel.target-track.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskRead, IdempotencyMode: IdempotencyNone,
		ProtocolVersions: []string{"2022"}, StandardRefs: []string{"平台最近一次目标跟踪意图；设备不提供执行确认"}, RequestSchema: "{}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "ptz.preset.list", Scope: "ptz:preset:list", Name: "预置位列表", Description: "读取通道已知的云台预置位。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/presets", Method: http.MethodGet,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/presets", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets",
		AdapterKey: "delegated.ptz.preset-list.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskRead, IdempotencyMode: IdempotencyNone,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"GB/T 28181-2016 A.2.4.8", "GB/T 28181-2022 A.2.4.8"}, RequestSchema: "{}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "ptz.preset.save", Scope: "ptz:preset:save", Name: "保存预置位", Description: "创建或更新通道云台预置位。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/presets", Method: http.MethodPost,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/presets", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets",
		AdapterKey: "delegated.ptz.preset-save.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskControl, IdempotencyMode: IdempotencyRequired,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"GB/T 28181-2016 A.2.3.1.2", "GB/T 28181-2022 A.2.3.1.2"},
		RequestSchema: "{\"type\":\"object\",\"required\":[\"presetId\",\"name\"],\"additionalProperties\":false}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "ptz.preset.call", Scope: "ptz:preset:call", Name: "调用预置位", Description: "调用通道云台预置位，设备执行状态以操作结果为准。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId/call", Method: http.MethodPost,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/presets/:presetId/call", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}/call",
		AdapterKey: "delegated.ptz.preset-call.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskControl, IdempotencyMode: IdempotencyRequired,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"GB/T 28181-2016 A.2.3.1.2", "GB/T 28181-2022 A.2.3.1.2"}, RequestSchema: "{\"type\":\"object\",\"additionalProperties\":false}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "ptz.preset.delete", Scope: "ptz:preset:delete", Name: "删除预置位", Description: "删除指定云台预置位记录。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId", Method: http.MethodDelete,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/presets/:presetId", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}",
		AdapterKey: "delegated.ptz.preset-delete.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskControl, IdempotencyMode: IdempotencyRequired,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"GB/T 28181-2016 A.2.3.1.2", "GB/T 28181-2022 A.2.3.1.2"}, RequestSchema: "{\"type\":\"object\",\"additionalProperties\":false}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "ptz.operation.read", Scope: "ptz:operation:read", Name: "云台操作结果", Description: "读取指定通道上一条云台操作的处理结果。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/operations/:operationId", Method: http.MethodGet,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/operations/:operationId", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/operations/{operationId}",
		AdapterKey: "delegated.ptz.operation-read.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskRead, IdempotencyMode: IdempotencyNone,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"Platform operation lifecycle"}, RequestSchema: "{}", ResponseSchema: "{}",
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "ptz.move", Scope: "ptz:move", Name: "云台与镜头控制", Description: "控制通道云台方向、变倍、聚焦和光圈；结果表示已下发，不表示设备已执行。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/ptz", Method: http.MethodPost,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/move", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/move",
		AdapterKey: "delegated.ptz.move.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskControl, IdempotencyMode: IdempotencyRequired,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"GB/T 28181-2022 7, 9.3, 附录 A"},
		RequestSchema:  `{"type":"object","required":["action","speed"],"additionalProperties":false,"properties":{"action":{"type":"string","enum":["stop","left","right","up","down","left_up","right_up","left_down","right_down","zoom_in","zoom_out","focus_far","focus_near","iris_open","iris_close","lens_stop"]},"speed":{"type":"integer","minimum":0,"maximum":255}}}`,
		ResponseSchema: `{"type":"object","required":["operationId","status","action","sn","result","deviceAcknowledged"],"additionalProperties":false}`,
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "ptz.precise", Scope: "ptz:precise", Name: "精准云台控制", Description: "按设备 2022 协议能力下发精准云台坐标控制。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/precise", Method: http.MethodPost,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/precise", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/precise",
		AdapterKey: "delegated.ptz.precise.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskControl, IdempotencyMode: IdempotencyRequired,
		ProtocolVersions: []string{"2022"}, StandardRefs: []string{"GB/T 28181-2022 7, 9.3, 附录 A"},
		RequestSchema:  `{"type":"object","minProperties":1,"additionalProperties":false,"properties":{"pan":{"type":"number","minimum":-360,"maximum":360},"tilt":{"type":"number","minimum":-180,"maximum":180},"zoom":{"type":"number","minimum":0,"maximum":100000}}}`,
		ResponseSchema: `{"type":"object","required":["operationId","status","action","sn","result","deviceAcknowledged"],"additionalProperties":false}`,
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "ptz.extended", Scope: "ptz:extended", Name: "扫描与巡航点控制", Description: "执行设备支持的扫描及巡航点配置动作。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/extended", Method: http.MethodPost,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/extended", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/extended",
		AdapterKey: "delegated.ptz.extended.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskControl, IdempotencyMode: IdempotencyRequired,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"GB/T 28181-2022 7, 9.3, 附录 A"},
		RequestSchema:  `{"type":"object","required":["action","id"],"additionalProperties":false,"properties":{"action":{"type":"string","enum":["scan_start","scan_stop","scan_set_left","scan_set_right","scan_set_speed","cruise_add_stop","cruise_delete_stop","cruise_set_speed","cruise_set_dwell"]},"id":{"type":"integer","minimum":0,"maximum":255},"presetId":{"type":"integer","minimum":0,"maximum":255},"value":{"type":"integer","minimum":1,"maximum":4095}}}`,
		ResponseSchema: `{"type":"object","required":["operationId","status","action","sn","result","deviceAcknowledged"],"additionalProperties":false}`,
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "ptz.wiper", Scope: "ptz:wiper", Name: "雨刷控制", Description: "按标准定义的雨刷辅助开关编号控制雨刷。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/wiper", Method: http.MethodPost,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/wiper", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/wiper",
		AdapterKey: "delegated.ptz.wiper.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskControl, IdempotencyMode: IdempotencyRequired,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"GB/T 28181-2022 7, 9.3, 附录 A"},
		RequestSchema:  `{"type":"object","required":["action"],"additionalProperties":false,"properties":{"action":{"type":"string","enum":["on","off"]}}}`,
		ResponseSchema: `{"type":"object","required":["operationId","status","action","sn","result","deviceAcknowledged"],"additionalProperties":false}`,
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "ptz.home-position.set", Scope: "ptz:home-position:set", Name: "看守位设置", Description: "设置或关闭通道看守位，并报告设备应答状态。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/home-position", SysAPIMethod: http.MethodPatch, Method: http.MethodPost,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/home-position", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/home-position",
		AdapterKey: "delegated.ptz.home-position-set.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskControl, IdempotencyMode: IdempotencyRequired,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"GB/T 28181-2022 7, 9.3, 附录 A"},
		RequestSchema:  `{"type":"object","required":["enabled"],"additionalProperties":false,"properties":{"enabled":{"type":"boolean"},"resetTime":{"type":"integer","minimum":10,"maximum":3600},"presetId":{"type":"integer","minimum":0,"maximum":255}},"description":"When enabled is true, resetTime and presetId are required; when false, both are omitted."}`,
		ResponseSchema: `{"type":"object","required":["operationId","status","action","sn","result","deviceAcknowledged"],"additionalProperties":false}`,
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "ptz.cruise.control", Scope: "ptz:cruise:control", Name: "巡航控制", Description: "启动、停止或删除通道上的指定巡航轨迹。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/ptz/cruise", Method: http.MethodPost,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/cruise/control", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/cruise/control",
		AdapterKey: "delegated.ptz.cruise-control.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskControl, IdempotencyMode: IdempotencyRequired,
		ProtocolVersions: []string{"2016", "2022"}, StandardRefs: []string{"GB/T 28181-2022 7, 9.3, 附录 A"},
		RequestSchema:  `{"type":"object","required":["action","trackId"],"additionalProperties":false,"properties":{"action":{"type":"string","enum":["start","stop","delete"]},"trackId":{"type":"integer","minimum":0,"maximum":255}}}`,
		ResponseSchema: `{"type":"object","required":["operationId","status","action","sn","result","deviceAcknowledged"],"additionalProperties":false}`,
	},
	{
		GroupCode: "device-control", GroupName: "设备控制", Code: "ptz.target-track", Scope: "ptz:target-track", Name: "目标跟踪控制", Description: "仅对 2022 设备明确上报支持时下发目标跟踪；返回值不代表设备执行确认。",
		InternalPath: "/api/gb28181/device-mgmt/channel/:id/target-track", Method: http.MethodPost,
		GinPath: "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/target-track", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/target-track",
		AdapterKey: "delegated.ptz.target-track.v1", AdapterVersion: "v1", ResourceType: "channel", RiskLevel: RiskControl, IdempotencyMode: IdempotencyRequired,
		ProtocolVersions: []string{"2022"}, StandardRefs: []string{"GB/T 28181-2022 7, 9.3, 附录 A"},
		RequestSchema:  `{"type":"object","required":["mode"],"additionalProperties":false,"properties":{"mode":{"type":"string","enum":["auto","manual","stop"]},"deviceId2":{"type":"string","pattern":"^[0-9]{20}$"},"area":{"type":"object","required":["length","width","midPointX","midPointY","lengthX","lengthY"],"additionalProperties":false,"properties":{"length":{"type":"integer","minimum":1},"width":{"type":"integer","minimum":1},"midPointX":{"type":"integer","minimum":0},"midPointY":{"type":"integer","minimum":0},"lengthX":{"type":"integer","minimum":1},"lengthY":{"type":"integer","minimum":1}}}}}`,
		ResponseSchema: `{"type":"object","required":["operationId","status","action","sn","result","responseRequired","deviceAcknowledged"],"additionalProperties":false,"properties":{"responseRequired":{"const":false},"deviceAcknowledged":{"const":false}}}`,
	},
}

func init() {
	operationDefinitions = append(operationDefinitions, []Operation{
		additionalOperation("device-control", "设备控制", "channel.recording.start", "device:recording:start", "启动设备录像", "按设备上报能力向指定通道发送录像开始命令。", "/api/gb28181/device-mgmt/channel/:id/device-control", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/recording/start", "device.control.record-start.v1", "channel", RiskControl, IdempotencyRequired, []string{"2016", "2022"}, []string{"GB/T 28181-2022 7, 9.3, 附录 A"}, false, `{"type":"object","additionalProperties":false}`, `{"type":"object","required":["operationId","status","responseRequired","deviceAcknowledged"]}`, nil),
		additionalOperation("device-control", "设备控制", "channel.recording.stop", "device:recording:stop", "停止设备录像", "按设备上报能力向指定通道发送录像停止命令。", "/api/gb28181/device-mgmt/channel/:id/device-control", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/recording/stop", "device.control.record-stop.v1", "channel", RiskControl, IdempotencyRequired, []string{"2016", "2022"}, []string{"GB/T 28181-2022 7, 9.3, 附录 A"}, false, `{"type":"object","additionalProperties":false}`, `{"type":"object","required":["operationId","status","responseRequired","deviceAcknowledged"]}`, nil),
		additionalOperation("device-control", "设备控制", "channel.alarm.guard-set", "device:alarm:guard:set", "设备报警布防", "向指定设备通道发送报警布防命令并返回平台操作状态。", "/api/gb28181/device-mgmt/channel/:id/device-control", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/alarms/guard", "device.control.alarm-guard-set.v1", "channel", RiskControl, IdempotencyRequired, []string{"2016", "2022"}, []string{"GB/T 28181-2022 7, 9.3, 附录 A"}, false, `{"type":"object","additionalProperties":false}`, `{"type":"object","required":["operationId","status","responseRequired","deviceAcknowledged"]}`, nil),
		additionalOperation("device-control", "设备控制", "channel.alarm.guard-reset", "device:alarm:guard:reset", "设备报警撤防", "向指定设备通道发送报警撤防命令并返回平台操作状态。", "/api/gb28181/device-mgmt/channel/:id/device-control", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/alarms/guard/reset", "device.control.alarm-guard-reset.v1", "channel", RiskControl, IdempotencyRequired, []string{"2016", "2022"}, []string{"GB/T 28181-2022 7, 9.3, 附录 A"}, false, `{"type":"object","additionalProperties":false}`, `{"type":"object","required":["operationId","status","responseRequired","deviceAcknowledged"]}`, nil),
		additionalOperation("device-control", "设备控制", "channel.alarm.reset", "device:alarm:reset", "设备报警复位", "向指定设备报警目标发送复位命令；结果按设备协议应答语义报告。", "/api/gb28181/device-mgmt/channel/:id/device-control", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/alarms/reset", "device.control.alarm-reset.v1", "channel", RiskControl, IdempotencyRequired, []string{"2016", "2022"}, []string{"GB/T 28181-2022 7, 9.3, 附录 A"}, false, `{"type":"object","additionalProperties":false,"properties":{"alarmMethod":{"type":"string"},"alarmType":{"type":"string"}}}`, `{"type":"object","required":["operationId","status","responseRequired","deviceAcknowledged"]}`, nil),
		additionalOperation("device-control", "设备控制", "device.maintenance.reboot", "device:maintenance:reboot", "设备重启", "对客户端可见设备执行远程重启；必须显式确认并返回可查询操作状态。", "/api/gb28181/device-mgmt/device/:id/reboot", http.MethodPost, "/openapi/v1/devices/:deviceId/maintenance/reboot", "device.maintenance.reboot.v1", "device", RiskDestructive, IdempotencyRequired, []string{"2016", "2022"}, []string{"GB/T 28181-2022 7, 9.3, 附录 A"}, true, `{"type":"object","required":["confirmed"],"additionalProperties":false,"properties":{"confirmed":{"const":true}}}`, `{"type":"object","required":["operationId","status","responseRequired","deviceAcknowledged","deduplicated"]}`, nil),
		additionalOperation("device-control", "设备控制", "device.firmware-upgrade.create", "device:firmware-upgrade:execute", "设备固件升级", "对 GB/T 28181-2022 设备升级已批准且匹配的固件；必须显式确认，并通过 operationId 查询结果。", "/api/gb28181/device-mgmt/device/:id/firmware-upgrade", http.MethodPost, "/openapi/v1/devices/:deviceId/firmware-upgrades", "device.maintenance.firmware-upgrade.v1", "device", RiskDestructive, IdempotencyRequired, []string{"2022"}, []string{"GB/T 28181-2022 9.13, 附录 A.2.3.1.12、A.2.5.9"}, true, `{"type":"object","required":["confirmed","firmwareId"],"additionalProperties":false,"properties":{"confirmed":{"const":true},"firmwareId":{"type":"string","minLength":1,"maxLength":64,"pattern":"^[A-Za-z0-9_-]+$"}}}`, `{"type":"object","required":["operationId","status","submissionStatus","deviceStatus","firmware","currentFirmware","manufacturer","sipStatus","errorCode","failedReason","createdAt"],"additionalProperties":false,"properties":{"operationId":{"type":"string"},"status":{"type":"string","enum":["queued","sent","accepted","succeeded","failed","rejected","unknown"]},"submissionStatus":{"type":"string","enum":["pending","sent","rejected","unknown"]},"deviceStatus":{"type":"string","enum":["pending","accepted","succeeded","failed","rejected","unknown"]},"firmware":{"type":"string"},"currentFirmware":{"type":"string"},"manufacturer":{"type":"string"},"sipStatus":{"type":"integer"},"errorCode":{"type":"string"},"failedReason":{"type":"string"},"createdAt":{"type":"string","format":"date-time"},"sentAt":{"type":"string","format":"date-time","nullable":true},"acceptedAt":{"type":"string","format":"date-time","nullable":true},"completedAt":{"type":"string","format":"date-time","nullable":true},"deadlineAt":{"type":"string","format":"date-time","nullable":true},"deduplicated":{"type":"boolean"}}}`, nil),
		additionalOperation("device-management", "设备管理", "device.firmware-upgrade.read", "device:firmware-upgrade:read", "设备固件升级结果", "按设备、部门和客户端归属读取固件升级状态；SIP 信令已发送不代表设备已接受或升级成功。", "/api/gb28181/device-mgmt/device/:id/firmware-upgrades", http.MethodGet, "/openapi/v1/devices/:deviceId/firmware-upgrades/:operationId", "device.maintenance.firmware-upgrade-read.v1", "device", RiskRead, IdempotencyNone, []string{"2022"}, []string{"GB/T 28181-2022 9.13, 附录 A.2.3.1.12、A.2.5.9"}, false, `{}`, `{"type":"object","required":["operationId","status","submissionStatus","deviceStatus","firmware","currentFirmware","manufacturer","sipStatus","errorCode","failedReason","createdAt"],"additionalProperties":false,"properties":{"operationId":{"type":"string"},"status":{"type":"string","enum":["queued","sent","accepted","succeeded","failed","rejected","unknown"]},"submissionStatus":{"type":"string","enum":["pending","sent","rejected","unknown"]},"deviceStatus":{"type":"string","enum":["pending","accepted","succeeded","failed","rejected","unknown"]},"firmware":{"type":"string"},"currentFirmware":{"type":"string"},"manufacturer":{"type":"string"},"sipStatus":{"type":"integer"},"errorCode":{"type":"string"},"failedReason":{"type":"string"},"createdAt":{"type":"string","format":"date-time"},"sentAt":{"type":"string","format":"date-time","nullable":true},"acceptedAt":{"type":"string","format":"date-time","nullable":true},"completedAt":{"type":"string","format":"date-time","nullable":true},"deadlineAt":{"type":"string","format":"date-time","nullable":true},"deduplicated":{"type":"boolean"}}}`, nil),
		additionalOperation("device-control", "设备控制", "channel.storage-card.format", "channel:storage-card:format", "存储卡格式化", "格式化指定设备存储卡并清除其录像；必须提供卡编号和显式确认。", "/api/gb28181/device-mgmt/channel/:id/storage-cards/format", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/storage-cards/format", "device.maintenance.storage-card-format.v1", "channel", RiskDestructive, IdempotencyRequired, []string{"2022"}, []string{"GB/T 28181-2022 7, 9.3, 附录 A.2.3.1.13"}, true, `{"type":"object","required":["confirmed","cardIndex"],"additionalProperties":false,"properties":{"confirmed":{"const":true},"cardIndex":{"type":"integer","minimum":0}}}`, `{"type":"object","required":["operationId","status","responseRequired","deviceAcknowledged"]}`, nil),
		additionalOperation("device-management", "设备管理", "device.maintenance.operation.read", "device:maintenance:operation:read", "维护操作状态", "按设备和客户端归属读取重启、录像控制、报警控制或格式化操作结果。", "/api/gb28181/device-mgmt/device/:id/maintenance-operations", http.MethodGet, "/openapi/v1/devices/:deviceId/maintenance/operations/:operationId", "device.maintenance.operation-read.v1", "device", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"平台操作状态；设备确认字段依协议语义返回"}, false, `{}`, `{"type":"object","required":["operationId","status","deviceAcknowledged"]}`, nil),
		additionalOperation("device-management", "设备管理", "device.maintenance.operation.list", "device:maintenance:operation:list", "设备维护操作记录", "分页列出该设备的重启与存储卡格式化记录，含设备未确认及进程中断后状态未知的历史行；不扩大平台维护审计的动作范围。", "/api/gb28181/device-mgmt/device/:id/maintenance-operations", http.MethodGet, "/openapi/v1/devices/:deviceId/maintenance/operations", "devicequery.device.maintenance-operations.v1", "device", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"平台操作状态；设备确认字段依协议语义返回"}, false, `{}`, `{"type":"object","required":["items","page","pageSize","total"],"additionalProperties":false,"properties":{"items":{"type":"array","items":{"type":"object","required":["operationId","action","status","sipStatus","errorMessage","deviceError","targetCode","requestedAt","sentAt","completedAt"],"additionalProperties":false}},"page":{"type":"integer"},"pageSize":{"type":"integer"},"total":{"type":"integer"}}}`, []string{"page", "pageSize"}),
		additionalOperation("device-management", "设备管理", "channel.mounts.read", "channel:mounts:read", "通道目录挂载位置", "列出该通道被挂载到的全部目录节点并标明主挂载位置；挂载关系与目录节点同时在部门范围内校验。", "/api/gb28181/device-mgmt/channel/:id/mounts", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/mounts", "devicequery.channel.mounts.v1", "channel", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"平台目录与多视图挂载关系"}, false, `{}`, `{"type":"object","required":["channelId","items"],"additionalProperties":false,"properties":{"channelId":{"type":"string"},"items":{"type":"array","items":{"type":"object","required":["id","parentNodeId","parentName","parentPath","displayName","isPrimary","mountSource"],"additionalProperties":false}}}}`, nil),
		additionalOperation("device-management", "设备管理", "channel.timeline.read", "channel:timeline:read", "通道 24 小时在线时序", "返回通道最近 24 小时的可用性网格；平台尚未保存真实历史，simplified 为 true 时网格仅重复当前状态，不得据此计算可用率。", "/api/gb28181/device-mgmt/channel/:id/timeline", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/timeline", "devicequery.channel.timeline.v1", "channel", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"平台通道在线状态快照"}, false, `{}`, `{"type":"object","required":["channelId","range","slotMinutes","simplified","slots"],"additionalProperties":false,"properties":{"channelId":{"type":"string"},"range":{"type":"string","enum":["24h"]},"slotMinutes":{"type":"integer"},"simplified":{"type":"boolean"},"slots":{"type":"array","items":{"type":"object","required":["start","end","status"],"additionalProperties":false}}}}`, nil),
		additionalOperation("device-management", "设备管理", "device.record-query.options", "device:record-query:options", "读取录像检索参数", "返回该通道录像检索可接受的时间窗口、时区、平台当前时间与可检索记录类型；通道离线时仍返回参数并标明状态。", "/api/gb28181/device-mgmt/channel/:id/record-query/options", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/recordings/options", "device.record-query-options.v1", "channel", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2016 9.7", "GB/T 28181-2022 9.7"}, false, `{}`, `{"type":"object","required":["device","channel","timezone","serverNow","maxRangeHours","supportedTypes"],"additionalProperties":false,"properties":{"device":{"type":"object","required":["id","name","online"],"additionalProperties":false},"channel":{"type":"object","required":["id","name"],"additionalProperties":false},"timezone":{"type":"string"},"serverNow":{"type":"string","format":"date-time"},"maxRangeHours":{"type":"integer"},"supportedTypes":{"type":"array","items":{"type":"string","enum":["all","manual","alarm"]}}}}`, nil),
		additionalOperation("device-management", "设备管理", "device.catalog.refresh", "device:catalog:refresh", "手动刷新设备目录", "向在线设备下发 Catalog 查询，使其重新上报通道树；同一设备已有进行中的刷新时复用该操作号而不重复下发。", "/api/gb28181/device-mgmt/device/:id/catalog/refresh", http.MethodPost, "/openapi/v1/devices/:deviceId/catalog/refresh", "device.catalog.refresh.v1", "device", RiskControl, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2016 9.4", "GB/T 28181-2022 9.4"}, false, `{}`, `{"type":"object","required":["deviceId","operationId","deduplicated"],"additionalProperties":false,"properties":{"deviceId":{"type":"string"},"operationId":{"type":"string"},"deduplicated":{"type":"boolean"}}}`, nil),
		additionalOperation("device-management", "设备管理", "device.catalog.refresh.read", "device:catalog:refresh:read", "读取目录刷新进度", "按设备和操作号读取异步 Catalog 交换的进度；操作号必须属于同一设备，否则视为未找到。", "/api/gb28181/device-mgmt/device/:id/catalog/refresh/:operationId", http.MethodGet, "/openapi/v1/devices/:deviceId/catalog/refresh/:operationId", "device.catalog.refresh-progress.v1", "device", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2016 9.4", "GB/T 28181-2022 9.4"}, false, `{}`, `{"type":"object","required":["deviceId","operationId","status","receivedCount","errorMessage","startedAt","updatedAt"],"additionalProperties":false,"properties":{"deviceId":{"type":"string"},"operationId":{"type":"string"},"status":{"type":"string","enum":["waiting","receiving","persisting","completed","failed","timeout"]},"receivedCount":{"type":"integer"},"totalCount":{"type":"integer","nullable":true},"errorMessage":{"type":"string"},"startedAt":{"type":"string","format":"date-time"},"updatedAt":{"type":"string","format":"date-time"},"completedAt":{"type":"string","format":"date-time","nullable":true}}}`, nil),
		additionalOperation("device-management", "设备管理", "device.record-query", "device:record-query", "检索设备录像", "按设备通道和时间段检索录像；返回多响应聚合状态及短期客户端隔离游标。", "/api/gb28181/device-mgmt/channel/:id/record-query", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/recordings/search", "device.record-query.v1", "channel", RiskRead, IdempotencyOptional, []string{"2016", "2022"}, []string{"GB/T 28181-2016 9.7", "GB/T 28181-2022 9.7"}, false, `{"type":"object","required":["startTime","endTime"],"additionalProperties":false,"properties":{"startTime":{"type":"string","format":"date-time"},"endTime":{"type":"string","format":"date-time"},"type":{"type":"string","enum":["all","manual","alarm"]},"secrecy":{"type":"integer","minimum":0,"maximum":5},"recorderId":{"type":"string","maxLength":20}}}`, `{"type":"object","required":["items","totalRecords","pageSize","protocolVersion","status","incomplete"]}`, []string{"cursor", "pageSize"}),
		additionalOperation("device-control", "设备控制", "device.playback.create", "device:playback-create", "创建设备录像回放", "根据本客户端最近检索到的录像记录创建回放会话；信令成功不表示媒体首帧已到达。", "/api/gb28181/device-mgmt/channel/:id/playback-sessions", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/playback-sessions", "device.playback-create.v1", "channel", RiskMedia, IdempotencyRequired, []string{"2016", "2022"}, []string{"GB/T 28181-2022 7, 9.8, 附录 B、G、L"}, false, `{"type":"object","required":["recordKey","playFrom"],"additionalProperties":false,"properties":{"recordKey":{"type":"string","minLength":1},"playFrom":{"type":"string","format":"date-time"}}}`, `{"type":"object","required":["sessionId","status","mode","protocolVersion"]}`, nil),
		additionalOperation("device-control", "设备控制", "device.playback.read", "device:playback-read", "读取设备回放会话", "读取当前客户端创建且仍在保留期内的设备录像回放状态。", "/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/playback-sessions/:sessionId", "device.playback-read.v1", "channel", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2022 7, 9.8, 附录 B"}, false, `{}`, `{"type":"object","required":["sessionId","status","mode","protocolVersion"]}`, nil),
		additionalOperation("device-control", "设备控制", "device.playback.action", "device:playback-action", "控制设备录像回放", "对当前客户端拥有的回放会话执行暂停、恢复、定位或倍速操作。", "/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId/actions", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/playback-sessions/:sessionId/actions", "device.playback-action.v1", "channel", RiskControl, IdempotencyRequired, []string{"2016", "2022"}, []string{"GB/T 28181-2022 7, 9.8, 附录 B"}, false, `{"type":"object","required":["action"],"additionalProperties":false,"properties":{"action":{"type":"string","enum":["pause","resume","seek","scale"]},"positionSeconds":{"type":"number","minimum":0},"scale":{"type":"number"}}}`, `{"type":"object","required":["sessionId","status","mode","protocolVersion"]}`, nil),
		additionalOperation("device-control", "设备控制", "device.playback.stop", "device:playback-stop", "结束设备录像回放", "结束当前客户端拥有的回放会话并释放媒体资源。", "/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/playback-sessions/:sessionId/stop", "device.playback-stop.v1", "channel", RiskControl, IdempotencyRequired, []string{"2016", "2022"}, []string{"GB/T 28181-2022 7, 9.8, 附录 B"}, false, `{"type":"object","additionalProperties":false}`, `{"type":"object","required":["sessionId","status"]}`, nil),
		additionalOperation("device-media", "设备媒体", "device.download.create", "device:download-create", "创建设备录像下载", "创建当前客户端拥有的设备录像下载会话；返回媒体会话状态，不伪造文件进度或下载地址。", "/api/gb28181/device-mgmt/channel/:id/download-sessions", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/download-sessions", "device.download-create.v1", "channel", RiskMedia, IdempotencyRequired, []string{"2016", "2022"}, []string{"GB/T 28181-2022 7, 9.9", "GB/T 28181-2022 附录 B、G、L"}, false, `{"type":"object","required":["recordKey","playFrom","downloadSpeed"],"additionalProperties":false,"properties":{"recordKey":{"type":"string","minLength":1},"playFrom":{"type":"string","format":"date-time"},"downloadSpeed":{"type":"integer","enum":[1,2,4,8]}}}`, `{"type":"object","required":["sessionId","status","mode","protocolVersion"],"properties":{"mode":{"const":"download"},"sessionId":{"type":"string"},"status":{"type":"string"},"protocolVersion":{"type":"string"}}}`, nil),
		additionalOperation("device-media", "设备媒体", "device.download.read", "device:download-read", "读取设备录像下载", "读取当前客户端创建的设备录像下载会话状态；文件访问和实际传输仍由媒体会话决定。", "/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/download-sessions/:sessionId", "device.download-read.v1", "channel", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2022 7, 9.9", "GB/T 28181-2022 附录 B、G"}, false, `{}`, `{"type":"object","required":["sessionId","status","mode","protocolVersion"],"properties":{"mode":{"const":"download"}}}`, nil, "设备控制"),
		additionalOperation("device-media", "设备媒体", "device.download.cancel", "device:download-cancel", "取消设备录像下载", "取消当前客户端拥有的设备录像下载会话并释放媒体资源。", "/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId", http.MethodDelete, "/openapi/v1/devices/:deviceId/channels/:channelId/download-sessions/:sessionId", "device.download-cancel.v1", "channel", RiskControl, IdempotencyRequired, []string{"2016", "2022"}, []string{"GB/T 28181-2022 7, 9.9, 附录 B"}, false, `{}`, `{"type":"object","required":["sessionId","status"]}`, nil),
		additionalOperation("device-control", "设备控制", "ptz.cruise.track.create", "ptz:cruise:track:create", "创建巡航轨迹", "按顺序下发清空、添加巡航站点、设置速度和停留时间命令；返回各步平台发送状态，不声称设备已确认。", "/api/gb28181/device-mgmt/channel/:id/ptz/cruise/tracks", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/cruise/tracks", "delegated.ptz.cruise-track-create.v1", "channel", RiskControl, IdempotencyRequired, []string{"2016", "2022"}, []string{"GB/T 28181-2016 7, 9.3, 附录 A", "GB/T 28181-2022 7, 9.3, 附录 A"}, false, `{"type":"object","required":["trackId","stops"],"additionalProperties":false,"properties":{"trackId":{"type":"integer","minimum":0,"maximum":255},"name":{"type":"string","maxLength":255},"speed":{"type":"integer","minimum":0,"maximum":4095},"dwellSec":{"type":"integer","minimum":0,"maximum":4095},"stops":{"type":"array","minItems":1,"maxItems":32,"items":{"type":"object","required":["presetId"],"additionalProperties":false,"properties":{"presetId":{"type":"integer","minimum":1,"maximum":255}}}},"replaceExisting":{"type":"boolean"},"confirmed":{"type":"boolean"}}}`, `{"type":"object","required":["trackId","name","result","deviceAcknowledged","totalStops","completedStops","steps"],"additionalProperties":false,"properties":{"result":{"type":"string","enum":["sent_result_unconfirmed","partial_failure"]},"deviceAcknowledged":{"const":false},"steps":{"type":"array","items":{"type":"object","required":["step","status"],"additionalProperties":true}}}}`, nil),
		additionalOperation("device-control", "设备控制", "ptz.cruise.track.read", "ptz:cruise:track:read", "读取单条巡航轨迹", "读取客户端部门范围内的单条巡航轨迹缓存；不主动向设备发起查询，返回缓存来源与观测时间。", "/api/gb28181/device-mgmt/channel/:id/ptz/cruise-tracks/:trackId", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/cruise/tracks/:trackId", "delegated.ptz.cruise-track-read.v1", "channel", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"平台缓存读取，不触发设备查询"}, false, `{}`, `{"type":"object","required":["track","freshness"],"additionalProperties":false,"properties":{"track":{"type":"object","required":["trackId","name","source"],"additionalProperties":false},"freshness":{"type":"string","enum":["fresh","stale","unknown"]},"observedAt":{"type":"string","format":"date-time","nullable":true}}}`, nil),
		additionalOperation("device-media", "设备媒体", "device.talk.create", "device:talk:create", "创建设备对讲会话", "创建受客户端和通道范围保护的设备语音对讲会话。", "/api/gb28181/device-mgmt/channel/:id/talk-sessions", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/talk-sessions", "device.voice-talk-create.v1", "channel", RiskMedia, IdempotencyRequired, []string{"2016", "2022"}, []string{"GB/T 28181-2022 9.12"}, false, `{"type":"object","additionalProperties":false}`, `{"type":"object","required":["sessionId","state","mode"],"properties":{"sessionId":{"type":"string"},"state":{"type":"string"},"mode":{"type":"string"}}}`, nil),
		additionalOperation("device-media", "设备媒体", "device.talk.read", "device:talk:read", "读取设备对讲会话", "读取当前客户端拥有的对讲会话状态。", "/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/talk-sessions/:sessionId", "device.voice-talk-read.v1", "channel", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2022 9.12"}, false, `{}`, `{"type":"object","required":["sessionId","state","mode"]}`, nil),
		additionalOperation("device-media", "设备媒体", "device.talk.end", "device:talk:end", "结束设备对讲会话", "结束当前客户端拥有的对讲会话并释放媒体资源。", "/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId/end", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/talk-sessions/:sessionId/end", "device.voice-talk-end.v1", "channel", RiskControl, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2022 9.12"}, false, `{"type":"object","additionalProperties":false}`, `{"type":"object","required":["sessionId","state","mode"]}`, nil),
		additionalOperation("device-media", "设备媒体", "device.talk.media", "device:talk:media", "提交设备对讲媒体", "提交 SDP offer 并由平台代理到媒体节点；媒体节点地址和上行凭证不会返回给客户端。", "/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId/uplink", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/talk-sessions/:sessionId/media", "device.voice-talk-media.v1", "channel", RiskMedia, IdempotencyNone, []string{"2016", "2022"}, []string{"平台 WHIP 媒体代理扩展"}, false, `{"type":"object","required":["sdp"],"additionalProperties":false,"properties":{"sdp":{"type":"string","minLength":4,"maxLength":60000}}}`, `{"type":"object","required":["sdp"],"additionalProperties":false,"properties":{"sdp":{"type":"string","minLength":1}}}`, nil),
		additionalOperation("device-media", "设备媒体", "device.broadcast.create", "device:broadcast:create", "创建设备广播会话", "创建受客户端和通道范围保护的设备广播会话。", "/api/gb28181/device-mgmt/channel/:id/broadcast-sessions", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/broadcast-sessions", "device.voice-broadcast-create.v1", "channel", RiskMedia, IdempotencyRequired, []string{"2016", "2022"}, []string{"GB/T 28181-2022 9.12"}, false, `{"type":"object","additionalProperties":false}`, `{"type":"object","required":["sessionId","state","mode"]}`, nil),
		additionalOperation("device-media", "设备媒体", "device.broadcast.read", "device:broadcast:read", "读取设备广播会话", "读取当前客户端拥有的广播会话状态。", "/api/gb28181/device-mgmt/channel/:id/broadcast-sessions/:sessionId", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/broadcast-sessions/:sessionId", "device.voice-broadcast-read.v1", "channel", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2022 9.12"}, false, `{}`, `{"type":"object","required":["sessionId","state","mode"]}`, nil),
		additionalOperation("device-media", "设备媒体", "device.broadcast.end", "device:broadcast:end", "结束设备广播会话", "结束当前客户端拥有的广播会话并释放媒体资源。", "/api/gb28181/device-mgmt/channel/:id/broadcast-sessions/:sessionId/end", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/broadcast-sessions/:sessionId/end", "device.voice-broadcast-end.v1", "channel", RiskControl, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2022 9.12"}, false, `{"type":"object","additionalProperties":false}`, `{"type":"object","required":["sessionId","state","mode"]}`, nil),
		additionalOperation("device-media", "设备媒体", "device.broadcast.media", "device:broadcast:media", "提交设备广播媒体", "提交 SDP offer 并由平台代理到媒体节点；媒体节点地址和上行凭证不会返回给客户端。", "/api/gb28181/device-mgmt/channel/:id/broadcast-sessions/:sessionId/uplink", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/broadcast-sessions/:sessionId/media", "device.voice-broadcast-media.v1", "channel", RiskMedia, IdempotencyNone, []string{"2016", "2022"}, []string{"平台 WHIP 媒体代理扩展"}, false, `{"type":"object","required":["sdp"],"additionalProperties":false,"properties":{"sdp":{"type":"string","minLength":4,"maxLength":60000}}}`, `{"type":"object","required":["sdp"],"additionalProperties":false,"properties":{"sdp":{"type":"string","minLength":1}}}`, nil),
		additionalOperation("device-events", "设备事件与位置", "device.subscription.create", "device:subscription:create", "创建设备事件订阅", "创建客户端隔离的设备告警、状态或移动位置订阅。", "/api/gb28181/device-mgmt/device/:id/subscriptions", http.MethodPost, "/openapi/v1/devices/:deviceId/subscriptions", "device.events.subscription.create.v1", "device", RiskControl, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2022 9.11"}, false, `{"type":"object","required":["kind"],"additionalProperties":false,"properties":{"kind":{"type":"string","enum":["alarm","device_status","mobile_position"]},"expiresSeconds":{"type":"integer","minimum":60,"maximum":86400},"intervalSeconds":{"type":"integer","minimum":0,"maximum":86400}}}`, `{"type":"object","required":["id","deviceId","kind","enabled","status","expiresAt"]}`, nil),
		additionalOperation("device-events", "设备事件与位置", "device.subscription.read", "device:subscription:read", "读取设备事件订阅", "读取当前客户端拥有且仍在设备部门范围内的订阅。", "/api/gb28181/device-mgmt/device/:id/subscriptions/:subscriptionId", http.MethodGet, "/openapi/v1/devices/:deviceId/subscriptions/:subscriptionId", "device.events.subscription.read.v1", "device", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2022 9.11"}, false, `{}`, `{"type":"object","required":["id","deviceId","kind","enabled","status","expiresAt"]}`, nil),
		additionalOperation("device-events", "设备事件与位置", "device.subscription.renew", "device:subscription:renew", "续订设备事件订阅", "按订阅原有有效期续订 SIP 事件订阅。", "/api/gb28181/device-mgmt/device/:id/subscriptions/:subscriptionId/renew", http.MethodPost, "/openapi/v1/devices/:deviceId/subscriptions/:subscriptionId/renew", "device.events.subscription.renew.v1", "device", RiskControl, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2022 9.11"}, false, `{"type":"object","additionalProperties":false}`, `{"type":"object","required":["id","deviceId","kind","enabled","status","expiresAt"]}`, nil),
		additionalOperation("device-events", "设备事件与位置", "device.subscription.cancel", "device:subscription:cancel", "取消设备事件订阅", "取消当前客户端拥有的逻辑订阅；共享 SIP 订阅由平台其他服务继续管理。", "/api/gb28181/device-mgmt/device/:id/subscriptions/:subscriptionId", http.MethodDelete, "/openapi/v1/devices/:deviceId/subscriptions/:subscriptionId", "device.events.subscription.cancel.v1", "device", RiskControl, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2022 9.11"}, false, `{}`, `{"type":"object","required":["id","deviceId","kind","enabled","status","expiresAt"]}`, nil),
		additionalOperation("device-events", "设备事件与位置", "device.events.read", "device:events:read", "续读设备事件", "按客户端绑定的签名游标读取订阅中的告警、状态或移动位置事件。", "/api/gb28181/device-mgmt/device/:id/subscriptions/:subscriptionId/events", http.MethodGet, "/openapi/v1/devices/:deviceId/subscriptions/:subscriptionId/events", "device.events.read.v1", "device", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2022 9.11"}, false, `{}`, `{"type":"object","required":["items","nextCursor","hasMore"]}`, []string{"cursor", "pageSize"}),
		additionalOperation("device-events", "设备事件与位置", "device.position.read", "device:position:read", "读取设备最新位置", "读取平台已采集的设备移动位置；未上报时返回空列表。", "/api/gb28181/device-mgmt/device/:id/mobile-positions", http.MethodGet, "/openapi/v1/devices/:deviceId/positions", "device.events.position.latest.v1", "device", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2022 7, 9.11, 附录 A"}, false, `{}`, `{"type":"object","required":["items"]}`, nil),
		additionalOperation("device-events", "设备事件与位置", "device.position.history.read", "device:position:history:read", "续读设备历史位置", "按客户端绑定的签名游标读取平台已保存的移动位置历史。", "/api/gb28181/device-mgmt/device/:id/mobile-positions/history", http.MethodGet, "/openapi/v1/devices/:deviceId/positions/history", "device.events.position.history.v1", "device", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2022 7, 9.11, 附录 A"}, false, `{}`, `{"type":"object","required":["items","nextCursor","hasMore"]}`, []string{"cursor", "pageSize"}),
		additionalOperation("device.config", "设备配置", "device.config.refresh", "device:config:refresh", "刷新设备配置", "按设备协商版本查询其支持的配置块，并返回可查询的操作状态。", "/api/gb28181/device-mgmt/channel/:id/device-configs/refresh", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/device-configs/refresh", "device.config.refresh.v1", "channel", RiskRead, IdempotencyRequired, []string{"2016", "2022"}, []string{"GB/T 28181-2016 附录 A（DeviceConfig）", "GB/T 28181-2022 附录 A（MANSCDP 配置命令）"}, false, `{"type":"object","additionalProperties":false,"properties":{"configTypes":{"type":"array","minItems":1,"maxItems":9,"uniqueItems":true,"items":{"type":"string","enum":["BasicParam","VideoParamOpt","VideoRecordPlan","VideoAlarmRecord","PictureMask","FrameMirror","AlarmReport","OSDConfig","SnapShotConfig"]}}}}`, `{"type":"object","required":["operationId","action","status","sn","protocolVersion","deviceAcknowledged","reconciliation"]}`, nil),
		additionalOperation("device.config", "设备配置", "device.config.read", "device:config:read", "读取设备配置", "读取平台保存的设备配置快照，并标注协议版本和数据新鲜度。", "/api/gb28181/device-mgmt/channel/:id/device-configs", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/device-configs", "device.config.read.v1", "channel", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2016 附录 A（DeviceConfig）", "GB/T 28181-2022 附录 A（MANSCDP 配置命令）"}, false, `{}`, `{"type":"object","required":["deviceId","channelId","protocolVersion","freshness","items"]}`, nil),
		additionalOperation("device.config", "设备配置", "device.config.apply", "device:config:apply", "下发设备配置", "校验配置块后下发，并返回设备应答与回读对账状态。", "/api/gb28181/device-mgmt/channel/:id/device-configs", http.MethodPut, "/openapi/v1/devices/:deviceId/channels/:channelId/device-configs", "device.config.apply.v1", "channel", RiskControl, IdempotencyRequired, []string{"2016", "2022"}, []string{"GB/T 28181-2016 附录 A（DeviceConfig）", "GB/T 28181-2022 附录 A（MANSCDP 配置命令）"}, false, `{"type":"object","required":["blocks"],"additionalProperties":false,"properties":{"blocks":{"type":"object","minProperties":1,"additionalProperties":false,"properties":{"basicParam":{"type":"object"},"videoRecordPlan":{"type":"object"},"videoAlarmRecord":{"type":"object"},"pictureMask":{"type":"object"},"frameMirror":{"type":"object"},"alarmReport":{"type":"object"},"osdConfig":{"type":"object"}}}}}`, `{"type":"object","required":["operationId","action","status","sn","protocolVersion","deviceAcknowledged","reconciliation"]}`, nil),
		additionalOperation("device.config", "设备配置", "channel.video-params.refresh", "channel:video-params:refresh", "刷新视频参数", "向在线设备查询视频码流参数并返回异步操作状态。", "/api/gb28181/device-mgmt/channel/:id/video-params/refresh", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/video-params/refresh", "device.video-params.refresh.v1", "channel", RiskRead, IdempotencyRequired, []string{"2022"}, []string{"GB/T 28181-2022 附录 A（VideoParamAttribute）"}, false, `{"type":"object","additionalProperties":false}`, `{"type":"object","required":["operationId","action","status","sn","protocolVersion","deviceAcknowledged","reconciliation"]}`, nil),
		additionalOperation("device.config", "设备配置", "channel.video-params.read", "channel:video-params:read", "读取视频参数", "读取平台保存的视频码流参数快照，并标注协议版本和数据新鲜度。", "/api/gb28181/device-mgmt/channel/:id/video-params", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/video-params", "device.video-params.read.v1", "channel", RiskRead, IdempotencyNone, []string{"2022"}, []string{"GB/T 28181-2022 附录 A（VideoParamAttribute）"}, false, `{}`, `{"type":"object","required":["deviceId","channelId","protocolVersion","freshness","items"]}`, nil),
		additionalOperation("device.config", "设备配置", "channel.video-params.apply", "channel:video-params:apply", "下发视频参数", "校验码流参数后下发，并返回设备应答与回读对账状态。", "/api/gb28181/device-mgmt/channel/:id/video-params", http.MethodPut, "/openapi/v1/devices/:deviceId/channels/:channelId/video-params", "device.video-params.apply.v1", "channel", RiskControl, IdempotencyRequired, []string{"2022"}, []string{"GB/T 28181-2022 附录 A（VideoParamAttribute）"}, false, `{"type":"object","required":["items"],"additionalProperties":false,"properties":{"items":{"type":"array","minItems":1,"maxItems":8,"items":{"type":"object","required":["streamNumber","videoFormat","resolution","frameRate","bitRateType"],"additionalProperties":false,"properties":{"streamNumber":{"type":"integer","minimum":0},"videoFormat":{"type":"string","enum":["1","2","3","4","5"]},"resolution":{"type":"string"},"frameRate":{"type":"string","pattern":"^(0|[1-9][0-9]?)$"},"bitRateType":{"type":"string","enum":["1","2"]},"videoBitRate":{"type":"string"}}}}}}`, `{"type":"object","required":["operationId","action","status","sn","protocolVersion","deviceAcknowledged","reconciliation"]}`, nil),
		additionalOperation("device.config", "设备配置", "device.config.operation.read", "device:config:operation:read", "读取设备配置操作", "查询本客户端、通道范围内的配置刷新、下发及视频参数操作结果。", "/api/gb28181/device-mgmt/channel/:id/device-configs/operations/:operationId", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/config-operations/:operationId", "device.config.operation-read.v1", "channel", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"平台配置操作生命周期"}, false, `{}`, `{"type":"object","required":["operationId","action","status","sn","protocolVersion","deviceAcknowledged","reconciliation"]}`, nil),
		additionalOperation("device-events", "设备事件与位置", "device.subscription.list", "device:subscription:list", "设备订阅列表", "列出当前客户端在该设备上建立的全部逻辑订阅，含已取消与已过期项，便于调用方核对自身状态。", "/api/gb28181/device-mgmt/device/:id/subscriptions", http.MethodGet, "/openapi/v1/devices/:deviceId/subscriptions", "device.events.subscription.list.v1", "device", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2022 9.11"}, false, `{}`, `{"type":"object","required":["items"],"additionalProperties":false,"properties":{"items":{"type":"array","items":{"type":"object","required":["id","deviceId","kind","enabled","status","expiresAt"],"additionalProperties":false}}}}`, nil),
		additionalOperation("device-management", "设备管理", "device.firmware-upgrade.list", "device:firmware-upgrade:list", "设备固件升级历史", "按设备和客户端归属列出最近的固件升级尝试，按时间倒序，便于调用方核对自身发起的操作。", "/api/gb28181/device-mgmt/device/:id/firmware-upgrades", http.MethodGet, "/openapi/v1/devices/:deviceId/firmware-upgrades", "device.maintenance.firmware-upgrade-list.v1", "device", RiskRead, IdempotencyNone, []string{"2022"}, []string{"GB/T 28181-2022 9.13, 附录 A.2.3.1.12、A.2.5.9"}, false, `{}`, `{"type":"object","required":["items","totalRecords"],"additionalProperties":false,"properties":{"items":{"type":"array","items":{"type":"object"}},"totalRecords":{"type":"integer","minimum":0}}}`, nil),
		additionalOperation("device-media", "设备媒体", "channel.snapshot.create", "channel:snapshot:create", "设备抓图", "向 2022 版设备下发抓拍配置并创建抓图会话；图像由设备经 SIP 异步回传，需按 sessionId 轮询结果。同一 Idempotency-Key 重放返回同一会话，参数不同则冲突。", "/api/gb28181/device-mgmt/channel/:id/snapshot-sessions", http.MethodPost, "/openapi/v1/devices/:deviceId/channels/:channelId/snapshot-sessions", "device.capture.create.v1", "channel", RiskControl, IdempotencyRequired, []string{"2022"}, []string{"GB/T 28181-2022 附录 A.2.3.2.12（SnapShotConfig）", "GB/T 28181-2022 附录 A.2.5.7（图像抓拍传输完成通知）"}, false, `{"type":"object","required":["snapNum","interval"],"additionalProperties":false,"properties":{"snapNum":{"type":"integer","minimum":1,"maximum":10},"interval":{"type":"integer","minimum":1,"maximum":3600}}}`, `{"type":"object","additionalProperties":false,"properties":{"sessionId":{"type":"string"},"deviceId":{"type":"string"},"channelId":{"type":"string"},"state":{"type":"string","enum":["creating","waiting","completed","failed","unknown"]},"commandStatus":{"type":"string"},"snapNum":{"type":"integer"},"interval":{"type":"integer"},"receivedCount":{"type":"integer"},"notifiedCount":{"type":"integer"},"images":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"fileName":{"type":"string"},"size":{"type":"integer"},"md5":{"type":"string"},"capturedAt":{"type":"string","format":"date-time"},"contentUrl":{"type":"string"}}}},"createdAt":{"type":"string","format":"date-time"},"updatedAt":{"type":"string","format":"date-time"}}}`, nil),
		additionalOperation("device-media", "设备媒体", "channel.snapshot.read", "channel:snapshot:read", "读取抓图会话", "读取本客户端创建的抓图会话及其已回传图像清单；进程重启后内存态丢失时状态报 unknown，不根据已落盘文件反推完成。", "/api/gb28181/device-mgmt/channel/:id/snapshot-sessions/:sessionId", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/snapshot-sessions/:sessionId", "device.capture.read.v1", "channel", RiskRead, IdempotencyNone, []string{"2022"}, []string{"GB/T 28181-2022 附录 A.2.3.2.12（SnapShotConfig）", "GB/T 28181-2022 附录 A.2.5.7（图像抓拍传输完成通知）"}, false, `{}`, `{"type":"object","additionalProperties":false,"properties":{"sessionId":{"type":"string"},"deviceId":{"type":"string"},"channelId":{"type":"string"},"state":{"type":"string","enum":["creating","waiting","completed","failed","unknown"]},"commandStatus":{"type":"string"},"snapNum":{"type":"integer"},"interval":{"type":"integer"},"receivedCount":{"type":"integer"},"notifiedCount":{"type":"integer"},"images":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"fileName":{"type":"string"},"size":{"type":"integer"},"md5":{"type":"string"},"capturedAt":{"type":"string","format":"date-time"},"contentUrl":{"type":"string"}}}},"createdAt":{"type":"string","format":"date-time"},"updatedAt":{"type":"string","format":"date-time"}}}`, nil),
		additionalOperation("device-media", "设备媒体", "channel.snapshot.image.read", "channel:snapshot:image:read", "读取抓图图像内容", "按会话与文件名读取本客户端抓图产生的图像字节；字节以 base64 放在 JSON 信封内，读取时校验大小与 MD5 摘要。", "/api/gb28181/device-mgmt/channel/:id/snapshot-sessions/:sessionId/images/:fileName", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/snapshot-sessions/:sessionId/images/:fileName/content", "device.capture.image-content.v1", "channel", RiskRead, IdempotencyNone, []string{"2022"}, []string{"GB/T 28181-2022 附录 A.2.5.7（图像抓拍传输完成通知）"}, false, `{}`, `{"type":"object","additionalProperties":false,"properties":{"fileName":{"type":"string"},"contentType":{"type":"string"},"size":{"type":"integer"},"md5":{"type":"string"},"contentBase64":{"type":"string"}}}`, nil),
		additionalOperation("snapshot-library", "图像库", "channel.snapshot-library.read", "channel:snapshot:library:read", "通道抓拍图列表", "分页列出该通道图像库中的抓拍图（设备回传、媒体节点截帧与浏览器截图汇于同一张表），只出元数据与取图地址；按拍摄时刻倒序，时间窗打在拍摄时刻而非平台接收时刻。", "/api/gb28181/device-mgmt/snapshots", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/snapshot-library", "device.capture.library.v1", "channel", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2022 附录 A.2.5.7（图像抓拍传输完成通知）", "平台图像库：设备抓拍、媒体节点截帧与浏览器截图汇总"}, false, `{}`, `{"type":"object","required":["items","page","pageSize","total"],"additionalProperties":false,"properties":{"items":{"type":"array","items":{"type":"object"}},"page":{"type":"integer"},"pageSize":{"type":"integer"},"total":{"type":"integer"}}}`, []string{"from", "page", "pageSize", "source", "to"}),
		additionalOperation("snapshot-library", "图像库", "channel.snapshot-library.image.read", "channel:snapshot:library:image:read", "读取抓拍图内容", "按图像库主键读取该通道一张抓拍图的字节；字节以 base64 放在 JSON 信封内，读取时校验大小与 MD5 摘要，超出 8MiB 的异常文件按取不到处理。", "/api/gb28181/device-mgmt/snapshots/:id/content", http.MethodGet, "/openapi/v1/devices/:deviceId/channels/:channelId/snapshot-library/:snapshotId/content", "device.capture.library-image.v1", "channel", RiskRead, IdempotencyNone, []string{"2016", "2022"}, []string{"GB/T 28181-2022 附录 A.2.5.7（图像抓拍传输完成通知）", "平台图像库：设备抓拍、媒体节点截帧与浏览器截图汇总"}, false, `{}`, `{"type":"object","required":["fileName","contentType","size","md5","contentBase64"],"additionalProperties":false,"properties":{"fileName":{"type":"string"},"contentType":{"type":"string"},"size":{"type":"integer"},"md5":{"type":"string"},"contentBase64":{"type":"string"}}}`, nil),
		additionalOperation("snapshot-library", "图像库", "channel.snapshot-library.delete", "channel:snapshot:library:delete", "批量删除抓拍图", "彻底删除该通道图像库中的指定抓拍图：库行与磁盘文件一并删除且不可恢复，必须显式确认。只删本通道且部门可见范围内的图，其余主键计入 missing 而不区分不存在与越权。", "/api/gb28181/device-mgmt/snapshots", http.MethodDelete, "/openapi/v1/devices/:deviceId/channels/:channelId/snapshot-library", "device.capture.library-delete.v1", "channel", RiskDestructive, IdempotencyOptional, []string{"2016", "2022"}, []string{"平台图像库保留策略；删除语义由平台定义，非协议命令"}, true, `{"type":"object","required":["ids","confirmed"],"additionalProperties":false,"properties":{"ids":{"type":"array","minItems":1,"maxItems":100,"items":{"type":"integer","minimum":1}},"confirmed":{"const":true}}}`, `{"type":"object","required":["deleted","filesFailed","missing"],"additionalProperties":false,"properties":{"deleted":{"type":"integer"},"filesFailed":{"type":"integer"},"missing":{"type":"integer"}}}`, nil),
	}...)
}

func additionalOperation(groupCode, groupName, code, scope, name, description, internalPath, method, ginPath, adapterKey, resourceType, risk, idempotency string, versions, standardRefs []string, confirmation bool, requestSchema, responseSchema string, queryAllowlist []string, sysAPIGroup ...string) Operation {
	operation := Operation{
		GroupCode: groupCode, GroupName: groupName, Code: code, Scope: scope, Name: name, Description: description,
		InternalPath: internalPath, Method: method, GinPath: ginPath, ExternalPath: openAPIPath(ginPath),
		AdapterKey: adapterKey, AdapterVersion: "v1", ResourceType: resourceType, RiskLevel: risk, IdempotencyMode: idempotency,
		ProtocolVersions: versions, StandardRefs: standardRefs, RequiresConfirmation: confirmation,
		RequestSchema: requestSchema, ResponseSchema: responseSchema, QueryAllowlist: queryAllowlist,
	}
	if len(sysAPIGroup) > 0 {
		operation.SysAPIGroup = sysAPIGroup[0]
	}
	return operation
}

func openAPIPath(ginPath string) string {
	segments := strings.Split(ginPath, "/")
	for index, segment := range segments {
		if strings.HasPrefix(segment, ":") && len(segment) > 1 {
			segments[index] = "{" + strings.TrimPrefix(segment, ":") + "}"
		}
	}
	return strings.Join(segments, "/")
}

// Operations returns a deep copy so callers cannot mutate the contract used by
// other startup and request paths.
func Operations() []Operation {
	result := make([]Operation, len(operationDefinitions))
	for index, operation := range operationDefinitions {
		result[index] = operation
		result[index].ProtocolVersions = append([]string(nil), operation.ProtocolVersions...)
		result[index].StandardRefs = append([]string(nil), operation.StandardRefs...)
		result[index].QueryAllowlist = append([]string(nil), operation.QueryAllowlist...)
	}
	return result
}

// Validate rejects catalog definitions that could not be safely registered or
// published as a one-to-one scope, route, and code-owned adapter contract.
func Validate(operations []Operation) error {
	if len(operations) == 0 {
		return fmt.Errorf("OpenAPI contract registry is empty")
	}
	scopes := make(map[string]struct{}, len(operations))
	codes := make(map[string]struct{}, len(operations))
	routes := make(map[string]struct{}, len(operations))
	for index, operation := range operations {
		prefix := fmt.Sprintf("operation %d", index)
		for name, value := range map[string]string{
			"group code": operation.GroupCode, "group name": operation.GroupName, "code": operation.Code,
			"scope": operation.Scope, "name": operation.Name, "description": operation.Description, "method": operation.Method,
			"gin path": operation.GinPath, "external path": operation.ExternalPath,
			"adapter key": operation.AdapterKey, "adapter version": operation.AdapterVersion,
			"resource type": operation.ResourceType, "risk": operation.RiskLevel,
			"idempotency mode": operation.IdempotencyMode,
		} {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("%s has empty %s", prefix, name)
			}
		}
		if !validScope(operation.Scope) {
			return fmt.Errorf("%s has invalid scope %q", prefix, operation.Scope)
		}
		if operation.Method != http.MethodGet && operation.Method != http.MethodPost && operation.Method != http.MethodPut && operation.Method != http.MethodPatch && operation.Method != http.MethodDelete {
			return fmt.Errorf("%s has unsupported method %q", prefix, operation.Method)
		}
		if operation.SysAPIMethod != "" && operation.SysAPIMethod != http.MethodGet && operation.SysAPIMethod != http.MethodPost && operation.SysAPIMethod != http.MethodPut && operation.SysAPIMethod != http.MethodPatch && operation.SysAPIMethod != http.MethodDelete {
			return fmt.Errorf("%s has unsupported internal sys_api method %q", prefix, operation.SysAPIMethod)
		}
		if !json.Valid([]byte(operation.RequestSchema)) || !json.Valid([]byte(operation.ResponseSchema)) {
			return fmt.Errorf("%s has invalid request or response schema JSON", prefix)
		}
		if !strings.HasPrefix(operation.GinPath, "/openapi/v1/") || !strings.HasPrefix(operation.ExternalPath, "/openapi/v1/") {
			return fmt.Errorf("%s is outside the versioned public prefix", prefix)
		}
		if !strings.HasPrefix(operation.AdapterKey, "resource.") && !strings.HasPrefix(operation.AdapterKey, "delegated.") && !strings.HasPrefix(operation.AdapterKey, "device.") && !strings.HasPrefix(operation.AdapterKey, "devicequery.") {
			return fmt.Errorf("%s adapter key %q is not code-owned", prefix, operation.AdapterKey)
		}
		if operation.RiskLevel != RiskRead && operation.RiskLevel != RiskMedia && operation.RiskLevel != RiskControl && operation.RiskLevel != RiskDestructive {
			return fmt.Errorf("%s has unknown risk %q", prefix, operation.RiskLevel)
		}
		if operation.IdempotencyMode != IdempotencyNone && operation.IdempotencyMode != IdempotencyOptional && operation.IdempotencyMode != IdempotencyRequired {
			return fmt.Errorf("%s has unknown idempotency mode %q", prefix, operation.IdempotencyMode)
		}
		if operation.RequiresConfirmation && operation.RiskLevel != RiskDestructive {
			return fmt.Errorf("%s requires confirmation but is not destructive", prefix)
		}
		if !sameNames(pathParams(operation.GinPath, ':', 0), pathParams(operation.ExternalPath, '{', '}')) {
			return fmt.Errorf("%s Gin and OpenAPI path parameters differ", prefix)
		}
		if _, exists := scopes[operation.Scope]; exists {
			return fmt.Errorf("duplicate OpenAPI scope %q", operation.Scope)
		}
		scopes[operation.Scope] = struct{}{}
		if _, exists := codes[operation.Code]; exists {
			return fmt.Errorf("duplicate OpenAPI operation code %q", operation.Code)
		}
		codes[operation.Code] = struct{}{}
		route := operation.Method + " " + operation.GinPath
		if _, exists := routes[route]; exists {
			return fmt.Errorf("duplicate OpenAPI route %q", route)
		}
		routes[route] = struct{}{}
	}
	return nil
}

func validScope(scope string) bool {
	if len(scope) == 0 || len(scope) > 96 {
		return false
	}
	for _, r := range scope {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ':' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func pathParams(path string, open byte, closing byte) []string {
	var params []string
	for index := 0; index < len(path); index++ {
		if path[index] != open {
			continue
		}
		end := index + 1
		if closing == 0 {
			for end < len(path) && path[end] != '/' {
				end++
			}
		} else {
			for end < len(path) && path[end] != closing {
				end++
			}
		}
		if end == index+1 || (closing != 0 && end >= len(path)) {
			return nil
		}
		params = append(params, path[index+1:end])
		index = end
	}
	return params
}

func sameNames(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	sort.Strings(left)
	sort.Strings(right)
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
