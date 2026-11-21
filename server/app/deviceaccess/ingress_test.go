package deviceaccess

import (
	"context"
	"crypto/tls"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestResolveIngress(t *testing.T) {
	for _, tt := range []struct {
		name, peer, host, advertised, want string
		trusted                            []string
		tls, reject                        bool
	}{
		{name: "direct retains port", peer: "203.0.113.9:9000", host: "192.168.1.9:5177", want: "http://192.168.1.9:5177"},
		{name: "trusted nginx HTTP from HTTPS console", peer: "127.0.0.1:9000", host: "camera.example:51001", advertised: "http://camera.example:51000", want: "http://camera.example:51000"},
		{name: "trusted external TLS port", peer: "172.18.0.2:9000", trusted: []string{"172.18.0.0/16"}, host: "backend:8280", advertised: "https://camera.example:8443", want: "https://camera.example:8443"},
		{name: "untrusted header ignored", peer: "203.0.113.9:9000", host: "camera.example:8080", advertised: "https://attacker.example", want: "http://camera.example:8080"},
		{name: "direct TLS", peer: "203.0.113.9:9000", host: "camera.example:8443", tls: true, want: "https://camera.example:8443"},
		{name: "IPv6 retains port", peer: "127.0.0.1:9000", host: "[2001:db8::8]:5177", want: "http://[2001:db8::8]:5177"},
		{name: "localhost", peer: "127.0.0.1:9000", host: "localhost:5177", reject: true},
		{name: "unspecified", host: "[::]:5177", reject: true},
		{name: "invalid proxy metadata fails closed", peer: "127.0.0.1:9000", host: "camera.example", advertised: "http://evil.example/path?key=x", reject: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://example.test/api", nil)
			r.Host = tt.host
			r.RemoteAddr = tt.peer
			r.Header.Set(Header, tt.advertised)
			r.Header.Set("X-Forwarded-Host", "forged.example")
			r.Header.Set("X-Forwarded-Proto", "https")
			if tt.tls {
				r.TLS = &tls.ConnectionState{}
			}
			got, err := Resolve(r, tt.trusted)
			if tt.reject {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
func TestContextAndBackgroundIngress(t *testing.T) {
	_, err := FromContext(context.Background())
	require.Error(t, err)
	ctx := WithBaseURL(context.Background(), "http://192.168.1.8:5177")
	base, err := FromContext(ctx)
	require.NoError(t, err)
	require.Equal(t, "http://192.168.1.8:5177", base)
	base, err = ForHost("192.168.1.8", "15500")
	require.NoError(t, err)
	require.Equal(t, "http://192.168.1.8:15500", base)
	_, err = ForHost("0.0.0.0", "15500")
	require.Error(t, err)
	_, err = ForHost("192.168.1.8", "0")
	require.Error(t, err)
}

func TestInvalidIngress(t *testing.T) {
	for _, raw := range []string{"ftp://camera.example", "http://camera.example:70000", "http://user:pass@camera.example", "http://camera.example?token=x", "http://camera.example/#secret", "http://camera.example/path", "http://foo.localhost:5177", "http://[fe80::1%25en0]:5177", "http://224.0.0.1"} {
		_, err := Validate(raw)
		require.Error(t, err, raw)
	}
}
