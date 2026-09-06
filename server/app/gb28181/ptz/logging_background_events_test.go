package ptz

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
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

	logUnmatchedPTZResponse(ctx, "device-1", "call-1", "9", manscdp.MessageHead{
		CmdType: manscdp.CmdDeviceControl, SN: "9", DeviceID: "channel-1",
	}, []string{"operation-1"}, "no_candidate", []byte("<Response><Result>ERROR</Result></Response>"))

	require.Len(t, entries.All(), 1)
	entry := entries.All()[0]
	require.Equal(t, "ptz", entry.LoggerName)
	require.Equal(t, "ptz.response.unmatched", entry.ContextMap()["event"])
	require.Equal(t, "ptz-response-1", entry.ContextMap()["request_id"])
	require.Equal(t, "device-1", entry.ContextMap()["deviceCode"])
	require.Equal(t, "channel-1", entry.ContextMap()["channelCode"])
	require.Equal(t, "no_candidate", entry.ContextMap()["reason"])
}

func TestLoggingBackgroundEventsQueryKeepsRequestScope(t *testing.T) {
	service, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	core, entries := observer.New(zap.DebugLevel)
	root := zap.New(core)
	previous := app.ZapLog
	app.ZapLog = root
	t.Cleanup(func() { app.ZapLog = previous })
	ctx := logging.WithContext(context.Background(), logging.WithIdentity(root,
		zap.String("request_id", "query-request-1"),
		zap.String("execution_id", "query-execution-1")))

	operation, err := service.Refresh(context.Background(), testTarget(), QueryPreset, 0, "query-log-scope")
	require.NoError(t, err)
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).
		Where("id = ?", operation.ID).
		Updates(map[string]interface{}{
			"status":                gbmodels.PTZOperationUnknown,
			"transport_deadline_at": operation.CreatedAt.Add(-time.Minute),
		}).Error)
	body := []byte(`<Response><CmdType>PresetQuery</CmdType><SN>` + strconv.Itoa(operation.SN) + `</SN><DeviceID>C</DeviceID><SumNum>1</SumNum><PresetList Num="1"><Item><PresetID>1</PresetID><PresetName>late</PresetName></Item></PresetList></Response>`)
	require.NoError(t, service.OnPTZMessage(ctx, "D", "query-call", "1", body))

	var entry observer.LoggedEntry
	for _, candidate := range entries.All() {
		if candidate.ContextMap()["event"] == "ptz.response.ignored" {
			entry = candidate
			break
		}
	}
	require.Equal(t, "ptz.response.ignored", entry.ContextMap()["event"])
	require.Equal(t, "query-request-1", entry.ContextMap()["request_id"])
	require.Equal(t, "query-execution-1", entry.ContextMap()["execution_id"])
	require.Equal(t, operation.OperationID, entry.ContextMap()["operationId"])
}
