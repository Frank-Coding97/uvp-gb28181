package zlm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestGetServerConfigParseErrorDoesNotExposeResponseBody(t *testing.T) {
	const capability = "replayable-callback-capability"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not-json ` + capability))
	}))
	defer server.Close()
	parts := strings.Split(strings.TrimPrefix(server.URL, "http://"), ":")
	port, err := strconv.Atoi(parts[1])
	require.NoError(t, err)
	client := NewClientForNode(&node.Node{Host: parts[0], APIPort: port, APISecret: "secret"})

	_, err = client.GetServerConfig(context.Background())
	require.Error(t, err)
	require.NotContains(t, err.Error(), capability)
	require.NotContains(t, err.Error(), "not-json")
}

func TestSetServerConfigTransportErrorRedactsCallbackCapability(t *testing.T) {
	const capability = "replayable-callback-capability"
	const apiSecret = "zlm-api-secret"
	client := NewClientForNode(&node.Node{
		Host: "127.0.0.1", APIPort: 18080, APISecret: apiSecret, MediaServerUUID: "node-a",
	})
	client.http = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New(request.URL.String())
	})}

	err := client.SetServerConfig(context.Background(), map[string]string{
		"hook.on_stream_not_found": "http://platform/index/hook/on_stream_not_found?cap=" + capability,
	})
	require.Error(t, err)
	require.NotContains(t, err.Error(), capability)
	require.NotContains(t, err.Error(), apiSecret)
}
