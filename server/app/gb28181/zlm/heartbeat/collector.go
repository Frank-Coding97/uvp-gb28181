// Package heartbeat 实现 ZLM 节点心跳收集与离线监测。
//
// Collector 解析 ZLM on_server_keepalive Hook 上报,把 stats 写入 node.Registry 内存表。
// Watcher 周期扫描 Registry 中所有 active 节点,LastHeartbeatAt 超阈值则 MarkOffline。
//
// 设计要点:
//   - Stats 不写 DB(高频心跳数据只放内存),由 Watcher 在状态翻转时(active→offline)才落库
//   - 未知 mediaServerId 静默忽略(节点可能刚被删,不是错误)
//   - Watcher 用 Clock 接口,便于测试注入 FakeClock
package heartbeat

import (
	"encoding/json"
	"errors"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// ErrEmptyMediaServerID payload 缺 mediaServerId,无法反查节点
var ErrEmptyMediaServerID = errors.New("heartbeat: empty mediaServerId in payload")

// Collector 心跳收集器,把 ZLM on_server_keepalive payload 解析为 Stats 写入 Registry。
type Collector struct {
	registry  *node.Registry
	scheduler ConfigConvergenceScheduler
	notifier  NodeEventNotifier
}

type ConfigConvergenceScheduler interface {
	ScheduleConfigConvergence(nodeID int64) bool
}

// RestartPendingChecker is optional. A restart notifier that owns the
// offline→heartbeat convergence can implement it so the generic config
// scheduler does not race the operation's verified convergence.
type RestartPendingChecker interface {
	RestartPending(nodeID int64) bool
}

// NewCollector 构造
func NewCollector(reg *node.Registry) *Collector {
	return &Collector{registry: reg}
}

func NewCollectorWithConfigScheduler(reg *node.Registry, scheduler ConfigConvergenceScheduler) *Collector {
	return &Collector{registry: reg, scheduler: scheduler}
}

// NewCollectorWithNotifier wires restart lifecycle events while preserving the
// existing constructor used by older bootstrap code.
func NewCollectorWithNotifier(reg *node.Registry, scheduler ConfigConvergenceScheduler, notifier NodeEventNotifier) *Collector {
	return &Collector{registry: reg, scheduler: scheduler, notifier: notifier}
}

func (c *Collector) SetNotifier(notifier NodeEventNotifier) { c.notifier = notifier }

// keepalivePayload ZLM on_server_keepalive 回调载荷
//
// ZLM 真实字段(直接抓 ~/open-project/ZLMediaKit/server/WebApi.cpp::getStatisticJson 验证):
//
//	{
//	  "mediaServerId": "<uuid>",
//	  "data": {
//	    "MediaSource":     7,          // 当前流数
//	    "TcpSession":      1,          // TCP 会话数
//	    "UdpSession":      0,          // UDP 会话数
//	    "MultiMediaSourceMuxer": 7,    // 暂未用
//	    "Socket":          130,        // 暂未用
//	    "TcpServer":       96, "TcpClient": 1, "UdpServer": 32,
//	    "FrameImp": 0, "Frame": 0,
//	    "Buffer": 259, "BufferLikeString": 2, "BufferList": 0, "BufferRaw": 257,
//	    "RtmpPacket": 0, "RtpPacket": 0
//	  }
//	}
//
// **ZLM keepalive 不含**:NetThreadLoad / WorkThreadLoad / memUsage / totalBytesIn / totalBytesOut。
// 线程负载需单独调 REST `/index/api/getThreadsLoad` + `/getWorkThreadsLoad`(M3 由 Watcher 周期主动拉)。
// 累计流量 ZLM 不直接提供(需 getMediaList 累加,代价大,M3 不实现)。
type keepalivePayload struct {
	MediaServerID string `json:"mediaServerId"`
	Data          struct {
		MediaSource int `json:"MediaSource"`
		TcpSession  int `json:"TcpSession"`
		UdpSession  int `json:"UdpSession"`
	} `json:"data"`
}

// Receive 解析 ZLM keepalive payload,更新 Registry 内对应节点的 Stats。
//
// 只更新心跳直接含的字段:MediaSourceCount / SessionCount(=TcpSession+UdpSession)/ LastHeartbeatAt。
// 线程负载由 ThreadLoadPoller 周期独立拉取(getThreadsLoad / getWorkThreadsLoad REST)。
//
// 错误场景:
//   - JSON 解析失败 → 返回错误(由调用方记日志)
//   - mediaServerId 为空 → 返回 ErrEmptyMediaServerID
//   - mediaServerId 不在 Registry → 静默忽略(节点可能刚被删,正常)
func (c *Collector) Receive(payload []byte) error {
	var body keepalivePayload
	if err := json.Unmarshal(payload, &body); err != nil {
		return err
	}
	if body.MediaServerID == "" {
		return ErrEmptyMediaServerID
	}
	previous, _ := c.registry.GetByUUID(body.MediaServerID)
	// 锁内字段级更新:与 ThreadLoadPoller 的负载字段互不覆盖
	c.registry.UpdateHeartbeatFields(body.MediaServerID,
		body.Data.MediaSource, body.Data.TcpSession+body.Data.UdpSession, time.Now())
	current, ok := c.registry.GetByUUID(body.MediaServerID)
	restartHandled := false
	if previous != nil && previous.State == node.StateOffline && ok && current.IsActive() && c.notifier != nil {
		// Check before and after delivery. The first check covers notifiers whose
		// callback is asynchronous; the second covers coordinators that mark the
		// operation as converging synchronously in OnNodeHeartbeat.
		if checker, ok := c.notifier.(RestartPendingChecker); ok {
			restartHandled = checker.RestartPending(current.ID)
		}
		c.notifier.OnNodeHeartbeat(current.ID)
		if !restartHandled {
			if checker, ok := c.notifier.(RestartPendingChecker); ok {
				restartHandled = checker.RestartPending(current.ID)
			}
		}
	}
	if ok && current.IsActive() && !restartHandled &&
		!c.registry.IsAutoOnDemandReady(current.ID) && c.scheduler != nil {
		c.scheduler.ScheduleConfigConvergence(current.ID)
	}
	return nil
}
