package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

// portRangeConfigKey 多端口收流范围在 ZLM 配置里的键。
//
// 它是**唯一真源**:节点登记值(meta_node.rtp_port_start/end)只是镜像,解析规则见
// node.ParseRTPPortRange。下发成功后必须把镜像对齐(见 reconcileRTPPortRange)。
const portRangeConfigKey = "rtp_proxy.port_range"

var ErrManagedConfigKey = errors.New("platform-managed ZLM config key")

var ErrReadOnlyConfig = errors.New("read-only ZLM config key")

var ErrConfigReadbackMismatch = errors.New("config_readback_mismatch")

var ErrConfigReadbackFailed = errors.New("config_readback_failed")

var ErrConfigSetFailed = errors.New("config_set_failed")

var ErrInvalidConfigValue = errors.New("invalid_config_value")

// ErrConfigTargetChanged means the node identity/version changed while a hot
// update was in flight. The command may have reached the old endpoint, so the
// service must not report the requested values as applied to the current node.
var ErrConfigTargetChanged = errors.New("config_target_changed")

// ErrRestartRequiredUnsupported 平台尚未实现待重启配置的持久化与应用流程
var ErrRestartRequiredUnsupported = errors.New("ZLM config requires restart, not supported yet")

var platformManagedConfigKeys = map[string]struct{}{
	"api.secret":            {},
	"general.flowThreshold": {},
	"general.mediaServerId": {},
}

// ConfigItem 单条 ZLM 配置元数据
type ConfigItem struct {
	Key             string     `json:"key"`                // ZLM 配置 key,如 http.port
	Value           string     `json:"value"`              // 当前值(GetGrouped 时填)
	Default         string     `json:"default"`            // 默认值
	Mode            ConfigMode `json:"mode"`               // 权威配置变更模式
	HotReloadable   bool       `json:"hotReloadable"`      // 是否可在线热改
	RestartRequired bool       `json:"restartRequired"`    // 修改后是否要重启 ZLM
	Comment         string     `json:"comment"`            // 中文说明
	DictCode        string     `json:"dictCode,omitempty"` // 可选系统字典编码,用于产品化编辑控件
}

// ConfigMode is the authoritative UI/service policy. The legacy boolean
// fields remain for old clients and are derived-compatible with this mode.
type ConfigMode string

const (
	ConfigModeReadOnly                   ConfigMode = "read_only"
	ConfigModeHotReload                  ConfigMode = "hot_reload"
	ConfigModeRestartRequired            ConfigMode = "restart_required"
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
			{Key: "http.port", Default: "80", Mode: ConfigModeRestartRequired, RestartRequired: true, Comment: "ZLM HTTP API 端口；修改后还需同步节点登记的 API 端口"},
			{Key: "http.sslport", Default: "443", Mode: ConfigModeRestartRequired, RestartRequired: true, Comment: "HTTPS 端口，0 表示禁用"},
			{Key: "rtmp.port", Default: "1935", Mode: ConfigModeRestartRequired, RestartRequired: true, Comment: "RTMP 推流端口，0 表示禁用"},
			{Key: "rtmp.sslport", Default: "0", Mode: ConfigModeRestartRequired, RestartRequired: true, Comment: "RTMPS 端口，0 表示禁用"},
			{Key: "rtsp.port", Default: "554", Mode: ConfigModeRestartRequired, RestartRequired: true, Comment: "RTSP 端口，0 表示禁用"},
			{Key: "rtsp.sslport", Default: "0", Mode: ConfigModeRestartRequired, RestartRequired: true, Comment: "RTSPS 端口，0 表示禁用"},
			{Key: "rtp_proxy.port", Default: "10000", Mode: ConfigModeRestartRequired, RestartRequired: true, Comment: "RTP 单端口监听，0 表示禁用；修改后需重启节点并同步节点登记的单端口端口号"},
			{Key: portRangeConfigKey, Default: "30000-35000", Mode: ConfigModeRestartRequired, RestartRequired: true, Comment: "RTP 多端口收流范围；改完平台会自动同步节点登记的收流范围，需重启节点生效"},
			{Key: "shell.port", Default: "9000", Mode: ConfigModeRestartRequired, RestartRequired: true, Comment: "telnet 调试端口，0 表示禁用"},
		},
	},
	{
		Name: "Hook 回调",
		Items: []ConfigItem{
			{Key: "hook.enable", Default: "0", HotReloadable: true, Comment: "Hook 总开关", DictCode: "status"},
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
			{Key: "hook.alive_interval", Default: "30.0", Mode: ConfigModeRestartRequired, RestartRequired: true, Comment: "心跳上报周期(秒)，保存后重启媒体节点生效；周期过长可能导致离线误判"},
		},
	},
	{
		Name: "协议开关",
		Items: []ConfigItem{
			{Key: "protocol.enable_rtsp", Default: "1", HotReloadable: true, Comment: "是否生成 RTSP 流", DictCode: "status"},
			{Key: "protocol.enable_rtmp", Default: "1", HotReloadable: true, Comment: "是否生成 RTMP 流", DictCode: "status"},
			{Key: "protocol.enable_hls", Default: "1", HotReloadable: true, Comment: "是否生成 HLS 流", DictCode: "status"},
			{Key: "protocol.enable_ts", Default: "1", HotReloadable: true, Comment: "是否生成 TS 流", DictCode: "status"},
			{Key: "protocol.enable_fmp4", Default: "1", HotReloadable: true, Comment: "是否生成 fMP4 流", DictCode: "status"},
			{Key: "protocol.enable_mp4", Default: "0", HotReloadable: true, Comment: "是否开启 MP4 录制", DictCode: "status"},
			{Key: "protocol.enable_audio", Default: "1", HotReloadable: true, Comment: "是否启用音频", DictCode: "status"},
			{Key: "protocol.add_mute_audio", Default: "1", HotReloadable: true, Comment: "无音频时自动添加静音帧", DictCode: "status"},
		},
	},
	{
		// WebRTC 独立成组:它和 general.* 的「运行时策略」不是同一类东西 ——
		// 前者是浏览器播放/对讲那条链路的**对外可达性**(地址 + 端口),后者是
		// ZLM 的内部调度参数。而且 rtc.externIP 是跨网段部署唯一必须人工填的
		// 开关:它同时充当 STUN 下发门禁(talk/ice.go 只在它非空时才给浏览器发
		// iceServers),混在 general.* 里既难找、又容易被当成「没人看的默认值」。
		//
		// 两个地址项都留空是合法状态(都表示"用本机地址"),但只有 externIP
		// 能热改:interfaces 是 ICE 绑定网卡,改了必须重启才重新 bind。
		//
		// 四个端口项可改(restart_required),但它们必须**成对同号**:
		//   port ↔ tcpPort(UDP/TCP 各占一个协议栈)
		//   icePort ↔ iceTcpPort
		// 只改一半就是 2026-10-12 那个事故的复刻(tcpPort 留在 8000 被占 ⇒ ZLM 启动
		// 即退出)。补齐由 pairWebRTCPorts 做,不指望人记得。
		//
		// ⛔ 改了必须持久化到部署端口规划(config/config.env):ctl.sh 每次启动按那里
		// 重写 ini,不写回去 = 下次启动被改回,而且用户以为改成功了。
		//
		// port_range 仍然只读:它是 TURN 中继端口池,而 TURN 被强制关闭
		// (ctl.sh 每次启动写 enableTurn=0),现在填了不生效;它的默认值又落在
		// Linux 临时端口区,乱改更容易撞端口。
		Name: "WebRTC",
		Items: []ConfigItem{
			{Key: "rtc.externIP", Default: "", HotReloadable: true, Comment: "WebRTC 对外地址；公网/NAT 部署填写客户端可达的公网 IP，多个地址用逗号分隔；留空使用本机地址。保存后新建会话生效，已有会话需重新连接"},
			{Key: "rtc.interfaces", Default: "", Mode: ConfigModeRestartRequired, RestartRequired: true, Comment: "ICE 绑定的网卡地址；多网卡或弹性公网 IP（机器本身 bind 不到公网 IP）时填本机内网 IP，多个用逗号分隔；留空自动选择。保存后需重启媒体节点生效"},
			// 端口项的 Default 留空是刻意的:实际端口由部署端口规划决定(绿色包 510xx,
			// 与上游出厂 8000/3478 并不相同),这里填一个"默认端口"会在 ZLM 没返回
			// 该键时把假数字当成事实显示出来 —— 宁可显示空。
			{Key: "rtc.port", Default: "", Mode: ConfigModeRestartRequired, RestartRequired: true, Comment: "WebRTC 媒体端口(UDP)；与 tcpPort 同号，保存后写回部署端口规划，重启媒体节点生效。还需防火墙放行"},
			{Key: "rtc.tcpPort", Default: "", Mode: ConfigModeRestartRequired, RestartRequired: true, Comment: "WebRTC over TCP 回退端口；必须与 port 同号（UDP/TCP 各占一个协议栈），保存时会自动对齐"},
			{Key: "rtc.icePort", Default: "", Mode: ConfigModeRestartRequired, RestartRequired: true, Comment: "内置 STUN 监听端口(UDP)；与 iceTcpPort 同号，保存后写回部署端口规划，重启媒体节点生效。跨网段用 WebRTC 时防火墙需放行"},
			{Key: "rtc.iceTcpPort", Default: "", Mode: ConfigModeRestartRequired, RestartRequired: true, Comment: "内置 STUN/TURN 的 TCP 监听端口；必须与 icePort 同号，保存时会自动对齐"},
			{Key: "rtc.port_range", Default: "", Mode: ConfigModeReadOnly, Comment: "TURN 中继端口池(UDP)；TURN 已由部署脚本强制关闭，此键当前不生效，故只读"},
		},
	},
	{
		Name: "运行时策略",
		Items: []ConfigItem{
			{Key: "general.streamNoneReaderDelayMS", Default: "20000", HotReloadable: true, Comment: "无人观看自动断流延迟(毫秒)"},
			{Key: "general.mediaServerId", Default: "", HotReloadable: true, Comment: "节点 UUID,业务侧生成"},
			{Key: "general.maxStreamWaitMS", Default: "15000", HotReloadable: true, Comment: "流就绪最大等待(毫秒)"},
			{Key: "general.publishToHls", Default: "1", HotReloadable: true, Comment: "推流时同步生成 HLS", DictCode: "status"},
			{Key: "general.publishToMP4", Default: "0", HotReloadable: true, Comment: "推流时自动录制 MP4", DictCode: "status"},
			{Key: "general.mergeWriteMS", Default: "300", HotReloadable: true, Comment: "合并写延迟(毫秒,降 CPU)"},
			{Key: "general.resetWhenRePlay", Default: "1", HotReloadable: true, Comment: "重新播放时重置 GOP 缓存", DictCode: "status"},
		},
	},
	{
		Name: "GB28181 国标",
		Items: []ConfigItem{
			{Key: "rtp_proxy.checkSource", Default: "1", HotReloadable: true, Comment: "是否校验 SSRC", DictCode: "status"},
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
			{Key: "api.apiDebug", Default: "1", HotReloadable: true, Comment: "API 调试模式(生产应关)", DictCode: "status"},
			{Key: "general.check_nvr_status", Default: "0", HotReloadable: true, Comment: "NVR 心跳检查", DictCode: "status"},
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
	// Hints 是保存后**还需要人做的事**（目前只有改端口后的防火墙放行）。
	// 单独一列而不是塞进 RequiresRestart：后者是"配置项状态"，这里是"操作清单"，
	// 混在一起前端只能靠猜文案。
	Hints []string `json:"hints,omitempty"`
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
	logger   *zap.Logger
	// portPlan 是「页面改端口」的持久化出口（部署端口规划 config.env）。
	// nil 表示当前部署没有这个文件（容器编排由外部注入端口）⇒ 端口改动必须
	// fail-closed 拒绝，见 Update 里的 ErrPortPlanUnavailable。
	portPlan *PortPlan
}

// NewConfigService 构造
func NewConfigService(reg *node.Registry, cli ZLMConfigClient) *ConfigService {
	return &ConfigService{registry: reg, client: cli, logger: zap.NewNop()}
}

// SetPortPlan 装配端口规划写回器（nil 安全：不装配 = 不支持页面改端口）。
func (s *ConfigService) SetPortPlan(plan *PortPlan) {
	s.portPlan = plan
}

// SetLogger 装配日志(nil 安全)。未装配时静默,便于测试直接构造。
func (s *ConfigService) SetLogger(logger *zap.Logger) {
	if logger != nil {
		s.logger = logger.Named("zlm.config")
	}
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
		case ConfigModeHotReload, ConfigModeRestartRequired:
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
	for _, key := range keys {
		if value, ok := hotParams[key]; ok {
			if err := validateConfigValue(key, value, n.MediaServerUUID); err != nil {
				return nil, err
			}
		}
	}
	// WebRTC 端口成对同号：只改 UDP 不改 TCP 就是 2026-10-12 那个事故
	// （tcpPort 留在 8000 被占 ⇒ ZLM 启动即退出）。补齐发生在这里而不是前端，
	// 因为接口可以被直接调用。
	rtcPort, icePort, err := pairWebRTCPorts(hotParams)
	if err != nil {
		return nil, err
	}
	needsPortPlan := rtcPort > 0 || icePort > 0
	if needsPortPlan && s.portPlan == nil {
		// 先拒后发：没有持久化出口时下发等于假改（ZLM 接受了，下次启动被改回）。
		return nil, fmt.Errorf("%w: 当前部署不支持在页面修改 WebRTC 端口，请直接改部署端口规划", ErrPortPlanUnavailable)
	}
	if needsPortPlan {
		// 撞车必须在下发之前发现：端口规划是重启时生效的那份，撞了不会在保存时炸，
		// 而是下次启动 ZLM 直接退出（`address already in use`），用户却以为保存成功。
		conflict, checkErr := s.portPlan.FindConflict(rtcPort, icePort)
		if checkErr != nil {
			return nil, checkErr
		}
		if conflict != nil {
			return nil, fmt.Errorf("%w: 端口 %d 已被部署端口规划里的 %s=%s 占用，请换一个端口",
				ErrInvalidConfigValue, conflict.Port, conflict.Key, conflict.Value)
		}
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
			if configMode(catalogIndex[key]) == ConfigModeRestartRequired {
				resp.RequiresRestart = append(resp.RequiresRestart, key)
			} else {
				resp.Applied = append(resp.Applied, key)
			}
		}
		if _, ok := hotParams[portRangeConfigKey]; ok {
			s.reconcileRTPPortRange(ctx, n, actual)
		}
		sort.Strings(resp.Applied)
		sort.Strings(resp.RequiresRestart)
		if needsPortPlan {
			// 回读成功之后才落盘：先证明 ZLM 接受，再让它成为下次启动的规划。
			if persistErr := s.portPlan.PersistWebRTCPorts(rtcPort, icePort); persistErr != nil {
				// ZLM 已改、规划没落盘 ⇒ 重启会退回旧端口。报出来而不是当成功，
				// 否则用户会在下次重启后遇到「端口自己变回去了」且毫无线索。
				return nil, fmt.Errorf("%w: %v", ErrPortPlanPersistFailed, persistErr)
			}
			resp.Hints = append(resp.Hints, webrtcPortHints(rtcPort, icePort)...)
		}
	}
	return resp, nil
}

// pairWebRTCPorts 让成对端口一起变，返回需要写回部署端口规划的 (媒体端口, STUN 端口)。
//
// 传 0 表示该项没改。前端也会做同样的对齐，但接口可以被直接调用（curl / 其他
// 客户端），所以约束必须在服务端成立。
func pairWebRTCPorts(changes map[string]string) (rtcPort, icePort int, err error) {
	pairs := [][2]string{
		{"rtc.port", "rtc.tcpPort"},       // 同一个媒体端口，UDP 与 TCP 各占一个协议栈
		{"rtc.icePort", "rtc.iceTcpPort"}, // STUN/TURN 的 UDP 与 TCP
	}
	for _, pair := range pairs {
		left, leftOK := changes[pair[0]]
		right, rightOK := changes[pair[1]]
		switch {
		case leftOK && rightOK && left != right:
			return 0, 0, fmt.Errorf("%w: %s 与 %s 必须同号（UDP/TCP 各占一个协议栈，不同号会让其中一路监听留在旧端口）",
				ErrInvalidConfigValue, pair[0], pair[1])
		case leftOK && !rightOK:
			changes[pair[1]] = left
		case !leftOK && rightOK:
			changes[pair[0]] = right
		}
	}
	parse := func(key string) (int, error) {
		value, ok := changes[key]
		if !ok {
			return 0, nil
		}
		port, convErr := strconv.Atoi(strings.TrimSpace(value))
		if convErr != nil {
			return 0, fmt.Errorf("%w: %s 必须是整数端口", ErrInvalidConfigValue, key)
		}
		return port, nil
	}
	if rtcPort, err = parse("rtc.port"); err != nil {
		return 0, 0, err
	}
	if icePort, err = parse("rtc.icePort"); err != nil {
		return 0, 0, err
	}
	if rtcPort > 0 && icePort > 0 && rtcPort == icePort {
		// 两个都是 UDP 监听，同 IP 同端口必然抢不到 bind。
		// （只有一侧被改时由 FindConflict 拿端口规划里的另一侧值判，见 PortPlan.FindConflict）
		return 0, 0, fmt.Errorf("%w: 媒体端口与 STUN 端口不能同号（%d）", ErrInvalidConfigValue, rtcPort)
	}
	return rtcPort, icePort, nil
}

// webrtcPortHints 给出改端口后**还需要人做**的事。
//
// 端口改了防火墙没放行 = 浏览器能拿到信令但拿不到画面，症状与"节点没起来"极像，
// 所以这条提示必须具体到端口号和协议，而不是一句"请检查防火墙"。
func webrtcPortHints(rtcPort, icePort int) []string {
	hints := make([]string, 0, 2)
	if rtcPort > 0 {
		hints = append(hints, fmt.Sprintf(
			"WebRTC 媒体端口已改为 %d（UDP/TCP 同号）：重启媒体节点后生效，并请放行 %d/udp 与 %d/tcp，否则浏览器拿不到画面。",
			rtcPort, rtcPort, rtcPort))
	}
	if icePort > 0 {
		hints = append(hints, fmt.Sprintf(
			"STUN 端口已改为 %d：重启媒体节点后生效，跨网段/公网播放请放行 %d/udp 与 %d/tcp。",
			icePort, icePort, icePort))
	}
	return hints
}

// reconcileRTPPortRange 把 ZLM 回读到的收流范围写回节点登记值(镜像对齐真源)。
//
// 为什么放在下发流程里:范围的真源是 ZLM 的 `[rtp_proxy] port_range`,而平台登记的
// rtp_port_start/end 只用于算端口容量(capacity.go 的 PortUsage)。不改的话,登记值
// 与实际段不一致时会一直按错的范围判"是否接近满" —— 登记的段更大时调度永不判满,
// 直到 ZLM 端口真的耗光、推流开始失败。
//
// 失败只记 warn、不把整个请求判失败:ZLM 侧已经改成功,报错只会诱导用户重复下发。
func (s *ConfigService) reconcileRTPPortRange(ctx context.Context, n *node.Node, actual map[string]string) {
	start, end, ok := node.ParseRTPPortRange(actual[portRangeConfigKey])
	if !ok {
		return
	}
	changed, err := s.registry.ReconcileRTPPortRange(ctx, n.ID, start, end)
	switch {
	case err != nil:
		s.logger.Warn("GB28181 ZLM 收流范围对账失败",
			zap.String("event", "zlm.node.rtp_range_reconcile_failed"),
			zap.Int64("node_id", n.ID),
			zap.String("reason", err.Error()))
	case changed:
		s.logger.Info("GB28181 ZLM 收流范围已同步到节点登记值",
			zap.String("event", "zlm.node.rtp_range_reconciled"),
			zap.Int64("node_id", n.ID),
			zap.Int("rtp_port_start", start),
			zap.Int("rtp_port_end", end),
			zap.String("source", "config_apply"))
	}
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

func validateConfigValue(key, value, mediaServerUUID string) error {
	if err := validateNetworkPortConfig(key, value); err != nil {
		return err
	}
	if key == "hook.alive_interval" {
		seconds, err := strconv.ParseFloat(value, 32)
		if err != nil || seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
			return fmt.Errorf("%w: %s 必须为大于 0 的有效秒数", ErrInvalidConfigValue, key)
		}
		return nil
	}
	if key == "hook.enable" {
		if value != "0" && value != "1" {
			return fmt.Errorf("%w: %s 只能为 0 或 1", ErrInvalidConfigValue, key)
		}
		return nil
	}
	if !strings.HasPrefix(key, "hook.on_") || value == "" {
		return nil
	}
	invalid := fmt.Errorf("%w: %s 必须为有效的 HTTP/HTTPS 回调地址，不能包含账号密码或片段", ErrInvalidConfigValue, key)
	parsed, err := url.Parse(value)
	if err != nil || strings.TrimSpace(value) != value || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || strings.ContainsAny(value, " \t\r\n") {
		return invalid
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return invalid
		}
	}
	if _, err := url.ParseQuery(parsed.RawQuery); err != nil {
		return invalid
	}
	if mediaServerUUID != "" && parsed.Query().Get("node") == mediaServerUUID && parsed.Query().Get("cap") != "" {
		return fmt.Errorf("%w: %s 不得使用当前节点的平台回调身份", ErrInvalidConfigValue, key)
	}
	return nil
}

func validateNetworkPortConfig(key, value string) error {
	if key == "rtp_proxy.port_range" {
		// 解析规则只有一份(node.ParseRTPPortRange)—— 这里若自己再写一遍,
		// 就会出现"下发能过、读值解析不过"(或反过来)的静默漂移。
		if _, _, ok := node.ParseRTPPortRange(value); !ok {
			return fmt.Errorf("%w: %s 必须为起始端口-结束端口，例如 30000-35000", ErrInvalidConfigValue, key)
		}
		return nil
	}
	allowDisabled := key == "rtp_proxy.port" || key == "http.sslport" || key == "rtmp.port" || key == "rtmp.sslport" || key == "rtsp.port" || key == "rtsp.sslport" || key == "shell.port"
	// WebRTC 四个端口没有"0 = 禁用"的语义：ZLM 会把 0 当成随机端口 bind，
	// 客户端拿到的候选地址不可预测 —— 只能当非法值拒掉。
	isRTCMediaPort := key == "rtc.port" || key == "rtc.tcpPort" || key == "rtc.icePort" || key == "rtc.iceTcpPort"
	isPort := key == "http.port" || allowDisabled || isRTCMediaPort
	if !isPort {
		return nil
	}
	port, err := strconv.Atoi(value)
	minimum := 1
	if allowDisabled {
		minimum = 0
	}
	maximum := 65535
	if key == "rtp_proxy.port" {
		maximum = 65534
	}
	if err != nil || port < minimum || port > maximum {
		if allowDisabled {
			return fmt.Errorf("%w: %s 必须为 0-65535 的整数，0 表示禁用", ErrInvalidConfigValue, key)
		}
		return fmt.Errorf("%w: %s 必须为 1-65535 的整数", ErrInvalidConfigValue, key)
	}
	return nil
}

func visibleConfigValue(key, value string) string {
	if key == "api.secret" {
		return ""
	}
	if !strings.HasPrefix(key, "hook.on_") || value == "" {
		return value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.User = nil
	if parsed.Query().Get("cap") != "" {
		parsed.RawQuery = ""
	}
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
