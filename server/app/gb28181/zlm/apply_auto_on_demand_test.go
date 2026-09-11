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

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
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
	require.Equal(t, "30000", applied.Get("general.maxStreamWaitMS"))
	require.Equal(t, "node-a", applied.Get("general.mediaServerId"))
}

func TestApplyConfigForNodeFailsWhenAutoOnDemandConfigDoesNotConverge(t *testing.T) {
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
				"general.maxStreamWaitMS":  "15000",
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
	require.ErrorContains(t, err, "general.maxStreamWaitMS")
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
