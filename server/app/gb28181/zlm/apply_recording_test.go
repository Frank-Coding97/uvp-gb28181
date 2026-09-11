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
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestApplyConfigForNodeIncludesRecordMP4Hook(t *testing.T) {
	var query url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/setServerConfig") {
			query = r.URL.Query()
			_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
			return
		}
		config := make(map[string]string, len(query))
		for key := range query {
			if key != "secret" {
				config[key] = query.Get(key)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code": 0, "data": []map[string]string{config},
		})
	}))
	defer server.Close()
	parts := strings.Split(strings.TrimPrefix(server.URL, "http://"), ":")
	port, err := strconv.Atoi(parts[1])
	require.NoError(t, err)
	client := NewClientForNode(&node.Node{Host: parts[0], APIPort: port, APISecret: "secret", MediaServerUUID: "node-uuid"})

	err = client.ApplyConfigForNode(context.Background(), gbconfig.MediaConfig{HookHost: "platform", HookPort: 8280})
	require.NoError(t, err)
	recordHook, err := url.Parse(query.Get("hook.on_record_mp4"))
	require.NoError(t, err)
	require.Equal(t, "http://platform:8280/index/hook/on_record_mp4", recordHook.Scheme+"://"+recordHook.Host+recordHook.Path)
	require.Equal(t, "node-uuid", recordHook.Query().Get("node"))
	require.NotEmpty(t, recordHook.Query().Get("cap"))
	require.Equal(t, "3600", query.Get("protocol.mp4_max_second"))
}
