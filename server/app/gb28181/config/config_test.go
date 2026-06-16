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

// TestLoad 验证 gb28181 配置能从 config.yml 正确加载
// 对应 tasks T1-测:读取 config 能拿到 gb28181.sip 配置
func TestLoad(t *testing.T) {
	// 初始化全局配置(指向 server/config 目录)
	if app.ConfigYml == nil {
		app.ConfigYml = ymlconfig.CreateYamlFactory(serverConfigDir())
	}

	cfg := Load()

	if !cfg.Enabled {
		t.Errorf("期望 gb28181.enabled=true, 实际 false")
	}
	if cfg.SIP.Port <= 0 {
		t.Errorf("期望 sip.port > 0, 实际 %d", cfg.SIP.Port)
	}
	if len(cfg.SIP.Transport) != 2 {
		t.Errorf("期望 transport 双栈 2 项, 实际 %d 项", len(cfg.SIP.Transport))
	}
	if cfg.SIP.ServerID == "" {
		t.Errorf("期望 serverid 非空")
	}
	if cfg.Device.KeepaliveTimeoutCount <= 0 {
		t.Errorf("期望 keepalive_timeout_count > 0, 实际 %d", cfg.Device.KeepaliveTimeoutCount)
	}
}
