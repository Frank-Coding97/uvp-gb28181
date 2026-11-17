package alarmpopup

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHubPublishesToEverySubscriber(t *testing.T) {
	hub := NewHub()
	first, cancelFirst := hub.Subscribe()
	defer cancelFirst()
	second, cancelSecond := hub.Subscribe()
	defer cancelSecond()
	require.Equal(t, 2, hub.SubscriberCount())

	event := Event{AlarmID: 5, DeviceCode: "D", DeviceName: "门口", ReceivedAt: time.Now()}
	require.Equal(t, 2, hub.Publish(event))
	require.Equal(t, event, <-first)
	require.Equal(t, event, <-second)
}

// 取消订阅后通道必须关闭且计数归零：否则每个断开的 SSE 连接都会永久占一个槽，
// 内存缓慢上涨，而 Publish 仍然"成功"投递给死通道。
func TestHubCancelClosesChannelAndStopsDelivery(t *testing.T) {
	hub := NewHub()
	events, cancel := hub.Subscribe()
	cancel()
	cancel() // 重复调用必须安全

	require.Equal(t, 0, hub.SubscriberCount())
	_, ok := <-events
	require.False(t, ok, "取消后通道应已关闭")
	require.Equal(t, 0, hub.Publish(Event{}))
}

// 慢客户端不许阻塞广播：缓冲满了直接丢这一条，绝不能卡住 SIP 报文处理链路。
func TestHubSlowSubscriberDoesNotBlockPublish(t *testing.T) {
	hub := NewHub()
	_, cancel := hub.Subscribe()
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for index := 0; index < subscriberBuffer+32; index++ { // 故意溢出缓冲
			hub.Publish(Event{AlarmID: uint64(index)})
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("发布被慢订阅者阻塞")
	}
}

func TestNilHubIsSafe(t *testing.T) {
	var hub *Hub
	events, cancel := hub.Subscribe()
	cancel()
	_, ok := <-events
	require.False(t, ok)
	require.Equal(t, 0, hub.Publish(Event{}))
	require.Equal(t, 0, hub.SubscriberCount())
}
