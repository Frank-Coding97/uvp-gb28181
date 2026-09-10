package reconciler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

type backgroundEventSink struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *backgroundEventSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *backgroundEventSink) Sync() error  { return nil }
func (s *backgroundEventSink) Close() error { return nil }

func newBackgroundRuntime(t *testing.T) (*logging.Runtime, *backgroundEventSink) {
	t.Helper()
	sink := &backgroundEventSink{}
	runtime, err := logging.NewRuntime(logging.Options{
		Config: logging.Config{
			Outputs:      []string{"stdout"},
			Level:        zapcore.DebugLevel,
			Modules:      map[string]zapcore.Level{"play.reconcile": zapcore.DebugLevel},
			FileFormat:   "json",
			StdoutFormat: "json",
			MaxSizeMB:    1,
			MaxBackups:   1,
			MaxAgeDays:   1,
		},
		Sinks: map[string]zapcore.WriteSyncer{"stdout": sink},
	})
	require.NoError(t, err)
	oldRuntime, oldLogger := app.LogRuntime, app.ZapLog
	app.LogRuntime, app.ZapLog = runtime, runtime.Root
	t.Cleanup(func() {
		app.LogRuntime, app.ZapLog = oldRuntime, oldLogger
		require.NoError(t, runtime.Close())
	})
	return runtime, sink
}

func backgroundRecords(t *testing.T, sink *backgroundEventSink) []map[string]interface{} {
	t.Helper()
	sink.mu.Lock()
	data := append([]byte(nil), sink.b.Bytes()...)
	sink.mu.Unlock()
	var records []map[string]interface{}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record map[string]interface{}
		require.NoError(t, json.Unmarshal(line, &record))
		records = append(records, record)
	}
	return records
}

func backgroundRecordWithEvent(t *testing.T, records []map[string]interface{}, event string) map[string]interface{} {
	t.Helper()
	for _, record := range records {
		if record["event"] == event {
			return record
		}
	}
	t.Fatalf("background log event not found: %s; records=%v", event, records)
	return nil
}

type backgroundPanicLister struct{}

func (backgroundPanicLister) ListPlayingChannels(context.Context) (gbmodels.GbChannelList, error) {
	panic(backgroundPanicValue{})
}

type backgroundPanicValue struct{}

func (backgroundPanicValue) String() string { return "panic-secret" }
func (backgroundPanicValue) Error() string  { return "panic-secret" }

func TestLoggingBackgroundEvents(t *testing.T) {
	t.Run("reconciliation failure keeps state identifiers", func(t *testing.T) {
		_, sink := newBackgroundRuntime(t)
		stopper := &fakeStopper{stopErr: errors.New("stop secret")}
		reconciler := New(0, stopper,
			WithMediaProbe(&fakeProbe{}),
			WithChannelLister(&fakeLister{channels: gbmodels.GbChannelList{makeChannel("stream-1")}}))
		ctx := logging.WithContext(context.Background(), logging.WithIdentity(app.ZapLog, zap.String("execution_id", "reconcile-1")))

		stats := reconciler.runOnce(ctx)
		require.Equal(t, 1, stats.Scanned)
		require.Equal(t, 1, stats.Failed)
		record := backgroundRecordWithEvent(t, backgroundRecords(t, sink), "play.reconcile.cleanup_failed")
		require.Equal(t, "play.reconcile", record["component"])
		require.Equal(t, "stream-1", record["streamID"])
		require.Equal(t, "dev-stream-1", record["deviceID"])
		require.Equal(t, "ch-stream-1", record["channelID"])
		require.Equal(t, "reconcile-1", record["execution_id"])
		require.NotContains(t, strings.TrimSpace(string(mustJSON(t, record))), "stop secret")
	})

	t.Run("panic log is type and stack only", func(t *testing.T) {
		_, sink := newBackgroundRuntime(t)
		reconciler := New(time.Hour, &fakeStopper{}, WithChannelLister(backgroundPanicLister{}))
		reconciler.wg.Add(1)
		reconciler.loop(context.Background())

		record := backgroundRecordWithEvent(t, backgroundRecords(t, sink), "play.reconcile.panic")
		require.Equal(t, "reconciler.backgroundPanicValue", record["panic_type"])
		require.Contains(t, record["stack"], "reconciler.(*Reconciler).loop")
		require.NotContains(t, strings.TrimSpace(string(mustJSON(t, record))), "panic-secret")
		require.NotContains(t, record, "recover")
	})
}

func mustJSON(t *testing.T, value interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}
