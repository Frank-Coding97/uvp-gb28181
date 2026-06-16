package sdp

import (
	"strings"
	"testing"
)

// TestGenRealtimeSSRC T4-测1: SSRC 符合国标(10位,实时0开头,唯一)
func TestGenRealtimeSSRC(t *testing.T) {
	domain := "34020000002000000001"
	seen := make(map[string]bool)
	for i := 0; i < 10; i++ {
		s := GenRealtimeSSRC(domain)
		if len(s) != 10 {
			t.Errorf("SSRC 应10位,实际%d位: %s", len(s), s)
		}
		if s[0] != '0' {
			t.Errorf("实时流 SSRC 首位应为0: %s", s)
		}
		seen[s] = true
	}
	if len(seen) < 2 {
		t.Error("SSRC 应有序号区分,未体现唯一性")
	}
}

// TestExtractMid8 中间8位提取
func TestExtractMid8(t *testing.T) {
	got := extractMid8("34020000002000000001")
	if len(got) != 8 {
		t.Errorf("应8位,实际%d: %s", len(got), got)
	}
}

// TestBuildPlaySDP T4-测2: SDP 构造符合国标
func TestBuildPlaySDP(t *testing.T) {
	sdp := BuildPlaySDP(PlayParams{
		ServerID: "34020000002000000001",
		RecvIP:   "192.168.10.222",
		RecvPort: 40000,
		SSRC:     "0200000001",
	})
	checks := []string{
		"v=0",
		"s=Play",
		"c=IN IP4 192.168.10.222",
		"m=video 40000 RTP/AVP",
		"a=recvonly",
		"a=rtpmap:96 PS/90000",
		"y=0200000001",
	}
	for _, c := range checks {
		if !strings.Contains(sdp, c) {
			t.Errorf("SDP 缺少 %q\n完整SDP:\n%s", c, sdp)
		}
	}
}

// TestBuildPlaySDP_TCP TCP 被动模式
func TestBuildPlaySDP_TCP(t *testing.T) {
	sdp := BuildPlaySDP(PlayParams{ServerID: "x", RecvIP: "1.2.3.4", RecvPort: 40000, SSRC: "0200000001", TCPMode: true})
	if !strings.Contains(sdp, "a=setup:passive") {
		t.Error("TCP模式应含 a=setup:passive")
	}
}
