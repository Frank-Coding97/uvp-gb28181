// Package deviceaccess resolves the shared QR and device-upload ingress.
package deviceaccess

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Header is overwritten by the bundled Nginx and development proxy.
const Header = "X-UVP-Device-Base-URL"

type contextKey struct{}

func Validate(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("无法确定设备接入地址，请通过设备可访问的局域网 IP 或域名打开平台")
	}
	host := strings.ToLower(u.Hostname())
	ip := net.ParseIP(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.ContainsAny(host, "% ,\t\r\n") || host == "" || (ip != nil && (ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast())) {
		return "", fmt.Errorf("设备无法访问接入地址 %q，请使用平台的局域网 IP 或域名打开平台", u.Host)
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("设备接入端口无效")
		}
	}
	return u.Scheme + "://" + u.Host, nil
}

// Resolve accepts ingress metadata only from local or explicitly trusted peers.
// Standard forwarded headers alone never override the device ingress.
func Resolve(r *http.Request, trustedProxies []string) (string, error) {
	if trustedPeer(r.RemoteAddr, trustedProxies) {
		if raw := r.Header.Get(Header); raw != "" {
			return Validate(raw)
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return Validate(scheme + "://" + r.Host)
}
func trustedPeer(remote string, proxies []string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, raw := range proxies {
		raw = strings.TrimSpace(raw)
		if peer := net.ParseIP(raw); peer != nil && peer.Equal(ip) {
			return true
		}
		if _, network, err := net.ParseCIDR(raw); err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}
func WithBaseURL(ctx context.Context, base string) context.Context {
	return context.WithValue(ctx, contextKey{}, base)
}
func FromContext(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("设备接入请求上下文不可用")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	base, _ := ctx.Value(contextKey{}).(string)
	return Validate(base)
}

// ForHost supports SIP-triggered captures, which have no HTTP request. The
// launcher supplies the actual Nginx HTTP port; the existing SIP address is reused.
func ForHost(host, port string) (string, error) {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("设备 HTTP 入口端口尚未由启动器提供")
	}
	return Validate("http://" + net.JoinHostPort(host, port))
}
