package sdp

import (
	"fmt"
	"strings"
)

// TalkParams are the values that vary in the first-phase TCP/PCMA talk SDP.
type TalkParams struct {
	ServerID string
	RecvIP   string
	RecvPort int
	SSRC     string
}

// BuildTalkSDP describes the passive TCP socket opened by ZLM. GB28181 talk
// is bidirectional, so the media direction is sendrecv rather than recvonly.
func BuildTalkSDP(p TalkParams) string {
	var b strings.Builder
	b.WriteString("v=0\r\n")
	b.WriteString(fmt.Sprintf("o=%s 0 0 IN IP4 %s\r\n", p.ServerID, p.RecvIP))
	b.WriteString("s=Talk\r\n")
	b.WriteString(fmt.Sprintf("c=IN IP4 %s\r\n", p.RecvIP))
	b.WriteString("t=0 0\r\n")
	b.WriteString(fmt.Sprintf("m=audio %d TCP/RTP/AVP 8\r\n", p.RecvPort))
	b.WriteString("a=setup:passive\r\n")
	b.WriteString("a=connection:new\r\n")
	b.WriteString("a=sendrecv\r\n")
	b.WriteString("a=rtpmap:8 PCMA/8000\r\n")
	b.WriteString(fmt.Sprintf("y=%s\r\n", p.SSRC))
	b.WriteString("f=v/////a/1/8/1\r\n")
	return b.String()
}
