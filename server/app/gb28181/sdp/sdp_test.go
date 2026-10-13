package sdp

import (
	"strings"
	"testing"
)

// TestGenRealtimeSSRC T4-测1: SSRC 符合国标(10位,实时0开头,唯一)
func TestGenRealtimeSSRC(t *testing.T) {
	domain := "3402000000"
	seen := make(map[string]bool)
	for i := 0; i < 10; i++ {
		s, err := GenRealtimeSSRC(domain)
		if err != nil {
			t.Fatalf("生成 SSRC 失败: %v", err)
		}
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

func TestFormatRealtimeSSRCUsesDomainFiveDigitsAndFourDigitSequence(t *testing.T) {
	got, err := FormatRealtimeSSRC("3402000000", 0)
	if err != nil {
		t.Fatalf("格式化 SSRC 失败: %v", err)
	}
	if got != "0200000000" {
		t.Fatalf("SSRC 分段错误: got=%s want=0200000000", got)
	}
}

func TestFormatRealtimeSSRCRejectsInvalidDomain(t *testing.T) {
	for _, domain := range []string{"", "340200000", "34020000000", "34020x0000"} {
		if _, err := FormatRealtimeSSRC(domain, 0); err == nil {
			t.Fatalf("非法域应被拒绝: %q", domain)
		}
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
	for _, want := range []string{
		"m=video 40000 TCP/RTP/AVP",
		"a=setup:passive",
		"a=connection:new",
	} {
		if !strings.Contains(sdp, want) {
			t.Errorf("TCP模式应含 %q\n完整SDP:\n%s", want, sdp)
		}
	}
	if strings.Contains(sdp, "m=video 40000 RTP/AVP") {
		t.Fatalf("TCP模式不应声明 UDP 传输:\n%s", sdp)
	}
}

func TestBuildPlaySDPExtendedCompatibility(t *testing.T) {
	got := BuildPlaySDP(PlayParams{
		ServerID: "34020000002000000001", RecvIP: "192.0.2.10", RecvPort: 40000,
		SSRC: "0200000001", Extended: true,
	})

	for _, want := range []string{
		"m=video 40000 RTP/AVP 96 126 125 99 34 98 97\r\n",
		"a=rtpmap:126 H264/90000\r\n",
		"a=rtpmap:125 H264S/90000\r\n",
		"a=rtpmap:99 H265/90000\r\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("扩展兼容 SDP 缺少 %q:\n%s", want, got)
		}
	}
}

func TestBuildPlaySDPDefaultCompatibility(t *testing.T) {
	got := BuildPlaySDP(PlayParams{ServerID: "x", RecvIP: "1.2.3.4", RecvPort: 40000, SSRC: "0200000001"})
	if !strings.Contains(got, "m=video 40000 RTP/AVP 96 97 98 99\r\n") ||
		!strings.Contains(got, "a=rtpmap:99 H265/90000\r\n") {
		t.Fatalf("默认 SDP 应保留 WVP 基础兼容负载:\n%s", got)
	}
	if strings.Contains(got, "H264S") {
		t.Fatalf("默认 SDP 不应包含扩展编码声明:\n%s", got)
	}
}
