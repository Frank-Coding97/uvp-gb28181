package controllers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.com/uvp-gb28181/app/controllers"
	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/logcleanup"
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

// DeviceAccessConfig 是「设备接入地址」：平台下发给**设备**的两个基址。
//
// ⛔⛔ 两者都必须是设备能访问到的地址，不是浏览器打开平台的地址。2026-10 两次现场
// 事故同源：扫码配网用了 window.location.origin（自签 HTTPS ⇒ 设备握手失败），
// 抓拍回传用了浏览器入口（自签 HTTPS ⇒ 设备传不回来且零报错）。所以这两项必须
// 允许现场在页面上改成设备真正能到达的地址（内网 IP / 公网明文口）。
//
// 留空 = 未配置：扫码回退 host+port（再空则出码时 503 拒绝），抓拍回退请求 Host
// 派生（仅设备与平台直连时可靠）。清空是合法操作，故不能用零值判断"没传"——
// presence 标记见 UnmarshalJSON。
type DeviceAccessConfig struct {
	QRProvisionBaseURL    string `json:"qrProvisionBaseUrl"`
	SnapshotUploadBaseURL string `json:"snapshotUploadBaseUrl"`
}

// SIPLogUpdateResult 是 SIP 原始报文 Trace 开关及运行时应用结果。
// 保留天数不在这里配置：唯一入口是系统配置的日志清理页签，由它写入
// gbconfig.SIPTraceRetentionDaysConfigKey 并负责重载 SIP 运行时。
type SIPLogUpdateResult struct {
	Enabled    bool   `json:"enabled"`
	Applied    bool   `json:"applied"`
	ApplyError string `json:"applyError,omitempty"`
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
	RecordCacheTailToleranceSec      int                                    `json:"recordCacheTailToleranceSec"`
	SIPLog                           SIPLogUpdateResult                     `json:"sipLog"`
	Cascade                          *gbconfig.CascadeSettings              `json:"cascade,omitempty"`
	DeviceAccess                     DeviceAccessConfig                     `json:"deviceAccess"`
	recordCacheTailTolerancePresent  bool
	// deviceAccessPresent / deviceAccess*Present 是三级 presence：
	// 整个 deviceAccess 缺失（老前端整包 PUT）⇒ 两项都保留当前值；
	// 对象在但某字段缺失 ⇒ 只保留那一项；字段存在且为空 ⇒ 按"清空"执行。
	deviceAccessPresent               bool
	deviceAccessQRProvisionPresent    bool
	deviceAccessSnapshotUploadPresent bool
}

// UnmarshalJSON remembers whether the optional compatibility field was sent.
// Older clients may submit the aggregate without it; those requests retain the
// current value instead of turning the tolerance into an unintended zero.
func (s *ServiceConfig) UnmarshalJSON(data []byte) error {
	type serviceConfigAlias ServiceConfig
	var decoded serviceConfigAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*s = ServiceConfig(decoded)
	_, s.recordCacheTailTolerancePresent = fields["recordCacheTailToleranceSec"]
	// ⛔ 只有 deviceAccess 是**对象**才算"传了"：把它写成 null 或省略时清空两项是
	// 灾难性误伤（老前端/部分客户端会带 null），所以 null 归入"没传"。
	rawDeviceAccess := fields["deviceAccess"]
	var accessFields map[string]json.RawMessage
	if err := json.Unmarshal(rawDeviceAccess, &accessFields); err == nil && accessFields != nil {
		s.deviceAccessPresent = true
		_, s.deviceAccessQRProvisionPresent = accessFields["qrProvisionBaseUrl"]
		_, s.deviceAccessSnapshotUploadPresent = accessFields["snapshotUploadBaseUrl"]
	}
	return nil
}

func (sc *ServiceConfigController) GetServiceConfig(c *gin.Context) {
	sc.Success(c, sc.currentServiceConfig())
}

func (sc *ServiceConfigController) currentServiceConfig() ServiceConfig {
	position := PositionHistoryConfig{Enabled: false, RetentionDays: defaultPositionHistoryRetentionDays}
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
	cascade := gbconfig.CurrentCascadeSettings()
	deviceAccess := currentDeviceAccessConfig()
	return ServiceConfig{
		PositionHistory: position, CloudRecordingRetention: CloudRecordingRetentionConfig{RetentionDays: cloud},
		SDPExtension:         SDPExtensionConfig{Enabled: gbconfig.SDPExtensionEnabled()},
		SyncChannelsOnOnline: SyncChannelsOnOnlineConfig{Enabled: gbconfig.SyncChannelsOnOnline()}, OnlineOnHeartbeat: OnlineOnHeartbeatConfig{Enabled: gbconfig.OnlineOnHeartbeat()},
		SaveAlarmMessages: SaveAlarmMessagesConfig{Enabled: gbconfig.SaveAlarmMessages()}, SIPCommandTimeout: SIPCommandTimeoutConfig{TimeoutSec: gbconfig.SIPCommandTimeoutSec()},
		PreallocationMode: PreallocationModeConfig{Enabled: gbconfig.PreallocationMode()}, IgnoreChannelOfflineStatusNotify: IgnoreChannelOfflineStatusNotifyConfig{Enabled: gbconfig.IgnoreChannelOfflineStatusNotify()},
		PTZDefaultSpeed: PTZDefaultSpeedConfig{Level: currentPTZDefaultSpeedLevel()}, DefaultChannelStreamTransport: DefaultChannelStreamTransportConfig{Transport: gbconfig.CurrentDefaultChannelStreamTransport()},
		DefaultPlaybackProtocol: DefaultPlaybackProtocolConfig{Protocol: gbconfig.CurrentDefaultPlaybackProtocol()}, GlobalSubscriptions: GlobalSubscriptionConfig{Items: gbconfig.GlobalSubscriptionItems()},
		DefaultChannelAudio: DefaultChannelAudioConfig{Enabled: gbconfig.DefaultChannelAudioEnabled()}, PlaybackSettings: gbconfig.CurrentPlaybackSettings(), FixedAddressPlayback: gbconfig.CurrentFixedAddressPlaybackSettings(),
		PlayAuth: gbconfig.CurrentPlayAuthSettings(), RecordCacheTailToleranceSec: gbconfig.CurrentRecordCacheTailToleranceSec(), SIPLog: SIPLogUpdateResult{Enabled: gbconfig.SIPTraceEnabled(), Applied: sc.sipTraceApplied(gbconfig.SIPTraceEnabled())},
		Cascade:      &cascade,
		DeviceAccess: deviceAccess,
	}
}

// currentDeviceAccessConfig 读原始配置值（不是 EffectiveBaseURL）：
// 页面要显示"现场实际写的是什么"，好让运维判断它是不是设备可达的地址；
// 拼装出来的 host+port 形式若被回显并保存，会把两套写法混进配置文件。
func currentDeviceAccessConfig() DeviceAccessConfig {
	if app.ConfigYml == nil {
		return DeviceAccessConfig{}
	}
	return DeviceAccessConfig{
		QRProvisionBaseURL:    strings.TrimSpace(app.ConfigYml.GetString(gbconfig.QRProvisionBaseURLConfigKey)),
		SnapshotUploadBaseURL: strings.TrimSpace(app.ConfigYml.GetString(gbconfig.SnapshotUploadBaseURLConfigKey)),
	}
}

func (sc *ServiceConfigController) UpdateServiceConfig(c *gin.Context) {
	var request ServiceConfig
	if err := c.ShouldBindJSON(&request); err != nil {
		sc.Fail(c, "保存国标服务配置失败：请求格式无效", err, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}
	if !request.recordCacheTailTolerancePresent {
		request.RecordCacheTailToleranceSec = gbconfig.CurrentRecordCacheTailToleranceSec()
	}
	request.DeviceAccess = mergeDeviceAccess(request)
	if err := validateServiceConfig(request); err != nil {
		sc.Fail(c, "保存国标服务配置失败："+err.Error(), err, http.StatusBadRequest)
		return
	}
	sc.playbackSettingsMu.Lock()
	defer sc.playbackSettingsMu.Unlock()
	unlock := logcleanup.LockConfig()
	previous := sc.currentServiceConfig()
	if request.Cascade == nil {
		request.Cascade = previous.Cascade
	}
	applyServiceConfig(request)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		applyServiceConfig(previous)
		unlock()
		sc.Fail(c, "保存国标服务配置失败", err, http.StatusInternalServerError)
		return
	}
	if sc.playAuthTTLUpdater != nil && previous.PlayAuth.TTLSeconds != request.PlayAuth.TTLSeconds {
		if err := sc.playAuthTTLUpdater(time.Duration(request.PlayAuth.TTLSeconds) * time.Second); err != nil {
			applyServiceConfig(previous)
			_ = app.ConfigYml.SaveConfig()
			unlock()
			sc.Fail(c, "国标服务配置已保存，但播放鉴权有效期未应用", err, http.StatusInternalServerError)
			return
		}
	}
	unlock()
	if previous.SIPLog.Enabled != request.SIPLog.Enabled {
		if sc.reload != nil {
			if err := sc.reload(); err != nil {
				request.SIPLog.Applied = false
				request.SIPLog.ApplyError = err.Error()
			}
		}
	}
	sc.SuccessWithMessage(c, "国标服务配置已更新", request)
}

// mergeDeviceAccess 按 presence 决定"采用请求值"还是"保留当前值"。
//
// ⛔⛔ 老前端/第三方客户端整包 PUT 时不带 deviceAccess ⇒ 必须保留当前值。否则一次
// 保存就会把现场配好的公网地址清成空：扫码立刻失效（出码 503），抓拍回传退回
// 请求 Host 派生（自签 HTTPS / 丢端口，2026-10-20 现场就是这么静默失效的）。
func mergeDeviceAccess(request ServiceConfig) DeviceAccessConfig {
	current := currentDeviceAccessConfig()
	if !request.deviceAccessPresent {
		return current
	}
	merged := current
	if request.deviceAccessQRProvisionPresent {
		merged.QRProvisionBaseURL = normalizeQRProvisionBaseURL(request.DeviceAccess.QRProvisionBaseURL)
	}
	if request.deviceAccessSnapshotUploadPresent {
		merged.SnapshotUploadBaseURL = normalizeSnapshotUploadBaseURL(request.DeviceAccess.SnapshotUploadBaseURL)
	}
	return merged
}

// normalizeQRProvisionBaseURL / normalizeSnapshotUploadBaseURL 把页面输入的地址规范成
// 入库形态（去尾斜杠、scheme 小写），用的是与校验、运行时**同一个** EffectiveBaseURL。
//
// ⛔ 非法值原样返回，交给 validateDeviceAccessBaseURLs 报错 —— 这里静默兜底会让用户在
// 页面上看到「保存成功」而实际地址没被接受。
func normalizeQRProvisionBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	cfg := gbconfig.QRProvisionConfig{BaseURL: raw, RequireTLS: gbconfig.QRProvisionConfigFrom(app.ConfigYml).RequireTLS}
	if normalized, err := cfg.EffectiveBaseURL(); err == nil {
		return normalized
	}
	return raw
}

func normalizeSnapshotUploadBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	cfg := gbconfig.SnapshotUploadConfig{BaseURL: raw}
	if normalized, err := cfg.EffectiveBaseURL(); err == nil {
		return normalized
	}
	return raw
}

// validateDeviceAccessBaseURLs 只校验**请求显式提交**的字段，且只在非空时校验
// （留空是合法的"未配置"）。
//
// ⛔⛔ 为什么按 presence 而不是按合并后的值校验：配置文件里的历史值可能本来就非法
// （现场手改过）。若把它也纳入校验，操作员改任何一个别的配置项都会被这个历史值
// 卡住 —— 那是"改 A 被 B 拦住"的经典死锁。
// ⛔⛔ 校验规则**只复用**两个 EffectiveBaseURL，绝不另写一套：页面放行了后端却拒发
// （或反之），是这类"设备可达地址"最典型的坑。扫码的 RequireTLS 一并带上，避免
// 在"已要求 TLS"的部署里从页面塞进一个明文地址。
func validateDeviceAccessBaseURLs(request ServiceConfig) error {
	if !request.deviceAccessPresent {
		return nil
	}
	if request.deviceAccessQRProvisionPresent {
		qr := gbconfig.QRProvisionConfig{
			BaseURL:    strings.TrimSpace(request.DeviceAccess.QRProvisionBaseURL),
			RequireTLS: gbconfig.QRProvisionConfigFrom(app.ConfigYml).RequireTLS,
		}
		if qr.BaseURL != "" {
			if _, err := qr.EffectiveBaseURL(); err != nil {
				return fmt.Errorf("扫码配网地址：%w", err)
			}
		}
	}
	if request.deviceAccessSnapshotUploadPresent {
		snapshot := gbconfig.SnapshotUploadConfig{BaseURL: strings.TrimSpace(request.DeviceAccess.SnapshotUploadBaseURL)}
		if snapshot.BaseURL != "" {
			if _, err := snapshot.EffectiveBaseURL(); err != nil {
				return fmt.Errorf("抓拍回传地址：%w", err)
			}
		}
	}
	return nil
}

func validateServiceConfig(v ServiceConfig) error {
	if err := validateDeviceAccessBaseURLs(v); err != nil {
		return err
	}
	if v.Cascade != nil {
		if err := gbconfig.ValidateCascadeSettings(*v.Cascade); err != nil {
			return err
		}
	}
	if err := gbconfig.ValidatePlaybackSettings(v.PlaybackSettings); err != nil {
		return err
	}
	if err := gbconfig.ValidateFixedAddressPlaybackSettings(v.FixedAddressPlayback); err != nil {
		return err
	}
	if err := gbconfig.ValidatePlayAuthSettings(v.PlayAuth, v.FixedAddressPlayback); err != nil {
		return err
	}
	if err := gbconfig.ValidateRecordCacheTailToleranceSec(v.RecordCacheTailToleranceSec); err != nil {
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
	if v.SIPCommandTimeout.TimeoutSec < 1 || v.SIPCommandTimeout.TimeoutSec > gbconfig.MaxSIPCommandTimeoutSec {
		return errors.New("SIP命令超时时间无效")
	}
	return nil
}

func applyServiceConfig(v ServiceConfig) {
	if v.Cascade != nil {
		app.ConfigYml.Set(gbconfig.CascadeUserAgentConfigKey, v.Cascade.UserAgent)
		app.ConfigYml.Set(gbconfig.CascadePlatformNameConfigKey, v.Cascade.PlatformName)
		app.ConfigYml.Set(gbconfig.CascadeManufacturerConfigKey, v.Cascade.Manufacturer)
		app.ConfigYml.Set(gbconfig.CascadeModelConfigKey, v.Cascade.Model)
		app.ConfigYml.Set(gbconfig.CascadeCivilCodeConfigKey, v.Cascade.CivilCode)
		app.ConfigYml.Set(gbconfig.CascadeAddressConfigKey, v.Cascade.Address)
	}
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
	app.ConfigYml.Set(gbconfig.RecordCacheTailToleranceSecConfigKey, v.RecordCacheTailToleranceSec)
	app.ConfigYml.Set(gbconfig.SIPTraceEnabledConfigKey, v.SIPLog.Enabled)
	app.ConfigYml.Set(gbconfig.QRProvisionBaseURLConfigKey, strings.TrimSpace(v.DeviceAccess.QRProvisionBaseURL))
	app.ConfigYml.Set(gbconfig.SnapshotUploadBaseURLConfigKey, strings.TrimSpace(v.DeviceAccess.SnapshotUploadBaseURL))
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
