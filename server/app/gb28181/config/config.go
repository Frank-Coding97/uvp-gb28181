package config

import (
	"fmt"
	"strings"
	"time"

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
	SIPTraceEnabledConfigKey                  = "gb28181.trace.enabled"

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

	MaxRecordQueryTimeoutSec = 300
	MaxRecordQueryRangeHours = 168
	MaxRecordQueryActive     = 1024
	MaxRecordsPerQuery       = 100000
	MaxResultTTLSec          = 86400
	MaxMediaWaitSec          = 120
	MaxPlaybackIdleSec       = 3600
	MaxPlaybackSessionSec    = 86400
	MaxSIPCommandTimeoutSec  = 300
)

// SIPTraceEnabledFrom returns whether raw SIP trace collection is enabled.
// Missing configuration intentionally defaults to false because trace storage
// may contain sensitive signaling payloads and can incur ClickHouse writes.
func SIPTraceEnabledFrom(c valueSource) bool {
	return c != nil && c.Get(SIPTraceEnabledConfigKey) != nil && c.GetBool(SIPTraceEnabledConfigKey)
}

// SIPTraceEnabled reads the live configuration used by the service-config API.
func SIPTraceEnabled() bool {
	return SIPTraceEnabledFrom(app.ConfigYml)
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
	Address          string
	Database         string
	Username         string
	PasswordEnv      string
	TLS              bool
	QueueCapacity    int
	BatchSize        int
	FlushIntervalMS  int
	EncryptionKeyEnv string
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
	ReconcileIntervalSec int
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
			Address:          c.GetString("gb28181.trace.address"),
			Database:         c.GetString("gb28181.trace.database"),
			Username:         c.GetString("gb28181.trace.username"),
			PasswordEnv:      c.GetString("gb28181.trace.password_env"),
			TLS:              c.GetBool("gb28181.trace.tls"),
			QueueCapacity:    c.GetInt("gb28181.trace.queue_capacity"),
			BatchSize:        c.GetInt("gb28181.trace.batch_size"),
			FlushIntervalMS:  c.GetInt("gb28181.trace.flush_interval_ms"),
			EncryptionKeyEnv: c.GetString("gb28181.trace.encryption_key_env"),
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
			ReconcileIntervalSec: c.GetInt("gb28181.recording.reconcile_interval_sec"),
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
