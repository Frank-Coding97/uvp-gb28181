package controllers

import (
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

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

	// 云端录像（含设备录像缓存）默认保留天数。一处配置、全平台生效：
	// 录像文件与缓存任务的过期时间都由它推导。
	cloudRecordingRetentionDaysConfigKey = "gb28181.cloud_recording.retention_days"
	defaultCloudRecordingRetentionDays   = 7
	minCloudRecordingRetentionDays       = 1
	maxCloudRecordingRetentionDays       = 365
)

// PositionHistoryConfig 是国标服务配置页面的位置历史配置。
type PositionHistoryConfig struct {
	Enabled       bool `json:"enabled"`
	RetentionDays int  `json:"retentionDays"`
}

// CloudRecordingRetentionConfig 是国标服务配置页面的「云端录像默认保留天数」。
// 设备录像下载产生的服务器缓存文件同样跟随这个天数过期，避免两处各配一套。
type CloudRecordingRetentionConfig struct {
	RetentionDays int `json:"retentionDays"`
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
type PlayAuthTTLUpdater func(time.Duration) error

var playAuthRuntimeReady atomic.Bool

// ServiceConfig is the single public contract for all dynamic GB service
// configuration. Keeping the aggregate here also gives OpenAPI consumers one
// stable resource instead of exposing every internal configuration family.
type ServiceConfig struct {
	PositionHistory                  PositionHistoryConfig                  `json:"positionHistory"`
	CloudRecordingRetention          CloudRecordingRetentionConfig          `json:"cloudRecordingRetention"`
	SDPExtension                     SDPExtensionConfig                     `json:"sdpExtension"`
	SyncChannelsOnOnline             SyncChannelsOnOnlineConfig             `json:"syncChannelsOnOnline"`
	OnlineOnHeartbeat                OnlineOnHeartbeatConfig                `json:"onlineOnHeartbeat"`
	SaveAlarmMessages                SaveAlarmMessagesConfig                `json:"saveAlarmMessages"`
	SIPCommandTimeout                SIPCommandTimeoutConfig                `json:"sipCommandTimeout"`
	PreallocationMode                PreallocationModeConfig                `json:"preallocationMode"`
	IgnoreChannelOfflineStatusNotify IgnoreChannelOfflineStatusNotifyConfig `json:"ignoreChannelOfflineStatusNotify"`
	PTZDefaultSpeed                  PTZDefaultSpeedConfig                  `json:"ptzDefaultSpeed"`
	DefaultChannelStreamTransport    DefaultChannelStreamTransportConfig    `json:"defaultChannelStreamTransport"`
	DefaultPlaybackProtocol          DefaultPlaybackProtocolConfig          `json:"defaultPlaybackProtocol"`
	GlobalSubscriptions              GlobalSubscriptionConfig               `json:"globalSubscriptions"`
	DefaultChannelAudio              DefaultChannelAudioConfig              `json:"defaultChannelAudio"`
	PlaybackSettings                 gbconfig.PlaybackSettings              `json:"playbackSettings"`
	FixedAddressPlayback             gbconfig.FixedAddressPlaybackSettings  `json:"fixedAddressPlayback"`
	PlayAuth                         gbconfig.PlayAuthSettings              `json:"playAuth"`
	SIPLog                           SIPLogUpdateResult                     `json:"sipLog"`
}

func (sc *ServiceConfigController) GetServiceConfig(c *gin.Context) {
	sc.Success(c, sc.currentServiceConfig())
}

func (sc *ServiceConfigController) currentServiceConfig() ServiceConfig {
	position := PositionHistoryConfig{Enabled: true, RetentionDays: defaultPositionHistoryRetentionDays}
	if app.ConfigYml != nil {
		if app.ConfigYml.Get(positionHistoryConfigKey) != nil {
			position.Enabled = app.ConfigYml.GetBool(positionHistoryConfigKey)
		}
		if days := app.ConfigYml.GetInt(positionHistoryRetentionDaysConfigKey); days >= 1 && days <= 365 {
			position.RetentionDays = days
		}
	}
	cloud := defaultCloudRecordingRetentionDays
	if app.ConfigYml != nil {
		if days := app.ConfigYml.GetInt(cloudRecordingRetentionDaysConfigKey); days >= 1 && days <= 365 {
			cloud = days
		}
	}
	return ServiceConfig{
		PositionHistory: position, CloudRecordingRetention: CloudRecordingRetentionConfig{RetentionDays: cloud},
		SDPExtension:         SDPExtensionConfig{Enabled: gbconfig.SDPExtensionEnabled()},
		SyncChannelsOnOnline: SyncChannelsOnOnlineConfig{Enabled: gbconfig.SyncChannelsOnOnline()}, OnlineOnHeartbeat: OnlineOnHeartbeatConfig{Enabled: gbconfig.OnlineOnHeartbeat()},
		SaveAlarmMessages: SaveAlarmMessagesConfig{Enabled: gbconfig.SaveAlarmMessages()}, SIPCommandTimeout: SIPCommandTimeoutConfig{TimeoutSec: gbconfig.SIPCommandTimeoutSec()},
		PreallocationMode: PreallocationModeConfig{Enabled: gbconfig.PreallocationMode()}, IgnoreChannelOfflineStatusNotify: IgnoreChannelOfflineStatusNotifyConfig{Enabled: gbconfig.IgnoreChannelOfflineStatusNotify()},
		PTZDefaultSpeed: PTZDefaultSpeedConfig{Level: currentPTZDefaultSpeedLevel()}, DefaultChannelStreamTransport: DefaultChannelStreamTransportConfig{Transport: gbconfig.CurrentDefaultChannelStreamTransport()},
		DefaultPlaybackProtocol: DefaultPlaybackProtocolConfig{Protocol: gbconfig.CurrentDefaultPlaybackProtocol()}, GlobalSubscriptions: GlobalSubscriptionConfig{Items: gbconfig.GlobalSubscriptionItems()},
		DefaultChannelAudio: DefaultChannelAudioConfig{Enabled: gbconfig.DefaultChannelAudioEnabled()}, PlaybackSettings: gbconfig.CurrentPlaybackSettings(), FixedAddressPlayback: gbconfig.CurrentFixedAddressPlaybackSettings(),
		PlayAuth: gbconfig.CurrentPlayAuthSettings(), SIPLog: SIPLogUpdateResult{Enabled: gbconfig.SIPTraceEnabled(), RetentionDays: gbconfig.SIPTraceRetentionDays(), Applied: sc.sipTraceApplied(gbconfig.SIPTraceEnabled())},
	}
}

func (sc *ServiceConfigController) UpdateServiceConfig(c *gin.Context) {
	var request ServiceConfig
	if err := c.ShouldBindJSON(&request); err != nil {
		sc.Fail(c, "保存国标服务配置失败：请求格式无效", err, http.StatusBadRequest)
		return
	}
	if err := validateServiceConfig(request); err != nil {
		sc.Fail(c, "保存国标服务配置失败："+err.Error(), err, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}
	sc.playbackSettingsMu.Lock()
	defer sc.playbackSettingsMu.Unlock()
	previous := sc.currentServiceConfig()
	applyServiceConfig(request)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		applyServiceConfig(previous)
		sc.Fail(c, "保存国标服务配置失败", err, http.StatusInternalServerError)
		return
	}
	if previous.SIPLog.Enabled != request.SIPLog.Enabled || previous.SIPLog.RetentionDays != request.SIPLog.RetentionDays {
		if sc.reload != nil {
			if err := sc.reload(); err != nil {
				request.SIPLog.Applied = false
				request.SIPLog.ApplyError = err.Error()
			}
		}
	}
	if sc.playAuthTTLUpdater != nil && previous.PlayAuth.TTLSeconds != request.PlayAuth.TTLSeconds {
		if err := sc.playAuthTTLUpdater(time.Duration(request.PlayAuth.TTLSeconds) * time.Second); err != nil {
			applyServiceConfig(previous)
			_ = app.ConfigYml.SaveConfig()
			sc.Fail(c, "国标服务配置已保存，但播放鉴权有效期未应用", err, http.StatusInternalServerError)
			return
		}
	}
	sc.SuccessWithMessage(c, "国标服务配置已更新", request)
}

func validateServiceConfig(v ServiceConfig) error {
	if err := gbconfig.ValidatePlaybackSettings(v.PlaybackSettings); err != nil {
		return err
	}
	if err := gbconfig.ValidateFixedAddressPlaybackSettings(v.FixedAddressPlayback); err != nil {
		return err
	}
	if err := gbconfig.ValidatePlayAuthSettings(v.PlayAuth, v.FixedAddressPlayback); err != nil {
		return err
	}
	if v.PositionHistory.RetentionDays < 1 || v.PositionHistory.RetentionDays > 365 || v.CloudRecordingRetention.RetentionDays < 1 || v.CloudRecordingRetention.RetentionDays > 365 {
		return errors.New("保留天数必须为1-365")
	}
	if v.PTZDefaultSpeed.Level < 1 || v.PTZDefaultSpeed.Level > 10 {
		return errors.New("云台默认速度必须为1-10")
	}
	if !gbconfig.IsSupportedChannelStreamTransport(v.DefaultChannelStreamTransport.Transport) {
		return errors.New("默认流传输方式无效")
	}
	if !gbconfig.IsSupportedPlaybackProtocol(v.DefaultPlaybackProtocol.Protocol) {
		return errors.New("默认播放协议无效")
	}
	seenSubscriptions := make(map[string]struct{}, len(v.GlobalSubscriptions.Items))
	for _, item := range v.GlobalSubscriptions.Items {
		if !gbconfig.IsSupportedGlobalSubscriptionItem(item) {
			return errors.New("全局订阅项目无效")
		}
		if _, exists := seenSubscriptions[item]; exists {
			return errors.New("全局订阅项目不能重复")
		}
		seenSubscriptions[item] = struct{}{}
	}
	if v.SIPLog.RetentionDays < gbconfig.MinSIPTraceRetentionDays || v.SIPLog.RetentionDays > gbconfig.MaxSIPTraceRetentionDays {
		return errors.New("SIP日志保留天数必须为1-365")
	}
	if v.SIPCommandTimeout.TimeoutSec < 1 || v.SIPCommandTimeout.TimeoutSec > gbconfig.MaxSIPCommandTimeoutSec {
		return errors.New("SIP命令超时时间无效")
	}
	return nil
}

func applyServiceConfig(v ServiceConfig) {
	app.ConfigYml.Set(positionHistoryConfigKey, v.PositionHistory.Enabled)
	app.ConfigYml.Set(positionHistoryRetentionDaysConfigKey, v.PositionHistory.RetentionDays)
	app.ConfigYml.Set(cloudRecordingRetentionDaysConfigKey, v.CloudRecordingRetention.RetentionDays)
	app.ConfigYml.Set(gbconfig.SDPExtensionConfigKey, v.SDPExtension.Enabled)
	app.ConfigYml.Set(gbconfig.SyncChannelsOnOnlineConfigKey, v.SyncChannelsOnOnline.Enabled)
	app.ConfigYml.Set(gbconfig.OnlineOnHeartbeatConfigKey, v.OnlineOnHeartbeat.Enabled)
	app.ConfigYml.Set(gbconfig.SaveAlarmMessagesConfigKey, v.SaveAlarmMessages.Enabled)
	app.ConfigYml.Set(gbconfig.SIPCommandTimeoutSecConfigKey, v.SIPCommandTimeout.TimeoutSec)
	app.ConfigYml.Set(gbconfig.PreallocationModeConfigKey, v.PreallocationMode.Enabled)
	app.ConfigYml.Set(gbconfig.IgnoreChannelOfflineStatusNotifyConfigKey, v.IgnoreChannelOfflineStatusNotify.Enabled)
	app.ConfigYml.Set(ptzDefaultSpeedLevelConfigKey, v.PTZDefaultSpeed.Level)
	app.ConfigYml.Set(gbconfig.DefaultChannelStreamTransportConfigKey, v.DefaultChannelStreamTransport.Transport)
	app.ConfigYml.Set(gbconfig.DefaultPlaybackProtocolConfigKey, v.DefaultPlaybackProtocol.Protocol)
	app.ConfigYml.Set(gbconfig.GlobalSubscriptionItemsConfigKey, v.GlobalSubscriptions.Items)
	app.ConfigYml.Set(gbconfig.DefaultChannelAudioEnabledConfigKey, v.DefaultChannelAudio.Enabled)
	app.ConfigYml.Set(gbconfig.PlayRequestTimeoutMsConfigKey, v.PlaybackSettings.PlayTimeoutMs)
	app.ConfigYml.Set(gbconfig.DefaultChannelOnDemandLiveConfigKey, v.PlaybackSettings.OnDemandLive)
	app.ConfigYml.Set(gbconfig.DefaultChannelCloudRecordingConfigKey, v.PlaybackSettings.CloudRecordingEnabled)
	app.ConfigYml.Set(gbconfig.FixedAddressEnabledConfigKey, v.FixedAddressPlayback.FixedAddressEnabled)
	app.ConfigYml.Set(gbconfig.AutoOnDemandEnabledConfigKey, v.FixedAddressPlayback.AutoOnDemandEnabled)
	app.ConfigYml.Set(gbconfig.PlayAuthEnabledConfigKey, v.PlayAuth.Enabled)
	app.ConfigYml.Set(gbconfig.PlayAuthBindClientIPConfigKey, v.PlayAuth.BindClientIP)
	app.ConfigYml.Set(gbconfig.PlayAuthTTLSecondsConfigKey, v.PlayAuth.TTLSeconds)
	app.ConfigYml.Set(gbconfig.SIPTraceEnabledConfigKey, v.SIPLog.Enabled)
	app.ConfigYml.Set(gbconfig.SIPTraceRetentionDaysConfigKey, v.SIPLog.RetentionDays)
}

func SetPlayAuthRuntimeReady(ready bool) {
	playAuthRuntimeReady.Store(ready)
}

// ServiceConfigController 提供国标服务配置页面使用的单项动态配置接口。
// 这里不复用 /api/config/update，避免页面提交时覆盖系统和安全配置。
type ServiceConfigController struct {
	controllers.Common
	reload             SIPTraceReloader
	runtimeEnabled     SIPTraceRuntimeProvider
	playAuthTTLUpdater PlayAuthTTLUpdater
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

func (sc *ServiceConfigController) SetPlayAuthTTLUpdater(updater PlayAuthTTLUpdater) {
	sc.playAuthTTLUpdater = updater
}

func (sc *ServiceConfigController) sipTraceApplied(enabled bool) bool {
	return sc.runtimeEnabled == nil || sc.runtimeEnabled() == enabled
}

func currentPTZDefaultSpeedLevel() int {
	if app.ConfigYml != nil {
		if level := app.ConfigYml.GetInt(ptzDefaultSpeedLevelConfigKey); level >= 1 && level <= 10 {
			return level
		}
	}
	return defaultPTZSpeedLevel
}

// CloudRecordingRetentionDays 返回云端录像及录像缓存统一使用的保留天数。
// 未配置或配置非法时使用默认值，供运行时服务装配读取。
func CloudRecordingRetentionDays() int {
	if app.ConfigYml != nil {
		if days := app.ConfigYml.GetInt(cloudRecordingRetentionDaysConfigKey); days >= minCloudRecordingRetentionDays && days <= maxCloudRecordingRetentionDays {
			return days
		}
	}
	return defaultCloudRecordingRetentionDays
}
