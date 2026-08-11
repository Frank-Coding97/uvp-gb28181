package controllers

import (
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

const (
	positionHistoryConfigKey              = "gb28181.position_history.enabled"
	positionHistoryRetentionDaysConfigKey = "gb28181.position_history.retention_days"
	defaultPositionHistoryRetentionDays   = 7
	sdpExtensionConfigKey                 = gbconfig.SDPExtensionConfigKey
	ptzDefaultSpeedLevelConfigKey         = "gb28181.ptz.default_speed_level"
	defaultPTZSpeedLevel                  = 6
)

// PositionHistoryConfig 是国标服务配置页面的位置历史配置。
type PositionHistoryConfig struct {
	Enabled       bool `json:"enabled"`
	RetentionDays int  `json:"retentionDays"`
}

// SDPExtensionConfig controls WVP-compatible extended media payloads in
// newly created Play and Playback SDP offers.
type SDPExtensionConfig struct {
	Enabled bool `json:"enabled"`
}

type SyncChannelsOnOnlineConfig struct {
	Enabled bool `json:"enabled"`
}

type OnlineOnHeartbeatConfig struct {
	Enabled bool `json:"enabled"`
}

type SaveAlarmMessagesConfig struct {
	Enabled bool `json:"enabled"`
}

type SIPCommandTimeoutConfig struct {
	TimeoutSec int `json:"timeoutSec"`
}

type PreallocationModeConfig struct {
	Enabled bool `json:"enabled"`
}

type IgnoreChannelOfflineStatusNotifyConfig struct {
	Enabled bool `json:"enabled"`
}

type DefaultChannelStreamTransportConfig struct {
	Transport string `json:"transport"`
}

type DefaultPlaybackProtocolConfig struct {
	Protocol string `json:"protocol"`
}

type GlobalSubscriptionConfig struct {
	Items []string `json:"items"`
}

type DefaultChannelAudioConfig struct {
	Enabled bool `json:"enabled"`
}

// PTZDefaultSpeedConfig 是云台控制界面初始使用的 1-10 档速度。
type PTZDefaultSpeedConfig struct {
	Level int `json:"level"`
}

// SIPLogUpdateResult 是 SIP 原始报文 Trace 开关及运行时应用结果。
type SIPLogUpdateResult struct {
	Enabled       bool   `json:"enabled"`
	RetentionDays int    `json:"retentionDays"`
	Applied       bool   `json:"applied"`
	ApplyError    string `json:"applyError,omitempty"`
}

type SIPTraceReloader func() error
type SIPTraceRuntimeProvider func() bool

var playAuthRuntimeReady atomic.Bool

func SetPlayAuthRuntimeReady(ready bool) {
	playAuthRuntimeReady.Store(ready)
}

// ServiceConfigController 提供国标服务配置页面使用的单项动态配置接口。
// 这里不复用 /api/config/update，避免页面提交时覆盖系统和安全配置。
type ServiceConfigController struct {
	controllers.Common
	reload             SIPTraceReloader
	runtimeEnabled     SIPTraceRuntimeProvider
	playbackSettingsMu sync.Mutex
}

func NewServiceConfigController() *ServiceConfigController {
	return &ServiceConfigController{}
}

func (sc *ServiceConfigController) SetSIPTraceReloader(reload SIPTraceReloader) {
	sc.reload = reload
}

func (sc *ServiceConfigController) SetSIPTraceRuntimeProvider(provider SIPTraceRuntimeProvider) {
	sc.runtimeEnabled = provider
}

// GetPlaybackSettings GET /api/gb28181/sip/service-config/playback-settings
func (sc *ServiceConfigController) GetPlaybackSettings(c *gin.Context) {
	sc.playbackSettingsMu.Lock()
	defer sc.playbackSettingsMu.Unlock()
	sc.Success(c, gbconfig.CurrentPlaybackSettings())
}

// UpdatePlaybackSettings PUT /api/gb28181/sip/service-config/playback-settings
func (sc *ServiceConfigController) UpdatePlaybackSettings(c *gin.Context) {
	var request struct {
		PlayTimeoutMs         *int  `json:"playTimeoutMs"`
		OnDemandLive          *bool `json:"onDemandLive"`
		CloudRecordingEnabled *bool `json:"cloudRecordingEnabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.PlayTimeoutMs == nil || request.OnDemandLive == nil || request.CloudRecordingEnabled == nil {
		sc.Fail(c, "保存播放配置失败：必须提交完整配置", err, http.StatusBadRequest)
		return
	}
	settings := gbconfig.PlaybackSettings{
		PlayTimeoutMs:         *request.PlayTimeoutMs,
		OnDemandLive:          *request.OnDemandLive,
		CloudRecordingEnabled: *request.CloudRecordingEnabled,
	}
	if err := gbconfig.ValidatePlaybackSettings(settings); err != nil {
		sc.Fail(c, "保存播放配置失败：playTimeoutMs 必须为 1000-300000 的整数", err, http.StatusBadRequest)
		return
	}

	sc.playbackSettingsMu.Lock()
	defer sc.playbackSettingsMu.Unlock()
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previous := gbconfig.CurrentPlaybackSettings()
	app.ConfigYml.Set(gbconfig.PlayRequestTimeoutMsConfigKey, settings.PlayTimeoutMs)
	app.ConfigYml.Set(gbconfig.DefaultChannelOnDemandLiveConfigKey, settings.OnDemandLive)
	app.ConfigYml.Set(gbconfig.DefaultChannelCloudRecordingConfigKey, settings.CloudRecordingEnabled)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		app.ConfigYml.Set(gbconfig.PlayRequestTimeoutMsConfigKey, previous.PlayTimeoutMs)
		app.ConfigYml.Set(gbconfig.DefaultChannelOnDemandLiveConfigKey, previous.OnDemandLive)
		app.ConfigYml.Set(gbconfig.DefaultChannelCloudRecordingConfigKey, previous.CloudRecordingEnabled)
		sc.Fail(c, "保存播放配置失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "播放配置已更新", settings)
}

// GetFixedAddressPlayback GET /api/gb28181/sip/service-config/fixed-address-playback
func (sc *ServiceConfigController) GetFixedAddressPlayback(c *gin.Context) {
	sc.Success(c, gbconfig.CurrentFixedAddressPlaybackSettings())
}

// UpdateFixedAddressPlayback PUT /api/gb28181/sip/service-config/fixed-address-playback
func (sc *ServiceConfigController) UpdateFixedAddressPlayback(c *gin.Context) {
	var request struct {
		FixedAddressEnabled *bool `json:"fixedAddressEnabled"`
		AutoOnDemandEnabled *bool `json:"autoOnDemandEnabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.FixedAddressEnabled == nil || request.AutoOnDemandEnabled == nil {
		sc.Fail(c, "保存固定地址播放配置失败：必须提交完整配置", err, http.StatusBadRequest)
		return
	}
	settings := gbconfig.FixedAddressPlaybackSettings{
		FixedAddressEnabled: *request.FixedAddressEnabled,
		AutoOnDemandEnabled: *request.AutoOnDemandEnabled,
	}
	if err := gbconfig.ValidateFixedAddressPlaybackSettings(settings); err != nil {
		sc.Fail(c, "保存固定地址播放配置失败：自动点播依赖固定播放地址", err, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}
	if settings.AutoOnDemandEnabled && !gbconfig.CurrentPlayAuthSettings().Enabled {
		sc.Fail(c, "保存固定地址播放配置失败：自动点播依赖播放鉴权", nil, http.StatusBadRequest)
		return
	}
	if err := gbconfig.SaveFixedAddressPlaybackSettings(app.ConfigYml, settings); err != nil {
		sc.Fail(c, "保存固定地址播放配置失败", err, http.StatusInternalServerError)
		return
	}
	sc.SuccessWithMessage(c, "固定地址播放配置已更新", settings)
}

// GetPlayAuth GET /api/gb28181/sip/service-config/play-auth
func (sc *ServiceConfigController) GetPlayAuth(c *gin.Context) {
	sc.Success(c, gbconfig.CurrentPlayAuthSettings())
}

// UpdatePlayAuth PUT /api/gb28181/sip/service-config/play-auth
func (sc *ServiceConfigController) UpdatePlayAuth(c *gin.Context) {
	var request struct {
		Enabled      *bool `json:"authEnabled"`
		BindClientIP *bool `json:"authBindClientIP"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil || request.BindClientIP == nil {
		sc.Fail(c, "保存播放鉴权配置失败：必须提交完整配置", err, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}
	settings := gbconfig.PlayAuthSettings{Enabled: *request.Enabled, BindClientIP: *request.BindClientIP}
	if err := gbconfig.ValidatePlayAuthSettings(settings, gbconfig.CurrentFixedAddressPlaybackSettings()); err != nil {
		sc.Fail(c, "保存播放鉴权配置失败：请检查 IP 绑定和自动点播依赖", err, http.StatusBadRequest)
		return
	}
	if settings.Enabled && !playAuthRuntimeReady.Load() {
		sc.Fail(c, "播放鉴权密钥不可用，请配置独立 active key 后重启服务", nil, http.StatusServiceUnavailable)
		return
	}
	if err := gbconfig.SavePlayAuthSettings(app.ConfigYml, settings); err != nil {
		sc.Fail(c, "保存播放鉴权配置失败", err, http.StatusInternalServerError)
		return
	}
	sc.SuccessWithMessage(c, "播放鉴权配置已更新", settings)
}

// GetSIPLog GET /api/gb28181/sip/service-config/sip-log
func (sc *ServiceConfigController) GetSIPLog(c *gin.Context) {
	enabled := gbconfig.SIPTraceEnabled()
	sc.Success(c, SIPLogUpdateResult{Enabled: enabled, RetentionDays: gbconfig.SIPTraceRetentionDays(), Applied: sc.sipTraceApplied(enabled)})
}

// UpdateSIPLog PUT /api/gb28181/sip/service-config/sip-log
func (sc *ServiceConfigController) UpdateSIPLog(c *gin.Context) {
	var request struct {
		Enabled       *bool `json:"enabled"`
		RetentionDays *int  `json:"retentionDays"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || (request.Enabled == nil && request.RetentionDays == nil) {
		sc.Fail(c, "保存 SIP 日志配置失败：enabled 或 retentionDays 参数无效", err, http.StatusBadRequest)
		return
	}
	if request.RetentionDays != nil && (*request.RetentionDays < gbconfig.MinSIPTraceRetentionDays || *request.RetentionDays > gbconfig.MaxSIPTraceRetentionDays) {
		sc.Fail(c, "保存 SIP 日志配置失败：retentionDays 必须为 1-365 的整数", nil, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previousEnabled := gbconfig.SIPTraceEnabled()
	previousRetentionDays := gbconfig.SIPTraceRetentionDays()
	enabled := previousEnabled
	retentionDays := previousRetentionDays
	if request.Enabled != nil {
		enabled = *request.Enabled
		app.ConfigYml.Set(gbconfig.SIPTraceEnabledConfigKey, enabled)
	}
	if request.RetentionDays != nil {
		retentionDays = *request.RetentionDays
		app.ConfigYml.Set(gbconfig.SIPTraceRetentionDaysConfigKey, retentionDays)
	}
	if err := app.ConfigYml.SaveConfig(); err != nil {
		app.ConfigYml.Set(gbconfig.SIPTraceEnabledConfigKey, previousEnabled)
		app.ConfigYml.Set(gbconfig.SIPTraceRetentionDaysConfigKey, previousRetentionDays)
		sc.Fail(c, "保存 SIP 日志配置失败", err, http.StatusInternalServerError)
		return
	}

	result := SIPLogUpdateResult{Enabled: enabled, RetentionDays: retentionDays, Applied: true}
	if (previousEnabled != enabled || previousRetentionDays != retentionDays) && sc.reload != nil {
		if err := sc.reload(); err != nil {
			result.Applied = false
			result.ApplyError = err.Error()
			sc.SuccessWithMessage(c, "SIP 日志配置已保存，但 SIP 服务重载失败", result)
			return
		}
	}
	result.Applied = sc.sipTraceApplied(enabled)
	sc.SuccessWithMessage(c, "SIP 日志配置已更新", result)
}

func (sc *ServiceConfigController) sipTraceApplied(enabled bool) bool {
	return sc.runtimeEnabled == nil || sc.runtimeEnabled() == enabled
}

// GetSDPExtension GET /api/gb28181/sip/service-config/sdp-extension
func (sc *ServiceConfigController) GetSDPExtension(c *gin.Context) {
	sc.Success(c, SDPExtensionConfig{Enabled: gbconfig.SDPExtensionEnabled()})
}

// UpdateSDPExtension PUT /api/gb28181/sip/service-config/sdp-extension
func (sc *ServiceConfigController) UpdateSDPExtension(c *gin.Context) {
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		sc.Fail(c, "保存扩展 SDP 兼容模式失败：enabled 必须为布尔值", err, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previous := gbconfig.SDPExtensionEnabled()
	app.ConfigYml.Set(sdpExtensionConfigKey, *request.Enabled)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		app.ConfigYml.Set(sdpExtensionConfigKey, previous)
		sc.Fail(c, "保存扩展 SDP 兼容模式失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "扩展 SDP 兼容模式已更新", SDPExtensionConfig{Enabled: *request.Enabled})
}

// GetSyncChannelsOnOnline GET /api/gb28181/sip/service-config/sync-channels-on-online
func (sc *ServiceConfigController) GetSyncChannelsOnOnline(c *gin.Context) {
	sc.Success(c, SyncChannelsOnOnlineConfig{Enabled: gbconfig.SyncChannelsOnOnline()})
}

// UpdateSyncChannelsOnOnline PUT /api/gb28181/sip/service-config/sync-channels-on-online
func (sc *ServiceConfigController) UpdateSyncChannelsOnOnline(c *gin.Context) {
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		sc.Fail(c, "保存设备上线同步通道配置失败：enabled 必须为布尔值", err, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previous := gbconfig.SyncChannelsOnOnline()
	app.ConfigYml.Set(gbconfig.SyncChannelsOnOnlineConfigKey, *request.Enabled)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		app.ConfigYml.Set(gbconfig.SyncChannelsOnOnlineConfigKey, previous)
		sc.Fail(c, "保存设备上线同步通道配置失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "设备上线同步通道配置已更新", SyncChannelsOnOnlineConfig{Enabled: *request.Enabled})
}

// GetOnlineOnHeartbeat GET /api/gb28181/sip/service-config/online-on-heartbeat
func (sc *ServiceConfigController) GetOnlineOnHeartbeat(c *gin.Context) {
	sc.Success(c, OnlineOnHeartbeatConfig{Enabled: gbconfig.OnlineOnHeartbeat()})
}

// UpdateOnlineOnHeartbeat PUT /api/gb28181/sip/service-config/online-on-heartbeat
func (sc *ServiceConfigController) UpdateOnlineOnHeartbeat(c *gin.Context) {
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		sc.Fail(c, "保存收到心跳恢复设备上线配置失败：enabled 必须为布尔值", err, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previous := gbconfig.OnlineOnHeartbeat()
	app.ConfigYml.Set(gbconfig.OnlineOnHeartbeatConfigKey, *request.Enabled)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		app.ConfigYml.Set(gbconfig.OnlineOnHeartbeatConfigKey, previous)
		sc.Fail(c, "保存收到心跳恢复设备上线配置失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "收到心跳恢复设备上线配置已更新", OnlineOnHeartbeatConfig{Enabled: *request.Enabled})
}

// GetSaveAlarmMessages GET /api/gb28181/sip/service-config/save-alarm-messages
func (sc *ServiceConfigController) GetSaveAlarmMessages(c *gin.Context) {
	sc.Success(c, SaveAlarmMessagesConfig{Enabled: gbconfig.SaveAlarmMessages()})
}

// UpdateSaveAlarmMessages PUT /api/gb28181/sip/service-config/save-alarm-messages
func (sc *ServiceConfigController) UpdateSaveAlarmMessages(c *gin.Context) {
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		sc.Fail(c, "保存报警消息存储配置失败：enabled 必须为布尔值", err, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previous := gbconfig.SaveAlarmMessages()
	app.ConfigYml.Set(gbconfig.SaveAlarmMessagesConfigKey, *request.Enabled)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		app.ConfigYml.Set(gbconfig.SaveAlarmMessagesConfigKey, previous)
		sc.Fail(c, "保存报警消息存储配置失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "报警消息存储配置已更新", SaveAlarmMessagesConfig{Enabled: *request.Enabled})
}

// GetSIPCommandTimeout GET /api/gb28181/sip/service-config/sip-command-timeout
func (sc *ServiceConfigController) GetSIPCommandTimeout(c *gin.Context) {
	sc.Success(c, SIPCommandTimeoutConfig{TimeoutSec: gbconfig.SIPCommandTimeoutSec()})
}

// UpdateSIPCommandTimeout PUT /api/gb28181/sip/service-config/sip-command-timeout
func (sc *ServiceConfigController) UpdateSIPCommandTimeout(c *gin.Context) {
	var request struct {
		TimeoutSec *int `json:"timeoutSec"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.TimeoutSec == nil || *request.TimeoutSec < 1 || *request.TimeoutSec > gbconfig.MaxSIPCommandTimeoutSec {
		sc.Fail(c, "保存 SIP 命令超时时间失败：timeoutSec 必须为 1-300 的整数", err, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previous := gbconfig.SIPCommandTimeoutSec()
	app.ConfigYml.Set(gbconfig.SIPCommandTimeoutSecConfigKey, *request.TimeoutSec)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		app.ConfigYml.Set(gbconfig.SIPCommandTimeoutSecConfigKey, previous)
		sc.Fail(c, "保存 SIP 命令超时时间失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "SIP 命令超时时间已更新", SIPCommandTimeoutConfig{TimeoutSec: *request.TimeoutSec})
}

// GetPreallocationMode GET /api/gb28181/sip/service-config/preallocation-mode
func (sc *ServiceConfigController) GetPreallocationMode(c *gin.Context) {
	sc.Success(c, PreallocationModeConfig{Enabled: gbconfig.PreallocationMode()})
}

// UpdatePreallocationMode PUT /api/gb28181/sip/service-config/preallocation-mode
func (sc *ServiceConfigController) UpdatePreallocationMode(c *gin.Context) {
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		sc.Fail(c, "保存预分配模式失败：enabled 必须为布尔值", err, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previous := gbconfig.PreallocationMode()
	app.ConfigYml.Set(gbconfig.PreallocationModeConfigKey, *request.Enabled)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		app.ConfigYml.Set(gbconfig.PreallocationModeConfigKey, previous)
		sc.Fail(c, "保存预分配模式失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "预分配模式已更新", PreallocationModeConfig{Enabled: *request.Enabled})
}

// GetIgnoreChannelOfflineStatusNotify GET /api/gb28181/sip/service-config/ignore-channel-offline-status-notify
func (sc *ServiceConfigController) GetIgnoreChannelOfflineStatusNotify(c *gin.Context) {
	sc.Success(c, IgnoreChannelOfflineStatusNotifyConfig{Enabled: gbconfig.IgnoreChannelOfflineStatusNotify()})
}

// UpdateIgnoreChannelOfflineStatusNotify PUT /api/gb28181/sip/service-config/ignore-channel-offline-status-notify
func (sc *ServiceConfigController) UpdateIgnoreChannelOfflineStatusNotify(c *gin.Context) {
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		sc.Fail(c, "保存忽略通道离线/异常通知配置失败：enabled 必须为布尔值", err, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previous := gbconfig.IgnoreChannelOfflineStatusNotify()
	app.ConfigYml.Set(gbconfig.IgnoreChannelOfflineStatusNotifyConfigKey, *request.Enabled)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		app.ConfigYml.Set(gbconfig.IgnoreChannelOfflineStatusNotifyConfigKey, previous)
		sc.Fail(c, "保存忽略通道离线/异常通知配置失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "忽略通道离线/异常通知配置已更新", IgnoreChannelOfflineStatusNotifyConfig{Enabled: *request.Enabled})
}

// GetDefaultChannelStreamTransport GET /api/gb28181/sip/service-config/default-channel-stream-transport
func (sc *ServiceConfigController) GetDefaultChannelStreamTransport(c *gin.Context) {
	sc.Success(c, DefaultChannelStreamTransportConfig{Transport: gbconfig.CurrentDefaultChannelStreamTransport()})
}

// UpdateDefaultChannelStreamTransport PUT /api/gb28181/sip/service-config/default-channel-stream-transport
func (sc *ServiceConfigController) UpdateDefaultChannelStreamTransport(c *gin.Context) {
	var request struct {
		Transport *string `json:"transport"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Transport == nil || !gbconfig.IsSupportedChannelStreamTransport(*request.Transport) {
		sc.Fail(c, "保存新通道默认流传输模式失败：transport 必须为 UDP、TCP-Active 或 TCP-Passive", err, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previous := gbconfig.CurrentDefaultChannelStreamTransport()
	app.ConfigYml.Set(gbconfig.DefaultChannelStreamTransportConfigKey, *request.Transport)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		app.ConfigYml.Set(gbconfig.DefaultChannelStreamTransportConfigKey, previous)
		sc.Fail(c, "保存新通道默认流传输模式失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "新通道默认流传输模式已更新", DefaultChannelStreamTransportConfig{Transport: *request.Transport})
}

// GetDefaultPlaybackProtocol GET /api/gb28181/sip/service-config/default-playback-protocol
func (sc *ServiceConfigController) GetDefaultPlaybackProtocol(c *gin.Context) {
	sc.Success(c, DefaultPlaybackProtocolConfig{Protocol: gbconfig.CurrentDefaultPlaybackProtocol()})
}

// UpdateDefaultPlaybackProtocol PUT /api/gb28181/sip/service-config/default-playback-protocol
func (sc *ServiceConfigController) UpdateDefaultPlaybackProtocol(c *gin.Context) {
	var request struct {
		Protocol *string `json:"protocol"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Protocol == nil || !gbconfig.IsSupportedPlaybackProtocol(*request.Protocol) {
		sc.Fail(c, "保存默认播放协议失败：protocol 必须为 ws-flv、http-flv、hls 或 webrtc", err, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previous := gbconfig.CurrentDefaultPlaybackProtocol()
	app.ConfigYml.Set(gbconfig.DefaultPlaybackProtocolConfigKey, *request.Protocol)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		app.ConfigYml.Set(gbconfig.DefaultPlaybackProtocolConfigKey, previous)
		sc.Fail(c, "保存默认播放协议失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "默认播放协议已更新", DefaultPlaybackProtocolConfig{Protocol: *request.Protocol})
}

// GetGlobalSubscriptions GET /api/gb28181/sip/service-config/global-subscriptions
func (sc *ServiceConfigController) GetGlobalSubscriptions(c *gin.Context) {
	sc.Success(c, GlobalSubscriptionConfig{Items: gbconfig.GlobalSubscriptionItems()})
}

// UpdateGlobalSubscriptions PUT /api/gb28181/sip/service-config/global-subscriptions
func (sc *ServiceConfigController) UpdateGlobalSubscriptions(c *gin.Context) {
	var request struct {
		Items *[]string `json:"items"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Items == nil {
		sc.Fail(c, "保存全局订阅项目失败：items 必须为数组", err, http.StatusBadRequest)
		return
	}
	seen := make(map[string]struct{}, len(*request.Items))
	for _, item := range *request.Items {
		if !gbconfig.IsSupportedGlobalSubscriptionItem(item) {
			sc.Fail(c, "保存全局订阅项目失败：仅支持 catalog、mobile_position、alarm、ptz_precise_position", nil, http.StatusBadRequest)
			return
		}
		if _, ok := seen[item]; ok {
			sc.Fail(c, "保存全局订阅项目失败：items 不能重复", nil, http.StatusBadRequest)
			return
		}
		seen[item] = struct{}{}
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previous := gbconfig.GlobalSubscriptionItems()
	items := append([]string(nil), (*request.Items)...)
	app.ConfigYml.Set(gbconfig.GlobalSubscriptionItemsConfigKey, items)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		app.ConfigYml.Set(gbconfig.GlobalSubscriptionItemsConfigKey, previous)
		sc.Fail(c, "保存全局订阅项目失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "全局订阅项目已更新", GlobalSubscriptionConfig{Items: items})
}

// GetDefaultChannelAudio GET /api/gb28181/sip/service-config/default-channel-audio
func (sc *ServiceConfigController) GetDefaultChannelAudio(c *gin.Context) {
	sc.Success(c, DefaultChannelAudioConfig{Enabled: gbconfig.DefaultChannelAudioEnabled()})
}

// UpdateDefaultChannelAudio PUT /api/gb28181/sip/service-config/default-channel-audio
func (sc *ServiceConfigController) UpdateDefaultChannelAudio(c *gin.Context) {
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		sc.Fail(c, "保存全局通道音频配置失败：enabled 必须为布尔值", err, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previous := gbconfig.DefaultChannelAudioEnabled()
	app.ConfigYml.Set(gbconfig.DefaultChannelAudioEnabledConfigKey, *request.Enabled)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		app.ConfigYml.Set(gbconfig.DefaultChannelAudioEnabledConfigKey, previous)
		sc.Fail(c, "保存全局通道音频配置失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "全局通道音频配置已更新", DefaultChannelAudioConfig{Enabled: *request.Enabled})
}

// GetPTZDefaultSpeed GET /api/gb28181/sip/service-config/ptz-default-speed
func (sc *ServiceConfigController) GetPTZDefaultSpeed(c *gin.Context) {
	sc.Success(c, PTZDefaultSpeedConfig{Level: currentPTZDefaultSpeedLevel()})
}

// UpdatePTZDefaultSpeed PUT /api/gb28181/sip/service-config/ptz-default-speed
func (sc *ServiceConfigController) UpdatePTZDefaultSpeed(c *gin.Context) {
	var request struct {
		Level *int `json:"level"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Level == nil {
		sc.Fail(c, "保存云台默认速度失败：level 必须为 1-10 的整数", err, http.StatusBadRequest)
		return
	}
	if *request.Level < 1 || *request.Level > 10 {
		sc.Fail(c, "保存云台默认速度失败：速度档位需在 1-10 之间", nil, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previous := currentPTZDefaultSpeedLevel()
	app.ConfigYml.Set(ptzDefaultSpeedLevelConfigKey, *request.Level)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		app.ConfigYml.Set(ptzDefaultSpeedLevelConfigKey, previous)
		sc.Fail(c, "保存云台默认速度失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "云台默认速度已更新", PTZDefaultSpeedConfig{Level: *request.Level})
}

// GetPositionHistory GET /api/gb28181/sip/service-config/position-history
func (sc *ServiceConfigController) GetPositionHistory(c *gin.Context) {
	// 未显式配置时保持历史兼容行为：默认保存轨迹。
	enabled := true
	if app.ConfigYml != nil && app.ConfigYml.Get(positionHistoryConfigKey) != nil {
		enabled = app.ConfigYml.GetBool(positionHistoryConfigKey)
	}
	sc.Success(c, PositionHistoryConfig{Enabled: enabled, RetentionDays: currentPositionHistoryRetentionDays()})
}

// UpdatePositionHistory PUT /api/gb28181/sip/service-config/position-history
func (sc *ServiceConfigController) UpdatePositionHistory(c *gin.Context) {
	var request struct {
		Enabled       *bool `json:"enabled"`
		RetentionDays *int  `json:"retentionDays"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		sc.Fail(c, "保存移动位置历史轨迹配置失败：enabled 必须为布尔值", err, http.StatusBadRequest)
		return
	}
	if request.RetentionDays != nil && (*request.RetentionDays < 1 || *request.RetentionDays > 365) {
		sc.Fail(c, "保存移动位置历史轨迹配置失败：保留天数需在 1-365 天之间", nil, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previousEnabled := true
	if app.ConfigYml.Get(positionHistoryConfigKey) != nil {
		previousEnabled = app.ConfigYml.GetBool(positionHistoryConfigKey)
	}
	previousRetentionDays := currentPositionHistoryRetentionDays()
	retentionDays := previousRetentionDays
	if request.RetentionDays != nil {
		retentionDays = *request.RetentionDays
	}
	app.ConfigYml.Set(positionHistoryConfigKey, *request.Enabled)
	app.ConfigYml.Set(positionHistoryRetentionDaysConfigKey, retentionDays)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		// 保存失败时恢复内存值，避免运行时行为与配置文件不一致。
		app.ConfigYml.Set(positionHistoryConfigKey, previousEnabled)
		app.ConfigYml.Set(positionHistoryRetentionDaysConfigKey, previousRetentionDays)
		sc.Fail(c, "保存移动位置历史轨迹配置失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "移动位置历史轨迹配置已更新", PositionHistoryConfig{
		Enabled:       *request.Enabled,
		RetentionDays: retentionDays,
	})
}

func currentPositionHistoryRetentionDays() int {
	if app.ConfigYml != nil {
		if days := app.ConfigYml.GetInt(positionHistoryRetentionDaysConfigKey); days >= 1 && days <= 365 {
			return days
		}
	}
	return defaultPositionHistoryRetentionDays
}

func currentPTZDefaultSpeedLevel() int {
	if app.ConfigYml != nil {
		if level := app.ConfigYml.GetInt(ptzDefaultSpeedLevelConfigKey); level >= 1 && level <= 10 {
			return level
		}
	}
	return defaultPTZSpeedLevel
}
