package zlm

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestStartGBSendRTPParameters(t *testing.T) {
	for _, test := range []struct {
		name          string
		transport     GBSendRTPTransport
		path          string
		isUDP         string
		rtcpKeepalive bool
		rtcpTimeout   string
	}{{name: "udp default", transport: GBSendRTPUDP, path: "/index/api/startSendRtp", isUDP: "1", rtcpTimeout: "0"},
		{name: "udp keepalive", transport: GBSendRTPUDP, path: "/index/api/startSendRtp", isUDP: "1", rtcpKeepalive: true, rtcpTimeout: "1"},
		{name: "tcp active", transport: GBSendRTPTCPActive, path: "/index/api/startSendRtp", isUDP: "0"},
		{name: "tcp active with keepalive", transport: GBSendRTPTCPActive, path: "/index/api/startSendRtp", isUDP: "0", rtcpKeepalive: true},
		{name: "tcp passive", transport: GBSendRTPTCPPassive, path: "/index/api/startSendRtpPassive", isUDP: "0"},
		{name: "tcp passive with keepalive", transport: GBSendRTPTCPPassive, path: "/index/api/startSendRtpPassive", isUDP: "0", rtcpKeepalive: true}} {
		t.Run(test.name, func(t *testing.T) {
			c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.path {
					t.Fatalf("path=%s, want %s", r.URL.Path, test.path)
				}
				want := map[string]string{
					"vhost": "__defaultVhost__", "app": "rtp", "stream": "source-1",
					"ssrc": "0200000001", "is_udp": test.isUDP, "only_audio": "0",
					"pt": "96", "use_ps": "1",
				}
				if test.transport != GBSendRTPTCPPassive {
					want["dst_url"] = "192.0.2.20"
					want["dst_port"] = "30000"
				}
				if test.transport == GBSendRTPUDP {
					want["udp_rtcp_timeout"] = test.rtcpTimeout
				} else if r.URL.Query().Has("udp_rtcp_timeout") {
					t.Error("TCP sender must not enable UDP RTCP timeout")
				}
				for key, value := range want {
					if got := r.URL.Query().Get(key); got != value {
						t.Errorf("%s=%q, want %q", key, got, value)
					}
				}
				_, _ = w.Write([]byte(`{"code":0,"local_port":31000}`))
			})
			defer server.Close()

			result, err := c.StartGBSendRTP(context.Background(), GBSendRTPRequest{
				VHost: "__defaultVhost__", App: "rtp", Stream: "source-1", SSRC: "0200000001",
				PayloadType: 96, RemoteIP: "192.0.2.20", RemotePort: 30000, Transport: test.transport,
				RTCPKeepalive: test.rtcpKeepalive,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.LocalPort != 31000 {
				t.Fatalf("localPort=%d", result.LocalPort)
			}
		})
	}
}

func TestGetGBSendRTPInfoReadsOptionalFirstPacketSample(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/listRtpSender" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if got := r.URL.Query().Get("ssrc"); got != "" {
			t.Fatalf("listRtpSender must not use an unsupported ssrc query, got %q", got)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":["0200000001"],"rtp_info":{"0200000001":{"seq":321,"rtptime":654321}}}`))
	})
	defer server.Close()

	info, err := c.GetGBSendRTPInfo(context.Background(), GBSendRTPRequest{
		VHost: "__defaultVhost__", App: "rtp", Stream: "source-1", SSRC: "0200000001", RemoteIP: "192.0.2.20",
	})
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected first packet sample")
	}
	if info.URL != "rtsp://192.0.2.20/rtp/source-1" || info.Seq != 321 || info.RTPTime != 654321 {
		t.Fatalf("unexpected RTP info: %+v", info)
	}
}

func TestGetGBSendRTPInfoUsesLocalMediaURLWhenProvided(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/listRtpSender" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":["0200000001"],"rtp_info":{"0200000001":{"seq":321,"rtptime":654321}}}`))
	})
	defer server.Close()

	info, err := c.GetGBSendRTPInfo(context.Background(), GBSendRTPRequest{
		VHost: "__defaultVhost__", App: "rtp", Stream: "source-1", SSRC: "0200000001",
		RemoteIP: "198.51.100.20", RTPInfoURL: "rtsp://192.0.2.10/rtp/source-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || info.URL != "rtsp://192.0.2.10/rtp/source-1" || info.Seq != 321 || info.RTPTime != 654321 {
		t.Fatalf("unexpected RTP info: %+v", info)
	}
}

func TestGetGBSendRTPInfoOmitsSampleForLegacyNode(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":["0200000001"]}`))
	})
	defer server.Close()

	info, err := c.GetGBSendRTPInfo(context.Background(), GBSendRTPRequest{
		VHost: "v", App: "a", Stream: "s", SSRC: "0200000001", RemoteIP: "192.0.2.1",
	})
	if err != nil || info != nil {
		t.Fatalf("legacy node must omit optional info: info=%+v err=%v", info, err)
	}
}

func TestGetGBSendRTPInfoRequiresCompletePacketSample(t *testing.T) {
	for _, sample := range []string{`{}`, `{"seq":321}`, `{"rtptime":654321}`, `{"seq":null,"rtptime":654321}`, `{"seq":321,"rtptime":null}`} {
		t.Run(sample, func(t *testing.T) {
			c, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"code":0,"rtp_info":{"0200000001":` + sample + `}}`))
			})
			defer server.Close()
			info, err := c.GetGBSendRTPInfo(context.Background(), GBSendRTPRequest{
				VHost: "v", App: "a", Stream: "s", SSRC: "0200000001", RTPInfoURL: "rtsp://192.0.2.10/a/s",
			})
			if err != nil || info != nil {
				t.Fatalf("incomplete sample must not manufacture zero values: info=%+v err=%v", info, err)
			}
		})
	}
}

func TestGetGBSendRTPInfoPreservesValidZeroPacketValues(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"rtp_info":{"0200000001":{"seq":0,"rtptime":0}}}`))
	})
	defer server.Close()
	info, err := c.GetGBSendRTPInfo(context.Background(), GBSendRTPRequest{
		VHost: "v", App: "a", Stream: "s", SSRC: "0200000001", RTPInfoURL: "rtsp://192.0.2.10/a/s",
	})
	if err != nil || info == nil || info.Seq != 0 || info.RTPTime != 0 {
		t.Fatalf("explicit zero packet values are valid: info=%+v err=%v", info, err)
	}
}

func TestResetGBSendRTPInfoUsesScopedSender(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/resetRtpSenderInfo" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		for key, want := range map[string]string{"vhost": "v", "app": "a", "stream": "s", "ssrc": "0200000001"} {
			if got := r.URL.Query().Get(key); got != want {
				t.Fatalf("%s=%q, want %q", key, got, want)
			}
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	})
	defer server.Close()

	if err := c.ResetGBSendRTPInfo(context.Background(), GBSendRTPRequest{VHost: "v", App: "a", Stream: "s", SSRC: "0200000001"}); err != nil {
		t.Fatal(err)
	}
}

func TestStartGBSendRTPValidatesRTCPPortBeforeHTTP(t *testing.T) {
	for _, test := range []struct {
		name      string
		transport GBSendRTPTransport
		port      int
		keepalive bool
		wantError bool
	}{
		{name: "UDP with RTCP needs adjacent port", transport: GBSendRTPUDP, port: 65535, keepalive: true, wantError: true},
		{name: "UDP with RTCP highest valid port", transport: GBSendRTPUDP, port: 65534, keepalive: true},
		{name: "UDP without RTCP allows last port", transport: GBSendRTPUDP, port: 65535},
		{name: "TCP active ignores UDP RTCP", transport: GBSendRTPTCPActive, port: 65535, keepalive: true},
		{name: "TCP passive ignores UDP RTCP", transport: GBSendRTPTCPPassive, keepalive: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			c, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				_, _ = w.Write([]byte(`{"code":0,"local_port":31000}`))
			})
			defer server.Close()
			_, err := c.StartGBSendRTP(context.Background(), GBSendRTPRequest{
				VHost: "v", App: "a", Stream: "s", SSRC: "1", PayloadType: 96,
				RemoteIP: "192.0.2.1", RemotePort: test.port, Transport: test.transport,
				RTCPKeepalive: test.keepalive,
			})
			if test.wantError {
				if err == nil || calls != 0 {
					t.Fatalf("invalid RTCP port must be rejected before HTTP: err=%v calls=%d", err, calls)
				}
			} else if err != nil || calls != 1 {
				t.Fatalf("valid RTP request failed: err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestStartGBSendRTPRejectsInvalidInputWithoutHTTP(t *testing.T) {
	cases := []GBSendRTPRequest{
		{VHost: "v", App: "a", SSRC: "1", PayloadType: 96, RemoteIP: "192.0.2.1", RemotePort: 1, Transport: GBSendRTPUDP},
		{VHost: "v", App: "a", Stream: "s", SSRC: "1", PayloadType: 128, RemoteIP: "192.0.2.1", RemotePort: 1, Transport: GBSendRTPUDP},
		{VHost: "v", App: "a", Stream: "s", SSRC: "1", PayloadType: 96, RemoteIP: "invalid", RemotePort: 1, Transport: GBSendRTPUDP},
		{VHost: "v", App: "a", Stream: "s", SSRC: "1", PayloadType: 96, RemoteIP: "192.0.2.1", RemotePort: 0, Transport: GBSendRTPUDP},
		{VHost: "v", App: "a", Stream: "s", SSRC: "1", PayloadType: 96, Transport: "invalid"},
	}
	for i, input := range cases {
		c := &Client{}
		if _, err := c.StartGBSendRTP(context.Background(), input); err == nil {
			t.Fatalf("case %d must fail", i)
		}
	}
}

func TestStartGBSendRTPDoesNotLeakSecret(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":-1,"msg":"denied"}`))
	})
	defer server.Close()

	_, err := c.StartGBSendRTP(context.Background(), GBSendRTPRequest{
		VHost: "v", App: "a", Stream: "s", SSRC: "1", PayloadType: 96,
		RemoteIP: "192.0.2.1", RemotePort: 30000, Transport: GBSendRTPUDP,
	})
	if err == nil || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("err=%v", err)
	}
}

func TestStartGBSendRTPConnectionErrorDoesNotLeakSecret(t *testing.T) {
	c, server := newMockClient(t, func(http.ResponseWriter, *http.Request) {})
	server.Close()

	_, err := c.StartGBSendRTP(context.Background(), GBSendRTPRequest{
		VHost: "v", App: "a", Stream: "s", SSRC: "1", PayloadType: 96,
		RemoteIP: "192.0.2.1", RemotePort: 30000, Transport: GBSendRTPUDP,
	})
	if err == nil || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("err=%v", err)
	}
}

func TestIsGBSendRTPActiveUsesExactMediaTupleAndSSRC(t *testing.T) {
	for _, test := range []struct {
		name      string
		response  string
		active    bool
		wantError bool
	}{
		{name: "sender exists", response: `{"code":0,"data":["0200000001","0200000002"]}`, active: true},
		{name: "only another sender exists", response: `{"code":0,"data":["0200000002"]}`},
		{name: "all senders stopped", response: `{"code":0,"data":[]}`},
		{name: "no sender data", response: `{"code":0,"data":null}`},
		{name: "source disappeared", response: `{"code":-500,"msg":"can not find the source stream"}`},
		{name: "authentication failure", response: `{"code":-100,"msg":"denied test-secret"}`, wantError: true},
		{name: "unsupported API", response: `{"code":-404,"msg":"api not found"}`, wantError: true},
		{name: "missing response code", response: `{"data":[]}`, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/index/api/listRtpSender" {
					t.Errorf("unexpected sender check path: %s", r.URL.Path)
				}
				for key, value := range map[string]string{"vhost": "__defaultVhost__", "app": "rtp", "stream": "source-1"} {
					if r.URL.Query().Get(key) != value {
						t.Errorf("%s did not use exact source tuple", key)
					}
				}
				_, _ = w.Write([]byte(test.response))
			})
			defer server.Close()
			active, err := c.IsGBSendRTPActive(context.Background(), GBSendRTPRequest{
				VHost: "__defaultVhost__", App: "rtp", Stream: "source-1", SSRC: "0200000001",
			})
			if (err != nil) != test.wantError || active != test.active {
				t.Fatalf("active=%v err=%v, want active=%v error=%v", active, err, test.active, test.wantError)
			}
			if err != nil && strings.Contains(err.Error(), "test-secret") {
				t.Fatal("sender check leaked node secret")
			}
		})
	}
}

func TestIsGBSendRTPActiveRejectsIncompleteIdentityWithoutHTTP(t *testing.T) {
	for _, request := range []GBSendRTPRequest{
		{App: "a", Stream: "s", SSRC: "1"},
		{VHost: "v", Stream: "s", SSRC: "1"},
		{VHost: "v", App: "a", SSRC: "1"},
		{VHost: "v", App: "a", Stream: "s"},
	} {
		c := &Client{}
		if _, err := c.IsGBSendRTPActive(context.Background(), request); err == nil {
			t.Fatal("sender check must require a complete tuple and SSRC")
		}
	}
}

func TestIsGBSendRTPActiveConnectionFailureRemainsError(t *testing.T) {
	c, server := newMockClient(t, func(http.ResponseWriter, *http.Request) {})
	server.Close()
	active, err := c.IsGBSendRTPActive(context.Background(), GBSendRTPRequest{
		VHost: "v", App: "a", Stream: "s", SSRC: "1",
	})
	if err == nil || active || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("transport error must not be reported as stopped: active=%v err=%v", active, err)
	}
}

func TestStartGBSendRTPRequiresCompletePacketSample(t *testing.T) {
	for _, sample := range []string{`{}`, `{"url":"rtsp://192.0.2.10/a/s","seq":321}`, `{"url":"rtsp://192.0.2.10/a/s","rtptime":654321}`} {
		t.Run(sample, func(t *testing.T) {
			c, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"code":0,"local_port":31000,"rtp_info":` + sample + `}`))
			})
			defer server.Close()
			result, err := c.StartGBSendRTP(context.Background(), GBSendRTPRequest{
				VHost: "v", App: "a", Stream: "s", SSRC: "0200000001", Transport: GBSendRTPUDP,
				RemoteIP: "192.0.2.20", RemotePort: 30000, RTPInfoURL: "rtsp://192.0.2.10/a/s",
			})
			if err != nil || result == nil || result.RTPInfo != nil {
				t.Fatalf("incomplete sample must not manufacture packet values: result=%+v err=%v", result, err)
			}
		})
	}
}

func TestResetGBSendRTPInfoRequiresExplicitSuccess(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	defer server.Close()
	if err := c.ResetGBSendRTPInfo(context.Background(), GBSendRTPRequest{
		VHost: "v", App: "a", Stream: "s", SSRC: "0200000001",
	}); err == nil {
		t.Fatal("missing code must not acknowledge sampler reset")
	}
}

func TestStartGBSendRTPKeepsCompleteSampleAndLocalMediaURL(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"local_port":31000,"rtp_info":{"url":"rtsp://198.51.100.20/a/s","seq":0,"rtptime":0}}`))
	})
	defer server.Close()
	result, err := c.StartGBSendRTP(context.Background(), GBSendRTPRequest{
		VHost: "v", App: "a", Stream: "s", SSRC: "0200000001", Transport: GBSendRTPUDP,
		RemoteIP: "198.51.100.20", RemotePort: 30000, RTPInfoURL: "rtsp://192.0.2.10/a/s",
	})
	if err != nil || result == nil || result.RTPInfo == nil {
		t.Fatalf("complete zero sample must be retained: result=%+v err=%v", result, err)
	}
	if result.RTPInfo.URL != "rtsp://192.0.2.10/a/s" || result.RTPInfo.Seq != 0 || result.RTPInfo.RTPTime != 0 {
		t.Fatalf("incorrect RTP info: %+v", result.RTPInfo)
	}
}
