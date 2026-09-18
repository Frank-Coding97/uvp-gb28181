package zlm

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// OpenAPIControlTLS is a trusted operator-provided control-plane binding, not
// an HTTP DTO or a claim inferred from private IPs/tags. Root validation AND
// the explicit per-node SPKI pin must succeed. Nil Roots uses system roots.
// This transport does not prove exclusive deployment topology or media policy.
type OpenAPIControlTLS struct {
	Endpoint   string
	Roots      *x509.CertPool
	SPKISHA256 [32]byte
}

// OpenAPIRuntimeControl deliberately exposes no legacy kick or config mutation
// method. It is not automatically installed into ordinary backend node clients.
type OpenAPIRuntimeControl struct {
	client    *Client
	transport *http.Transport
}

func NewOpenAPIRuntimeControl(n node.Node, settings OpenAPIControlTLS) (*OpenAPIRuntimeControl, error) {
	endpoint, err := url.Parse(settings.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Path != "/index/api" || endpoint.RawPath != "" || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" || endpoint.Opaque != "" || endpoint.String() != settings.Endpoint || n.ID <= 0 || n.Revision == 0 || strings.TrimSpace(n.MediaServerUUID) == "" || n.APISecret == "" || settings.SPKISHA256 == ([32]byte{}) {
		return nil, ErrRuntimeControlUnavailable
	}
	roots := settings.Roots
	if roots != nil {
		roots = roots.Clone()
	}
	pin := settings.SPKISHA256
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		TLSHandshakeTimeout:   2 * time.Second,
		ResponseHeaderTimeout: 2 * time.Second,
		ForceAttemptHTTP2:     false,
		TLSNextProto:          make(map[string]func(string, *tls.Conn) http.RoundTripper),
		DisableKeepAlives:     true, // also prevents transparent retry of reused connections
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12, RootCAs: roots, NextProtos: []string{"http/1.1"},
			// Standard chain/hostname/validity verification runs first. There
			// is no InsecureSkipVerify escape hatch, even for a matching pin.
			VerifyConnection: func(state tls.ConnectionState) error {
				if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 || state.NegotiatedProtocol != "" && state.NegotiatedProtocol != "http/1.1" {
					return ErrRuntimeControlUnavailable
				}
				actual := sha256.Sum256(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
				if subtle.ConstantTimeCompare(actual[:], pin[:]) != 1 {
					return ErrRuntimeControlUnavailable
				}
				return nil
			},
		},
	}
	client := &Client{node: &n, baseURL: settings.Endpoint, secret: n.APISecret, http: &http.Client{
		Transport: transport, Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	return &OpenAPIRuntimeControl{client: client, transport: transport}, nil
}

func (c *OpenAPIRuntimeControl) Close() {
	if c != nil && c.transport != nil {
		c.transport.CloseIdleConnections()
	}
}

func (c *OpenAPIRuntimeControl) GetRuntimeIdentity(ctx context.Context) (RuntimeIdentity, error) {
	if c == nil || c.client == nil || ctx == nil {
		return RuntimeIdentity{}, ErrRuntimeControlUnavailable
	}
	return c.client.GetRuntimeIdentity(ctx)
}

func (c *OpenAPIRuntimeControl) GetRuntimeSessions(ctx context.Context) (RuntimeSessions, error) {
	if c == nil || c.client == nil || ctx == nil {
		return RuntimeSessions{}, ErrRuntimeControlUnavailable
	}
	return c.client.GetRuntimeSessions(ctx)
}

func (c *OpenAPIRuntimeControl) GetRuntimeMediaPlayers(ctx context.Context, target StreamTarget) (RuntimePlayers, error) {
	if c == nil || c.client == nil || ctx == nil {
		return RuntimePlayers{}, ErrRuntimeControlUnavailable
	}
	return c.client.GetRuntimeMediaPlayers(ctx, target)
}

func (c *OpenAPIRuntimeControl) KickSessionIfMatch(ctx context.Context, bootNonce, identifier string) (ConditionalKickResult, error) {
	if c == nil || c.client == nil || ctx == nil {
		return "", ErrRuntimeControlUnavailable
	}
	return c.client.KickSessionIfMatch(ctx, bootNonce, identifier)
}
