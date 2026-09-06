package zlm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

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
