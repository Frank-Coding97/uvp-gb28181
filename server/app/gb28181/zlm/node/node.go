package node

import (
	"fmt"
	"strings"
	"time"
)

// State 节点生命周期状态
type State string

const (
	StateActive      State = "active"      // 在调度池
	StateMaintenance State = "maintenance" // 维护态:不分配新流,旧流自然结束
	StateOffline     State = "offline"     // 心跳超时 / 进程挂
)

// Node 一台 ZLM 节点的逻辑表示
type Node struct {
	ID                  int64             // DB 自增主键
	Revision            uint64            // 持久化版本,用于跨调用方 CAS
	Name                string            // 显示名
	Host                string            // ZLM API host
	ReceiveHost         string            // 设备收流地址,写入 SDP 的 c= 地址
	PlaybackHost        string            // 播放访问地址,返回给浏览器/客户端
	APIPort             int               // ZLM API port
	APISecret           string            // ZLM api.secret
	MediaServerUUID     string            // 业务侧生成,启动时写入 ZLM general.mediaServerId
	Weight              int               // 0-100,加权轮询,默认 50
	Tags                map[string]string // 任意标签
	State               State
	RecoveryRequired    bool   // 外部配置收敛不确定时 fail-close
	RecoveryReason      string // 安全、有限长的恢复原因
	RecoveryFingerprint string // candidate endpoint 的不透明 SHA-256 指纹
	RTPPortStart        int    // rtp_proxy.port_range 起
	RTPPortEnd          int    // rtp_proxy.port_range 止
	Stats               Stats  // 实时状态,内存,心跳更新
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// EffectiveReceiveHost 返回设备应向其发送 RTP 的地址。
// 旧节点未配置时回退到 API host,保持升级兼容。
func (n Node) EffectiveReceiveHost() string {
	if host := strings.TrimSpace(n.ReceiveHost); host != "" {
		return host
	}
	return n.Host
}

// EffectivePlaybackHost 返回播放 URL 使用的地址。
// 旧节点未配置时回退到 API host,保持升级兼容。
func (n Node) EffectivePlaybackHost() string {
	if host := strings.TrimSpace(n.PlaybackHost); host != "" {
		return host
	}
	return n.Host
}

// Stats 实时状态(由心跳更新,内存表)
type Stats struct {
	LastHeartbeatAt   time.Time `json:"lastHeartbeatAt"`
	MediaSourceCount  int       `json:"mediaSourceCount"`  // 当前媒体源数(流数)
	SessionCount      int       `json:"sessionCount"`      // 当前会话数
	NetThreadLoadAvg  float64   `json:"netThreadLoadAvg"`  // 网络 I/O 线程负载平均 0-1
	WorkThreadLoadAvg float64   `json:"workThreadLoadAvg"` // 工作线程负载平均 0-1
	MemoryUsageBytes  int64     `json:"memoryUsageBytes"`  // 内存占用
	TotalBytesIn      int64     `json:"totalBytesIn"`      // 累计入流量
	TotalBytesOut     int64     `json:"totalBytesOut"`     // 累计出流量
}

// HTTPEndpoint 返回 ZLM HTTP API 根地址
func (n Node) HTTPEndpoint() string {
	return fmt.Sprintf("http://%s:%d/index/api", n.Host, n.APIPort)
}

// IsActive 是否处于调度池
func (n Node) IsActive() bool {
	return n.State == StateActive
}
