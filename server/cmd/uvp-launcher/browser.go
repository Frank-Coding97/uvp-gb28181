package main

import (
	"encoding/base64"
	"net"
	"net/url"
	"strings"
)

func validManagementURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "http" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && net.ParseIP(parsed.Hostname()).IsLoopback()
}

func validBootstrapURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Path != "/" || u.RawPath != "" || !net.ParseIP(u.Hostname()).IsLoopback() {
		return false
	}
	const prefix = "/standalone-setup?bootstrap_token="
	if !strings.HasPrefix(u.Fragment, prefix) {
		return false
	}
	token := strings.TrimPrefix(u.Fragment, prefix)
	raw, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(raw) == 32 && base64.RawURLEncoding.EncodeToString(raw) == token
}
