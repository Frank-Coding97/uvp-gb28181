package ptz

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

type ptzLoggingConfig map[string]interface{}

func (c ptzLoggingConfig) Get(key string) interface{} { return c[key] }

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

func TestLoggingBackgroundEventsBodySummaryUsesLength(t *testing.T) {
	cfg, err := logging.ParseConfig(ptzLoggingConfig{
		"logs.outputs":      []string{"stdout"},
		"logs.stdoutformat": "json",
		"logs.level":        "debug",
	}, t.TempDir())
	require.NoError(t, err)
	var sink bytes.Buffer
	runtime, err := logging.NewRuntime(logging.Options{
		Config:   cfg,
		Service:  "uvp-test",
		Version:  "test",
		Instance: "ptz-test",
		Sinks:    map[string]zapcore.WriteSyncer{"stdout": zapcore.AddSync(&sink)},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, runtime.Close()) }()
	previous := app.ZapLog
	app.ZapLog = runtime.Root
	t.Cleanup(func() { app.ZapLog = previous })
	ctx := logging.WithContext(context.Background(), logging.WithIdentity(runtime.Root,
		zap.String("request_id", "ptz-runtime-request")))
	body := []byte(`<Response><Token>fake-token-value</Token><Password>fake-password-value</Password></Response>`)
	operation := gbmodels.GbPTZOperation{OperationID: "operation-1", Status: gbmodels.PTZOperationUnknown}

	logIgnoredPTZResponse(ctx, operation, "response-call-1", "7", manscdp.MessageHead{
		CmdType: manscdp.CmdPresetQuery, SN: "7", DeviceID: "channel-1",
	}, body)

	output := sink.String()
	require.NotContains(t, output, "fake-token-value")
	require.NotContains(t, output, "fake-password-value")
	require.NotContains(t, output, "<Response>")
	require.NotContains(t, output, "bodySummary")
	require.Contains(t, output, `"body_bytes":`+strconv.Itoa(len(body)))
	require.Contains(t, output, `"operationId":"operation-1"`)
	require.Contains(t, output, `"responseCallId":"response-call-1"`)
	require.Contains(t, output, `"request_id":"ptz-runtime-request"`)
	require.True(t, strings.Contains(output, `"event":"ptz.response.ignored"`), output)
}
