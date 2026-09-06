package ptz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func TestLoggingBackgroundEvents(t *testing.T) {
	core, entries := observer.New(zap.DebugLevel)
	root := zap.New(core)
	previous := app.ZapLog
	app.ZapLog = root
	t.Cleanup(func() { app.ZapLog = previous })
	ctx := logging.WithContext(context.Background(), logging.WithIdentity(root, zap.String("request_id", "ptz-response-1")))

	logUnmatchedPTZResponse("device-1", "call-1", "9", manscdp.MessageHead{
		CmdType: manscdp.CmdDeviceControl, SN: "9", DeviceID: "channel-1",
	}, []string{"operation-1"}, "no_candidate", []byte("<Response><Result>ERROR</Result></Response>"), ctx)

	require.Len(t, entries.All(), 1)
	entry := entries.All()[0]
	require.Equal(t, "ptz", entry.LoggerName)
	require.Equal(t, "ptz.response.unmatched", entry.ContextMap()["event"])
	require.Equal(t, "ptz-response-1", entry.ContextMap()["request_id"])
	require.Equal(t, "device-1", entry.ContextMap()["deviceCode"])
	require.Equal(t, "channel-1", entry.ContextMap()["channelCode"])
	require.Equal(t, "no_candidate", entry.ContextMap()["reason"])
}
