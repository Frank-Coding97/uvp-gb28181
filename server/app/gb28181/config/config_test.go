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
}
