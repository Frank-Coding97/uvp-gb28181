package talk

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

const maxUplinkMessageBytes = 64 << 10

var ErrInvalidUplinkTarget = errors.New("talk uplink target is invalid")
var ErrInvalidUplinkResponse = errors.New("talk uplink response is invalid")

type UplinkAnswer struct {
	StatusCode int
	Body       []byte
	Location   string
}

// NewUplinkProxyClient builds the platform-to-media-node client used by both
// the operator and OpenAPI WHIP proxies. Redirects are returned to the caller
// and never followed to another target.
func NewUplinkProxyClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			// #nosec G402 -- media nodes may use private self-signed certificates.
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			ForceAttemptHTTP2: false,
			MaxIdleConns:      8,
			IdleConnTimeout:   90 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// ProxyUplink forwards one WHIP offer to a service-owned media target. The
// target URL is never returned to the caller.
func ProxyUplink(ctx context.Context, client *http.Client, target *UplinkTarget, offer []byte) (UplinkAnswer, error) {
	if ctx == nil || client == nil || target == nil || len(offer) == 0 || len(offer) > maxUplinkMessageBytes ||
		target.ContentType != "application/sdp" {
		return UplinkAnswer{}, ErrInvalidUplinkTarget
	}
	parsed, err := url.Parse(target.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return UplinkAnswer{}, ErrInvalidUplinkTarget
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(offer))
	if err != nil {
		return UplinkAnswer{}, ErrInvalidUplinkTarget
	}
	request.Header.Set("Content-Type", target.ContentType)
	request.Header.Set("Accept", target.ContentType)
	response, err := client.Do(request)
	if err != nil {
		return UplinkAnswer{}, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxUplinkMessageBytes+1))
	if err != nil || len(body) > maxUplinkMessageBytes {
		return UplinkAnswer{}, ErrInvalidUplinkResponse
	}
	return UplinkAnswer{StatusCode: response.StatusCode, Body: body, Location: response.Header.Get("Location")}, nil
}
