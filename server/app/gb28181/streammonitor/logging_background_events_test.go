package streammonitor

import (
	"context"
	"errors"
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
