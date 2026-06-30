package zlm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/ymlconfig"
)

// loadNode 从 server/config 读 yaml,构造一个临时 Node 用于联通真机 ZLM 的 IT 测试
func loadNode(t *testing.T) *node.Node {
	_, thisFile, _, _ := runtime.Caller(0)
	configDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "config")
	if app.ConfigYml == nil {
		app.ConfigYml = ymlconfig.CreateYamlFactory(configDir)
	}
	cfg := gbconfig.Load().ZLM
	if cfg.Host == "" || cfg.Secret == "" {
		t.Skip("跳过(ZLM 未配置)")
	}
	return &node.Node{
		Host:      cfg.Host,
		APIPort:   cfg.HTTPPort,
		APISecret: cfg.Secret,
	}
}

// TestGetServerConfig T1-测1: 连通 uvp-zlm
func TestGetServerConfig(t *testing.T) {
	c := NewClientForNode(loadNode(t))
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
	c := NewClientForNode(loadNode(t))
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
	c := NewClientForNode(loadNode(t))
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
	c := NewClientForNode(loadNode(t))
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

// newMockClient 构造一个连到 httptest 假 ZLM 的 Client
//
// path 形如 "/index/api/kick_sessions",handler 直接写 JSON 响应体
func newMockClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	// 解析 host / port
	u := srv.URL // http://127.0.0.1:54321
	// 用 http.NewRequest 拆 host
	req, _ := http.NewRequest("GET", u, nil)
	host := req.URL.Hostname()
	portStr := req.URL.Port()
	port := 0
	if portStr != "" {
		// 简易解析
		for _, ch := range portStr {
			port = port*10 + int(ch-'0')
		}
	}
	n := &node.Node{Host: host, APIPort: port, APISecret: "test-secret"}
	return NewClientForNode(n), srv
}

// TestKickSessions_MockedZLM T3.5-R: 验证 KickSessions 调用 /kick_sessions 并解析 count
func TestKickSessions_MockedZLM(t *testing.T) {
	c, srv := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/kick_sessions" {
			t.Errorf("调错路径: %s", r.URL.Path)
		}
		if r.URL.Query().Get("secret") != "test-secret" {
			t.Errorf("secret 没带上: %s", r.URL.Query().Get("secret"))
		}
		_, _ = w.Write([]byte(`{"code":0,"count":42}`))
	})
	defer srv.Close()

	n, err := c.KickSessions(context.Background(), nil)
	if err != nil {
		t.Fatalf("KickSessions 报错: %v", err)
	}
	if n != 42 {
		t.Errorf("期望 count=42 实际=%d", n)
	}
}

// TestKickSessions_WithFilter T3.5-R: filter 应该作为 query 参数透传
func TestKickSessions_WithFilter(t *testing.T) {
	c, srv := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("local_port"); got != "10000" {
			t.Errorf("local_port filter 没透传: %s", got)
		}
		_, _ = w.Write([]byte(`{"code":0,"count":3}`))
	})
	defer srv.Close()

	n, err := c.KickSessions(context.Background(), map[string]string{"local_port": "10000"})
	if err != nil {
		t.Fatalf("KickSessions 报错: %v", err)
	}
	if n != 3 {
		t.Errorf("期望 count=3 实际=%d", n)
	}
}

// TestRestartServer_MockedZLM T3.5-R: 验证 restartServer 调用,真机不能跑(会真重启)
func TestRestartServer_MockedZLM(t *testing.T) {
	called := false
	c, srv := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/restartServer" {
			t.Errorf("调错路径: %s", r.URL.Path)
		}
		called = true
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	})
	defer srv.Close()

	if err := c.RestartServer(context.Background(), 5000); err != nil {
		t.Fatalf("RestartServer 报错: %v", err)
	}
	if !called {
		t.Error("restartServer 端点未被调用")
	}
}

// TestRestartServer_CodeNonZero T3.5-R: ZLM 返非 0 应报错
func TestRestartServer_CodeNonZero(t *testing.T) {
	c, srv := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":-1,"msg":"forbidden"}`))
	})
	defer srv.Close()

	err := c.RestartServer(context.Background(), 0)
	if err == nil {
		t.Fatal("期望报错,实际 nil")
	}
}

// TestCloseStreams_MockedZLM T3.5-R: close_streams 同 kick_sessions 形态
func TestCloseStreams_MockedZLM(t *testing.T) {
	c, srv := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/close_streams" {
			t.Errorf("调错路径: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":0,"count":7}`))
	})
	defer srv.Close()

	n, err := c.CloseStreams(context.Background(), nil)
	if err != nil {
		t.Fatalf("CloseStreams 报错: %v", err)
	}
	if n != 7 {
		t.Errorf("期望 count=7 实际=%d", n)
	}
}
