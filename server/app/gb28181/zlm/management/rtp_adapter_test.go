package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestNodeRTPClientAdapterBindsNodeAndPassesCompleteOpenRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "node-secret", request.URL.Query().Get("secret"))
		switch request.URL.Path {
		case "/index/api/listRtpServer":
			_, _ = writer.Write([]byte(`{"code":0,"data":[]}`))
		case "/index/api/openRtpServer":
			require.Equal(t, "vhost-a", request.URL.Query().Get("vhost"))
			require.Equal(t, "ingress", request.URL.Query().Get("app"))
			require.Equal(t, "stream-a", request.URL.Query().Get("stream_id"))
			require.Equal(t, "0200000001", request.URL.Query().Get("ssrc"))
			require.Equal(t, "40000", request.URL.Query().Get("port"))
			require.Equal(t, "2", request.URL.Query().Get("tcp_mode"))
			require.Equal(t, "1", request.URL.Query().Get("only_track"))
			require.Equal(t, "192.0.2.20", request.URL.Query().Get("local_ip"))
			require.Equal(t, "1", request.URL.Query().Get("re_use_port"))
			_, _ = writer.Write([]byte(`{"code":0,"port":40000}`))
		case "/index/api/closeRtpServer":
			require.Equal(t, "vhost-a", request.URL.Query().Get("vhost"))
			require.Equal(t, "ingress", request.URL.Query().Get("app"))
			require.Equal(t, "stream-a", request.URL.Query().Get("stream_id"))
			_, _ = writer.Write([]byte(`{"code":0,"hit":1}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(endpoint.Port())
	require.NoError(t, err)
	registry := node.NewRegistry(newExecutorMemoryRepo())
	current := addExecutorNode(t, registry, node.Node{
		Name: "node-a", Host: endpoint.Hostname(), APIPort: port,
		APISecret: "node-secret", State: node.StateActive,
	})
	adapter := NewNodeRTPClientAdapter(NewNodeExecutor(registry, zlm.NewClientForNode))

	items, err := adapter.ListRtpServers(context.Background(), current.ID)
	require.NoError(t, err)
	require.Empty(t, items)

	opened, err := adapter.OpenRtpServer(context.Background(), current.ID, RTPServerCreateRequest{
		VHost: "vhost-a", App: "ingress", Stream: "stream-a", SSRC: "0200000001",
		Port: 40000, TCPMode: 2, OnlyTrack: 1, LocalIP: "192.0.2.20", Reuse: true,
	})
	require.NoError(t, err)
	require.Equal(t, &RTPServerOpenResult{Key: "stream-a", Port: 40000}, opened)

	closed, err := adapter.CloseRtpServer(context.Background(), current.ID, "vhost-a", "ingress", "stream-a")
	require.NoError(t, err)
	require.Equal(t, &RTPServerCloseResult{Stream: "stream-a", Hit: true, Released: false}, closed)
}

func TestNodeRTPClientAdapterFailsClosedWhenUnconfigured(t *testing.T) {
	adapter := NewNodeRTPClientAdapter(nil)
	_, err := adapter.ListRtpServers(context.Background(), 1)
	require.Error(t, err)
	_, err = adapter.OpenRtpServer(context.Background(), 1, RTPServerCreateRequest{Stream: "stream-a"})
	require.Error(t, err)
	_, err = adapter.CloseRtpServer(context.Background(), 1, "v", "a", "stream-a")
	require.Error(t, err)
}
