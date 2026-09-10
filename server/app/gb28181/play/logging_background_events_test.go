package play

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func TestLoggingBackgroundEvents(t *testing.T) {
	core, entries := observer.New(zap.DebugLevel)
	root := zap.New(core)
	previous := app.ZapLog
	app.ZapLog = root
	t.Cleanup(func() { app.ZapLog = previous })
	ctx := logging.WithContext(context.Background(), logging.WithIdentity(root, zap.String("execution_id", "auto-start-1")))

	logAutoStartResult(autoStartJob{key: autoStartKey{
		deviceID: "device-1", channelID: "channel-1", requiredNode: 7,
	}}, nil, errors.New("device secret must stay out of logs"), ctx)
	logAutoStartResult(autoStartJob{key: autoStartKey{
		deviceID: "device-1", channelID: "channel-1", requiredNode: 7,
	}}, &Result{StreamID: "stream-1"}, nil, ctx)

	require.Len(t, entries.All(), 2)
	entry := entries.All()[0]
	require.Equal(t, "play.auto_start", entry.LoggerName)
	require.Equal(t, "play.auto_start.failed", entry.ContextMap()["event"])
	require.Equal(t, "device-1", entry.ContextMap()["deviceId"])
	require.Equal(t, "channel-1", entry.ContextMap()["channelId"])
	require.EqualValues(t, 7, entry.ContextMap()["nodeId"])
	require.Equal(t, "auto-start-1", entry.ContextMap()["execution_id"])
	require.NotContains(t, entry.Message, "device secret")
	success := entries.All()[1]
	require.Equal(t, "play.auto_start.succeeded", success.ContextMap()["event"])
	require.Equal(t, "stream-1", success.ContextMap()["streamId"])
}
