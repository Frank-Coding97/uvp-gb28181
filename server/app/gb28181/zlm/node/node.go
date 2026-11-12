package node

import (
	"fmt"
	"net"
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
	ID                   int64             // DB 自增主键
	Revision             uint64            // 持久化版本,用于跨调用方 CAS
	Name                 string            // 显示名
	Host                 string            // ZLM API host
	HookIP               string            // ZLM Hook 回调地址节点覆盖
	PlaybackHost         string            // 播放访问地址,返回给浏览器/客户端
	SDPIP                string            // 设备推流地址(写入 SDP 的 c= 地址,设备据此把 RTP 推回来)
	PlatformSDPIP        string            // 平台默认 SDP 地址,仅运行时填充
	PlatformPlaybackHost string            // 平台默认播放地址,仅运行时填充
	APIPort              int               // ZLM API port
	APISecret            string            // ZLM api.secret
	MediaServerUUID      string            // 业务侧生成,启动时写入 ZLM general.mediaServerId
	Weight               int               // 0-100,加权轮询,默认 50
	Tags                 map[string]string // 任意标签
	State                State
	// AdminState is the operator-controlled admission intent. Empty means
	// enabled for backwards compatibility with nodes created before this field.
	AdminState          string
	RecoveryRequired    bool   // 外部配置收敛不确定时 fail-close
	RecoveryReason      string // 安全、有限长的恢复原因
	RecoveryFingerprint string // candidate endpoint 的不透明 SHA-256 指纹
	RTPReceiveMode      string // single or multi; empty legacy value means multi
	RTPProxyPort        int    // fixed shared RTP listener
	RTPPortStart        int    // rtp_proxy.port_range 起
	RTPPortEnd          int    // rtp_proxy.port_range 止
	Stats               Stats  // 实时状态,内存,心跳更新
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// EffectiveSDPIP 返回写进 SDP c= 行、也就是告诉设备「把RTP 推到哪」 的地址。
//
// 这是收流地址的**唯一**来源:2026-10-20 起旧的 receive_host 列已并入 sdp_ip
// (两者原本都只是"写入 SDP 的 c= 地址",属于同一件事的两个名字 ⇒ 合并,不再保留兜底)。
//
// 兜底顺序：节点 sdp_ip → 平台 sdp_ip → 平台播放地址 → 节点播放地址。
//
// ⛔⛔ **这里不兜底到 n.Host**。Host 是 ZLM 的 API 地址,单机部署下是 127.0.0.1;
// 写进 SDP 等于让设备把流推向平台自己 —— 表征是"信令全成功、10 秒后 play_timeout、
// ZLM 零 RTP 包",现场完全看不出是配置问题。宁可让上层显式拒绝这种节点参与调度。
//
// 播放地址可以当最后兜底,因为它是运维显式填的"对外可达地址"(wvp-GB28181-pro
// 的 MediaServer 同样用 ip 兜底 sdpIp);而 Host 是系统推导出来的,不能当答案。
func (n Node) EffectiveSDPIP() string {
	if host := strings.TrimSpace(n.SDPIP); host != "" {
		return host
	}
	if host := strings.TrimSpace(n.PlatformSDPIP); host != "" {
		return host
	}
	if host := strings.TrimSpace(n.PlatformPlaybackHost); host != "" {
		return host
	}
	return strings.TrimSpace(n.PlaybackHost)
}

// HasUsableSDPIP 报告该节点能否被安全地下发 SDP。
func (n Node) HasUsableSDPIP() bool {
	return strings.TrimSpace(n.EffectiveSDPIP()) != ""
}

// EffectivePlaybackHost 返回播放 URL 使用的地址。
// 旧节点未配置时回退到 API host,保持升级兼容。
func (n Node) EffectivePlaybackHost() string {
	host := strings.TrimSpace(n.PlaybackHost)
	if host != "" && !isLoopbackPlaybackHost(host) {
		return host
	}
	if host := strings.TrimSpace(n.PlatformPlaybackHost); host != "" {
		return host
	}
	return n.Host
}

func isLoopbackPlaybackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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

// IsEnabled reports whether operators allow this node to receive new work.
// Legacy maintenance rows remain disabled until explicitly activated.
func (n Node) IsEnabled() bool {
	return n.AdminState != "disabled" && n.State != StateMaintenance
}

// IsSchedulable is the shared admission predicate for new work.
func (n Node) IsSchedulable() bool {
	return n.IsActive() && n.IsEnabled()
}

// EffectiveRTPReceiveMode preserves legacy multi-port nodes.
func (n Node) EffectiveRTPReceiveMode() string {
	if n.RTPReceiveMode == "" {
		return "multi"
	}
	return n.RTPReceiveMode
}
