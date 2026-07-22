package config

import (
	"path/filepath"
	"runtime"
	"testing"

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
	ints    map[string]int
	strings map[string]string
	bools   map[string]bool
	slices  map[string][]string
}

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
