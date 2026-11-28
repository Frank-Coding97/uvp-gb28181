package sdp

import "testing"

func TestBuildTalkSDP(t *testing.T) {
	got := BuildTalkSDP(TalkParams{
		ServerID: "34020000002000000001", RecvIP: "192.0.2.10",
		RecvPort: 32100, SSRC: "0200000001",
	})
	want := "v=0\r\n" +
		"o=34020000002000000001 0 0 IN IP4 192.0.2.10\r\n" +
		"s=Talk\r\n" +
		"c=IN IP4 192.0.2.10\r\n" +
		"t=0 0\r\n" +
		"m=audio 32100 TCP/RTP/AVP 8\r\n" +
		"a=setup:passive\r\n" +
		"a=connection:new\r\n" +
		"a=sendrecv\r\n" +
		"a=rtpmap:8 PCMA/8000\r\n" +
		"y=0200000001\r\n" +
		"f=v/////a/1/8/1\r\n"
	if got != want {
		t.Fatalf("unexpected talk SDP:\n%s\nwant:\n%s", got, want)
	}
}

// 设备只支持 G.711U 时，平台 offer 必须整体换成 PCMU：m 行 PT=0、
// rtpmap 写 PCMU/8000、f 行音频编码格式写 2。只改 rtpmap 不改 m 行 PT
// 是本仓踩过的类型错误（设备会按 PT 8 去解 μ-law 数据）。
func TestBuildTalkSDPPCMU(t *testing.T) {
	got := BuildTalkSDP(TalkParams{
		ServerID: "34020000002000000001", RecvIP: "192.0.2.10",
		RecvPort: 32100, SSRC: "0200000001", Codec: AudioCodecPCMU,
	})
	want := "v=0\r\n" +
		"o=34020000002000000001 0 0 IN IP4 192.0.2.10\r\n" +
		"s=Talk\r\n" +
		"c=IN IP4 192.0.2.10\r\n" +
		"t=0 0\r\n" +
		"m=audio 32100 TCP/RTP/AVP 0\r\n" +
		"a=setup:passive\r\n" +
		"a=connection:new\r\n" +
		"a=sendrecv\r\n" +
		"a=rtpmap:0 PCMU/8000\r\n" +
		"y=0200000001\r\n" +
		"f=v/////a/2/8/1\r\n"
	if got != want {
		t.Fatalf("unexpected PCMU talk SDP:\n%s\nwant:\n%s", got, want)
	}
}
