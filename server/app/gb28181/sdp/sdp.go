package sdp

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
)

// SSRC 国标格式:10 位十进制。实时流为 0 + 域第4-8位 + 4位序号。
const (
	SSRCRealtime = "0" // 实时流首位
	SSRCPlayback = "1" // 回放流首位
)

var ErrInvalidRealtimeSSRC = errors.New("非法实时SSRC参数")

var ssrcSeq uint32

// GenRealtimeSSRC is the compatibility generator used by the legacy dynamic
// start path. New coordinated live sessions use play.RealtimeSSRCAllocator.
func GenRealtimeSSRC(domain string) (string, error) {
	sequence := (atomic.AddUint32(&ssrcSeq, 1) - 1) % 10000
	return FormatRealtimeSSRC(domain, uint16(sequence))
}

func FormatRealtimeSSRC(domain string, sequence uint16) (string, error) {
	if len(domain) != 10 || sequence > 9999 {
		return "", ErrInvalidRealtimeSSRC
	}
	for _, digit := range domain {
		if digit < '0' || digit > '9' {
			return "", ErrInvalidRealtimeSSRC
		}
	}
	return fmt.Sprintf("%s%s%04d", SSRCRealtime, domain[3:8], sequence), nil
}

// PlayParams 实时点播 SDP 构造参数
type PlayParams struct {
	ServerID string // 平台国标编码(o= 行)
	RecvIP   string // 收流 IP(ZLM 地址)
	RecvPort int    // 收流端口(ZLM RTP 端口)
	SSRC     string // 媒体流 SSRC
	TCPMode  bool   // true=TCP被动收流, false=UDP
	Extended bool   // true=为兼容部分设备声明额外视频负载类型
}

// BuildPlaySDP 构造实时点播 SDP(平台作主叫,s=Play)
// 遵循 GB/T 28181 附录 SDP 格式，并兼容 WVP 使用的视频负载声明。
func BuildPlaySDP(p PlayParams) string {
	proto := "RTP/AVP"
	var b strings.Builder
	b.WriteString("v=0\r\n")
	b.WriteString(fmt.Sprintf("o=%s 0 0 IN IP4 %s\r\n", p.ServerID, p.RecvIP))
	b.WriteString("s=Play\r\n")
	b.WriteString(fmt.Sprintf("c=IN IP4 %s\r\n", p.RecvIP))
	b.WriteString("t=0 0\r\n")
	writeVideoMediaDescription(&b, p.RecvPort, proto, p.Extended)
	if p.TCPMode {
		b.WriteString("a=setup:passive\r\n")
		b.WriteString("a=connection:new\r\n")
	}
	// y= 行:国标扩展,声明 SSRC(10位)
	b.WriteString(fmt.Sprintf("y=%s\r\n", p.SSRC))
	return b.String()
}

func writeVideoMediaDescription(b *strings.Builder, port int, transport string, extended bool) {
	if extended {
		b.WriteString(fmt.Sprintf("m=video %d %s 96 126 125 99 34 98 97\r\n", port, transport))
		b.WriteString("a=recvonly\r\n")
		b.WriteString("a=rtpmap:96 PS/90000\r\n")
		b.WriteString("a=fmtp:126 profile-level-id=42e01e\r\n")
		b.WriteString("a=rtpmap:126 H264/90000\r\n")
		b.WriteString("a=rtpmap:125 H264S/90000\r\n")
		b.WriteString("a=fmtp:125 profile-level-id=42e01e\r\n")
		b.WriteString("a=rtpmap:99 H265/90000\r\n")
		b.WriteString("a=rtpmap:98 H264/90000\r\n")
		b.WriteString("a=rtpmap:97 MPEG4/90000\r\n")
		return
	}

	b.WriteString(fmt.Sprintf("m=video %d %s 96 97 98 99\r\n", port, transport))
	b.WriteString("a=recvonly\r\n")
	b.WriteString("a=rtpmap:96 PS/90000\r\n")
	b.WriteString("a=rtpmap:97 MPEG4/90000\r\n")
	b.WriteString("a=rtpmap:98 H264/90000\r\n")
	b.WriteString("a=rtpmap:99 H265/90000\r\n")
}
