package zlm

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestOpenAPITLSControlRequiresVerifiedPinnedTransport(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "fixture-secret", r.Header.Get("secret"))
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, 1, r.ProtoMajor)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, `{"code":0,"data":{"protocolVersion":1,"bootNonce":"000102030405060708090a0b0c0d0e0f"}}`)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	root := x509.NewCertPool()
	root.AddCert(server.Certificate())
	n := node.Node{ID: 1, Revision: 1, MediaServerUUID: "node-a", APISecret: "fixture-secret"}
	settings := OpenAPIControlTLS{Endpoint: server.URL + "/index/api", Roots: root, SPKISHA256: sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)}
	control, err := NewOpenAPIRuntimeControl(n, settings)
	require.NoError(t, err)
	t.Cleanup(control.Close)
	// No use of ProxyFromEnvironment and no fallback to Node.HTTPEndpoint.
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	identity, err := control.GetRuntimeIdentity(context.Background())
	require.NoError(t, err)
	require.Equal(t, "000102030405060708090a0b0c0d0e0f", identity.BootNonce)
	require.EqualValues(t, 1, calls.Load())
	for _, change := range []func(*OpenAPIControlTLS){
		func(c *OpenAPIControlTLS) { c.SPKISHA256[0] ^= 1 },
		func(c *OpenAPIControlTLS) { c.Roots = x509.NewCertPool() },
	} {
		bad := settings
		change(&bad)
		client, err := NewOpenAPIRuntimeControl(n, bad)
		require.NoError(t, err)
		_, err = client.GetRuntimeIdentity(context.Background())
		require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
		client.Close()
	}
	require.EqualValues(t, 1, calls.Load(), "wrong trust never reaches application or sends secret")
	for _, endpoint := range []string{strings.Replace(settings.Endpoint, "https:", "http:", 1), settings.Endpoint + "?x=1", settings.Endpoint + "?", settings.Endpoint + "#x", settings.Endpoint + "/", server.URL + "/index/%61pi", "https://user:password@example.test/index/api"} {
		bad := settings
		bad.Endpoint = endpoint
		_, err := NewOpenAPIRuntimeControl(n, bad)
		require.Error(t, err)
	}
	settings.SPKISHA256 = [32]byte{}
	_, err = NewOpenAPIRuntimeControl(n, settings)
	require.Error(t, err, "a private host or a CA alone does not replace the explicit node pin")
}

func TestOpenAPITLSControlNeverFollowsRedirect(t *testing.T) {
	var escaped atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { escaped.Add(1) }))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	control, err := NewOpenAPIRuntimeControl(node.Node{ID: 1, Revision: 1, MediaServerUUID: "node-a", APISecret: "fixture-secret"}, OpenAPIControlTLS{Endpoint: server.URL + "/index/api", Roots: roots, SPKISHA256: sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)})
	require.NoError(t, err)
	defer control.Close()
	_, err = control.GetRuntimeIdentity(context.Background())
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
	require.Zero(t, escaped.Load())
}
