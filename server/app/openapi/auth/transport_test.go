package auth

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAPITransportTrustsOnlyDirectTLSOrExplicitProxy(t *testing.T) {
	policy, err := NewTLSBoundary([]string{"127.0.0.1", "192.0.2.0/24", "2001:db8::/32"})
	require.NoError(t, err)
	for _, tc := range []struct {
		peer    string
		headers []string
		secure  bool
	}{
		{"192.0.2.3:3456", []string{"https"}, true},
		{"198.51.100.3:3456", []string{"https"}, false},
		{"192.0.2.3:3456", nil, false},
		{"192.0.2.3:3456", []string{"https", "https"}, false},
		{"192.0.2.3:3456", []string{"https,http"}, false},
		{"192.0.2.3:3456", []string{" https"}, false},
		{"192.0.2.3:3456", []string{"HTTPS"}, false},
		{"[2001:db8::1]:3456", []string{"https"}, true},
		{"[::ffff:127.0.0.1]:3456", []string{"https"}, true},
	} {
		r := httptest.NewRequest("GET", "/openapi/v1/devices", nil)
		r.RemoteAddr = tc.peer
		r.Header["X-Forwarded-Proto"] = tc.headers
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		require.Equal(t, tc.secure, policy.IsHTTPS(r), "%s %v", tc.peer, tc.headers)
	}
	r := httptest.NewRequest("GET", "/openapi/v1/devices", nil)
	r.RemoteAddr = "198.51.100.3:99"
	r.TLS = &tls.ConnectionState{HandshakeComplete: true}
	require.True(t, policy.IsHTTPS(r))
	policy, err = NewTLSBoundary(nil)
	require.NoError(t, err)
	r.TLS = nil
	r.Header.Set("X-Forwarded-Proto", "https")
	require.False(t, policy.IsHTTPS(r))
	for _, value := range []string{"*", "0.0.0.0/0", "::/0", "proxy.example", "bad/cidr"} {
		_, err = NewTLSBoundary([]string{value})
		require.Error(t, err, value)
	}
}
