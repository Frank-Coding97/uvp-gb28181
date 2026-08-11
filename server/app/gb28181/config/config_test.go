package config

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/ymlconfig"
)

// serverConfigDir 从本测试文件位置回溯到 server/config 目录
// (测试 cwd 是包目录 app/gb28181/config,需回溯到 server 根)
func serverConfigDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	// thisFile = <server>/app/gb28181/config/config_test.go → 上溯 4 层到 server
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "config")
}

// TestLoad 验证 gb28181 配置能从 config.yml 正确加载.
// 2026-07-20 起 SIP 明文字段权威源是 gb_sip_config 表,YAML 层只留 enabled + 运行时元参数,
// 因此测试只断言 enabled / device.keepalive 等仍在 YAML 里的字段.
// SIP 具体字段是否合法由 bootstrap.loadSIPConfigFromDB + 引导页保证.
func TestLoad(t *testing.T) {
	if app.ConfigYml == nil {
		app.ConfigYml = ymlconfig.CreateYamlFactory(serverConfigDir())
	}

	cfg := Load()

	if !cfg.Enabled {
		t.Errorf("期望 gb28181.enabled=true, 实际 false")
	}
	if cfg.Device.KeepaliveTimeoutCount <= 0 {
		t.Errorf("期望 keepalive_timeout_count > 0, 实际 %d", cfg.Device.KeepaliveTimeoutCount)
	}
	if cfg.Device.OfflineScanInterval <= 0 {
		t.Errorf("期望 offline_scan_interval > 0, 实际 %d", cfg.Device.OfflineScanInterval)
	}
	if cfg.Recording.ReconcileIntervalSec != 30 {
		t.Errorf("期望 recording.reconcile_interval_sec=30,实际 %d", cfg.Recording.ReconcileIntervalSec)
	}
}

// fakeSource 手写 valueSource 用于隔离测 LoadFrom.
type fakeSource struct {
	values  map[string]interface{}
	ints    map[string]int
	strings map[string]string
	bools   map[string]bool
	slices  map[string][]string
}

func (f fakeSource) Get(k string) interface{}         { return f.values[k] }
func (f fakeSource) GetBool(k string) bool            { return f.bools[k] }
func (f fakeSource) GetString(k string) string        { return f.strings[k] }
func (f fakeSource) GetInt(k string) int              { return f.ints[k] }
func (f fakeSource) GetStringSlice(k string) []string { return f.slices[k] }

// TestLoadFromPlayConfig 断言 PlayConfig 能从 valueSource 正确加载.
// 通道播放状态显示 T7 新增.
func TestLoadFromPlayConfig(t *testing.T) {
	cases := []struct {
		name     string
		yamlVal  int
		expected int
	}{
		{"默认 0 = 禁用", 0, 0},
		{"5 分钟 = 300 秒", 300, 300},
		{"1 分钟 = 60 秒", 60, 60},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := fakeSource{
				ints:    map[string]int{"gb28181.play.reconcile_interval_sec": c.yamlVal},
				strings: map[string]string{},
				bools:   map[string]bool{},
				slices:  map[string][]string{},
			}
			cfg := LoadFrom(src)
			if cfg.Play.ReconcileIntervalSec != c.expected {
				t.Errorf("期望 ReconcileIntervalSec=%d, 实际 %d",
					c.expected, cfg.Play.ReconcileIntervalSec)
			}
		})
	}
}

func TestLoadFromZLMAddressOverrides(t *testing.T) {
	src := fakeSource{strings: map[string]string{
		"gb28181.zlm.host":         "10.0.0.2",
		"gb28181.zlm.receivehost":  "203.0.113.10",
		"gb28181.zlm.playbackhost": "play.example.com",
	}}
	cfg := LoadFrom(src)
	require.Equal(t, "10.0.0.2", cfg.ZLM.Host)
	require.Equal(t, "203.0.113.10", cfg.ZLM.EffectiveReceiveHost())
	require.Equal(t, "play.example.com", cfg.ZLM.EffectivePlaybackHost())
}

func TestSDPExtensionEnabledFromReadsLatestValue(t *testing.T) {
	source := fakeSource{
		values: map[string]interface{}{SDPExtensionConfigKey: false},
		bools:  map[string]bool{SDPExtensionConfigKey: false},
	}
	require.False(t, SDPExtensionEnabledFrom(source))

	source.values[SDPExtensionConfigKey] = true
	source.bools[SDPExtensionConfigKey] = true
	require.True(t, SDPExtensionEnabledFrom(source))
}

func TestSyncChannelsOnOnlineFromDefaultsToEnabled(t *testing.T) {
	source := fakeSource{}
	require.True(t, SyncChannelsOnOnlineFrom(source))

	source.values = map[string]interface{}{SyncChannelsOnOnlineConfigKey: false}
	source.bools = map[string]bool{SyncChannelsOnOnlineConfigKey: false}
	require.False(t, SyncChannelsOnOnlineFrom(source))

	source.values[SyncChannelsOnOnlineConfigKey] = true
	source.bools[SyncChannelsOnOnlineConfigKey] = true
	require.True(t, SyncChannelsOnOnlineFrom(source))
}

func TestOnlineOnHeartbeatFromDefaultsToEnabled(t *testing.T) {
	source := fakeSource{}
	require.True(t, OnlineOnHeartbeatFrom(source))

	source.values = map[string]interface{}{OnlineOnHeartbeatConfigKey: false}
	source.bools = map[string]bool{OnlineOnHeartbeatConfigKey: false}
	require.False(t, OnlineOnHeartbeatFrom(source))

	source.values[OnlineOnHeartbeatConfigKey] = true
	source.bools[OnlineOnHeartbeatConfigKey] = true
	require.True(t, OnlineOnHeartbeatFrom(source))
}

func TestSaveAlarmMessagesFromDefaultsToEnabled(t *testing.T) {
	source := fakeSource{}
	require.True(t, SaveAlarmMessagesFrom(source))

	source.values = map[string]interface{}{SaveAlarmMessagesConfigKey: false}
	source.bools = map[string]bool{SaveAlarmMessagesConfigKey: false}
	require.False(t, SaveAlarmMessagesFrom(source))

	source.values[SaveAlarmMessagesConfigKey] = true
	source.bools[SaveAlarmMessagesConfigKey] = true
	require.True(t, SaveAlarmMessagesFrom(source))
}

func TestSIPCommandTimeoutSecFromDefaultsAndValidates(t *testing.T) {
	source := fakeSource{}
	require.Equal(t, 10, SIPCommandTimeoutSecFrom(source))

	source.values = map[string]interface{}{SIPCommandTimeoutSecConfigKey: 30}
	source.ints = map[string]int{SIPCommandTimeoutSecConfigKey: 30}
	require.Equal(t, 30, SIPCommandTimeoutSecFrom(source))

	for _, invalid := range []int{0, 301} {
		source.values[SIPCommandTimeoutSecConfigKey] = invalid
		source.ints[SIPCommandTimeoutSecConfigKey] = invalid
		require.Equal(t, 10, SIPCommandTimeoutSecFrom(source))
	}
}

func TestPreallocationModeFromDefaultsToDisabled(t *testing.T) {
	source := fakeSource{}
	require.False(t, PreallocationModeFrom(source))

	source.values = map[string]interface{}{PreallocationModeConfigKey: true}
	source.bools = map[string]bool{PreallocationModeConfigKey: true}
	require.True(t, PreallocationModeFrom(source))

	source.values[PreallocationModeConfigKey] = false
	source.bools[PreallocationModeConfigKey] = false
	require.False(t, PreallocationModeFrom(source))
}

func TestIgnoreChannelOfflineStatusNotifyFromDefaultsToDisabled(t *testing.T) {
	source := fakeSource{}
	require.False(t, IgnoreChannelOfflineStatusNotifyFrom(source))

	source.values = map[string]interface{}{IgnoreChannelOfflineStatusNotifyConfigKey: true}
	source.bools = map[string]bool{IgnoreChannelOfflineStatusNotifyConfigKey: true}
	require.True(t, IgnoreChannelOfflineStatusNotifyFrom(source))

	source.values[IgnoreChannelOfflineStatusNotifyConfigKey] = false
	source.bools[IgnoreChannelOfflineStatusNotifyConfigKey] = false
	require.False(t, IgnoreChannelOfflineStatusNotifyFrom(source))
}

func TestDefaultChannelStreamTransportFromDefaultsAndValidates(t *testing.T) {
	source := fakeSource{}
	require.Equal(t, "TCP-Passive", DefaultChannelStreamTransportFrom(source))

	for _, transport := range []string{"UDP", "TCP-Active", "TCP-Passive"} {
		source.values = map[string]interface{}{DefaultChannelStreamTransportConfigKey: transport}
		source.strings = map[string]string{DefaultChannelStreamTransportConfigKey: transport}
		require.Equal(t, transport, DefaultChannelStreamTransportFrom(source))
	}

	for _, invalid := range []string{"", "tcp-passive", "SCTP"} {
		source.values = map[string]interface{}{DefaultChannelStreamTransportConfigKey: invalid}
		source.strings = map[string]string{DefaultChannelStreamTransportConfigKey: invalid}
		require.Equal(t, "TCP-Passive", DefaultChannelStreamTransportFrom(source))
	}
}

func TestDefaultPlaybackProtocolFromDefaultsAndValidates(t *testing.T) {
	source := fakeSource{}
	require.Equal(t, "ws-flv", DefaultPlaybackProtocolFrom(source))
	for _, protocol := range []string{"ws-flv", "http-flv", "hls", "webrtc"} {
		source.values = map[string]interface{}{DefaultPlaybackProtocolConfigKey: protocol}
		source.strings = map[string]string{DefaultPlaybackProtocolConfigKey: protocol}
		require.Equal(t, protocol, DefaultPlaybackProtocolFrom(source))
	}
	for _, invalid := range []string{"", "WS-FLV", "rtmp", "http-flv "} {
		source.values = map[string]interface{}{DefaultPlaybackProtocolConfigKey: invalid}
		source.strings = map[string]string{DefaultPlaybackProtocolConfigKey: invalid}
		require.Equal(t, "ws-flv", DefaultPlaybackProtocolFrom(source))
	}
}

func TestDefaultChannelAudioEnabledFromDefaultsToEnabled(t *testing.T) {
	source := fakeSource{}
	require.True(t, DefaultChannelAudioEnabledFrom(source))

	source.values = map[string]interface{}{DefaultChannelAudioEnabledConfigKey: false}
	source.bools = map[string]bool{DefaultChannelAudioEnabledConfigKey: false}
	require.False(t, DefaultChannelAudioEnabledFrom(source))
}

func TestPlaybackSettingsFromDefaultsAndReadsExplicitFalse(t *testing.T) {
	source := fakeSource{}
	settings := PlaybackSettingsFrom(source)
	require.Equal(t, DefaultPlayRequestTimeoutMs, settings.PlayTimeoutMs)
	require.True(t, settings.OnDemandLive)
	require.False(t, settings.CloudRecordingEnabled)
	require.Equal(t, 10*time.Second, settings.PlayTimeout())

	source.values = map[string]interface{}{
		PlayRequestTimeoutMsConfigKey:         1500,
		DefaultChannelOnDemandLiveConfigKey:   false,
		DefaultChannelCloudRecordingConfigKey: false,
	}
	source.ints = map[string]int{PlayRequestTimeoutMsConfigKey: 1500}
	source.bools = map[string]bool{
		DefaultChannelOnDemandLiveConfigKey:   false,
		DefaultChannelCloudRecordingConfigKey: false,
	}
	settings = PlaybackSettingsFrom(source)
	require.Equal(t, 1500, settings.PlayTimeoutMs)
	require.False(t, settings.OnDemandLive)
	require.False(t, settings.CloudRecordingEnabled)
	require.Equal(t, 1500*time.Millisecond, settings.PlayTimeout())
}

func TestPlaybackSettingsFromFallsBackOnInvalidTimeout(t *testing.T) {
	source := fakeSource{
		values: map[string]interface{}{PlayRequestTimeoutMsConfigKey: 0},
		ints:   map[string]int{PlayRequestTimeoutMsConfigKey: 0},
	}
	require.Equal(t, DefaultPlayRequestTimeoutMs, PlaybackSettingsFrom(source).PlayTimeoutMs)

	for _, invalid := range []int{999, 300001, -1} {
		source.values[PlayRequestTimeoutMsConfigKey] = invalid
		source.ints[PlayRequestTimeoutMsConfigKey] = invalid
		require.Equal(t, DefaultPlayRequestTimeoutMs, PlaybackSettingsFrom(source).PlayTimeoutMs)
	}
	for _, valid := range []int{1000, 300000} {
		source.values[PlayRequestTimeoutMsConfigKey] = valid
		source.ints[PlayRequestTimeoutMsConfigKey] = valid
		require.Equal(t, valid, PlaybackSettingsFrom(source).PlayTimeoutMs)
	}
}

func TestValidatePlaybackSettings(t *testing.T) {
	require.NoError(t, ValidatePlaybackSettings(PlaybackSettings{PlayTimeoutMs: 1000}))
	require.NoError(t, ValidatePlaybackSettings(PlaybackSettings{PlayTimeoutMs: 300000}))
	for _, invalid := range []int{0, 999, 300001, -1} {
		err := ValidatePlaybackSettings(PlaybackSettings{PlayTimeoutMs: invalid})
		require.Error(t, err)
		var validationErr *ValidationError
		require.ErrorAs(t, err, &validationErr)
		require.Equal(t, PlayRequestTimeoutMsConfigKey, validationErr.Field)
	}
}

func TestPlaybackSettingsFromReadsLatestSourceValue(t *testing.T) {
	source := fakeSource{
		values: map[string]interface{}{PlayRequestTimeoutMsConfigKey: 1000},
		ints:   map[string]int{PlayRequestTimeoutMsConfigKey: 1000},
	}
	require.Equal(t, 1000, PlaybackSettingsFrom(source).PlayTimeoutMs)
	source.values[PlayRequestTimeoutMsConfigKey] = 300000
	source.ints[PlayRequestTimeoutMsConfigKey] = 300000
	require.Equal(t, 300000, PlaybackSettingsFrom(source).PlayTimeoutMs)
}

func TestFixedAddressPlaybackSettingsDefaultsAndValidStates(t *testing.T) {
	source := fakeSource{}
	require.Equal(t, FixedAddressPlaybackSettings{}, FixedAddressPlaybackSettingsFrom(source))

	for _, settings := range []FixedAddressPlaybackSettings{
		{},
		{FixedAddressEnabled: true},
		{FixedAddressEnabled: true, AutoOnDemandEnabled: true},
	} {
		source.values = map[string]interface{}{
			FixedAddressEnabledConfigKey: settings.FixedAddressEnabled,
			AutoOnDemandEnabledConfigKey: settings.AutoOnDemandEnabled,
		}
		source.bools = map[string]bool{
			FixedAddressEnabledConfigKey: settings.FixedAddressEnabled,
			AutoOnDemandEnabledConfigKey: settings.AutoOnDemandEnabled,
		}
		require.Equal(t, settings, FixedAddressPlaybackSettingsFrom(source))
		require.NoError(t, ValidateFixedAddressPlaybackSettings(settings))
	}
}

func TestValidateFixedAddressPlaybackSettingsRejectsAutoWithoutFixed(t *testing.T) {
	err := ValidateFixedAddressPlaybackSettings(FixedAddressPlaybackSettings{AutoOnDemandEnabled: true})
	require.Error(t, err)
	var validationErr *ValidationError
	require.ErrorAs(t, err, &validationErr)
	require.Equal(t, AutoOnDemandEnabledConfigKey, validationErr.Field)
}

func TestGlobalSubscriptionItemsFromFiltersInvalidAndDuplicateItems(t *testing.T) {
	source := fakeSource{}
	require.Empty(t, GlobalSubscriptionItemsFrom(source))

	source.values = map[string]interface{}{GlobalSubscriptionItemsConfigKey: []string{"catalog"}}
	source.slices = map[string][]string{
		GlobalSubscriptionItemsConfigKey: {"catalog", "alarm", "catalog", "ptz", "mobile_position", "ptz_precise_position"},
	}
	require.Equal(t, []string{"catalog", "alarm", "mobile_position", "ptz_precise_position"}, GlobalSubscriptionItemsFrom(source))
}

func TestSIPTraceEnabledFromDefaultsToDisabled(t *testing.T) {
	source := fakeSource{}
	require.False(t, SIPTraceEnabledFrom(source))

	source.values = map[string]interface{}{SIPTraceEnabledConfigKey: true}
	source.bools = map[string]bool{SIPTraceEnabledConfigKey: true}
	require.True(t, SIPTraceEnabledFrom(source))

	source.values[SIPTraceEnabledConfigKey] = false
	source.bools[SIPTraceEnabledConfigKey] = false
	require.False(t, SIPTraceEnabledFrom(source))
}

func TestSIPTraceRetentionDaysFromDefaultsAndBounds(t *testing.T) {
	source := fakeSource{}
	require.Equal(t, DefaultSIPTraceRetentionDays, SIPTraceRetentionDaysFrom(source))

	for _, days := range []int{MinSIPTraceRetentionDays, 30, MaxSIPTraceRetentionDays} {
		source.values = map[string]interface{}{SIPTraceRetentionDaysConfigKey: days}
		source.ints = map[string]int{SIPTraceRetentionDaysConfigKey: days}
		require.Equal(t, days, SIPTraceRetentionDaysFrom(source))
	}
	for _, days := range []int{-1, 0, MaxSIPTraceRetentionDays + 1} {
		source.values = map[string]interface{}{SIPTraceRetentionDaysConfigKey: days}
		source.ints = map[string]int{SIPTraceRetentionDaysConfigKey: days}
		require.Equal(t, DefaultSIPTraceRetentionDays, SIPTraceRetentionDaysFrom(source))
	}
}

func TestRecordRuntimeConfigDefaults(t *testing.T) {
	cfg, err := LoadValidatedFrom(fakeSource{})
	if err != nil {
		t.Fatalf("加载默认配置失败: %v", err)
	}

	if cfg.RecordQuery.Timezone != "Asia/Shanghai" || cfg.RecordQuery.TimeoutSec != 15 ||
		cfg.RecordQuery.MaxRangeHours != 24 || cfg.RecordQuery.MaxActiveQueries != 128 ||
		cfg.RecordQuery.MaxRecordsPerQuery != 20000 || cfg.RecordQuery.ResultTTLSec != 1800 {
		t.Fatalf("查询默认值不正确: %+v", cfg.RecordQuery)
	}
	if cfg.RecordQuery.Location == nil || cfg.RecordQuery.Location.String() != "Asia/Shanghai" {
		t.Fatalf("查询时区未装配: %v", cfg.RecordQuery.Location)
	}
	if cfg.Playback.MediaWaitSec != 10 || cfg.Playback.IdleTimeoutSec != 60 || cfg.Playback.MaxSessionSec != 86400 {
		t.Fatalf("回放默认值不正确: %+v", cfg.Playback)
	}
}

func TestRecordRuntimeConfigCustomValues(t *testing.T) {
	values := map[string]interface{}{
		"gb28181.record_query.timezone":              "UTC",
		"gb28181.record_query.timeout_sec":           20,
		"gb28181.record_query.max_range_hours":       12,
		"gb28181.record_query.max_active_queries":    32,
		"gb28181.record_query.max_records_per_query": 5000,
		"gb28181.record_query.result_ttl_sec":        600,
		"gb28181.playback.media_wait_sec":            5,
		"gb28181.playback.idle_timeout_sec":          30,
		"gb28181.playback.max_session_sec":           3600,
		"httpserver.handler_timeout":                 30,
	}
	ints := make(map[string]int, len(values))
	for key, value := range values {
		if intValue, ok := value.(int); ok {
			ints[key] = intValue
		}
	}
	src := fakeSource{values: values, ints: ints, strings: map[string]string{"gb28181.record_query.timezone": "UTC"}}

	cfg, err := LoadValidatedFrom(src)
	if err != nil {
		t.Fatalf("加载合法覆盖配置失败: %v", err)
	}
	if cfg.RecordQuery.TimeoutSec != 20 || cfg.RecordQuery.MaxRangeHours != 12 ||
		cfg.RecordQuery.MaxActiveQueries != 32 || cfg.RecordQuery.MaxRecordsPerQuery != 5000 ||
		cfg.RecordQuery.ResultTTLSec != 600 {
		t.Fatalf("查询覆盖值未保留: %+v", cfg.RecordQuery)
	}
	if cfg.RecordQuery.Location.String() != "UTC" {
		t.Fatalf("时区覆盖值未保留: %v", cfg.RecordQuery.Location)
	}
	if cfg.Playback.MediaWaitSec != 5 || cfg.Playback.IdleTimeoutSec != 30 || cfg.Playback.MaxSessionSec != 3600 {
		t.Fatalf("回放覆盖值未保留: %+v", cfg.Playback)
	}
}

func TestRecordRuntimeConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value interface{}
	}{
		{name: "非法时区", key: "gb28181.record_query.timezone", value: "Mars/Base"},
		{name: "本地时区别名", key: "gb28181.record_query.timezone", value: "Local"},
		{name: "TTL 为零", key: "gb28181.record_query.result_ttl_sec", value: 0},
		{name: "TTL 超限", key: "gb28181.record_query.result_ttl_sec", value: MaxResultTTLSec + 1},
		{name: "媒体等待超限", key: "gb28181.playback.media_wait_sec", value: MaxMediaWaitSec + 1},
		{name: "空闲超时为零", key: "gb28181.playback.idle_timeout_sec", value: 0},
		{name: "空闲超时超限", key: "gb28181.playback.idle_timeout_sec", value: MaxPlaybackIdleSec + 1},
		{name: "会话上限为零", key: "gb28181.playback.max_session_sec", value: 0},
		{name: "会话时长超限", key: "gb28181.playback.max_session_sec", value: MaxPlaybackSessionSec + 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := map[string]interface{}{tt.key: tt.value}
			src := fakeSource{values: values, ints: map[string]int{}, strings: map[string]string{}}
			switch value := tt.value.(type) {
			case int:
				src.ints[tt.key] = value
			case string:
				src.strings[tt.key] = value
			}

			_, err := LoadValidatedFrom(src)
			if err == nil {
				t.Fatal("期望配置校验失败")
			}
			var configErr *ValidationError
			if !errors.As(err, &configErr) {
				t.Fatalf("期望 typed ValidationError, 实际 %T: %v", err, err)
			}
			if configErr.Field != tt.key {
				t.Fatalf("错误字段=%q,期望 %q", configErr.Field, tt.key)
			}
		})
	}
}

func TestRecordQueryTimeoutMustBeBelowHTTPHandlerTimeout(t *testing.T) {
	for _, queryTimeout := range []int{30, 31} {
		values := map[string]interface{}{
			"gb28181.record_query.timeout_sec": queryTimeout,
			"httpserver.handler_timeout":       30,
		}
		_, err := LoadValidatedFrom(fakeSource{values: values, ints: map[string]int{
			"gb28181.record_query.timeout_sec": queryTimeout,
			"httpserver.handler_timeout":       30,
		}})
		if err == nil {
			t.Fatalf("query timeout=%d 时期望校验失败", queryTimeout)
		}
		var configErr *ValidationError
		if !errors.As(err, &configErr) || configErr.Field != "gb28181.record_query.timeout_sec" {
			t.Fatalf("期望 timeout typed error, 实际 %T: %v", err, err)
		}
	}
}

func TestRecordQueryTimeoutBelowHTTPHandlerTimeout(t *testing.T) {
	values := map[string]interface{}{
		"gb28181.record_query.timeout_sec": 29,
		"httpserver.handler_timeout":       30,
	}
	cfg, err := LoadValidatedFrom(fakeSource{values: values, ints: map[string]int{
		"gb28181.record_query.timeout_sec": 29,
		"httpserver.handler_timeout":       30,
	}})
	if err != nil {
		t.Fatalf("合法超时关系被拒绝: %v", err)
	}
	if cfg.RecordQuery.Timeout() != 29*time.Second {
		t.Fatalf("查询超时=%s,期望 29s", cfg.RecordQuery.Timeout())
	}
}

func TestLoadFromRecordingConfig(t *testing.T) {
	for _, value := range []int{0, 30, 60} {
		src := fakeSource{
			ints: map[string]int{"gb28181.recording.reconcile_interval_sec": value},
		}
		cfg := LoadFrom(src)
		if cfg.Recording.ReconcileIntervalSec != value {
			t.Fatalf("期望录像对账周期 %d,实际 %d", value, cfg.Recording.ReconcileIntervalSec)
		}
	}
}

func TestLoadFromRecordingCatalogConfig(t *testing.T) {
	src := fakeSource{values: map[string]interface{}{}, ints: map[string]int{}}
	cfg := LoadFrom(src)
	require.Equal(t, 6*60*60, cfg.Recording.CatalogReconcileIntervalSec)
	require.Equal(t, 2, cfg.Recording.CatalogPeriodicLookbackDays)
	require.Equal(t, 7, cfg.Recording.CatalogManualLookbackDays)
	require.Equal(t, "UVP_CLOUD_RECORDING_CAPABILITY_KEY", cfg.Recording.CapabilityKeyEnv)

	src.values = map[string]interface{}{
		"gb28181.recording.catalog_reconcile_interval_sec": 0,
		"gb28181.recording.catalog_periodic_lookback_days": 3,
		"gb28181.recording.catalog_manual_lookback_days":   8,
		"gb28181.recording.capability_key_env":             "UVP_TEST_RECORDING_CAPABILITY_KEY",
	}
	src.ints = map[string]int{
		"gb28181.recording.catalog_reconcile_interval_sec": 0,
		"gb28181.recording.catalog_periodic_lookback_days": 3,
		"gb28181.recording.catalog_manual_lookback_days":   8,
	}
	src.strings = map[string]string{
		"gb28181.recording.capability_key_env": "UVP_TEST_RECORDING_CAPABILITY_KEY",
	}
	cfg = LoadFrom(src)
	require.Zero(t, cfg.Recording.CatalogReconcileIntervalSec)
	require.Equal(t, 3, cfg.Recording.CatalogPeriodicLookbackDays)
	require.Equal(t, 8, cfg.Recording.CatalogManualLookbackDays)
	require.Equal(t, "UVP_TEST_RECORDING_CAPABILITY_KEY", cfg.Recording.CapabilityKeyEnv)
}
