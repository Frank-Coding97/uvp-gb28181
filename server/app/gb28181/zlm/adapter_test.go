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

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
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
