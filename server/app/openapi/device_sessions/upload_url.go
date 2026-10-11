package devicesessions

import (
	"context"
	"net"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

const deviceSnapshotUploadPath = "/api/gb28181/device-snapshots/uploads/"

// NewUploadURLBuilder creates device callback URLs from an administrator-set
// external base URL. Request Host and forwarded headers are never consulted.
func NewUploadURLBuilder(rawBaseURL string) (UploadURLBuilder, error) {
	base, err := url.Parse(strings.TrimSpace(rawBaseURL))
	if err != nil || !base.IsAbs() || base.Opaque != "" || base.Host == "" ||
		(!strings.EqualFold(base.Scheme, "http") && !strings.EqualFold(base.Scheme, "https")) ||
		base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" || base.RawFragment != "" ||
		unreachableUploadHost(base.Hostname()) {
		return nil, ErrInvalidUploadBaseURL
	}
	for _, segment := range strings.Split(base.Path, "/") {
		if segment == "." || segment == ".." {
			return nil, ErrInvalidUploadBaseURL
		}
	}

	return func(ctx context.Context, token string) (string, error) {
		if ctx == nil {
			return "", ErrInvalidRequest
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		parsed, err := uuid.Parse(token)
		if err != nil || parsed.String() != token {
			return "", ErrInvalidRequest
		}
		u := *base
		u.Path = strings.TrimRight(u.Path, "/") + deviceSnapshotUploadPath + token + "/"
		u.RawPath = ""
		return u.String(), nil
	}, nil
}

func unreachableUploadHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" || strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") || strings.Contains(host, "%") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}
