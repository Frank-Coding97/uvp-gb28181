// Package alarmpopup 提供设备告警的进程内实时广播（SSE 的"推送源"）。
//
// ⛔⛔ 为什么广播点必须放在「报文解析处」而不是「告警列表轮询」：
// 告警是否写入 gb_alarm_event 由「是否存储报警消息」开关决定，而该开关**默认关闭**
// （2026-10-20 交付包口径）。如果实时弹窗依赖落库记录，默认配置下就永远弹不出来 ——
// 两个开关是彼此独立的：可以"不存库但实时提醒"，也可以"存库但不弹窗"。
//
// 进程内单实例即可：本平台的 webserver 与 SIP 服务同进程，没有跨节点扇出需求。
// 将来若要横向扩展，替换本包的 Publish/Subscribe 实现即可，调用点不用动。
package alarmpopup

import (
	"sync"
	"time"
)

// subscriberBuffer 是单个订阅者的缓冲深度。
//
// 告警是低频事件（现场通常每分钟几条），64 足够扛住短时突发；
// 真出现持续积压说明前端连接已经卡死，此时宁可丢新的也不能阻塞 SIP 线程。
const subscriberBuffer = 64

// Event 是一条待弹出的设备告警。
//
// 字段刻意做成"前端拿起来就能渲染"的形状：名称/别名在服务端就查好，
// 前端不再为每条告警回查设备与通道（否则弹出瞬间会打出 N 个列表请求）。
type Event struct {
	// AlarmID 是 gb_alarm_event 主键；未落库时为 0（见包注释）。
	AlarmID uint64 `json:"alarmId,omitempty"`
	// DeviceDBID 是 gb_device 主键，仅用于服务端做数据范围校验，不下发给前端。
	DeviceDBID uint `json:"-"`
	// OwnerDeptID 设备归属部门，供服务端快速判断可见性（0 表示未取到，按严格处理）。
	OwnerDeptID uint `json:"-"`

	DeviceCode   string `json:"deviceCode"`
	DeviceName   string `json:"deviceName"`
	DeviceAlias  string `json:"deviceAlias,omitempty"`
	ChannelCode  string `json:"channelCode,omitempty"`
	ChannelName  string `json:"channelName,omitempty"`
	ChannelAlias string `json:"channelAlias,omitempty"`

	// SourceCode 是报警报文里的 DeviceID，可能是设备也可能是某个通道。
	SourceCode     string     `json:"sourceCode,omitempty"`
	Priority       *int       `json:"priority,omitempty"`
	Method         *int       `json:"method,omitempty"`
	AlarmType      *int       `json:"alarmType,omitempty"`
	AlarmTypeParam string     `json:"alarmTypeParam,omitempty"`
	Description    string     `json:"description,omitempty"`
	AlarmTime      *time.Time `json:"alarmTime,omitempty"`
	ReceivedAt     time.Time  `json:"receivedAt"`
}

// Hub 是订阅者集合。零值不可用，请用 NewHub 或包级 Publish/Subscribe。
type Hub struct {
	mu     sync.RWMutex
	nextID int
	subs   map[int]chan Event
}

func NewHub() *Hub {
	return &Hub{subs: make(map[int]chan Event)}
}

// Subscribe 注册一个订阅者，返回只读通道与取消函数。
//
// ⛔ 取消函数必须由调用方 defer，否则连接断开后通道会一直挂在 hub 里，
// 每来一条告警都往死通道里写（表现：内存缓慢增长 + 推送计数虚高）。
func (h *Hub) Subscribe() (<-chan Event, func()) {
	if h == nil {
		closed := make(chan Event)
		close(closed)
		return closed, func() {}
	}
	h.mu.Lock()
	id := h.nextID
	h.nextID++
	ch := make(chan Event, subscriberBuffer)
	h.subs[id] = ch
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			if existing, ok := h.subs[id]; ok {
				delete(h.subs, id)
				close(existing)
			}
			h.mu.Unlock()
		})
	}
	return ch, cancel
}

// Publish 把事件投递给所有订阅者，返回成功入队的人数。
//
// ⛔ 非阻塞：某个客户端读得慢时直接丢弃这一条，绝不等待 —— 广播跑在 SIP
// 报文处理链路上，一旦阻塞会拖住整个设备的报警接收。
func (h *Hub) Publish(event Event) int {
	if h == nil {
		return 0
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	delivered := 0
	for _, ch := range h.subs {
		select {
		case ch <- event:
			delivered++
		default:
		}
	}
	return delivered
}

// SubscriberCount 返回当前订阅者数量。
//
// 调用方用它做"没人听就别查库组装"的前置判断：没有前端连着时，
// 每来一条告警都不该为了拼设备名/通道名多打两次 SQL。
func (h *Hub) SubscriberCount() int {
	if h == nil {
		return 0
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}

var defaultHub = NewHub()

// Publish 向进程内所有订阅者广播一条告警。
func Publish(event Event) int { return defaultHub.Publish(event) }

// Subscribe 订阅进程内告警广播。
func Subscribe() (<-chan Event, func()) { return defaultHub.Subscribe() }

// SubscriberCount 返回当前告警订阅者数量。
func SubscriberCount() int { return defaultHub.SubscriberCount() }
