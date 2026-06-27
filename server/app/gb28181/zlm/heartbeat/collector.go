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
	registry *node.Registry
}

// NewCollector 构造
func NewCollector(reg *node.Registry) *Collector {
	return &Collector{registry: reg}
}

// keepalivePayload ZLM on_server_keepalive 回调载荷
//
// ZLM 实际字段(参考官方文档):
//
//	{
//	  "mediaServerId": "<uuid>",
//	  "data": {
//	    "MediaSource":  3,           // 当前流数
//	    "Session":      5,           // 当前会话数
//	    "NetThreadLoad":  [ {"load": 0.2}, ... ],   // 每个网络 I/O 线程负载
//	    "WorkThreadLoad": [ {"load": 0.1}, ... ],   // 每个工作线程负载
//	    "memUsage":     123456,      // 进程内存(byte)
//	    "totalBytesIn":  1000,        // 累计入流量
//	    "totalBytesOut": 2000         // 累计出流量
//	  }
//	}
type keepalivePayload struct {
	MediaServerID string `json:"mediaServerId"`
	Data          struct {
		MediaSource    int          `json:"MediaSource"`
		Session        int          `json:"Session"`
		NetThreadLoad  []threadLoad `json:"NetThreadLoad"`
		WorkThreadLoad []threadLoad `json:"WorkThreadLoad"`
		MemUsage       int64        `json:"memUsage"`
		TotalBytesIn   int64        `json:"totalBytesIn"`
		TotalBytesOut  int64        `json:"totalBytesOut"`
	} `json:"data"`
}

type threadLoad struct {
	Load float64 `json:"load"`
}

// avg 计算线程负载平均;空数组返 0
func avg(loads []threadLoad) float64 {
	if len(loads) == 0 {
		return 0
	}
	var sum float64
	for _, l := range loads {
		sum += l.Load
	}
	return sum / float64(len(loads))
}

// Receive 解析 ZLM keepalive payload,更新 Registry 内对应节点的 Stats。
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
	stats := node.Stats{
		LastHeartbeatAt:   time.Now(),
		MediaSourceCount:  body.Data.MediaSource,
		SessionCount:      body.Data.Session,
		NetThreadLoadAvg:  avg(body.Data.NetThreadLoad),
		WorkThreadLoadAvg: avg(body.Data.WorkThreadLoad),
		MemoryUsageBytes:  body.Data.MemUsage,
		TotalBytesIn:      body.Data.TotalBytesIn,
		TotalBytesOut:     body.Data.TotalBytesOut,
	}
	c.registry.UpdateStats(body.MediaServerID, stats)
	return nil
}
