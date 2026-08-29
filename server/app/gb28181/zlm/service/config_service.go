package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

var ErrManagedConfigKey = errors.New("platform-managed ZLM config key")

var ErrReadOnlyConfig = errors.New("read-only ZLM config key")

var ErrConfigReadbackMismatch = errors.New("config_readback_mismatch")

var ErrConfigReadbackFailed = errors.New("config_readback_failed")

var ErrConfigSetFailed = errors.New("config_set_failed")

// ErrConfigTargetChanged means the node identity/version changed while a hot
// update was in flight. The command may have reached the old endpoint, so the
// service must not report the requested values as applied to the current node.
var ErrConfigTargetChanged = errors.New("config_target_changed")

// ErrRestartRequiredUnsupported 平台尚未实现待重启配置的持久化与应用流程
var ErrRestartRequiredUnsupported = errors.New("ZLM config requires restart, not supported yet")

var platformManagedConfigKeys = map[string]struct{}{
	"api.secret":                 {},
	"hook.enable":                {},
	"hook.on_server_started":     {},
	"hook.on_server_keepalive":   {},
	"hook.on_stream_changed":     {},
	"hook.on_stream_none_reader": {},
	"hook.on_stream_not_found":   {},
	"hook.on_rtp_server_timeout": {},
	"hook.on_publish":            {},
	"hook.on_play":               {},
	"hook.on_record_mp4":         {},
	"hook.on_flow_report":        {},
	"hook.alive_interval":        {},
	"general.flowThreshold":      {},
	"general.mediaServerId":      {},
	"general.maxStreamWaitMS":    {},
}

// ConfigItem 单条 ZLM 配置元数据
type ConfigItem struct {
	Key             string     `json:"key"`             // ZLM 配置 key,如 http.port
	Value           string     `json:"value"`           // 当前值(GetGrouped 时填)
	Default         string     `json:"default"`         // 默认值
	Mode            ConfigMode `json:"mode"`            // 权威配置变更模式
	HotReloadable   bool       `json:"hotReloadable"`   // 是否可在线热改
	RestartRequired bool       `json:"restartRequired"` // 修改后是否要重启 ZLM
	Comment         string     `json:"comment"`         // 中文说明
}

// ConfigMode is the authoritative UI/service policy. The legacy boolean
// fields remain for old clients and are derived-compatible with this mode.
type ConfigMode string

const (
	ConfigModeReadOnly                   ConfigMode = "read_only"
	ConfigModeHotReload                  ConfigMode = "hot_reload"
	ConfigModePlatformManaged            ConfigMode = "platform_managed"
	ConfigModeRestartRequiredUnsupported ConfigMode = "restart_required_unsupported"
)

// ConfigGroup 产品化分组
type ConfigGroup struct {
	Name  string       `json:"name"`
	Items []ConfigItem `json:"items"`
}

// configCatalog 产品化分组 + 元数据(基于 ZLM 760 行 config.ini 调研,按业务相关性聚拢)
// 不覆盖全部 ZLM 配置项,只列 M1 阶段需要 UI 调的子集,后续按需扩展。
var configCatalog = []ConfigGroup{
	{
		Name: "网络端口",
		Items: []ConfigItem{
			{Key: "http.port", Default: "80", HotReloadable: false, RestartRequired: true, Comment: "ZLM HTTP API 端口"},
			{Key: "http.sslport", Default: "443", HotReloadable: false, RestartRequired: true, Comment: "HTTPS 端口"},
			{Key: "rtmp.port", Default: "1935", HotReloadable: false, RestartRequired: true, Comment: "RTMP 推流端口"},
			{Key: "rtmp.sslport", Default: "0", HotReloadable: false, RestartRequired: true, Comment: "RTMPS 端口,0 表示禁用"},
			{Key: "rtsp.port", Default: "554", HotReloadable: false, RestartRequired: true, Comment: "RTSP 端口"},
			{Key: "rtsp.sslport", Default: "0", HotReloadable: false, RestartRequired: true, Comment: "RTSPS 端口,0 表示禁用"},
			{Key: "rtp_proxy.port_range", Default: "30000-35000", HotReloadable: false, RestartRequired: true, Comment: "RTP 多端口收流范围"},
			{Key: "shell.port", Default: "9000", HotReloadable: false, RestartRequired: true, Comment: "telnet 调试端口"},
		},
	},
	{
		Name: "Hook 回调",
		Items: []ConfigItem{
			{Key: "hook.enable", Default: "0", HotReloadable: true, Comment: "Hook 总开关"},
			{Key: "hook.timeoutSec", Default: "10", HotReloadable: true, Comment: "Hook 超时秒数"},
			{Key: "hook.on_server_started", Default: "", HotReloadable: true, Comment: "ZLM 启动回调"},
			{Key: "hook.on_server_keepalive", Default: "", HotReloadable: true, Comment: "心跳回调,M2 多节点收集状态用"},
			{Key: "hook.on_stream_changed", Default: "", HotReloadable: true, Comment: "流注册/注销回调"},
			{Key: "hook.on_stream_none_reader", Default: "", HotReloadable: true, Comment: "无人观看回调"},
			{Key: "hook.on_stream_not_found", Default: "", HotReloadable: true, Comment: "播放未找到流回调"},
			{Key: "hook.on_rtp_server_timeout", Default: "", HotReloadable: true, Comment: "RTP 收流超时回调"},
			{Key: "hook.on_publish", Default: "", HotReloadable: true, Comment: "推流鉴权"},
			{Key: "hook.on_play", Default: "", HotReloadable: true, Comment: "播放鉴权"},
			{Key: "hook.on_record_mp4", Default: "", HotReloadable: true, Comment: "MP4 录制完成回调"},
			{Key: "hook.on_flow_report", Default: "", HotReloadable: true, Comment: "流量上报(计费用)"},
			{Key: "hook.alive_interval", Default: "30.0", HotReloadable: true, Comment: "心跳上报周期(秒)"},
		},
	},
	{
		Name: "协议开关",
		Items: []ConfigItem{
			{Key: "protocol.enable_rtsp", Default: "1", HotReloadable: true, Comment: "是否生成 RTSP 流"},
			{Key: "protocol.enable_rtmp", Default: "1", HotReloadable: true, Comment: "是否生成 RTMP 流"},
			{Key: "protocol.enable_hls", Default: "1", HotReloadable: true, Comment: "是否生成 HLS 流"},
			{Key: "protocol.enable_ts", Default: "1", HotReloadable: true, Comment: "是否生成 TS 流"},
			{Key: "protocol.enable_fmp4", Default: "1", HotReloadable: true, Comment: "是否生成 fMP4 流"},
			{Key: "protocol.enable_mp4", Default: "0", HotReloadable: true, Comment: "是否自动录制 MP4"},
			{Key: "protocol.enable_audio", Default: "1", HotReloadable: true, Comment: "是否启用音频"},
			{Key: "protocol.add_mute_audio", Default: "1", HotReloadable: true, Comment: "无音频时自动添加静音帧"},
		},
	},
	{
		Name: "运行时策略",
		Items: []ConfigItem{
			{Key: "general.streamNoneReaderDelayMS", Default: "20000", HotReloadable: true, Comment: "无人观看自动断流延迟(毫秒)"},
			{Key: "general.mediaServerId", Default: "", HotReloadable: true, Comment: "节点 UUID,业务侧生成"},
			{Key: "general.maxStreamWaitMS", Default: "15000", HotReloadable: true, Comment: "流就绪最大等待(毫秒)"},
			{Key: "general.publishToHls", Default: "1", HotReloadable: true, Comment: "推流时同步生成 HLS"},
			{Key: "general.publishToMP4", Default: "0", HotReloadable: true, Comment: "推流时自动录制 MP4"},
			{Key: "general.mergeWriteMS", Default: "300", HotReloadable: true, Comment: "合并写延迟(毫秒,降 CPU)"},
			{Key: "general.resetWhenRePlay", Default: "1", HotReloadable: true, Comment: "重新播放时重置 GOP 缓存"},
		},
	},
	{
		Name: "GB28181 国标",
		Items: []ConfigItem{
			{Key: "rtp_proxy.checkSource", Default: "1", HotReloadable: true, Comment: "是否校验 SSRC"},
			{Key: "rtp_proxy.timeoutSec", Default: "15", HotReloadable: true, Comment: "RTP 收流超时(秒)"},
			{Key: "rtp_proxy.h264_pt", Default: "98", HotReloadable: true, Comment: "H264 PayloadType"},
			{Key: "rtp_proxy.h265_pt", Default: "99", HotReloadable: true, Comment: "H265 PayloadType"},
			{Key: "rtp_proxy.ps_pt", Default: "96", HotReloadable: true, Comment: "PS PayloadType"},
			{Key: "rtp_proxy.dumpDir", Default: "", HotReloadable: true, Comment: "RTP 抓包目录(调试用)"},
		},
	},
	{
		Name: "录制与截图",
		Items: []ConfigItem{
			{Key: "record.appName", Default: "record", HotReloadable: true, Comment: "录像 app 名"},
			{Key: "record.filePath", Default: "./www/record", HotReloadable: true, Comment: "录像文件路径"},
			{Key: "record.fileSecond", Default: "3600", HotReloadable: true, Comment: "单录像文件秒数(切片)"},
			{Key: "record.sampleMS", Default: "500", HotReloadable: true, Comment: "录像采样间隔(毫秒)"},
			{Key: "api.snap_root", Default: "./www/snap/", HotReloadable: true, Comment: "截图保存路径"},
		},
	},
	{
		Name: "性能调优",
		Items: []ConfigItem{
			{Key: "general.flowThreshold", Default: "1024", HotReloadable: true, Comment: "流量上报阈值(KB)"},
			{Key: "rtp.audioMtuSize", Default: "600", HotReloadable: true, Comment: "音频 RTP MTU"},
			{Key: "rtp.videoMtuSize", Default: "1400", HotReloadable: true, Comment: "视频 RTP MTU"},
			{Key: "rtsp.handshakeSecond", Default: "15", HotReloadable: true, Comment: "RTSP 握手超时(秒)"},
			{Key: "rtmp.handshakeSecond", Default: "15", HotReloadable: true, Comment: "RTMP 握手超时(秒)"},
			{Key: "hls.fileBufSize", Default: "65536", HotReloadable: true, Comment: "HLS 缓冲区(字节)"},
		},
	},
	{
		Name: "安全",
		Items: []ConfigItem{
			{Key: "api.version", Default: "", HotReloadable: false, RestartRequired: false, Comment: "ZLM 版本信息,只读"},
			{Key: "api.secret", Default: "", HotReloadable: true, Comment: "API 鉴权 secret"},
			{Key: "api.apiDebug", Default: "1", HotReloadable: true, Comment: "API 调试模式(生产应关)"},
			{Key: "general.check_nvr_status", Default: "0", HotReloadable: true, Comment: "NVR 心跳检查"},
		},
	},
}

// catalogIndex 快速查 key → item 的索引
var catalogIndex = func() map[string]ConfigItem {
	m := map[string]ConfigItem{}
	for _, g := range configCatalog {
		for _, it := range g.Items {
			m[it.Key] = it
		}
	}
	return m
}()

func configMode(item ConfigItem) ConfigMode {
	// platformManaged is intentionally authoritative even for legacy catalog
	// entries that still advertise hotReloadable=true to old clients.
	if _, managed := platformManagedConfigKeys[item.Key]; managed {
		return ConfigModePlatformManaged
	}
	if item.Mode != "" {
		return item.Mode
	}
	switch {
	case item.HotReloadable && !item.RestartRequired:
		return ConfigModeHotReload
	case !item.HotReloadable && item.RestartRequired:
		return ConfigModeRestartRequiredUnsupported
	case !item.HotReloadable && !item.RestartRequired:
		return ConfigModeReadOnly
	default:
		// Mixed legacy booleans are not a valid public mode. Treating them as
		// restart-required is fail-closed and avoids an accidental Set.
		return ConfigModeRestartRequiredUnsupported
	}
}

// UpdateConfigReq 更新请求
type UpdateConfigReq struct {
	Changes map[string]string `json:"changes" binding:"required"`
}

// UpdateConfigResp 更新结果
type UpdateConfigResp struct {
	Applied         []string `json:"applied"`         // 已生效(热改成功)
	RequiresRestart []string `json:"requiresRestart"` // 需要重启 ZLM 才生效
	Unknown         []string `json:"unknown"`         // 未在 catalog 中的 key(原样下发)
}

// ConfigReadbackError contains only redacted actual values. It is returned
// when ZLM accepted a hot-reload command but the effective state cannot be
// proven by an immediate getServerConfig call.
type ConfigReadbackError struct {
	Code   error
	Actual map[string]string
	Cause  error
}

type configTargetSnapshot struct {
	id              int64
	revision        uint64
	host            string
	apiPort         int
	apiSecret       string
	mediaServerUUID string
	updatedAt       time.Time
}

func snapshotConfigTarget(n *node.Node) configTargetSnapshot {
	if n == nil {
		return configTargetSnapshot{}
	}
	return configTargetSnapshot{
		id: n.ID, revision: n.Revision, host: n.Host, apiPort: n.APIPort, apiSecret: n.APISecret,
		mediaServerUUID: n.MediaServerUUID, updatedAt: n.UpdatedAt,
	}
}

func (s *ConfigService) ensureConfigTargetUnchanged(expected configTargetSnapshot) error {
	current, ok := s.registry.Get(expected.id)
	if !ok || current == nil {
		return ErrConfigTargetChanged
	}
	actual := snapshotConfigTarget(current)
	if actual.id != expected.id || actual.revision != expected.revision ||
		actual.host != expected.host || actual.apiPort != expected.apiPort ||
		actual.apiSecret != expected.apiSecret || actual.mediaServerUUID != expected.mediaServerUUID ||
		!actual.updatedAt.Equal(expected.updatedAt) {
		return ErrConfigTargetChanged
	}
	return nil
}

func (e *ConfigReadbackError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("%v: %v actual=%v", e.Code, e.Cause, e.Actual)
	}
	return fmt.Sprintf("%v actual=%v", e.Code, e.Actual)
}

func (e *ConfigReadbackError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Code
}

// TestConnectionResult 探测结果
type TestConnectionResult struct {
	Online   bool   `json:"online"`
	HTTPPort string `json:"httpPort,omitempty"`
	Error    string `json:"error,omitempty"`
}

// ZLMConfigClient ZLM 配置面接口(GetServerConfig + SetServerConfig)
type ZLMConfigClient interface {
	GetServerConfig(ctx context.Context, n *node.Node) (map[string]string, error)
	SetServerConfig(ctx context.Context, n *node.Node, params map[string]string) error
}

// ConfigService 节点配置查询 + 下发
type ConfigService struct {
	registry *node.Registry
	client   ZLMConfigClient
}

// NewConfigService 构造
func NewConfigService(reg *node.Registry, cli ZLMConfigClient) *ConfigService {
	return &ConfigService{registry: reg, client: cli}
}

// GetGrouped 取节点当前 ZLM 配置,按产品化分组返回(含 hot_reloadable 元数据)
func (s *ConfigService) GetGrouped(ctx context.Context, nodeID int64) ([]ConfigGroup, error) {
	n, ok := s.registry.Get(nodeID)
	if !ok {
		return nil, ErrNodeNotFound
	}
	current, err := s.client.GetServerConfig(ctx, n)
	if err != nil {
		return nil, redactConfigError(err, n)
	}
	out := make([]ConfigGroup, 0, len(configCatalog))
	for _, g := range configCatalog {
		items := make([]ConfigItem, 0, len(g.Items))
		for _, it := range g.Items {
			it.Mode = configMode(it)
			if v, ok := current[it.Key]; ok {
				it.Value = visibleConfigValue(it.Key, v)
			} else {
				it.Value = it.Default
			}
			items = append(items, it)
		}
		out = append(out, ConfigGroup{Name: g.Name, Items: items})
	}
	return out, nil
}

// Update 下发配置变更,自动按 hot_reloadable 分流
func (s *ConfigService) Update(ctx context.Context, nodeID int64, req UpdateConfigReq) (*UpdateConfigResp, error) {
	n, ok := s.registry.Get(nodeID)
	if !ok {
		return nil, ErrNodeNotFound
	}
	resp := &UpdateConfigResp{
		Applied:         []string{},
		RequiresRestart: []string{},
		Unknown:         []string{},
	}
	hotParams := map[string]string{}
	keys := make([]string, 0, len(req.Changes))
	for k := range req.Changes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var managedKey, readOnlyKey, restartKey string
	for _, k := range keys {
		v := req.Changes[k]
		if _, managed := platformManagedConfigKeys[k]; managed {
			if managedKey == "" {
				managedKey = k
			}
			continue
		}
		meta, known := catalogIndex[k]
		if !known {
			// 未知 key 不再透传:透传属于无调用方的投机性灵活设计,
			// 且绕过 catalog 的校验与可维护性边界
			resp.Unknown = append(resp.Unknown, k)
			continue
		}
		switch configMode(meta) {
		case ConfigModeHotReload:
			hotParams[k] = v
		case ConfigModeReadOnly:
			if readOnlyKey == "" {
				readOnlyKey = k
			}
		case ConfigModeRestartRequiredUnsupported:
			// 平台尚未实现"重启后应用 desired state"的持久化流程,
			// 接受这类配置会谎报成功(前端提示需重启,但重启后值并不存在)
			if restartKey == "" {
				restartKey = k
			}
		case ConfigModePlatformManaged:
			if managedKey == "" {
				managedKey = k
			}
		}
	}
	// Validate all keys before issuing any command. The order is part of the
	// API contract and must not depend on map iteration or key spelling.
	if managedKey != "" {
		return nil, fmt.Errorf("%w: %s", ErrManagedConfigKey, managedKey)
	}
	if readOnlyKey != "" {
		return nil, fmt.Errorf("%w: %s", ErrReadOnlyConfig, readOnlyKey)
	}
	if restartKey != "" {
		return nil, fmt.Errorf("%w: %s", ErrRestartRequiredUnsupported, restartKey)
	}
	if len(hotParams) > 0 {
		target := snapshotConfigTarget(n)
		if err := s.client.SetServerConfig(ctx, n, hotParams); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrConfigSetFailed, redactConfigError(err, n, hotParams))
		}
		if err := s.ensureConfigTargetUnchanged(target); err != nil {
			return nil, err
		}
		actual, err := s.client.GetServerConfig(ctx, n)
		if err != nil {
			return nil, &ConfigReadbackError{
				Code:   ErrConfigReadbackFailed,
				Actual: visibleConfigValues(actual, hotParams),
				Cause:  redactConfigError(err, n, hotParams),
			}
		}
		if err := s.ensureConfigTargetUnchanged(target); err != nil {
			return nil, err
		}
		actualVisible := visibleConfigValues(actual, hotParams)
		mismatch := false
		for key, expected := range hotParams {
			if actual[key] != expected {
				mismatch = true
				break
			}
		}
		if mismatch {
			return nil, &ConfigReadbackError{
				Code:   ErrConfigReadbackMismatch,
				Actual: actualVisible,
			}
		}
		for key := range hotParams {
			resp.Applied = append(resp.Applied, key)
		}
		sort.Strings(resp.Applied)
	}
	return resp, nil
}

func visibleConfigValues(actual, keys map[string]string) map[string]string {
	result := make(map[string]string, len(keys))
	for key := range keys {
		result[key] = visibleConfigValue(key, actual[key])
	}
	return result
}

func redactConfigError(err error, n *node.Node, values ...map[string]string) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	if n != nil && n.APISecret != "" {
		message = strings.ReplaceAll(message, n.APISecret, "***")
	}
	for _, set := range values {
		for _, value := range set {
			if value != "" {
				message = strings.ReplaceAll(message, value, "***")
			}
		}
	}
	return redactedConfigError{err: err, message: message}
}

type redactedConfigError struct {
	err     error
	message string
}

func (e redactedConfigError) Error() string { return e.message }
func (e redactedConfigError) Unwrap() error { return e.err }

func visibleConfigValue(key, value string) string {
	if key == "api.secret" {
		return ""
	}
	if !strings.HasPrefix(key, "hook.") || value == "" {
		return value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// TestConnection 探测 ZLM 是否可达
// 总是返回 nil error(把"不可达"也算正常响应),失败信息塞 Result.Error
func (s *ConfigService) TestConnection(ctx context.Context, nodeID int64) (*TestConnectionResult, error) {
	n, ok := s.registry.Get(nodeID)
	if !ok {
		return nil, ErrNodeNotFound
	}
	conf, err := s.client.GetServerConfig(ctx, n)
	if err != nil {
		return &TestConnectionResult{Online: false, Error: redactConfigError(err, n).Error()}, nil
	}
	return &TestConnectionResult{Online: true, HTTPPort: conf["http.port"]}, nil
}
