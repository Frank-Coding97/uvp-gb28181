package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

const (
	SDPExtensionConfigKey                     = "gb28181.sdp.extension_enabled"
	SyncChannelsOnOnlineConfigKey             = "gb28181.device.sync_channels_on_online"
	OnlineOnHeartbeatConfigKey                = "gb28181.device.online_on_heartbeat"
	SaveAlarmMessagesConfigKey                = "gb28181.alarm.save_messages"
	SIPCommandTimeoutSecConfigKey             = "gb28181.sip_command_timeout_sec"
	PreallocationModeConfigKey                = "gb28181.device.preallocation_mode"
	IgnoreChannelOfflineStatusNotifyConfigKey = "gb28181.catalog.ignore_channel_offline_status_notify"
	DefaultChannelStreamTransportConfigKey    = "gb28181.catalog.default_channel_stream_transport"
	DefaultPlaybackProtocolConfigKey          = "gb28181.playback.default_protocol"
	DefaultChannelAudioEnabledConfigKey       = "gb28181.catalog.default_channel_audio_enabled"
	PlayRequestTimeoutMsConfigKey             = "gb28181.play.request_timeout_ms"
	DefaultChannelOnDemandLiveConfigKey       = "gb28181.catalog.default_channel_on_demand_live"
	DefaultChannelCloudRecordingConfigKey     = "gb28181.catalog.default_channel_cloud_recording_enabled"
	FixedAddressEnabledConfigKey              = "gb28181.play.fixed_address_enabled"
	AutoOnDemandEnabledConfigKey              = "gb28181.play.auto_on_demand_enabled"
	PlayAuthEnabledConfigKey                  = "gb28181.play.auth.enabled"
	PlayAuthBindClientIPConfigKey             = "gb28181.play.auth.bind_client_ip"
	PlayAuthTTLSecondsConfigKey               = "gb28181.play.auth.ttl_seconds"
	PlayAuthActiveKeyConfigKey                = "gb28181.play.auth.active_key"
	PlayAuthPreviousKeyConfigKey              = "gb28181.play.auth.previous_key"
	GlobalSubscriptionItemsConfigKey          = "gb28181.subscribe.global_items"
	SIPTraceEnabledConfigKey                  = "gb28181.trace.enabled"
	SIPTraceRetentionDaysConfigKey            = "gb28181.trace.retention_days"

	DefaultRecordQueryTimezone           = "Asia/Shanghai"
	DefaultRecordQueryTimeoutSec         = 15
	DefaultRecordQueryMaxRangeHours      = 24
	DefaultRecordQueryMaxActiveQueries   = 128
	DefaultRecordQueryMaxRecordsPerQuery = 20000
	DefaultRecordQueryResultTTLSec       = 1800
	DefaultPlaybackMediaWaitSec          = 10
	DefaultPlaybackIdleTimeoutSec        = 60
	DefaultPlaybackMaxSessionSec         = 86400
	DefaultSIPCommandTimeoutSec          = 10
	DefaultChannelStreamTransport        = "TCP-Passive"
	DefaultPlaybackProtocol              = "ws-flv"
	DefaultChannelAudioEnabledValue      = true
	DefaultPlayRequestTimeoutMs          = 10000
	MinPlayRequestTimeoutMs              = 1000
	MaxPlayRequestTimeoutMs              = 300000
	DefaultChannelOnDemandLive           = true
	DefaultChannelCloudRecordingEnabled  = false
	DefaultSIPTraceRetentionDays         = 7
	MinSIPTraceRetentionDays             = 1
	MaxSIPTraceRetentionDays             = 365

	MaxRecordQueryTimeoutSec = 300
	MaxRecordQueryRangeHours = 168
	MaxRecordQueryActive     = 1024
	MaxRecordsPerQuery       = 100000
	MaxResultTTLSec          = 86400
	MaxMediaWaitSec          = 120
	MaxPlaybackIdleSec       = 3600
	MaxPlaybackSessionSec    = 86400
	MaxSIPCommandTimeoutSec  = 300

	playAuthGeneratedKeyBytes = 32
	DefaultPlayAuthTTLSeconds = 120
	MinPlayAuthTTLSeconds     = 60
	MaxPlayAuthTTLSeconds     = 3600
)

var supportedGlobalSubscriptionItems = []string{"catalog", "mobile_position", "alarm", "ptz_precise_position"}

var fixedAddressPlaybackMu sync.RWMutex

// GlobalSubscriptionItemsFrom returns the subscription kinds used as defaults
// for devices that do not yet have a device-level subscription row.
// Missing or invalid entries are ignored, and the result is de-duplicated.
func GlobalSubscriptionItemsFrom(c valueSource) []string {
	if c == nil || c.Get(GlobalSubscriptionItemsConfigKey) == nil {
		return []string{}
	}
	allowed := make(map[string]struct{}, len(supportedGlobalSubscriptionItems))
	for _, item := range supportedGlobalSubscriptionItems {
		allowed[item] = struct{}{}
	}
	seen := make(map[string]struct{})
	items := make([]string, 0, len(supportedGlobalSubscriptionItems))
	for _, item := range c.GetStringSlice(GlobalSubscriptionItemsConfigKey) {
		if _, ok := allowed[item]; !ok {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		items = append(items, item)
	}
	return items
}

func GlobalSubscriptionItems() []string {
	return GlobalSubscriptionItemsFrom(app.ConfigYml)
}

func IsSupportedGlobalSubscriptionItem(item string) bool {
	for _, supported := range supportedGlobalSubscriptionItems {
		if item == supported {
			return true
		}
	}
	return false
}

// DefaultChannelAudioEnabledFrom returns the audio default for newly created
// Catalog channels. Missing configuration preserves the historical behavior.
func DefaultChannelAudioEnabledFrom(c valueSource) bool {
	if c == nil || c.Get(DefaultChannelAudioEnabledConfigKey) == nil {
		return DefaultChannelAudioEnabledValue
	}
	return c.GetBool(DefaultChannelAudioEnabledConfigKey)
}

func DefaultChannelAudioEnabled() bool {
	return DefaultChannelAudioEnabledFrom(app.ConfigYml)
}

// PlaybackSettings contains global playback behavior used as defaults for
// new channels and as the live timeout budget for real-time play requests.
type PlaybackSettings struct {
	PlayTimeoutMs         int  `json:"playTimeoutMs"`
	OnDemandLive          bool `json:"onDemandLive"`
	CloudRecordingEnabled bool `json:"cloudRecordingEnabled"`
}

func (s PlaybackSettings) PlayTimeout() time.Duration {
	return time.Duration(s.PlayTimeoutMs) * time.Millisecond
}

// PlaybackSettingsFrom reads the current values on every call. Missing or
// invalid timeout values fall back to the documented default while boolean
// values preserve an explicitly saved false.
func PlaybackSettingsFrom(c valueSource) PlaybackSettings {
	settings := PlaybackSettings{
		PlayTimeoutMs:         DefaultPlayRequestTimeoutMs,
		OnDemandLive:          DefaultChannelOnDemandLive,
		CloudRecordingEnabled: DefaultChannelCloudRecordingEnabled,
	}
	if c == nil {
		return settings
	}
	if c.Get(PlayRequestTimeoutMsConfigKey) != nil {
		value := c.GetInt(PlayRequestTimeoutMsConfigKey)
		if value >= MinPlayRequestTimeoutMs && value <= MaxPlayRequestTimeoutMs {
			settings.PlayTimeoutMs = value
		}
	}
	if c.Get(DefaultChannelOnDemandLiveConfigKey) != nil {
		settings.OnDemandLive = c.GetBool(DefaultChannelOnDemandLiveConfigKey)
	}
	if c.Get(DefaultChannelCloudRecordingConfigKey) != nil {
		settings.CloudRecordingEnabled = c.GetBool(DefaultChannelCloudRecordingConfigKey)
	}
	return settings
}

func CurrentPlaybackSettings() PlaybackSettings {
	return PlaybackSettingsFrom(app.ConfigYml)
}

// ValidatePlaybackSettings validates the complete aggregate submitted by the
// service-config API before any of its values are persisted.
func ValidatePlaybackSettings(settings PlaybackSettings) error {
	if settings.PlayTimeoutMs < MinPlayRequestTimeoutMs || settings.PlayTimeoutMs > MaxPlayRequestTimeoutMs {
		return invalid(PlayRequestTimeoutMsConfigKey, settings.PlayTimeoutMs,
			fmt.Sprintf("must be between %d and %d", MinPlayRequestTimeoutMs, MaxPlayRequestTimeoutMs))
	}
	return nil
}

type FixedAddressPlaybackSettings struct {
	FixedAddressEnabled bool `json:"fixedAddressEnabled"`
	AutoOnDemandEnabled bool `json:"autoOnDemandEnabled"`
}

// PlayAuthSettings is the public playback authorization policy. Key material
// is intentionally excluded from this DTO and is only read during bootstrap.
type PlayAuthSettings struct {
	Enabled      bool `json:"authEnabled"`
	BindClientIP bool `json:"authBindClientIP"`
	TTLSeconds   int  `json:"authTTLSeconds"`
}

func PlayAuthSettingsFrom(c valueSource) PlayAuthSettings {
	if c == nil {
		return PlayAuthSettings{TTLSeconds: DefaultPlayAuthTTLSeconds}
	}
	return PlayAuthSettings{
		Enabled:      c.Get(PlayAuthEnabledConfigKey) != nil && c.GetBool(PlayAuthEnabledConfigKey),
		BindClientIP: c.Get(PlayAuthBindClientIPConfigKey) != nil && c.GetBool(PlayAuthBindClientIPConfigKey),
		TTLSeconds:   playAuthTTLSecondsFrom(c),
	}
}

func playAuthTTLSecondsFrom(c valueSource) int {
	if c == nil || c.Get(PlayAuthTTLSecondsConfigKey) == nil {
		return DefaultPlayAuthTTLSeconds
	}
	ttl := c.GetInt(PlayAuthTTLSecondsConfigKey)
	if ttl < MinPlayAuthTTLSeconds || ttl > MaxPlayAuthTTLSeconds {
		return DefaultPlayAuthTTLSeconds
	}
	return ttl
}

func CurrentPlayAuthSettings() PlayAuthSettings {
	fixedAddressPlaybackMu.RLock()
	defer fixedAddressPlaybackMu.RUnlock()
	return PlayAuthSettingsFrom(app.ConfigYml)
}

func ValidatePlayAuthSettings(settings PlayAuthSettings, fixed FixedAddressPlaybackSettings) error {
	if settings.TTLSeconds < MinPlayAuthTTLSeconds || settings.TTLSeconds > MaxPlayAuthTTLSeconds {
		return invalid(PlayAuthTTLSecondsConfigKey, settings.TTLSeconds,
			fmt.Sprintf("must be between %d and %d", MinPlayAuthTTLSeconds, MaxPlayAuthTTLSeconds))
	}
	if settings.BindClientIP && !settings.Enabled {
		return invalid(PlayAuthBindClientIPConfigKey, true, "requires play authorization enabled")
	}
	return nil
}

func SavePlayAuthSettings(c mutableValueSource, settings PlayAuthSettings) error {
	if c == nil {
		return fmt.Errorf("配置服务尚未初始化")
	}
	fixedAddressPlaybackMu.Lock()
	defer fixedAddressPlaybackMu.Unlock()
	if err := ValidatePlayAuthSettings(settings, FixedAddressPlaybackSettingsFrom(c)); err != nil {
		return err
	}
	previous := PlayAuthSettingsFrom(c)
	c.Set(PlayAuthEnabledConfigKey, settings.Enabled)
	c.Set(PlayAuthBindClientIPConfigKey, settings.BindClientIP)
	c.Set(PlayAuthTTLSecondsConfigKey, settings.TTLSeconds)
	if err := c.SaveConfig(); err != nil {
		c.Set(PlayAuthEnabledConfigKey, previous.Enabled)
		c.Set(PlayAuthBindClientIPConfigKey, previous.BindClientIP)
		c.Set(PlayAuthTTLSecondsConfigKey, previous.TTLSeconds)
		return err
	}
	return nil
}

func PlayAuthKeyMaterialFrom(c valueSource) (active, previous string) {
	if c == nil {
		return "", ""
	}
	return strings.TrimSpace(c.GetString(PlayAuthActiveKeyConfigKey)), strings.TrimSpace(c.GetString(PlayAuthPreviousKeyConfigKey))
}

// EnsurePlayAuthActiveKey creates the server-owned playback authorization key
// on first initialization. The key is never exposed through the public config
// DTO and existing key material is preserved for rotation compatibility.
func EnsurePlayAuthActiveKey(c mutableValueSource) error {
	if c == nil {
		return fmt.Errorf("配置服务尚未初始化")
	}
	fixedAddressPlaybackMu.Lock()
	defer fixedAddressPlaybackMu.Unlock()
	return ensurePlayAuthActiveKey(c, rand.Reader)
}

func ensurePlayAuthActiveKey(c mutableValueSource, reader io.Reader) error {
	if strings.TrimSpace(c.GetString(PlayAuthActiveKeyConfigKey)) != "" {
		return nil
	}
	if reader == nil {
		return fmt.Errorf("生成播放鉴权密钥失败: 随机源为空")
	}

	keyBytes := make([]byte, playAuthGeneratedKeyBytes)
	if _, err := io.ReadFull(reader, keyBytes); err != nil {
		return fmt.Errorf("生成播放鉴权密钥失败: %w", err)
	}
	generated := base64.RawURLEncoding.EncodeToString(keyBytes)
	previous := c.Get(PlayAuthActiveKeyConfigKey)
	c.Set(PlayAuthActiveKeyConfigKey, generated)
	if err := c.SaveConfig(); err != nil {
		c.Set(PlayAuthActiveKeyConfigKey, previous)
		return fmt.Errorf("保存播放鉴权密钥失败: %w", err)
	}
	return nil
}

func FixedAddressPlaybackSettingsFrom(c valueSource) FixedAddressPlaybackSettings {
	if c == nil {
		return FixedAddressPlaybackSettings{}
	}
	settings := FixedAddressPlaybackSettings{}
	if c.Get(FixedAddressEnabledConfigKey) != nil {
		settings.FixedAddressEnabled = c.GetBool(FixedAddressEnabledConfigKey)
	}
	if c.Get(AutoOnDemandEnabledConfigKey) != nil {
		settings.AutoOnDemandEnabled = c.GetBool(AutoOnDemandEnabledConfigKey)
	}
	return settings
}

func ValidateFixedAddressPlaybackSettings(settings FixedAddressPlaybackSettings) error {
	if settings.AutoOnDemandEnabled && !settings.FixedAddressEnabled {
		return invalid(AutoOnDemandEnabledConfigKey, true, "requires fixed_address_enabled=true")
	}
	return nil
}

// CurrentFixedAddressPlaybackSettings shares the same lock as the aggregate
// save path, so runtime consumers can only observe a complete old or new pair.
func CurrentFixedAddressPlaybackSettings() FixedAddressPlaybackSettings {
	fixedAddressPlaybackMu.RLock()
	defer fixedAddressPlaybackMu.RUnlock()
	return FixedAddressPlaybackSettingsFrom(app.ConfigYml)
}

type mutableValueSource interface {
	valueSource
	Set(string, interface{})
	SaveConfig() error
}

func SaveFixedAddressPlaybackSettings(c mutableValueSource, settings FixedAddressPlaybackSettings) error {
	if err := ValidateFixedAddressPlaybackSettings(settings); err != nil {
		return err
	}
	if c == nil {
		return fmt.Errorf("配置服务尚未初始化")
	}

	fixedAddressPlaybackMu.Lock()
	defer fixedAddressPlaybackMu.Unlock()
	previous := FixedAddressPlaybackSettingsFrom(c)
	c.Set(FixedAddressEnabledConfigKey, settings.FixedAddressEnabled)
	c.Set(AutoOnDemandEnabledConfigKey, settings.AutoOnDemandEnabled)
	if err := c.SaveConfig(); err != nil {
		c.Set(FixedAddressEnabledConfigKey, previous.FixedAddressEnabled)
		c.Set(AutoOnDemandEnabledConfigKey, previous.AutoOnDemandEnabled)
		return err
	}
	return nil
}

// SIPTraceEnabledFrom returns whether raw SIP trace collection is enabled.
// Missing configuration intentionally defaults to false because trace storage
// may contain sensitive signaling payloads and can incur business-database writes.
func SIPTraceEnabledFrom(c valueSource) bool {
	return c != nil && c.Get(SIPTraceEnabledConfigKey) != nil && c.GetBool(SIPTraceEnabledConfigKey)
}

// SIPTraceEnabled reads the live configuration used by the service-config API.
func SIPTraceEnabled() bool {
	return SIPTraceEnabledFrom(app.ConfigYml)
}

func SIPTraceRetentionDaysFrom(c valueSource) int {
	if c == nil || c.Get(SIPTraceRetentionDaysConfigKey) == nil {
		return DefaultSIPTraceRetentionDays
	}
	days := c.GetInt(SIPTraceRetentionDaysConfigKey)
	if days < MinSIPTraceRetentionDays || days > MaxSIPTraceRetentionDays {
		return DefaultSIPTraceRetentionDays
	}
	return days
}

func SIPTraceRetentionDays() int {
	days := SIPTraceRetentionDaysFrom(app.ConfigYml)
	if app.ConfigYml != nil && app.ConfigYml.Get(SIPTraceRetentionDaysConfigKey) != nil {
		configured := app.ConfigYml.GetInt(SIPTraceRetentionDaysConfigKey)
		if configured < MinSIPTraceRetentionDays || configured > MaxSIPTraceRetentionDays {
			if app.ZapLog != nil {
				app.ZapLog.Warn("SIP trace retention days is invalid; using default", zap.Int("configured", configured), zap.Int("default", days))
			}
		}
	}
	return days
}

// SDPExtensionEnabledFrom returns the current SDP compatibility setting.
// Missing configuration intentionally defaults to false.
func SDPExtensionEnabledFrom(c valueSource) bool {
	return c != nil && c.Get(SDPExtensionConfigKey) != nil && c.GetBool(SDPExtensionConfigKey)
}

// SDPExtensionEnabled reads the live configuration so newly created INVITEs
// pick up a saved setting without rebuilding the GB28181 runtime.
func SDPExtensionEnabled() bool {
	return SDPExtensionEnabledFrom(app.ConfigYml)
}

// SyncChannelsOnOnlineFrom returns whether device registration/recovery should
// trigger a Catalog query. Missing configuration intentionally defaults to true
// to preserve the historical behavior.
func SyncChannelsOnOnlineFrom(c valueSource) bool {
	return c == nil || c.Get(SyncChannelsOnOnlineConfigKey) == nil || c.GetBool(SyncChannelsOnOnlineConfigKey)
}

// SyncChannelsOnOnline reads the live configuration so the next online event
// observes a saved setting without rebuilding the GB28181 runtime.
func SyncChannelsOnOnline() bool {
	return SyncChannelsOnOnlineFrom(app.ConfigYml)
}

// OnlineOnHeartbeatFrom returns whether a Keepalive should restore an offline
// device to online. Missing configuration defaults to true to preserve the
// historical behavior.
func OnlineOnHeartbeatFrom(c valueSource) bool {
	return c == nil || c.Get(OnlineOnHeartbeatConfigKey) == nil || c.GetBool(OnlineOnHeartbeatConfigKey)
}

// OnlineOnHeartbeat reads the live setting for the next device Keepalive.
func OnlineOnHeartbeat() bool {
	return OnlineOnHeartbeatFrom(app.ConfigYml)
}

// SaveAlarmMessagesFrom returns whether received alarm notifications should be
// persisted. Missing configuration defaults to true to preserve the historical
// behavior.
func SaveAlarmMessagesFrom(c valueSource) bool {
	return c == nil || c.Get(SaveAlarmMessagesConfigKey) == nil || c.GetBool(SaveAlarmMessagesConfigKey)
}

// SaveAlarmMessages reads the live setting for the next alarm notification.
func SaveAlarmMessages() bool {
	return SaveAlarmMessagesFrom(app.ConfigYml)
}

// SIPCommandTimeoutSecFrom returns the default timeout for outbound SIP
// transactions. Missing or invalid configuration falls back to ten seconds.
func SIPCommandTimeoutSecFrom(c valueSource) int {
	if c == nil || c.Get(SIPCommandTimeoutSecConfigKey) == nil {
		return DefaultSIPCommandTimeoutSec
	}
	value := c.GetInt(SIPCommandTimeoutSecConfigKey)
	if value < 1 || value > MaxSIPCommandTimeoutSec {
		return DefaultSIPCommandTimeoutSec
	}
	return value
}

func SIPCommandTimeoutSec() int {
	return SIPCommandTimeoutSecFrom(app.ConfigYml)
}

func SIPCommandTimeout() time.Duration {
	return time.Duration(SIPCommandTimeoutSec()) * time.Second
}

// PreallocationModeFrom returns whether REGISTER may only update devices that
// were created in advance. Missing configuration defaults to false.
func PreallocationModeFrom(c valueSource) bool {
	return c != nil && c.Get(PreallocationModeConfigKey) != nil && c.GetBool(PreallocationModeConfigKey)
}

func PreallocationMode() bool {
	return PreallocationModeFrom(app.ConfigYml)
}

// IgnoreChannelOfflineStatusNotifyFrom returns whether negative channel
// status events from Catalog NOTIFY should be ignored. Missing configuration
// defaults to false so standards-compliant status notifications remain active.
func IgnoreChannelOfflineStatusNotifyFrom(c valueSource) bool {
	return c != nil && c.Get(IgnoreChannelOfflineStatusNotifyConfigKey) != nil && c.GetBool(IgnoreChannelOfflineStatusNotifyConfigKey)
}

// IgnoreChannelOfflineStatusNotify reads the live compatibility setting.
func IgnoreChannelOfflineStatusNotify() bool {
	return IgnoreChannelOfflineStatusNotifyFrom(app.ConfigYml)
}

func IsSupportedChannelStreamTransport(transport string) bool {
	switch transport {
	case "UDP", "TCP-Active", "TCP-Passive":
		return true
	default:
		return false
	}
}

// DefaultChannelStreamTransportFrom returns the transport assigned when a
// Catalog response creates a channel. Existing channels keep their saved value.
func DefaultChannelStreamTransportFrom(c valueSource) string {
	if c == nil || c.Get(DefaultChannelStreamTransportConfigKey) == nil {
		return DefaultChannelStreamTransport
	}
	transport := c.GetString(DefaultChannelStreamTransportConfigKey)
	if !IsSupportedChannelStreamTransport(transport) {
		return DefaultChannelStreamTransport
	}
	return transport
}

func CurrentDefaultChannelStreamTransport() string {
	return DefaultChannelStreamTransportFrom(app.ConfigYml)
}

func IsSupportedPlaybackProtocol(protocol string) bool {
	switch protocol {
	case "ws-flv", "http-flv", "hls", "webrtc":
		return true
	default:
		return false
	}
}

// DefaultPlaybackProtocolFrom returns the preferred protocol for newly
// created playback sessions. Existing sessions keep their selected protocol.
func DefaultPlaybackProtocolFrom(c valueSource) string {
	if c == nil || c.Get(DefaultPlaybackProtocolConfigKey) == nil {
		return DefaultPlaybackProtocol
	}
	protocol := c.GetString(DefaultPlaybackProtocolConfigKey)
	if !IsSupportedPlaybackProtocol(protocol) {
		return DefaultPlaybackProtocol
	}
	return protocol
}

func CurrentDefaultPlaybackProtocol() string {
	return DefaultPlaybackProtocolFrom(app.ConfigYml)
}

// Config GB28181 国标平台配置
type Config struct {
	Enabled     bool
	SIP         SIPConfig
	Trace       TraceConfig
	Device      DeviceConfig
	ZLM         ZLMConfig
	Media       MediaConfig
	Play        PlayConfig
	Recording   RecordingConfig
	RecordQuery RecordQueryConfig
	Playback    PlaybackConfig
}

// RecordQueryConfig controls device-local RecordInfo queries.
type RecordQueryConfig struct {
	Timezone           string
	Location           *time.Location
	TimeoutSec         int
	MaxRangeHours      int
	MaxActiveQueries   int
	MaxRecordsPerQuery int
	ResultTTLSec       int
}

func (c RecordQueryConfig) Timeout() time.Duration { return time.Duration(c.TimeoutSec) * time.Second }
func (c RecordQueryConfig) ResultTTL() time.Duration {
	return time.Duration(c.ResultTTLSec) * time.Second
}

// PlaybackConfig controls device-local playback session lifetimes.
type PlaybackConfig struct {
	MediaWaitSec   int
	IdleTimeoutSec int
	MaxSessionSec  int
}

func (c PlaybackConfig) MediaWait() time.Duration {
	return time.Duration(c.MediaWaitSec) * time.Second
}
func (c PlaybackConfig) IdleTimeout() time.Duration {
	return time.Duration(c.IdleTimeoutSec) * time.Second
}
func (c PlaybackConfig) MaxSession() time.Duration {
	return time.Duration(c.MaxSessionSec) * time.Second
}

// ValidationError identifies the exact configuration field that prevents
// record query/playback runtime assembly.
type ValidationError struct {
	Field  string
	Value  interface{}
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid GB28181 configuration %s=%v: %s", e.Field, e.Value, e.Reason)
}

// TraceConfig controls the optional SIP trace module.
type TraceConfig struct {
	Enabled          bool
	QueueCapacity    int
	BatchSize        int
	FlushIntervalMS  int
	EncryptionKeyEnv string
	RetentionDays    int
}

// ZLMConfig ZLMediaKit 媒体服务器配置(数据面)
type ZLMConfig struct {
	Host         string // ZLM API 地址
	ReceiveHost  string // 设备收流地址,写入 SDP 的 c= 地址
	PlaybackHost string // 播放访问地址,返回给浏览器/客户端
	HTTPPort     int    // ZLM HTTP API 端口
	Secret       string // API secret
	RTPPort      int    // RTP 单端口收流
}

func (c ZLMConfig) EffectiveReceiveHost() string {
	if host := strings.TrimSpace(c.ReceiveHost); host != "" {
		return host
	}
	return c.Host
}

func (c ZLMConfig) EffectivePlaybackHost() string {
	if host := strings.TrimSpace(c.PlaybackHost); host != "" {
		return host
	}
	return c.Host
}

// MediaConfig 媒体/Hook 配置
type MediaConfig struct {
	HookHost                string // ZLM Hook 回调可达的本机地址
	HookPort                int    // 后端 HTTP 端口(Hook 端点)
	StreamNoneReaderTimeout int    // 无人观看断流秒数
	RTPServerTimeout        int    // RTP 收流超时(秒)
}

// PlayConfig 点播运行时配置
type PlayConfig struct {
	// ReconcileIntervalSec 兜底对账 goroutine 扫描周期(秒).
	// 0 = 禁用 reconciler(适合开发/调试场景).
	// 默认 300(5 分钟).
	ReconcileIntervalSec int
}

type RecordingConfig struct {
	// ReconcileIntervalSec 云端录像状态对账周期。0 仅禁用周期任务，手动开关仍可用。
	ReconcileIntervalSec        int
	CatalogReconcileIntervalSec int
	CatalogPeriodicLookbackDays int
	CatalogManualLookbackDays   int
	CapabilityKeyEnv            string
}

// SIPConfig SIP 服务配置
type SIPConfig struct {
	ListenIP         string
	AdvertiseIP      string
	DynamicAdvertise bool
	Port             int
	Transport        []string // 信令传输: udp / tcp
	Domain           string   // SIP 域(前 10 位行政区划)
	ServerID         string   // 平台国标编码(20 位)
	Password         string   // 统一接入密码
	XGBVersion       string   // 平台 REGISTER 响应声明的 X-GB-Ver,为空时默认 3.0
}

// DeviceConfig 设备相关配置
type DeviceConfig struct {
	KeepaliveInterval     int // 心跳周期(秒)
	KeepaliveTimeoutCount int // 连续丢失阈值
	KeepaliveGraceSeconds int // 离线判定宽限缓冲(秒),避开边界误判
	OfflineScanInterval   int // 离线扫描周期(秒)
}

// Load 从全局 ConfigYml 读取 gb28181 配置
type valueSource interface {
	Get(string) interface{}
	GetBool(string) bool
	GetString(string) string
	GetInt(string) int
	GetStringSlice(string) []string
}

func stringValue(c valueSource, key, fallback string) string {
	if c.Get(key) == nil {
		return fallback
	}
	return c.GetString(key)
}

func intValue(c valueSource, key string, fallback int) int {
	if c.Get(key) == nil {
		return fallback
	}
	return c.GetInt(key)
}

// LoadFrom reads GB28181 settings from a Viper-compatible value source.
func LoadFrom(c valueSource) Config {
	cfg, _ := loadFrom(c)
	return cfg
}

// LoadValidatedFrom loads and validates record query/playback settings. Callers
// assembling those runtimes must use this entry point and stop on error.
func LoadValidatedFrom(c valueSource) (Config, error) {
	return loadFrom(c)
}

func loadFrom(c valueSource) (Config, error) {
	listenIP := c.GetString("gb28181.sip.ip")
	advertiseIP := c.GetString("gb28181.sip.advertiseip")
	if advertiseIP == "" && listenIP != "0.0.0.0" {
		advertiseIP = listenIP
	}
	cfg := Config{
		Enabled: c.GetBool("gb28181.enabled"),
		SIP: SIPConfig{
			ListenIP:    listenIP,
			AdvertiseIP: advertiseIP,
			Port:        c.GetInt("gb28181.sip.port"),
			Transport:   c.GetStringSlice("gb28181.sip.transport"),
			Domain:      c.GetString("gb28181.sip.domain"),
			ServerID:    c.GetString("gb28181.sip.serverid"),
			Password:    c.GetString("gb28181.sip.password"),
			XGBVersion:  c.GetString("gb28181.sip.x_gb_ver"),
		},
		Trace: TraceConfig{
			Enabled:          c.GetBool("gb28181.trace.enabled"),
			QueueCapacity:    c.GetInt("gb28181.trace.queue_capacity"),
			BatchSize:        c.GetInt("gb28181.trace.batch_size"),
			FlushIntervalMS:  c.GetInt("gb28181.trace.flush_interval_ms"),
			EncryptionKeyEnv: c.GetString("gb28181.trace.encryption_key_env"),
			RetentionDays:    SIPTraceRetentionDaysFrom(c),
		},
		Device: DeviceConfig{
			KeepaliveInterval:     c.GetInt("gb28181.device.keepalive_interval"),
			KeepaliveTimeoutCount: c.GetInt("gb28181.device.keepalive_timeout_count"),
			KeepaliveGraceSeconds: c.GetInt("gb28181.device.keepalive_grace_seconds"),
			OfflineScanInterval:   c.GetInt("gb28181.device.offline_scan_interval"),
		},
		ZLM: ZLMConfig{
			Host:         c.GetString("gb28181.zlm.host"),
			ReceiveHost:  c.GetString("gb28181.zlm.receivehost"),
			PlaybackHost: c.GetString("gb28181.zlm.playbackhost"),
			HTTPPort:     c.GetInt("gb28181.zlm.httpport"),
			Secret:       c.GetString("gb28181.zlm.secret"),
			RTPPort:      c.GetInt("gb28181.zlm.rtpport"),
		},
		Media: MediaConfig{
			HookHost:                c.GetString("gb28181.media.hookhost"),
			HookPort:                c.GetInt("gb28181.media.hookport"),
			StreamNoneReaderTimeout: c.GetInt("gb28181.media.streamnonereadertimeout"),
			RTPServerTimeout:        c.GetInt("gb28181.media.rtpservertimeout"),
		},
		Play: PlayConfig{
			ReconcileIntervalSec: c.GetInt("gb28181.play.reconcile_interval_sec"),
		},
		Recording: RecordingConfig{
			ReconcileIntervalSec:        c.GetInt("gb28181.recording.reconcile_interval_sec"),
			CatalogReconcileIntervalSec: intValue(c, "gb28181.recording.catalog_reconcile_interval_sec", 6*60*60),
			CatalogPeriodicLookbackDays: intValue(c, "gb28181.recording.catalog_periodic_lookback_days", 2),
			CatalogManualLookbackDays:   intValue(c, "gb28181.recording.catalog_manual_lookback_days", 7),
			CapabilityKeyEnv:            stringValue(c, "gb28181.recording.capability_key_env", "UVP_CLOUD_RECORDING_CAPABILITY_KEY"),
		},
		RecordQuery: RecordQueryConfig{
			Timezone:           stringValue(c, "gb28181.record_query.timezone", DefaultRecordQueryTimezone),
			TimeoutSec:         intValue(c, "gb28181.record_query.timeout_sec", DefaultRecordQueryTimeoutSec),
			MaxRangeHours:      intValue(c, "gb28181.record_query.max_range_hours", DefaultRecordQueryMaxRangeHours),
			MaxActiveQueries:   intValue(c, "gb28181.record_query.max_active_queries", DefaultRecordQueryMaxActiveQueries),
			MaxRecordsPerQuery: intValue(c, "gb28181.record_query.max_records_per_query", DefaultRecordQueryMaxRecordsPerQuery),
			ResultTTLSec:       intValue(c, "gb28181.record_query.result_ttl_sec", DefaultRecordQueryResultTTLSec),
		},
		Playback: PlaybackConfig{
			MediaWaitSec:   intValue(c, "gb28181.playback.media_wait_sec", DefaultPlaybackMediaWaitSec),
			IdleTimeoutSec: intValue(c, "gb28181.playback.idle_timeout_sec", DefaultPlaybackIdleTimeoutSec),
			MaxSessionSec:  intValue(c, "gb28181.playback.max_session_sec", DefaultPlaybackMaxSessionSec),
		},
	}
	if err := validateRecordRuntimeConfig(&cfg, c.GetInt("httpserver.handler_timeout")); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func validateRecordRuntimeConfig(cfg *Config, handlerTimeoutSec int) error {
	const timezoneKey = "gb28181.record_query.timezone"
	if cfg.RecordQuery.Timezone == "" || cfg.RecordQuery.Timezone == "Local" {
		return invalid(timezoneKey, cfg.RecordQuery.Timezone, "must be an explicit IANA time zone")
	}
	location, err := time.LoadLocation(cfg.RecordQuery.Timezone)
	if err != nil {
		return invalid(timezoneKey, cfg.RecordQuery.Timezone, "unknown IANA time zone")
	}
	cfg.RecordQuery.Location = location

	if err := bounded("gb28181.record_query.timeout_sec", cfg.RecordQuery.TimeoutSec, MaxRecordQueryTimeoutSec); err != nil {
		return err
	}
	if handlerTimeoutSec > 0 && cfg.RecordQuery.TimeoutSec >= handlerTimeoutSec {
		return invalid("gb28181.record_query.timeout_sec", cfg.RecordQuery.TimeoutSec, "must be below httpserver.handler_timeout")
	}
	checks := []struct {
		key   string
		value int
		max   int
	}{
		{"gb28181.record_query.max_range_hours", cfg.RecordQuery.MaxRangeHours, MaxRecordQueryRangeHours},
		{"gb28181.record_query.max_active_queries", cfg.RecordQuery.MaxActiveQueries, MaxRecordQueryActive},
		{"gb28181.record_query.max_records_per_query", cfg.RecordQuery.MaxRecordsPerQuery, MaxRecordsPerQuery},
		{"gb28181.record_query.result_ttl_sec", cfg.RecordQuery.ResultTTLSec, MaxResultTTLSec},
		{"gb28181.playback.media_wait_sec", cfg.Playback.MediaWaitSec, MaxMediaWaitSec},
		{"gb28181.playback.idle_timeout_sec", cfg.Playback.IdleTimeoutSec, MaxPlaybackIdleSec},
		{"gb28181.playback.max_session_sec", cfg.Playback.MaxSessionSec, MaxPlaybackSessionSec},
	}
	for _, check := range checks {
		if err := bounded(check.key, check.value, check.max); err != nil {
			return err
		}
	}
	if cfg.Playback.IdleTimeoutSec > cfg.Playback.MaxSessionSec {
		return invalid("gb28181.playback.idle_timeout_sec", cfg.Playback.IdleTimeoutSec, "must not exceed max_session_sec")
	}
	return nil
}

func bounded(field string, value, max int) error {
	if value <= 0 || value > max {
		return invalid(field, value, fmt.Sprintf("must be between 1 and %d", max))
	}
	return nil
}

func invalid(field string, value interface{}, reason string) error {
	return &ValidationError{Field: field, Value: value, Reason: reason}
}

// Load reads GB28181 settings from the global application configuration.
func Load() Config {
	return LoadFrom(app.ConfigYml)
}

// LoadValidated reads global configuration for record query/playback runtime assembly.
func LoadValidated() (Config, error) {
	return LoadValidatedFrom(app.ConfigYml)
}
