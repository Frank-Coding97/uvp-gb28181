package logging

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestRealtimeHubSequenceSnapshotAndGap(t *testing.T) {
	hub := NewEventHub()
	for i := 0; i < defaultRealtimeRing+2; i++ {
		hub.Publish(RealtimeEvent{Event: "gb28181.play.started", Message: "start"})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, snapshot, gap := hub.Subscribe(ctx, RealtimeFilter{Event: "gb28181.play.started"}, 0)
	require.Len(t, snapshot, defaultRealtimeRing)
	require.Equal(t, uint64(3), snapshot[0].Sequence)
	_, _, gap = hub.Subscribe(ctx, RealtimeFilter{}, 1)
	require.True(t, gap)
}

func TestRealtimeHubSlowSubscriberKeepsLatestAndCountsDrop(t *testing.T) {
	hub := NewEventHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub, _, _ := hub.Subscribe(ctx, RealtimeFilter{}, 0)
	for i := 0; i < defaultRealtimeQueue+10; i++ {
		hub.Publish(RealtimeEvent{Event: "gb28181.play.started", Message: "event"})
	}
	require.Equal(t, uint64(10), sub.Dropped())
	var last RealtimeEvent
	for i := 0; i < defaultRealtimeQueue; i++ {
		last = <-sub.Events
	}
	require.Equal(t, uint64(defaultRealtimeQueue+10), last.Sequence)
}

func TestRuntimePublishesConsoleEventWithBoundIdentity(t *testing.T) {
	hub := NewEventHub()
	cfg := Config{Outputs: []string{"stdout"}, Level: zapcore.DebugLevel, Modules: map[string]zapcore.Level{}, FileFormat: "json", StdoutFormat: "json", MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1}
	runtime, err := NewRuntime(Options{Config: cfg, Instance: "instance-a", EventHub: hub, Sinks: map[string]zapcore.WriteSyncer{"stdout": zapcore.AddSync(io.Discard)}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Close()) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub, _, _ := hub.Subscribe(ctx, RealtimeFilter{}, 0)
	WithIdentity(runtime.Root, zap.String("request_id", "request-a")).Named("play").Info("点播开始", zap.String("event", "gb28181.play.started"), zap.String("device_id", "device-a"))
	select {
	case event := <-sub.Events:
		require.Equal(t, "request-a", event.RequestID)
		require.Equal(t, "device-a", event.DeviceID)
		require.Equal(t, "instance-a", event.InstanceID)
	case <-time.After(time.Second):
		t.Fatal("realtime event not published")
	}
}

func TestRuntimeConsoleMirrorMatchesDualOutputAccessPolicy(t *testing.T) {
	hub := NewEventHub()
	cfg := Config{Outputs: []string{"file", "stdout"}, Level: zapcore.InfoLevel, Modules: map[string]zapcore.Level{}, FileFormat: "json", StdoutFormat: "json", MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1}
	runtime, err := NewRuntime(Options{Config: cfg, Instance: "instance-a", EventHub: hub, Sinks: map[string]zapcore.WriteSyncer{"file": zapcore.AddSync(io.Discard), "stdout": zapcore.AddSync(io.Discard)}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Close()) })
	sub, _, _ := hub.Subscribe(context.Background(), RealtimeFilter{}, 0)
	t.Cleanup(func() { hub.Unsubscribe(sub.ID) })

	runtime.Root.Named("access").Info("request completed", zap.String("event", "http.access"))
	runtime.Root.Named("scheduler").Info("round completed")

	select {
	case event := <-sub.Events:
		require.Equal(t, "scheduler", event.Module)
		require.Equal(t, "legacy.log", event.Event)
	case <-time.After(time.Second):
		t.Fatal("console log not published")
	}
	select {
	case event := <-sub.Events:
		t.Fatalf("unexpected console log: %#v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestRuntimeConsoleMirrorKeepsAccessInStdoutOnlyMode(t *testing.T) {
	hub := NewEventHub()
	cfg := Config{Outputs: []string{"stdout"}, Level: zapcore.InfoLevel, Modules: map[string]zapcore.Level{}, FileFormat: "json", StdoutFormat: "json", MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1}
	runtime, err := NewRuntime(Options{Config: cfg, Instance: "instance-a", EventHub: hub, Sinks: map[string]zapcore.WriteSyncer{"stdout": zapcore.AddSync(io.Discard)}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Close()) })
	sub, _, _ := hub.Subscribe(context.Background(), RealtimeFilter{}, 0)
	t.Cleanup(func() { hub.Unsubscribe(sub.ID) })

	runtime.Root.Named("access").Info("request completed", zap.String("event", "http.access"))
	select {
	case event := <-sub.Events:
		require.Equal(t, "http.access", event.Event)
	case <-time.After(time.Second):
		t.Fatal("stdout-only access log not published")
	}
}

func TestRuntimeConsoleMirrorPublishesOnlySanitizedFields(t *testing.T) {
	hub := NewEventHub()
	cfg := Config{Outputs: []string{"stdout"}, Level: zapcore.InfoLevel, Modules: map[string]zapcore.Level{}, FileFormat: "json", StdoutFormat: "json", MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1}
	runtime, err := NewRuntime(Options{Config: cfg, Instance: "instance-a", EventHub: hub, Sinks: map[string]zapcore.WriteSyncer{"stdout": zapcore.AddSync(io.Discard)}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Close()) })
	sub, _, _ := hub.Subscribe(context.Background(), RealtimeFilter{}, 0)
	t.Cleanup(func() { hub.Unsubscribe(sub.ID) })

	runtime.Root.Info("service ready",
		zap.String("event", "service.ready"),
		zap.String("access_token", "secret-token"),
		zap.String("endpoint", "https://example.test/live?token=secret-query&x=1"),
	)
	event := <-sub.Events
	require.Equal(t, "[REDACTED]", event.Fields["access_token"])
	require.NotContains(t, event.Fields["endpoint"], "secret-query")
}

func TestRealtimeConsoleIncludesAllEnabledLogCategories(t *testing.T) {
	for _, event := range []string{"audit.operation", "legacy.log", "zlm.metrics.sample_failed", "play.reconcile.round_completed", "service.started"} {
		actual, ok := buildRealtimeEvent("i", zapcore.Entry{Time: time.Now(), Level: zapcore.InfoLevel}, []zap.Field{zap.String("event", event)}, false)
		require.True(t, ok, event)
		require.Equal(t, event, actual.Event)
	}
}

func TestRealtimeConsoleIncludesDebugWhenRuntimeLevelAllowsIt(t *testing.T) {
	actual, ok := buildRealtimeEvent("i", zapcore.Entry{Time: time.Now(), Level: zapcore.DebugLevel}, []zap.Field{zap.String("event", "legacy.log")}, false)
	require.True(t, ok)
	require.Equal(t, "debug", actual.Level)
}

func TestRealtimeConsoleMatchesRoutineAccessStdoutPolicy(t *testing.T) {
	entry := zapcore.Entry{Time: time.Now(), Level: zapcore.InfoLevel, LoggerName: "access"}
	fields := []zap.Field{zap.String("event", "http.access")}
	_, ok := buildRealtimeEvent("i", entry, fields, true)
	require.False(t, ok)

	entry.Level = zapcore.WarnLevel
	actual, ok := buildRealtimeEvent("i", entry, fields, true)
	require.True(t, ok)
	require.Equal(t, "warn", actual.Level)
}

func TestRealtimeConsoleIncludesSanitizedFieldsAndStack(t *testing.T) {
	entry := zapcore.Entry{Time: time.Now(), Level: zapcore.ErrorLevel, LoggerName: "db.statement", Message: "query failed", Stack: "main.go:42"}
	actual, ok := buildRealtimeEvent("i", entry, []zap.Field{
		zap.String("event", "db.statement_failed"),
		zap.String("error_code", "DB_QUERY_FAILED"),
		zap.String("token", "[REDACTED]"),
	}, false)
	require.True(t, ok)
	require.Equal(t, "DB_QUERY_FAILED", actual.Fields["error_code"])
	require.Equal(t, "[REDACTED]", actual.Fields["token"])
	require.Equal(t, "db.statement", actual.Module)
	require.Equal(t, "main.go:42", actual.Stack)
	_, err := json.Marshal(actual)
	require.NoError(t, err)
}

func TestRealtimeMessageRedactsSecretsAndURLQuery(t *testing.T) {
	event, ok := buildRealtimeEvent("i", zapcore.Entry{Time: time.Now(), Level: zapcore.InfoLevel, Message: "authorization: Bearer abc token=xyz url=https://example.test/live?token=secret&x=1"}, []zap.Field{zap.String("event", "gb28181.play.started")}, false)
	require.True(t, ok)
	require.NotContains(t, event.Message, "abc")
	require.NotContains(t, event.Message, "secret")
	require.Contains(t, event.Message, "[REDACTED]")
}

func TestRealtimeHubKeepsInstanceWithoutSnapshot(t *testing.T) {
	hub := NewEventHubWithInstance("instance-test")
	sub, _, _ := hub.Subscribe(context.Background(), RealtimeFilter{}, 0)
	require.Equal(t, "instance-test", hub.InstanceID())
	hub.Unsubscribe(sub.ID)
}

func TestRealtimeHubReportsRestartSequenceAndReplaysSnapshot(t *testing.T) {
	hub := NewEventHubWithInstance("new-instance")
	hub.Publish(RealtimeEvent{Event: "gb28181.play.started", Message: "start"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, snapshot, gap := hub.Subscribe(ctx, RealtimeFilter{}, 99)
	require.True(t, gap)
	require.Len(t, snapshot, 1)
}

func TestRealtimeFilterMatchesAllCorrelationFields(t *testing.T) {
	event := RealtimeEvent{RequestID: "request-a", OperationID: "operation-a", CorrelationID: "correlation-a"}
	require.True(t, (RealtimeFilter{RequestID: "request-a", OperationID: "operation-a", CorrelationID: "correlation-a"}).Matches(event))
	require.False(t, (RealtimeFilter{RequestID: "request-b"}).Matches(event))
}
