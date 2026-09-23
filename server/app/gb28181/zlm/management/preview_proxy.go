package management

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

const (
	// DefaultSnapshotTimeout bounds the complete upstream request, including
	// response headers and the JPEG body.
	DefaultSnapshotTimeout = 5 * time.Second
	MaxSnapshotTimeout     = 5 * time.Second
	// DefaultSnapshotMaxBytes is deliberately small for a single preview.
	DefaultSnapshotMaxBytes int64 = 2 << 20
	// MaxSnapshotBytes is an absolute process-wide safety bound for options.
	MaxSnapshotBytes int64 = 4 << 20
)

// SecurePreviewProtocol names URLs that can be exposed to a browser or
// client without falling back to an insecure port.
type SecurePreviewProtocol string

const (
	PreviewProtocolHTTPSFLV  SecurePreviewProtocol = "https-flv"
	PreviewProtocolWSSFLV    SecurePreviewProtocol = "wss-flv"
	PreviewProtocolHTTPSFMP4 SecurePreviewProtocol = "https-fmp4"
	PreviewProtocolWSSFMP4   SecurePreviewProtocol = "wss-fmp4"
	PreviewProtocolHTTPSHLS  SecurePreviewProtocol = "https-hls"
	PreviewProtocolHTTPSSTS  SecurePreviewProtocol = "https-ts"
	PreviewProtocolWSSSTS    SecurePreviewProtocol = "wss-ts"
	PreviewProtocolWebRTCS   SecurePreviewProtocol = "webrtcs"
	PreviewProtocolRTMPS     SecurePreviewProtocol = "rtmps"
	PreviewProtocolRTSPS     SecurePreviewProtocol = "rtsps"
)

// BuildSecurePreviewURL constructs one URL from runtime ZLM ports. It never
// substitutes HTTP/RTMP/RTSP ports when the requested secure port is absent.
func BuildSecurePreviewURL(playbackHost string, config node.ServerConfig, identity MediaIdentity, protocol, token string) (string, error) {
	if identity.Validate() != nil || strings.TrimSpace(token) == "" {
		return "", ErrPreviewUnsupported
	}
	protocol = strings.ToLower(strings.TrimSpace(protocol))

	port := 0
	scheme := ""
	path := ""
	query := identity.QueryValues()
	query.Set(PreviewQueryParameter, token)

	switch SecurePreviewProtocol(protocol) {
	case PreviewProtocolHTTPSFLV:
		port, scheme, path = config.HTTPSPort, "https", secureMediaPath(identity, ".live.flv")
	case PreviewProtocolWSSFLV:
		port, scheme, path = config.HTTPSPort, "wss", secureMediaPath(identity, ".live.flv")
	case PreviewProtocolHTTPSFMP4:
		if !config.FMP4Enabled {
			return "", ErrPreviewUnsupported
		}
		port, scheme, path = config.HTTPSPort, "https", secureMediaPath(identity, ".live.mp4")
	case PreviewProtocolWSSFMP4:
		if !config.FMP4Enabled {
			return "", ErrPreviewUnsupported
		}
		port, scheme, path = config.HTTPSPort, "wss", secureMediaPath(identity, ".live.mp4")
	case PreviewProtocolHTTPSHLS:
		if !config.HLSEnabled {
			return "", ErrPreviewUnsupported
		}
		port, scheme, path = config.HTTPSPort, "https", secureHLSPath(identity)
	case PreviewProtocolHTTPSSTS:
		if !config.TSEnabled {
			return "", ErrPreviewUnsupported
		}
		port, scheme, path = config.HTTPSPort, "https", secureMediaPath(identity, ".live.ts")
	case PreviewProtocolWSSSTS:
		if !config.TSEnabled {
			return "", ErrPreviewUnsupported
		}
		port, scheme, path = config.HTTPSPort, "wss", secureMediaPath(identity, ".live.ts")
	case PreviewProtocolWebRTCS:
		port, scheme, path = config.HTTPSPort, "https", "/index/api/webrtc"
		query.Set("type", "play")
	case PreviewProtocolRTMPS:
		if !config.RTMPEnabled {
			return "", ErrPreviewUnsupported
		}
		port, scheme, path = config.RTMPSPort, "rtmps", secureMediaPath(identity, "")
	case PreviewProtocolRTSPS:
		if !config.RTSPEnabled {
			return "", ErrPreviewUnsupported
		}
		port, scheme, path = config.RTSPSPort, "rtsps", secureMediaPath(identity, "")
	default:
		return "", ErrPreviewUnsupported
	}

	host, ok := securePreviewHost(playbackHost, port)
	if !ok {
		return "", ErrPreviewUnsupported
	}
	return scheme + "://" + host + path + "?" + query.Encode(), nil
}

func secureMediaPath(identity MediaIdentity, suffix string) string {
	return "/" + url.PathEscape(identity.App) + "/" + url.PathEscape(identity.Stream) + suffix
}

func secureHLSPath(identity MediaIdentity) string {
	return "/" + url.PathEscape(identity.App) + "/" + url.PathEscape(identity.Stream) + "/hls.m3u8"
}

func securePreviewHost(value string, port int) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || port <= 0 || port > 65535 || strings.ContainsAny(value, "/?#\\") {
		return "", false
	}
	parsed, err := url.Parse("//" + value)
	if err != nil || parsed.User != nil || parsed.Hostname() == "" || parsed.Port() != "" || parsed.Path != "" {
		return "", false
	}
	return net.JoinHostPort(parsed.Hostname(), strconv.Itoa(port)), true
}

// PreviewServerConfigProvider is satisfied by zlm.ServerConfigCache and
// keeps runtime port discovery outside this package's URL formatting code.
type PreviewServerConfigProvider interface {
	Refresh(context.Context, int64) (node.ServerConfig, error)
}

// RuntimeSecurePreviewURLResolver refreshes the actual node runtime config
// before constructing a URL. A cache miss or refresh failure is unsupported;
// it is not a reason to guess a port from node metadata.
type RuntimeSecurePreviewURLResolver struct {
	provider PreviewServerConfigProvider
}

func NewRuntimeSecurePreviewURLResolver(provider PreviewServerConfigProvider) *RuntimeSecurePreviewURLResolver {
	return &RuntimeSecurePreviewURLResolver{provider: provider}
}

func NewSecurePreviewURLResolver(provider PreviewServerConfigProvider) *RuntimeSecurePreviewURLResolver {
	return NewRuntimeSecurePreviewURLResolver(provider)
}

func (r *RuntimeSecurePreviewURLResolver) Resolve(ctx context.Context, mediaNode *node.Node, identity MediaIdentity, protocol, token string) (string, error) {
	if r == nil || r.provider == nil || mediaNode == nil {
		return "", ErrPreviewUnsupported
	}
	if ctx == nil {
		ctx = context.Background()
	}
	config, err := r.provider.Refresh(ctx, mediaNode.ID)
	if err != nil {
		return "", ErrPreviewUnsupported
	}
	return BuildSecurePreviewURL(mediaNode.EffectivePlaybackHost(), config, identity, protocol, token)
}

// SnapshotURLBuilder is the only hook a caller needs to construct the
// internal ZLM snapshot URL. Public proxy methods accept only a node and a
// media identity, so clients cannot supply an arbitrary upstream URL.
type SnapshotURLBuilder func(context.Context, *node.Node, MediaIdentity) (string, error)

// SnapshotProxyOption configures bounded proxy behavior.
type SnapshotProxyOption func(*JPEGProxy)

type JPEGProxy struct {
	builder  SnapshotURLBuilder
	client   *http.Client
	timeout  time.Duration
	maxBytes int64
	marker   func(map[string]any)
}

func NewJPEGProxy(builder SnapshotURLBuilder, options ...SnapshotProxyOption) *JPEGProxy {
	proxy := &JPEGProxy{
		builder:  builder,
		client:   &http.Client{},
		timeout:  DefaultSnapshotTimeout,
		maxBytes: DefaultSnapshotMaxBytes,
	}
	for _, option := range options {
		if option != nil {
			option(proxy)
		}
	}
	if proxy.timeout <= 0 {
		proxy.timeout = DefaultSnapshotTimeout
	}
	if proxy.maxBytes <= 0 {
		proxy.maxBytes = DefaultSnapshotMaxBytes
	}
	if proxy.maxBytes > MaxSnapshotBytes {
		proxy.maxBytes = MaxSnapshotBytes
	}
	return proxy
}

func WithSnapshotTimeout(timeout time.Duration) SnapshotProxyOption {
	return func(proxy *JPEGProxy) {
		if timeout > 0 {
			if timeout > MaxSnapshotTimeout {
				timeout = MaxSnapshotTimeout
			}
			proxy.timeout = timeout
		}
	}
}

func WithSnapshotMaxBytes(maxBytes int64) SnapshotProxyOption {
	return func(proxy *JPEGProxy) {
		switch {
		case maxBytes <= 0:
			proxy.maxBytes = DefaultSnapshotMaxBytes
		case maxBytes > MaxSnapshotBytes:
			proxy.maxBytes = MaxSnapshotBytes
		default:
			proxy.maxBytes = maxBytes
		}
	}
}

func WithSnapshotHTTPClient(client *http.Client) SnapshotProxyOption {
	return func(proxy *JPEGProxy) {
		if client != nil {
			proxy.client = client
		}
	}
}

func WithSnapshotMarker(marker func(map[string]any)) SnapshotProxyOption {
	return func(proxy *JPEGProxy) { proxy.marker = marker }
}

// SnapshotResult keeps the binary payload separate from the safe audit
// metadata. Body is never included by SafeAuditMetadata or JSON marshaling.
type SnapshotResult struct {
	Body        []byte        `json:"-"`
	ContentType string        `json:"contentType"`
	NodeUUID    string        `json:"nodeUuid"`
	Media       MediaIdentity `json:"media"`
}

func (result SnapshotResult) SafeAuditMetadata() map[string]any {
	return map[string]any{
		"nodeUuid": result.NodeUUID,
		"schema":   result.Media.Schema,
		"vhost":    result.Media.Vhost,
		"app":      result.Media.App,
		"stream":   result.Media.Stream,
		"result":   "jpeg",
		"bytes":    len(result.Body),
	}
}

// MarkSensitiveOperation supplies T14 with an audit-safe value before bytes
// are written. The callback receives no URL, token, or image data.
func (result SnapshotResult) MarkSensitiveOperation(mark func(map[string]any)) {
	if mark != nil {
		mark(result.SafeAuditMetadata())
	}
}

func (proxy *JPEGProxy) Fetch(ctx context.Context, mediaNode *node.Node, identity MediaIdentity) (SnapshotResult, error) {
	if proxy == nil || proxy.builder == nil || mediaNode == nil || identity.Validate() != nil {
		return SnapshotResult{}, ErrSnapshotUpstream
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rawURL, err := proxy.builder(ctx, mediaNode, identity)
	if err != nil || !validSnapshotURL(rawURL) {
		return SnapshotResult{}, ErrSnapshotUpstream
	}
	client := http.Client{}
	if proxy.client != nil {
		client = *proxy.client
	}
	client.Timeout = proxy.timeout
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	requestCtx, cancel := context.WithTimeout(ctx, proxy.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return SnapshotResult{}, ErrSnapshotUpstream
	}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
			return SnapshotResult{}, ErrSnapshotTimeout
		}
		return SnapshotResult{}, ErrSnapshotUpstream
	}
	defer response.Body.Close()
	contentTypes := response.Header.Values("Content-Type")
	if response.StatusCode != http.StatusOK || len(contentTypes) != 1 || strings.TrimSpace(contentTypes[0]) != "image/jpeg" {
		return SnapshotResult{}, ErrSnapshotInvalidResponse
	}
	maxBytes := proxy.maxBytes
	if maxBytes <= 0 || maxBytes > MaxSnapshotBytes {
		maxBytes = DefaultSnapshotMaxBytes
		if proxy.maxBytes > MaxSnapshotBytes {
			maxBytes = MaxSnapshotBytes
		}
	}
	if response.ContentLength > maxBytes {
		return SnapshotResult{}, ErrSnapshotTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
			return SnapshotResult{}, ErrSnapshotTimeout
		}
		return SnapshotResult{}, ErrSnapshotUpstream
	}
	if int64(len(body)) > maxBytes {
		return SnapshotResult{}, ErrSnapshotTooLarge
	}
	if len(body) < 3 || body[0] != 0xff || body[1] != 0xd8 || body[2] != 0xff {
		return SnapshotResult{}, ErrSnapshotInvalidResponse
	}
	return SnapshotResult{
		Body: body, ContentType: "image/jpeg", NodeUUID: mediaNode.MediaServerUUID, Media: identity,
	}, nil
}

// Proxy writes only a validated JPEG. The marker runs before WriteHeader and
// Write, allowing operation-log middleware to mark the response as sensitive.
func (proxy *JPEGProxy) Proxy(ctx context.Context, writer http.ResponseWriter, mediaNode *node.Node, identity MediaIdentity) error {
	if writer == nil {
		return ErrSnapshotUpstream
	}
	result, err := proxy.Fetch(ctx, mediaNode, identity)
	if err != nil {
		return err
	}
	if proxy.marker != nil {
		result.MarkSensitiveOperation(proxy.marker)
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "image/jpeg")
	writer.Header().Set("Content-Length", strconv.Itoa(len(result.Body)))
	writer.WriteHeader(http.StatusOK)
	if _, err := writer.Write(result.Body); err != nil {
		return ErrSnapshotUpstream
	}
	return nil
}

func validSnapshotURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Fragment != "" {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}
