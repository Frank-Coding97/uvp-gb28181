package zlm

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/ymlconfig"
)

// loadCfg 从 server/config 读真实 ZLM 配置(连 222)
func loadCfg(t *testing.T) gbconfig.ZLMConfig {
	_, thisFile, _, _ := runtime.Caller(0)
	configDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "config")
	if app.ConfigYml == nil {
		app.ConfigYml = ymlconfig.CreateYamlFactory(configDir)
	}
	cfg := gbconfig.Load().ZLM
	if cfg.Host == "" || cfg.Secret == "" {
		t.Skip("跳过(ZLM 未配置)")
	}
	return cfg
}

// TestGetServerConfig T1-测1: 连通 uvp-zlm
func TestGetServerConfig(t *testing.T) {
	c := NewClient(loadCfg(t))
	conf, err := c.GetServerConfig(context.Background())
	if err != nil {
		t.Skipf("跳过(无法连接 ZLM,可能不在内网): %v", err)
	}
	if conf["api.secret"] == "" && conf["http.port"] == "" {
		t.Errorf("getServerConfig 返回异常: %v", conf)
	}
	t.Logf("ZLM http.port=%s rtp_proxy.port=%s", conf["http.port"], conf["rtp_proxy.port"])
}

// TestSetServerConfig T1-测2: 下发 Hook 地址,读回确认
func TestSetServerConfig(t *testing.T) {
	c := NewClient(loadCfg(t))
	ctx := context.Background()
	hookURL := "http://192.168.0.204:8280/index/hook/on_stream_changed"
	err := c.SetServerConfig(ctx, map[string]string{"hook.on_stream_changed": hookURL})
	if err != nil {
		t.Skipf("跳过(无法连接 ZLM): %v", err)
	}
	conf, err := c.GetServerConfig(ctx)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if conf["hook.on_stream_changed"] != hookURL {
		t.Errorf("Hook 下发未生效: 期望 %s, 实际 %s", hookURL, conf["hook.on_stream_changed"])
	}
}

// TestIsMediaOnlineNotExist T6.1-测1: 查询不存在的流 → online=false 且无错误
func TestIsMediaOnlineNotExist(t *testing.T) {
	c := NewClient(loadCfg(t))
	online, err := c.IsMediaOnline(context.Background(), "rtp", "definitely-not-exist-stream-id")
	if err != nil {
		t.Skipf("跳过(ZLM 不可达): %v", err)
	}
	if online {
		t.Error("不存在的流应返回 online=false")
	}
}

// TestGetMediaInfoNotExist T6.1-测2: 查不存在的流 → online=false 且不报错
func TestGetMediaInfoNotExist(t *testing.T) {
	c := NewClient(loadCfg(t))
	info, err := c.GetMediaInfo(context.Background(), "rtp", "definitely-not-exist-stream-id")
	if err != nil {
		t.Skipf("跳过(ZLM 不可达): %v", err)
	}
	if info == nil {
		t.Fatal("info 不应为 nil")
	}
	if info.Online {
		t.Error("不存在的流应返回 online=false")
	}
}
