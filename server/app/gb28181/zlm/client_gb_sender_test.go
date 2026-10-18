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
