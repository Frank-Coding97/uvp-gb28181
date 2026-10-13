package zlm

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestStartGBSendRTPParameters(t *testing.T) {
	for _, test := range []struct {
		name      string
		transport GBSendRTPTransport
		path      string
		isUDP     string
	}{{name: "udp", transport: GBSendRTPUDP, path: "/index/api/startSendRtp", isUDP: "1"},
		{name: "tcp active", transport: GBSendRTPTCPActive, path: "/index/api/startSendRtp", isUDP: "0"},
		{name: "tcp passive", transport: GBSendRTPTCPPassive, path: "/index/api/startSendRtpPassive", isUDP: "0"}} {
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
