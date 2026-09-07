package main

import (
	"net"
	"net/url"
)

func validManagementURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "http" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && net.ParseIP(parsed.Hostname()).IsLoopback()
}
