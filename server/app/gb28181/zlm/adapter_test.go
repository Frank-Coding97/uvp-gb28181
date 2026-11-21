package zlm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/service"
)

func TestServiceAdapterPreservesConfiguredHookBaseURL(t *testing.T) {
	var applied url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index/api/setServerConfig":
			applied = r.URL.Query()
			_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
		case "/index/api/getServerConfig":
			config := make(map[string]string, len(applied))
			for key := range applied {
				if key != "secret" {
					config[key] = applied.Get(key)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "data": []map[string]string{config}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(endpoint.Port())
	require.NoError(t, err)
	mediaNode := &node.Node{Host: endpoint.Hostname(), APIPort: port, APISecret: "zlm-secret", MediaServerUUID: "node-a"}
	adapter := NewServiceAdapter(gbconfig.MediaConfig{HookBaseURL: server.URL + "/public/index/hook"})

	require.NoError(t, adapter.ApplyConfigForNode(context.Background(), mediaNode, service.MediaTuning{}))
	flowHook, err := url.Parse(applied.Get("hook.on_flow_report"))
	require.NoError(t, err)
	require.Equal(t, server.URL+"/public/index/hook/on_flow_report", flowHook.Scheme+"://"+flowHook.Host+flowHook.Path)
}

func TestServiceAdapterUsesNodeHookIPBeforePlatformDefault(t *testing.T) {
	for _, tc := range []struct {
		name       string
		nodeHookIP string
		platformIP string
		wantHost   string
	}{
		{name: "platform default", platformIP: "192.0.2.10", wantHost: "192.0.2.10"},
		{name: "node override", nodeHookIP: "198.51.100.20", platformIP: "192.0.2.10", wantHost: "198.51.100.20"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var applied url.Values
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/index/api/setServerConfig":
					applied = r.URL.Query()
					_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
				case "/index/api/getServerConfig":
					config := make(map[string]string, len(applied))
					for key := range applied {
						if key != "secret" {
							config[key] = applied.Get(key)
						}
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "data": []map[string]string{config}})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			endpoint, err := url.Parse(server.URL)
			require.NoError(t, err)
			port, err := strconv.Atoi(endpoint.Port())
			require.NoError(t, err)
			mediaNode := &node.Node{
				Host: endpoint.Hostname(), APIPort: port, APISecret: "zlm-secret", MediaServerUUID: "node-a",
				HookIP: tc.nodeHookIP,
			}
			adapter := NewServiceAdapter(gbconfig.MediaConfig{
				HookBaseURL: "https://old.example:9443/uvp/index/hook",
			})
			require.NoError(t, adapter.ApplyConfigForNode(context.Background(), mediaNode, service.MediaTuning{HookIP: tc.platformIP}))

			parsed, err := url.Parse(applied.Get("hook.on_play"))
			require.NoError(t, err)
			require.Equal(t, tc.wantHost, parsed.Hostname())
			require.Equal(t, "https", parsed.Scheme)
			require.Equal(t, "9443", parsed.Port())
			require.Equal(t, "/uvp/index/hook/on_play", parsed.Path)
			require.Equal(t, "node-a", parsed.Query().Get("node"))
		})
	}
}
