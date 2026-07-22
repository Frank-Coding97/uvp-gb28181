package zlm

import (
	"context"
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
		query = r.URL.Query()
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer server.Close()
	parts := strings.Split(strings.TrimPrefix(server.URL, "http://"), ":")
	port, err := strconv.Atoi(parts[1])
	require.NoError(t, err)
	client := NewClientForNode(&node.Node{Host: parts[0], APIPort: port, APISecret: "secret", MediaServerUUID: "node-uuid"})

	err = client.ApplyConfigForNode(context.Background(), gbconfig.MediaConfig{HookHost: "platform", HookPort: 8280})
	require.NoError(t, err)
	require.Equal(t, "http://platform:8280/index/hook/on_record_mp4", query.Get("hook.on_record_mp4"))
	require.Equal(t, "3600", query.Get("record.mp4_max_second"))
}
