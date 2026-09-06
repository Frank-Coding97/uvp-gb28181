package zlm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func TestLoggingGBHTTPZLMIsMediaOnlineSuccessKeepsScopeAndResult(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"online":true}`))
	})
	defer server.Close()

	root, observed := observer.New(zap.DebugLevel)
	ctx := logging.WithContext(context.Background(), logging.WithIdentity(zap.New(root), zap.String("request_id", "zlm-online-success")))

	online, err := client.IsMediaOnline(ctx, "rtp", "stream-1")

	require.NoError(t, err)
	require.True(t, online)
	entry := findZLMLog(t, observed, "zlm.media_online.ready")
	require.Equal(t, "zlm", entry.LoggerName)
	require.Equal(t, "zlm-online-success", entry.ContextMap()["request_id"])
	require.Equal(t, true, entry.ContextMap()["online"])
}

func TestLoggingGBHTTPZLMIsMediaOnlineNonZeroKeepsOfflineResult(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":-500,"msg":"stream not found","online":true}`))
	})
	defer server.Close()

	root, observed := observer.New(zap.DebugLevel)
	ctx := logging.WithContext(context.Background(), logging.WithIdentity(zap.New(root), zap.String("request_id", "zlm-online-missing")))

	online, err := client.IsMediaOnline(ctx, "rtp", "stream-1")

	require.NoError(t, err)
	require.False(t, online)
	entry := findZLMLog(t, observed, "zlm.media_online.not_ready")
	require.Equal(t, "zlm-online-missing", entry.ContextMap()["request_id"])
	require.Equal(t, int64(-500), entry.ContextMap()["code"])
}

func TestLoggingGBHTTPZLMIsMediaOnlineTransportFailureUsesSafeError(t *testing.T) {
	const rawError = "zlm credential should not appear"
	client, server := newMockClient(t, func(http.ResponseWriter, *http.Request) {})
	defer server.Close()
	client.http = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New(rawError)
	})}

	root, observed := observer.New(zap.DebugLevel)
	ctx := logging.WithContext(context.Background(), logging.WithIdentity(zap.New(root), zap.String("request_id", "zlm-online-transport")))

	online, err := client.IsMediaOnline(ctx, "rtp", "stream-1")

	require.Error(t, err)
	require.False(t, online)
	entry := findZLMLog(t, observed, "zlm.media_online.request_failed")
	require.Equal(t, "zlm-online-transport", entry.ContextMap()["request_id"])
	errorField, ok := entry.ContextMap()["error"].(map[string]interface{})
	require.True(t, ok)
	require.NotEmpty(t, errorField["class"])
	require.NotEmpty(t, errorField["type"])
	require.NotContains(t, fmt.Sprint(errorField), rawError)
}

func findZLMLog(t *testing.T, observed *observer.ObservedLogs, event string) observer.LoggedEntry {
	t.Helper()
	for _, entry := range observed.All() {
		if entry.ContextMap()["event"] == event {
			return entry
		}
	}
	t.Fatalf("ZLM log event not found: %s", event)
	return observer.LoggedEntry{}
}

func TestLoggingBackgroundEventsZLMNotReadyDoesNotFloodInfo(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":-500,"online":false}`))
	})
	defer server.Close()
	core, observed := observer.New(zap.InfoLevel)
	ctx := logging.WithContext(context.Background(), zap.New(core))
	for i := 0; i < 100; i++ {
		online, err := client.IsMediaOnline(ctx, "rtp", "stream-awaiting-media")
		require.NoError(t, err)
		require.False(t, online)
	}
	require.Zero(t, observed.Len(), "expected readiness polling must remain DEBUG")
}

func TestLoggingBackgroundEventsZLMEndpointCredentials(t *testing.T) {
	const secret = "zlm-host-private-credential"
	var output bytes.Buffer
	cfg, err := logging.ParseConfig(nil, "/app")
	require.NoError(t, err)
	cfg.Outputs, cfg.StdoutFormat = []string{"stdout"}, "json"
	runtime, err := logging.NewRuntime(logging.Options{Config: cfg, Sinks: map[string]zapcore.WriteSyncer{"stdout": zapcore.Lock(zapcore.AddSync(&output))}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Close()) })
	client := NewClientForNode(&node.Node{Host: "operator:" + secret + "@127.0.0.1", APIPort: 80})
	client.http = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection unavailable")
	})}
	online, err := client.IsMediaOnline(logging.WithContext(context.Background(), runtime.Root), "rtp", "safe-stream")
	require.Error(t, err)
	require.False(t, online)
	require.NoError(t, runtime.Close())
	require.NotContains(t, output.String(), secret)
	require.Contains(t, output.String(), `"endpoint":"http://127.0.0.1:80"`)
}
