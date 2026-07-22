package trace

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// StreamEvent 是 SSE 推送给客户端的一条报文摘要(已脱敏 + 元数据抽取)。
// 主动跟 StoredEvent 分开,避免推送带上加密 payload 之类不需要的字段。
type StreamEvent struct {
	EventID    string    `json:"eventId"`
	OccurredAt time.Time `json:"occurredAt"`
	Direction  Direction `json:"direction"`
	Transport  string    `json:"transport"`
	LocalAddr  string    `json:"localAddr"`
	RemoteAddr string    `json:"remoteAddr"`
	DeviceID   string    `json:"deviceId"`
	Method     string    `json:"method"`
	StatusCode uint16    `json:"statusCode"`
	CallID     string    `json:"callId"`
	CSeq       uint32    `json:"cseq"`
	CSeqMethod string    `json:"cseqMethod"`
	FromURI    string    `json:"fromUri,omitempty"`
	ToURI      string    `json:"toUri,omitempty"`
	UserAgent  string    `json:"userAgent,omitempty"`
	Malformed  bool      `json:"malformed"`
	Payload    string    `json:"payload"` // 已脱敏(默认)或明文(sensitive=true 订阅)
	Sensitive  bool      `json:"sensitive"`
}

// StreamFilter 是 SSE 客户端订阅时通过 URL 参数指定的过滤条件。
// 空字段表示不过滤;所有非空字段必须全部匹配才推送。
type StreamFilter struct {
	DeviceID string
	CallID   string
	Method   string
}

// Matches 判断一个事件是否符合订阅过滤条件。
func (f StreamFilter) Matches(event *StreamEvent) bool {
	if event == nil {
		return false
	}
	if f.DeviceID != "" && event.DeviceID != f.DeviceID {
		return false
	}
	if f.CallID != "" && event.CallID != f.CallID {
		return false
	}
	if f.Method != "" && event.Method != f.Method {
		return false
	}
	return true
}

// subscriber 是 StreamHub 内部一个订阅通道的记录。
// 采用非阻塞发送 + drop 计数,防止慢消费者拖垮采集主链路。
type subscriber struct {
	id        string
	ch        chan StreamEvent
	filter    StreamFilter
	sensitive bool
	dropped   atomic.Uint64
	// 客户端上下文取消时通知 Hub 清理
	ctx context.Context
}

// StreamHub 维护所有活跃 SSE 订阅,提供 Subscribe/Unsubscribe/Broadcast。
type StreamHub struct {
	mu          sync.RWMutex
	subscribers map[string]*subscriber
	// bufferSize 每个订阅者的消息通道缓冲,慢消费者超过就 drop
	bufferSize int
}

const defaultStreamBuffer = 128

// NewStreamHub 创建一个默认缓冲 128 的 hub。
func NewStreamHub() *StreamHub {
	return &StreamHub{
		subscribers: make(map[string]*subscriber),
		bufferSize:  defaultStreamBuffer,
	}
}

// StreamSubscription 是 Subscribe 返回的订阅句柄。
type StreamSubscription struct {
	ID        string
	Events    <-chan StreamEvent
	Sensitive bool
	dropped   *atomic.Uint64
}

// Dropped 返回该订阅到目前为止累计丢弃的事件数。
func (s StreamSubscription) Dropped() uint64 {
	if s.dropped == nil {
		return 0
	}
	return s.dropped.Load()
}

// Subscribe 注册一个新订阅。ctx 断开(客户端断开 SSE)时,订阅会被自动清理。
// sensitive=true 表示订阅者要看敏感字段的原文(controller 层已做管理员和 purpose 校验)。
func (h *StreamHub) Subscribe(ctx context.Context, filter StreamFilter, sensitive bool) StreamSubscription {
	sub := &subscriber{
		id:        uuid.NewString(),
		ch:        make(chan StreamEvent, h.bufferSize),
		filter:    filter,
		sensitive: sensitive,
		ctx:       ctx,
	}
	h.mu.Lock()
	h.subscribers[sub.id] = sub
	h.mu.Unlock()

	// 客户端断开后主动清理,防止 Broadcast 触到已断开的 chan
	go func() {
		<-ctx.Done()
		h.Unsubscribe(sub.id)
	}()

	return StreamSubscription{
		ID:        sub.id,
		Events:    sub.ch,
		Sensitive: sensitive,
		dropped:   &sub.dropped,
	}
}

// Unsubscribe 移除订阅并关闭通道。可多次调用,幂等。
func (h *StreamHub) Unsubscribe(id string) {
	h.mu.Lock()
	sub, ok := h.subscribers[id]
	if ok {
		delete(h.subscribers, id)
	}
	h.mu.Unlock()
	if ok {
		// 关闭前 recover 一次,防止外部竞态导致重复 close
		defer func() { _ = recover() }()
		close(sub.ch)
	}
}

// Broadcast 把一个明文事件分发给所有订阅者。
// sensitive 订阅者拿明文;非 sensitive 订阅者拿 raw 脱敏后的版本。
// 非阻塞发送:通道满时递增 dropped 计数,不影响采集主链路。
func (h *StreamHub) Broadcast(rawEvent StreamEvent, redactedPayload string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, sub := range h.subscribers {
		if !sub.filter.Matches(&rawEvent) {
			continue
		}
		payload := rawEvent
		if !sub.sensitive {
			// 每订阅者一份 copy,避免 sensitive/redacted 互相污染
			payload.Payload = redactedPayload
			payload.Sensitive = false
		} else {
			payload.Sensitive = true
		}
		select {
		case sub.ch <- payload:
		default:
			sub.dropped.Add(1)
		}
	}
}

// SubscriberCount 用于健康检查/监控暴露活跃订阅数。
func (h *StreamHub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}
