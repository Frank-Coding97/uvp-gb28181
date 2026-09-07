package playurl

import (
	"testing"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestWebRTCURLsRespectReportedTransportPorts(t *testing.T) {
	for _, tc := range []struct {
		name, udp, tcp string
		want           bool
	}{
		{"disabled", "0", "0", false},
		{"udp", "18000", "0", true},
		{"tcp", "0", "18000", true},
		{"invalid", "bad", "-1", false},
		{"out_of_range", "65536", "0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := node.ParseServerConfig(map[string]string{"http.port": "18080", "http.sslport": "18443", "rtc.port": tc.udp, "rtc.tcpPort": tc.tcp})
			urls := Build("192.0.2.52", cfg, "live", "camera")
			if (urls.WebRTC != nil) != tc.want || (urls.WebRTCS != nil) != tc.want {
				t.Fatal("WebRTC URL does not reflect actual transport capability")
			}
			if urls.HTTPFLV == nil || urls.WSFLV == nil {
				t.Fatal("RTC capability affected HTTP/WS playback")
			}
		})
	}
}

func TestLegacyNodeWithoutRTCFieldsKeepsURLContract(t *testing.T) {
	cfg := node.ParseServerConfig(map[string]string{"http.port": "18080"})
	if Build("127.0.0.1", cfg, "live", "camera").WebRTC == nil {
		t.Fatal("legacy capability response changed")
	}
}
