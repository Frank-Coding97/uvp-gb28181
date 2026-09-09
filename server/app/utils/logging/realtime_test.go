package logging

import (
	"context"
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

func TestRuntimePublishesWhitelistedEventWithBoundIdentity(t *testing.T) {
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

func TestRuntimeExcludesAccessAuditAndLegacyLogs(t *testing.T) {
	for _, event := range []string{"http.access", "audit.operation", "legacy.log", "zlm.metrics.sample_failed", "play.reconcile.round_completed"} {
		_, ok := buildRealtimeEvent("i", zapcore.Entry{Time: time.Now(), Level: zapcore.InfoLevel}, []zap.Field{zap.String("event", event)})
		require.False(t, ok, event)
	}
}

func TestRuntimeExcludesDebugBusinessEvents(t *testing.T) {
	_, ok := buildRealtimeEvent("i", zapcore.Entry{Time: time.Now(), Level: zapcore.DebugLevel}, []zap.Field{zap.String("event", "gb28181.play.requested")})
	require.False(t, ok)
}

func TestRealtimeMessageRedactsSecretsAndURLQuery(t *testing.T) {
	event, ok := buildRealtimeEvent("i", zapcore.Entry{Time: time.Now(), Level: zapcore.InfoLevel, Message: "authorization: Bearer abc token=xyz url=https://example.test/live?token=secret&x=1"}, []zap.Field{zap.String("event", "gb28181.play.started")})
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
