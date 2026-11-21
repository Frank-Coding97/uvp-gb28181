package zlm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

func TestApplyConfigForNodeConfiguresAndVerifiesAutoOnDemandHook(t *testing.T) {
	var applied url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/index/api/") {
		case "setServerConfig":
			applied = r.URL.Query()
			_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
		case "getServerConfig":
			config := make(map[string]string, len(applied))
			for key := range applied {
				if key != "secret" {
					config[key] = applied.Get(key)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0,
				"data": []map[string]string{config},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestApplyClient(t, server.URL, "zlm-secret", "node-a")
	require.NoError(t, client.ApplyConfigForNode(context.Background(), gbconfig.MediaConfig{
		HookHost: "platform", HookPort: 8280,
	}))

	rawHook := applied.Get("hook.on_stream_not_found")
	require.NotEmpty(t, rawHook)
	hookURL, err := url.Parse(rawHook)
	require.NoError(t, err)
	require.Equal(t, "http://platform:8280/index/hook/on_stream_not_found", hookURL.Scheme+"://"+hookURL.Host+hookURL.Path)
	wantCapability, err := playauth.HookCapability("zlm-secret", "node-a", playauth.HookOnStreamNotFound)
	require.NoError(t, err)
	require.Equal(t, "node-a", hookURL.Query().Get("node"))
	require.Equal(t, wantCapability, hookURL.Query().Get("cap"))
	require.NotContains(t, rawHook, "zlm-secret")
	flowURL, err := url.Parse(applied.Get("hook.on_flow_report"))
	require.NoError(t, err)
	require.Equal(t, "http://platform:8280/index/hook/on_flow_report", flowURL.Scheme+"://"+flowURL.Host+flowURL.Path)
	flowCapability, err := playauth.HookCapability("zlm-secret", "node-a", playauth.HookOnFlowReport)
	require.NoError(t, err)
	require.Equal(t, "node-a", flowURL.Query().Get("node"))
	require.Equal(t, flowCapability, flowURL.Query().Get("cap"))
	require.NotEqual(t, wantCapability, flowCapability)
	for _, event := range playauth.ManagedHookEvents() {
		raw := applied.Get("hook." + string(event))
		require.NotEmpty(t, raw, event)
		parsed, parseErr := url.Parse(raw)
		require.NoError(t, parseErr)
		require.Equal(t, "node-a", parsed.Query().Get("node"), event)
		require.True(t, playauth.VerifyHookCapability("zlm-secret", "node-a", event, parsed.Query().Get("cap")), event)
	}
	require.Equal(t, "0", applied.Get("general.flowThreshold"))
	require.Empty(t, applied.Get("general.maxStreamWaitMS"), "可由服务配置页面管理")
	require.NotContains(t, applied, "hook.alive_interval", "心跳周期由服务配置页面管理")
	require.Equal(t, "node-a", applied.Get("general.mediaServerId"))
}

func TestApplyConfigForNodeDoesNotManageStreamWaitConfig(t *testing.T) {
	var applied url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/index/api/") {
		case "setServerConfig":
			applied = r.URL.Query()
			_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
		case "getServerConfig":
			config := map[string]string{
				"hook.enable":              applied.Get("hook.enable"),
				"hook.on_stream_not_found": applied.Get("hook.on_stream_not_found"),
				"hook.on_flow_report":      applied.Get("hook.on_flow_report"),
				"general.flowThreshold":    applied.Get("general.flowThreshold"),
				"general.mediaServerId":    applied.Get("general.mediaServerId"),
			}
			for _, event := range playauth.ManagedHookEvents() {
				key := "hook." + string(event)
				config[key] = applied.Get(key)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0, "data": []map[string]string{config},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestApplyClient(t, server.URL, "zlm-secret", "node-a")
	err := client.ApplyConfigForNode(context.Background(), gbconfig.MediaConfig{HookHost: "platform", HookPort: 8280})
	require.NoError(t, err)
}

func TestApplyConfigForNodeFailsWhenHookRemainsDisabled(t *testing.T) {
	var applied url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/index/api/") {
		case "setServerConfig":
			applied = r.URL.Query()
			_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
		case "getServerConfig":
			config := map[string]string{
				"hook.enable":              "0",
				"hook.on_stream_not_found": applied.Get("hook.on_stream_not_found"),
				"hook.on_flow_report":      applied.Get("hook.on_flow_report"),
				"general.flowThreshold":    applied.Get("general.flowThreshold"),
				"general.mediaServerId":    applied.Get("general.mediaServerId"),
				"general.maxStreamWaitMS":  applied.Get("general.maxStreamWaitMS"),
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0, "data": []map[string]string{config},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestApplyClient(t, server.URL, "zlm-secret", "node-a")
	err := client.ApplyConfigForNode(context.Background(), gbconfig.MediaConfig{HookHost: "platform", HookPort: 8280})
	require.ErrorContains(t, err, "hook.enable")
}

func newTestApplyClient(t *testing.T, rawURL, secret, mediaServerID string) *Client {
	t.Helper()
	parts := strings.Split(strings.TrimPrefix(rawURL, "http://"), ":")
	port, err := strconv.Atoi(parts[1])
	require.NoError(t, err)
	return NewClientForNode(&node.Node{
		Host: parts[0], APIPort: port, APISecret: secret, MediaServerUUID: mediaServerID,
	})
}

func TestApplyConfigForNodePreservesCustomHooksOnRepeatedApply(t *testing.T) {
	config := map[string]string{"general.mediaServerId": "node-a", "hook.enable": "0", "hook.alive_interval": "45.5"}
	for _, event := range playauth.ManagedHookEvents() {
		config["hook."+string(event)] = "https://custom.example/" + string(event) + "?tenant=demo"
	}
	config["hook.on_play"] = ""
	var applied url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/setServerConfig") {
			applied = r.URL.Query()
			for key := range applied {
				if key != "secret" {
					config[key] = applied.Get(key)
				}
			}
			_, _ = w.Write([]byte(`{"code":0}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "data": []map[string]string{config}})
	}))
	defer server.Close()
	for i := 0; i < 2; i++ {
		client := newTestApplyClient(t, server.URL, "zlm-secret", "node-a")
		require.NoError(t, client.ApplyConfigForNode(context.Background(), gbconfig.MediaConfig{HookHost: "platform", HookPort: 8280}))
		require.NotContains(t, applied, "hook.enable")
		require.NotContains(t, applied, "hook.alive_interval")
		require.Equal(t, "45.5", config["hook.alive_interval"])
		for _, event := range playauth.ManagedHookEvents() {
			require.NotContains(t, applied, "hook."+string(event))
		}
		require.Equal(t, "0", config["hook.enable"])
		require.Equal(t, "", config["hook.on_play"])
		require.Equal(t, "https://custom.example/on_flow_report?tenant=demo", config["hook.on_flow_report"])
	}
}

func TestApplyConfigForNodeReadFailureDoesNotOverwriteHooks(t *testing.T) {
	sets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/setServerConfig") {
			sets++
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	err := newTestApplyClient(t, server.URL, "zlm-secret", "node-a").ApplyConfigForNode(context.Background(), gbconfig.MediaConfig{HookHost: "platform", HookPort: 8280})
	require.Error(t, err)
	require.Zero(t, sets)
}

func TestApplyConfigForNodeRefreshesPlatformHookButPreservesDisabledSwitch(t *testing.T) {
	config := map[string]string{
		"general.mediaServerId": "node-a", "hook.enable": "0",
		"hook.on_play":        "http://old-platform/index/hook/on_play?node=node-a&cap=old-signature",
		"hook.on_flow_report": "https://custom.example/flow",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/setServerConfig") {
			for key, values := range r.URL.Query() {
				if key != "secret" {
					config[key] = values[0]
				}
			}
			_, _ = w.Write([]byte(`{"code":0}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "data": []map[string]string{config}})
	}))
	defer server.Close()
	require.NoError(t, newTestApplyClient(t, server.URL, "zlm-secret", "node-a").ApplyConfigForNode(context.Background(), gbconfig.MediaConfig{HookHost: "new-platform", HookPort: 8280}))
	parsed, err := url.Parse(config["hook.on_play"])
	require.NoError(t, err)
	require.Equal(t, "new-platform:8280", parsed.Host)
	require.True(t, playauth.VerifyHookCapability("zlm-secret", "node-a", playauth.HookOnPlay, parsed.Query().Get("cap")))
	require.Equal(t, "0", config["hook.enable"])
	require.Equal(t, "https://custom.example/flow", config["hook.on_flow_report"])
}

// ⛔⛔ 回归锚点：2026-10-03 现场 —— 「对讲/广播点了没反应」，根因就是这条。
//
// ZLM 上的 `hook.on_stream_changed` 是一条**没有 cap/node 的旧值**
// （`http://192.168.0.204:8280/index/hook/on_stream_changed`：平台早先的 IP + 人工抄的），
// 而旧判据只认 cap+node ⇒ 把它当 user-owned ⇒ **永不刷新** ⇒ ZLM 每次回调都被 hook 认证
// 按 credentials_invalid 静默丢弃（HTTP 200 + `{"code":0}`，ZLM 以为成功，平台什么都没做）。
//
// 后果（平台侧一条 on_stream_changed 都收不到）：
//   - `ObserveTalkStream(regist=true)` 不触发 ⇒ 对讲/广播激活压根不开始：会话停在 publishing、
//     `signal_phase` 空、`broadcast_sn=0`、设备侧连一条 MESSAGE 都没收到，30s 后租约过期；
//   - `regist=false` 同样丢 ⇒ 浏览器停推后会话不会自动收尾。
//
// 要求：指着**本平台自己端点**的 URL（哪怕凭据丢了）必须被认领重写；换路径的自定义 URL、
// 空值照旧不碰。
func TestApplyConfigForNodeClaimsOwnEndpointWithoutCapability(t *testing.T) {
	config := map[string]string{
		"general.mediaServerId": "node-a",
		// 现场原样：旧平台 IP、无 cap、无 node。
		"hook.on_stream_changed": "http://192.168.0.204:8280/index/hook/on_stream_changed",
		// 别人的中继服务（换路径）必须原样保留。
		"hook.on_flow_report": "https://relay.example/hooks/flow?tenant=demo",
		// 空值仍是 user-owned 语义：不写。
		"hook.on_play": "",
	}
	var applied url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/setServerConfig") {
			applied = r.URL.Query()
			for key, values := range r.URL.Query() {
				if key != "secret" {
					config[key] = values[0]
				}
			}
			_, _ = w.Write([]byte(`{"code":0}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "data": []map[string]string{config}})
	}))
	defer server.Close()

	require.NoError(t, newTestApplyClient(t, server.URL, "zlm-secret", "node-a").ApplyConfigForNode(context.Background(),
		gbconfig.MediaConfig{HookHost: "platform", HookPort: 8280}))

	parsed, err := url.Parse(config["hook.on_stream_changed"])
	require.NoError(t, err)
	require.Equal(t, "platform:8280", parsed.Host, "认领后必须指回本平台")
	require.Equal(t, "/index/hook/on_stream_changed", parsed.Path)
	require.Equal(t, "node-a", parsed.Query().Get("node"))
	require.True(t, playauth.VerifyHookCapability("zlm-secret", "node-a", playauth.HookOnStreamChanged,
		parsed.Query().Get("cap")), "必须带上事件专属 cap，否则回调会被静默丢弃")

	require.Equal(t, "https://relay.example/hooks/flow?tenant=demo", config["hook.on_flow_report"])
	require.Equal(t, "", config["hook.on_play"])
	require.NotContains(t, applied, "hook.on_flow_report")
	require.NotContains(t, applied, "hook.on_play")
}
