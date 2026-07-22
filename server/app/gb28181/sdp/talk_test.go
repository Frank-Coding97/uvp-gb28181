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
