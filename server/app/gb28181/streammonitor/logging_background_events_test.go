package streammonitor

import (
	"bytes"
	"context"
	"errors"
	"go.uber.org/zap/zapcore"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func TestLoggingBackgroundEvents(t *testing.T) {
	core, entries := observer.New(zap.DebugLevel)
	root := zap.New(core)
	previous := app.ZapLog
	app.ZapLog = root
	t.Cleanup(func() { app.ZapLog = previous })
	ctx := logging.WithContext(context.Background(), logging.WithIdentity(root, zap.String("execution_id", "monitor-1")))
	service := NewService(nil, nil, func(*node.Node) MediaClient {
		return fakeMediaClient{err: errors.New("media failure")}
	}, nil)

	_, err := service.read(ctx, "stream-1", &node.Node{ID: 3, Host: "media-3", APIPort: 9000})
	require.ErrorIs(t, err, ErrNodeUnavailable)
	require.Len(t, entries.All(), 1)
	entry := entries.All()[0]
	require.Equal(t, "streammonitor", entry.LoggerName)
	require.Equal(t, "streammonitor.media_read_failed", entry.ContextMap()["event"])
	require.Equal(t, "stream-1", entry.ContextMap()["streamId"])
	require.EqualValues(t, 3, entry.ContextMap()["nodeId"])
	require.Equal(t, "monitor-1", entry.ContextMap()["execution_id"])
}

func TestLoggingBackgroundEventsEndpointCredentials(t *testing.T) {
	var output bytes.Buffer
	cfg, err := logging.ParseConfig(nil, t.TempDir())
	require.NoError(t, err)
	cfg.Outputs, cfg.StdoutFormat = []string{"stdout"}, "json"
	runtime, err := logging.NewRuntime(logging.Options{Config: cfg, Sinks: map[string]zapcore.WriteSyncer{"stdout": zapcore.AddSync(&output)}})
	require.NoError(t, err)
	previous := app.ZapLog
	app.ZapLog = runtime.Root
	t.Cleanup(func() { app.ZapLog = previous; require.NoError(t, runtime.Close()) })
	service := NewService(nil, nil, func(*node.Node) MediaClient {
		return fakeMediaClient{err: errors.New("media private error")}
	}, nil)
	_, err = service.read(context.Background(), "stream-1", &node.Node{ID: 3, Host: "operator:node-private-secret@127.0.0.1", APIPort: 9000})
	require.ErrorIs(t, err, ErrNodeUnavailable)
	require.NotContains(t, output.String(), "node-private-secret")
	require.NotContains(t, output.String(), "media private error")
	require.Contains(t, output.String(), `"endpoint":"http://127.0.0.1:9000"`)
}
