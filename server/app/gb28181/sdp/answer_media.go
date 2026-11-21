package sdp

import (
	"net"
	"strconv"
	"strings"
)

// AnswerMediaTarget 是设备 200 OK SDP 里声明的视频媒体目标。
//
// 平台作 TCP 主动方(a=setup:active)时,唯一能确定「往哪个地址回连」的依据
// 就是对端应答里的 m=video 端口;IP 优先取媒体级 c=,其次会话级 c=,都没有
// 才由调用方退回信令来源地址。
type AnswerMediaTarget struct {
	IP        string // 空 = 应答里没有可用地址
	Port      int    // 0 = 应答里没有视频端口
	Setup     string // "active" / "passive" / ""(未声明)
	Transport string // 例如 "TCP/RTP/AVP" / "RTP/AVP"
}

// WantsConnectionFromPeer 表示对端要求由它自己发起 TCP 连接(应答里的
// a=setup:active)。平台按被动监听才接得住这种应答。
func (t AnswerMediaTarget) WantsConnectionFromPeer() bool {
	return t.Setup == "active"
}

// ParseAnswerMediaTarget 从 SIP 应答 SDP 中取出视频段的目标地址与端口。
// ok=false 表示整份 SDP 没有 m=video 行。
func ParseAnswerMediaTarget(body []byte) (AnswerMediaTarget, bool) {
	var (
		target    AnswerMediaTarget
		sessionIP string
		seenMedia bool // 第一个 m= 之前出现的 c= 才是会话级地址
		inVideo   bool
		found     bool
	)
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "c="):
			// c=IN IP4 192.0.2.10;媒体级覆盖会话级,音频段里的地址不得影响视频。
			ip := connectionAddress(line)
			if ip == "" {
				continue
			}
			if inVideo {
				target.IP = ip
			} else if !seenMedia {
				sessionIP = ip
			}
		case strings.HasPrefix(line, "m="):
			seenMedia = true
			inVideo = false
			if found || !strings.HasPrefix(line, "m=video ") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			port, err := strconv.Atoi(fields[1])
			if err != nil || port <= 0 {
				// 端口为 0 在 SDP 里表示「本段禁用」,不能当回连目标。
				continue
			}
			target.Port = port
			if len(fields) >= 3 {
				target.Transport = fields[2]
			}
			inVideo = true
			found = true
		case inVideo && strings.HasPrefix(strings.ToLower(line), "a=setup:"):
			target.Setup = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(line), "a=setup:")))
		}
	}
	if !found {
		return AnswerMediaTarget{}, false
	}
	if target.IP == "" {
		target.IP = sessionIP
	}
	if ip := net.ParseIP(target.IP); ip != nil && (ip.IsUnspecified() || ip.IsLoopback()) {
		// 0.0.0.0 / :: 是「我在这里监听」的占位,不能拿去回连;交给调用方
		// 退回信令来源地址。
		target.IP = ""
	}
	return target, true
}

// connectionAddress 从 c= 行取出地址部分,过滤 IP4/IP6 前缀。
func connectionAddress(line string) string {
	fields := strings.Fields(strings.TrimPrefix(line, "c="))
	if len(fields) < 3 {
		return ""
	}
	addr := fields[2]
	// 组播/单播地址可能带 /ttl/count 后缀。
	if idx := strings.IndexByte(addr, '/'); idx >= 0 {
		addr = addr[:idx]
	}
	return strings.TrimSpace(addr)
}
