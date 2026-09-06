package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"gorm.io/gorm"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

type t14LogSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *t14LogSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *t14LogSink) Sync() error { return nil }

func (s *t14LogSink) Close() error { return nil }

func (s *t14LogSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func t14Runtime(t *testing.T) (*logging.Runtime, *t14LogSink) {
	t.Helper()
	old := app.ZapLog
	sink := &t14LogSink{}
	cfg := logging.Config{
		Outputs:      []string{"stdout"},
		Level:        zapcore.DebugLevel,
		Modules:      map[string]zapcore.Level{},
		FileFormat:   "json",
		StdoutFormat: "json",
		MaxSizeMB:    1,
		MaxBackups:   1,
		MaxAgeDays:   1,
	}
	runtime, err := logging.NewRuntime(logging.Options{
		Config:   cfg,
		Service:  "t14-test",
		Version:  "test",
		Instance: "handler",
		Sinks:    map[string]zapcore.WriteSyncer{"stdout": sink},
	})
	require.NoError(t, err)
	app.ZapLog = runtime.Root
	t.Cleanup(func() {
		app.ZapLog = old
		require.NoError(t, runtime.Close())
	})
	return runtime, sink
}

func t14Records(t *testing.T, sink *t14LogSink) []map[string]interface{} {
	t.Helper()
	var rows []map[string]interface{}
	for _, line := range bytes.Split([]byte(strings.TrimSpace(sink.String())), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var row map[string]interface{}
		require.NoError(t, json.Unmarshal(line, &row))
		rows = append(rows, row)
	}
	return rows
}

func t14RecordWithEvent(t *testing.T, sink *t14LogSink, event string) map[string]interface{} {
	t.Helper()
	for _, row := range t14Records(t, sink) {
		if row["event"] == event {
			return row
		}
	}
	t.Fatalf("event %q was not emitted; logs=%s", event, sink.String())
	return nil
}

func TestLoggingBackgroundEventsRegister(t *testing.T) {
	_, sink := t14Runtime(t)
	const deviceID = "34020000001320000088"
	h := NewRegisterHandler(securityTestCfg())
	h.SetSecurity(&fakeRegisterSecurity{})
	tx := &captureServerTransaction{}
	h.Handle(newSecurityRegisterRequest(deviceID, "34020000002000000099"), tx)

	require.Equal(t, sip.StatusForbidden, tx.response.StatusCode)
	row := t14RecordWithEvent(t, sink, "gb28181.register.server_id_mismatch")
	require.Equal(t, deviceID, row["device_id"])
	require.Equal(t, "security-register-test", row["call_id"])
	require.Equal(t, "34020000002000000099", row["server_id"])
	require.NotContains(t, sink.String(), securityTestPassword)
	_, hasRequestID := row["request_id"]
	require.False(t, hasRequestID)
}

type t14AlarmProcessor struct{ err error }

func (p t14AlarmProcessor) OnAlarmMessage(context.Context, string, string, string, []byte) error {
	return p.err
}

func TestLoggingBackgroundEventsMessage(t *testing.T) {
	_, sink := t14Runtime(t)
	const deviceID = "34020000001320000089"
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: "platform", Host: "3402000000"})
	prepareNotifyRequest(req)
	req.SetBody([]byte(`<Notify><CmdType>Alarm</CmdType><SN>7</SN><DeviceID>34020000001320000089</DeviceID></Notify>`))
	callID := sip.CallIDHeader("message-t14-alarm")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 7, MethodName: sip.MESSAGE})
	h := NewMessageHandler(gbconfig.Config{})
	h.SetAlarmProcessor(t14AlarmProcessor{err: errors.New("alarm backend secret t14")})
	tx := &captureServerTransaction{}
	h.Handle(req, tx)

	require.Equal(t, sip.StatusOK, tx.response.StatusCode)
	row := t14RecordWithEvent(t, sink, "gb28181.message.alarm_failed")
	require.Equal(t, deviceID, row["device_id"])
	require.Equal(t, "message-t14-alarm", row["call_id"])
	require.NotContains(t, sink.String(), "alarm backend secret t14")
}

func TestLoggingBackgroundEventsDeviceInfo(t *testing.T) {
	_, sink := t14Runtime(t)
	const deviceID = "34020000001320000090"
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "deviceinfo.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}))
	require.NoError(t, db.Create(&gbmodels.GbDevice{
		DeviceID: deviceID, Name: "old-name", Manufacturer: "old-maker", Model: "old-model", Firmware: "old-firmware",
	}).Error)
	oldDB := app.GormDbMysql
	app.GormDbMysql = db
	t.Cleanup(func() { app.GormDbMysql = oldDB })

	HandleDeviceInfoResponse(context.Background(), []byte(`<Response><CmdType>DeviceInfo</CmdType><SN>8</SN><DeviceID>34020000001320000090</DeviceID><DeviceName>new-name</DeviceName><Manufacturer>new-maker</Manufacturer><Model>new-model</Model><Firmware>firmware-secret-t14</Firmware></Response>`))

	row := t14RecordWithEvent(t, sink, "gb28181.deviceinfo.updated")
	require.Equal(t, deviceID, row["device_id"])
	require.Equal(t, float64(4), row["updated_field_count"])
	require.Equal(t, true, row["name_updated"])
	require.Equal(t, true, row["manufacturer_updated"])
	require.Equal(t, true, row["model_updated"])
	require.Equal(t, true, row["firmware_updated"])
	require.NotContains(t, sink.String(), "firmware-secret-t14")
	require.NotContains(t, sink.String(), `"updates"`)
}
