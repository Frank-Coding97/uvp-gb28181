package sdp

import (
	"fmt"
	"strings"
)

// TalkParams are the values that vary in the first-phase TCP talk SDP.
type TalkParams struct {
	ServerID string
	RecvIP   string
	RecvPort int
	SSRC     string
	// Codec 决定 m 行的静态载荷类型与 rtpmap。零值按 G.711A 处理，
	// 与 2026-11 之前只发 PCMA 的行为保持一致。
	Codec AudioCodec
}

// BuildTalkSDP describes the passive TCP socket opened by ZLM. GB28181 talk
// is bidirectional, so the media direction is sendrecv rather than recvonly.
func BuildTalkSDP(p TalkParams) string {
	codec := p.Codec.normalize()
	var b strings.Builder
	b.WriteString("v=0\r\n")
	fmt.Fprintf(&b, "o=%s 0 0 IN IP4 %s\r\n", p.ServerID, p.RecvIP)
	b.WriteString("s=Talk\r\n")
	fmt.Fprintf(&b, "c=IN IP4 %s\r\n", p.RecvIP)
	b.WriteString("t=0 0\r\n")
	fmt.Fprintf(&b, "m=audio %d TCP/RTP/AVP %d\r\n", p.RecvPort, codec.PayloadType())
	b.WriteString("a=setup:passive\r\n")
	b.WriteString("a=connection:new\r\n")
	b.WriteString("a=sendrecv\r\n")
	fmt.Fprintf(&b, "a=rtpmap:%d %s\r\n", codec.PayloadType(), codec.RTPMap())
	fmt.Fprintf(&b, "y=%s\r\n", p.SSRC)
	fmt.Fprintf(&b, "f=%s\r\n", codec.FLine())
	return b.String()
}
