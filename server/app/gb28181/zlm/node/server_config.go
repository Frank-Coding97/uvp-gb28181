package node

import (
	"strconv"
	"strings"
)

// ServerConfig ZLMediaKit 节点服务端口配置(对应 ZLM `/index/api/getServerConfig` 返回值中的端口字段)
//
// ZLM 的端口在其自身配置里,不通过 meta_node 表管理;运行时从 API 拉取,给需要构造播放 / 抓帧
// URL 的场景使用(如通道快照的 rtsp 拉流)。
type ServerConfig struct {
	HTTPPort     int // http.port,默认 80,项目里改成了 18080
	HTTPSPort    int // http.sslport
	RTSPPort     int // rtsp.port,默认 554,项目里 10554
	RTSPSPort    int // rtsp.sslport
	RTMPPort     int // rtmp.port,默认 1935,项目里 11935
	RTMPSPort    int // rtmp.sslport
	RTPProxyPort int // rtp_proxy.port(单端口收流)
	ONVIFPort    int // onvif.port

	// RTPPortRangeStart/End 对应 rtp_proxy.port_range(多端口收流的动态段)。
	//
	// 这是**平台登记值的真源**:读到它就该回写 meta_node.rtp_port_start/end
	// (见 ParseRTPPortRange 顶部注释)。0 表示 ZLM 没返回该键或值不可解析 ——
	// 调用方必须显式判断,不许把 0 当端口用。
	RTPPortRangeStart int
	RTPPortRangeEnd   int

	// WebRTC(rtc.*)段。与 rtp_proxy.port_range 无关:后者是 GB28181 设备推流的收流段,
	// 这里是浏览器 WebRTC 媒体面走的端口。
	RTCUDPPort int // rtc.port,WebRTC 媒体 UDP 单端口(默认 8000);所有 rtc 客户端共用这一个
	RTCTCPPort int // rtc.tcpPort,WebRTC over TCP 回退端口(udp 不通时启用)
	RTCICEPort int // rtc.icePort,内置 STUN/TURN 的 UDP 监听端口(默认 3478)
	// RTCExternalIP 对应 rtc.externIP,会被写进 SDP 的候选地址。为空表示未配置,
	// 此时 ZLM 只能给出本机网卡地址,跨网段对端连不上。
	RTCExternalIP string

	// 协议开关缺失时按 ZLM 默认启用,只有明确为 0 才关闭。
	RTSPEnabled bool
	RTMPEnabled bool
	HLSEnabled  bool
	TSEnabled   bool
	FMP4Enabled bool
}

// ParseServerConfig 从 ZLM getServerConfig 返回的 map[string]string 里抽端口字段。
//
// 只解析当前需要的字段;无法解析的字段静默跳过(用 0 兜底,调用方判断)。
func ParseServerConfig(m map[string]string) ServerConfig {
	rangeStart, rangeEnd, rangeOK := ParseRTPPortRange(m["rtp_proxy.port_range"])
	if !rangeOK {
		rangeStart, rangeEnd = 0, 0
	}
	return ServerConfig{
		HTTPPort:     atoiOrZero(m["http.port"]),
		HTTPSPort:    atoiOrZero(m["http.sslport"]),
		RTSPPort:     atoiOrZero(m["rtsp.port"]),
		RTSPSPort:    atoiOrZero(m["rtsp.sslport"]),
		RTMPPort:     atoiOrZero(m["rtmp.port"]),
		RTMPSPort:    atoiOrZero(m["rtmp.sslport"]),
		RTPProxyPort: atoiOrZero(m["rtp_proxy.port"]),
		ONVIFPort:    atoiOrZero(m["onvif.port"]),

		RTPPortRangeStart: rangeStart,
		RTPPortRangeEnd:   rangeEnd,

		RTCUDPPort:    atoiOrZero(m["rtc.port"]),
		RTCTCPPort:    atoiOrZero(m["rtc.tcpPort"]),
		RTCICEPort:    atoiOrZero(m["rtc.icePort"]),
		RTCExternalIP: strings.TrimSpace(m["rtc.externIP"]),

		RTSPEnabled: protocolEnabled(m["protocol.enable_rtsp"]),
		RTMPEnabled: protocolEnabled(m["protocol.enable_rtmp"]),
		HLSEnabled:  protocolEnabled(m["protocol.enable_hls"]),
		TSEnabled:   protocolEnabled(m["protocol.enable_ts"]),
		FMP4Enabled: protocolEnabled(m["protocol.enable_fmp4"]),
	}
}

func protocolEnabled(value string) bool { return value != "0" }

// atoiOrZero 容错的 Atoi(空 / 非法都返 0)
func atoiOrZero(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
