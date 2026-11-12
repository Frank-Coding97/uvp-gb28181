package sdp

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// HistoricalURI is the recording selector in GB/T 28181-2022 Annex G.
// FilePath is an escaped URI path; an empty path denotes the short form.
type HistoricalURI struct {
	DeviceID string
	Type     uint8
	FilePath string
}

func ParseHistoricalURI(value string) (HistoricalURI, error) {
	invalid := func() (HistoricalURI, error) {
		return HistoricalURI{}, fmt.Errorf("invalid historical recording URI")
	}
	if strings.ContainsFunc(value, unicode.IsSpace) || strings.ContainsFunc(value, unicode.IsControl) {
		return invalid()
	}
	var result HistoricalURI
	if strings.HasPrefix(value, "http://") {
		uri, err := url.Parse(value)
		if err != nil || uri.Scheme != "http" || uri.User != nil || uri.Port() != "" || uri.RawQuery != "" || uri.ForceQuery || uri.Fragment != "" ||
			!strings.HasPrefix(uri.Path, "/") || strings.HasSuffix(uri.Path, "/") || strings.ContainsFunc(uri.Path, unicode.IsControl) {
			return invalid()
		}
		for _, part := range strings.Split(uri.Path, "/") {
			if part == "." || part == ".." {
				return invalid()
			}
		}
		result.DeviceID, result.FilePath = uri.Host, uri.EscapedPath()
	} else {
		parts := strings.Split(value, ":")
		if len(parts) != 2 || len(parts[1]) != 1 || parts[1][0] < '0' || parts[1][0] > '3' {
			return invalid()
		}
		result.DeviceID, result.Type = parts[0], parts[1][0]-'0'
	}
	if len(result.DeviceID) != 20 {
		return invalid()
	}
	for _, c := range result.DeviceID {
		if c < '0' || c > '9' {
			return invalid()
		}
	}
	return result, nil
}

func (u HistoricalURI) String() string {
	if u.FilePath != "" {
		return "http://" + u.DeviceID + u.FilePath
	}
	return u.DeviceID + ":" + strconv.Itoa(int(u.Type))
}
