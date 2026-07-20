package node

import "strconv"

// ServerConfig ZLMediaKit 节点服务端口配置(对应 ZLM `/index/api/getServerConfig` 返回值中的端口字段)
//
// ZLM 的端口在其自身配置里,不通过 meta_node 表管理;运行时从 API 拉取,给需要构造播放 / 抓帧
// URL 的场景使用(如通道快照的 rtsp 拉流)。
type ServerConfig struct {
	HTTPPort    int // http.port,默认 80,项目里改成了 18080
	HTTPSPort   int // http.sslport
	RTSPPort    int // rtsp.port,默认 554,项目里 10554
	RTSPSPort   int // rtsp.sslport
	RTMPPort    int // rtmp.port,默认 1935,项目里 11935
	RTMPSPort   int // rtmp.sslport
	RTPProxyPort int // rtp_proxy.port(单端口收流)
	ONVIFPort   int // onvif.port
}

// ParseServerConfig 从 ZLM getServerConfig 返回的 map[string]string 里抽端口字段。
//
// 只解析当前需要的字段;无法解析的字段静默跳过(用 0 兜底,调用方判断)。
func ParseServerConfig(m map[string]string) ServerConfig {
	return ServerConfig{
		HTTPPort:     atoiOrZero(m["http.port"]),
		HTTPSPort:    atoiOrZero(m["http.sslport"]),
		RTSPPort:     atoiOrZero(m["rtsp.port"]),
		RTSPSPort:    atoiOrZero(m["rtsp.sslport"]),
		RTMPPort:     atoiOrZero(m["rtmp.port"]),
		RTMPSPort:    atoiOrZero(m["rtmp.sslport"]),
		RTPProxyPort: atoiOrZero(m["rtp_proxy.port"]),
		ONVIFPort:    atoiOrZero(m["onvif.port"]),
	}
}

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
