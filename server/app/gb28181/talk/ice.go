package talk

import (
	"net"
	"strconv"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// ICEServer 是下发给浏览器的最小 RTCIceServer 形状。
//
// 只带 urls;TURN 凭据(rtc.iceUfrag / rtc.icePwd)在启用中继时再补,当前不下发。
type ICEServer struct {
	URLs []string `json:"urls"`
}

// BuildICEServers 推导浏览器要用的 ICE 服务器列表。
//
// ⛔ 只有节点声明了对外可达地址(rtc.externIP)时才下发 STUN,不填就返回 nil
// (调用方保持「不带 iceServers」,浏览器只产 host candidate)。
//
// 为什么这样门禁 —— STUN 唯一的作用是让浏览器拿到自己 NAT 之后的映射地址
// (srflx candidate),而这只有在「浏览器与媒体节点之间存在 NAT」时才有意义,
// 也就是跨网段场景;而跨网段必须先填 rtc.externIP(ZLM 自己写进 SDP 的候选地址也要靠它)。
// 于是 rtc.externIP 顺理成章地同时充当「跨网段开关」。
//
// 反过来,同网段(rtc.externIP 为空)下 host candidate 本来就直连可达,再下发 STUN
// 一分收益没有,却凭空引入一个硬依赖:STUN 端口一旦不可达(实测 node 2 的 rtc.icePort
// 是 docker 容器内端口、没映射出来),浏览器会一直等 STUN 重传,iceGatheringState
// 迟迟到不了 complete,整个发布被拖到超时才失败 —— 一个纯装饰性的优化把会话拖死了。
//
// rtc.externIP 是对端实际能连的地址,弹性公网 IP / NAT 映射场景下它与节点登记的业务地址不同。
// 端口越界同样返回 nil。
func BuildICEServers(config node.ServerConfig) []ICEServer {
	endpointHost := strings.TrimSpace(config.RTCExternalIP)
	if endpointHost == "" || config.RTCICEPort <= 0 || config.RTCICEPort > 65535 {
		return nil
	}
	// 容错:externIP 可能被登记成 host:port,而 STUN 端口由 rtc.icePort 决定,
	// 直接拼会得到两段端口。
	if parsed, _, err := net.SplitHostPort(endpointHost); err == nil {
		endpointHost = parsed
	}
	endpoint := net.JoinHostPort(endpointHost, strconv.Itoa(config.RTCICEPort))
	return []ICEServer{{URLs: []string{"stun:" + endpoint}}}
}
