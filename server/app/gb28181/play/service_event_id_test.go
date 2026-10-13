package play

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServiceLifecycleEventIDsFitDatabaseColumn(t *testing.T) {
	writer := &fakeLifecycleWriter{}
	recorder := NewAsyncLifecycleRecorder(writer, 8)
	t.Cleanup(func() { require.NoError(t, recorder.Shutdown(context.Background())) })
	service := &Service{lifecycleRecorder: recorder, streamLifecycleRecorder: recorder}
	req := Request{LifecycleID: "0088d982-b3c4-466e-bbd6-a547fe6617b0", DeviceID: "device-1", ChannelID: "channel-1"}
	for range 2 {
		service.recordLifecycle(context.Background(), req, LifecycleEvent{EventName: EventValidationSucceeded})
		service.recordStreamLifecycle(context.Background(), strings.Repeat("s", 128), 1, LifecycleEvent{EventName: EventCleanupPartialFailure})
	}
	service.recordLifecycle(context.Background(), req, LifecycleEvent{EventID: "existing-event", EventName: EventMediaReady})
	require.NoError(t, recorder.Shutdown(context.Background()))
	require.Len(t, writer.events, 5)
	seen := make(map[string]bool)
	for _, event := range writer.events {
		require.NotEmpty(t, event.EventID)
		require.LessOrEqual(t, len(event.EventID), 64, "must fit gb_play_lifecycle_event.event_id")
		require.False(t, seen[event.EventID], "each event must have its own ID")
		seen[event.EventID] = true
	}
	require.Equal(t, "existing-event", writer.events[4].EventID, "preserve caller IDs for idempotency")
	require.Equal(t, req.DeviceID, writer.events[0].DeviceCode)
	require.Equal(t, req.ChannelID, writer.events[0].ChannelCode)
}
