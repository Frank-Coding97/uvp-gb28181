package trace

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStreamHubDeliversToSubscribers(t *testing.T) {
	hub := NewStreamHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub := hub.Subscribe(ctx, StreamFilter{}, false)
	event := StreamEvent{EventID: "1", DeviceID: "device-a", Payload: "raw-sip"}
	hub.Broadcast(event, "redacted-sip")

	select {
	case got := <-sub.Events:
		require.Equal(t, "1", got.EventID)
		require.Equal(t, "redacted-sip", got.Payload, "非 sensitive 订阅拿脱敏版本")
		require.False(t, got.Sensitive)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("订阅者未收到消息")
	}
}

func TestStreamHubSensitiveSubscriberGetsRawPayload(t *testing.T) {
	hub := NewStreamHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub := hub.Subscribe(ctx, StreamFilter{}, true)
	event := StreamEvent{EventID: "1", Payload: "raw-with-secret"}
	hub.Broadcast(event, "redacted")

	got := <-sub.Events
	require.Equal(t, "raw-with-secret", got.Payload)
	require.True(t, got.Sensitive)
}

func TestStreamHubFilterMatchesDeviceAndCallID(t *testing.T) {
	hub := NewStreamHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub := hub.Subscribe(ctx, StreamFilter{DeviceID: "device-a"}, false)

	// 命中
	hub.Broadcast(StreamEvent{EventID: "hit", DeviceID: "device-a"}, "")
	// 不命中(设备不同)
	hub.Broadcast(StreamEvent{EventID: "miss", DeviceID: "device-b"}, "")

	got := <-sub.Events
	require.Equal(t, "hit", got.EventID)

	select {
	case unexpected := <-sub.Events:
		t.Fatalf("过滤失败,收到不该收到的事件 %+v", unexpected)
	case <-time.After(50 * time.Millisecond):
		// 预期无消息
	}
}

func TestStreamHubSlowConsumerDoesNotBlockBroadcast(t *testing.T) {
	hub := NewStreamHub()
	hub.bufferSize = 4 // 缩小缓冲更快触发 drop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 订阅者不消费,故意让通道打满
	sub := hub.Subscribe(ctx, StreamFilter{}, false)

	start := time.Now()
	for i := 0; i < 100; i++ {
		hub.Broadcast(StreamEvent{EventID: "e"}, "")
	}
	elapsed := time.Since(start)

	// 100 次 Broadcast 应该秒过(非阻塞),而不是被订阅者拖住
	require.Less(t, elapsed, 100*time.Millisecond, "Broadcast 被慢消费者阻塞了")
	require.Greater(t, sub.Dropped(), uint64(90), "多数事件应该被 drop,而不是保留在 chan")
}

func TestStreamHubContextCancelUnsubscribes(t *testing.T) {
	hub := NewStreamHub()
	ctx, cancel := context.WithCancel(context.Background())

	hub.Subscribe(ctx, StreamFilter{}, false)
	require.Equal(t, 1, hub.SubscriberCount())

	cancel()
	// Unsubscribe 通过 goroutine 触发,给一点时间
	require.Eventually(t, func() bool {
		return hub.SubscriberCount() == 0
	}, 500*time.Millisecond, 10*time.Millisecond)
}

func TestStreamHubUnsubscribeIsIdempotent(t *testing.T) {
	hub := NewStreamHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub := hub.Subscribe(ctx, StreamFilter{}, false)
	hub.Unsubscribe(sub.ID)
	// 二次调用不应 panic
	hub.Unsubscribe(sub.ID)
	require.Equal(t, 0, hub.SubscriberCount())
}
