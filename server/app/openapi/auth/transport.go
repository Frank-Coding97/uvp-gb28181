package auth

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
)

// TLSBoundary is separate from forwarded-client-IP trust. Only a directly
// connected, explicitly authorized TLS terminator can assert outer HTTPS.
type TLSBoundary struct{ proxies []netip.Prefix }

func NewTLSBoundary(proxies []string) (*TLSBoundary, error) {
	b := &TLSBoundary{}
	for _, value := range proxies {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			address, parseErr := netip.ParseAddr(value)
			if parseErr != nil {
				return nil, errors.New("invalid OpenAPI TLS proxy")
			}
			address = address.Unmap()
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		if prefix.Bits() == 0 {
			return nil, errors.New("OpenAPI TLS proxy cannot trust every peer")
		}
		b.proxies = append(b.proxies, prefix.Masked())
	}
	return b, nil
}

func (b *TLSBoundary) IsHTTPS(r *http.Request) bool {
	if b == nil || r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	values := r.Header.Values("X-Forwarded-Proto")
	if len(values) != 1 || values[0] != "https" {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	address = address.Unmap()
	for _, prefix := range b.proxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
