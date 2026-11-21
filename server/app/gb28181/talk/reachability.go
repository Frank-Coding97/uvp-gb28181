package talk

import (
	"net"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/sdp"
)

// BroadcastTransportWarning 说明「广播信令已经通了，但音频大概率到不了设备」。
//
// 广播的传输方式由设备的 SDP offer 决定，平台只能照办：
//   - udp        平台直接往设备给的媒体地址发包，跨网段必丢；
//   - tcp_active 设备 offer 写 setup:passive（等平台来连），跨网段连不进去；
//   - tcp_passive 设备 offer 写 setup:active（设备来连平台），NAT 下唯一能通的形态。
//
// 跨网段用「设备 SDP 给的媒体地址」与「SIP 信令的真实来源地址」比 /24 判断。
// 后者是收到的 SIP 包源地址（models.GbDevice.IP），不是 Contact/Via 里那个
// —— 设备在 NAT 后面时 Contact/Via 通常还是自己的内网地址，拿它判断会一直判成"同网段"。
//
// ⛔ 只用于提示，不参与任何放行/拒绝判断：跨三层但路由已经打通的网络会被判成
// "不同段"，据此阻断会把本来能用的广播打死。文案里也刻意用"大概率"而非"一定"。
func BroadcastTransportWarning(mode models.TalkSessionMode, senderMode, remoteMediaIP, deviceIP string) string {
	if mode != models.TalkSessionModeBroadcast {
		return ""
	}
	senderMode = strings.ToLower(strings.TrimSpace(senderMode))
	// 没协商出传输方式（还没拿到设备 offer）时不判断。
	if senderMode == "" || senderMode == string(sdp.BroadcastSenderTCPPassive) {
		return ""
	}
	same, known := sameIPv4Subnet24(remoteMediaIP, deviceIP)
	if !known || same {
		return ""
	}
	if senderMode == string(sdp.BroadcastSenderUDP) {
		return "设备用 UDP 传音频，与平台不在同一网段，声音大概率传不到设备。请改用 Talk 模式，或在设备侧把媒体传输改成 TCP 主动（active）。"
	}
	return "设备要求由平台主动连接，跨网段时连不进去。请改用 Talk 模式，或在设备侧把媒体传输改成 TCP 主动（active）。"
}

// sameIPv4Subnet24 判断两个地址是否落在同一个 /24。
//
// GB28181 场景只有 IPv4；任一地址解析失败都返回 known=false —— 调用方据此
// 放弃判断，而不是当成"不同网段"去报警。
func sameIPv4Subnet24(a, b string) (same bool, known bool) {
	ipA := net.ParseIP(strings.TrimSpace(a)).To4()
	ipB := net.ParseIP(strings.TrimSpace(b)).To4()
	if ipA == nil || ipB == nil {
		return false, false
	}
	return ipA[0] == ipB[0] && ipA[1] == ipB[1] && ipA[2] == ipB[2], true
}
