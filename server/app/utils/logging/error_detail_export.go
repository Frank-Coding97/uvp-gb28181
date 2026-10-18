package logging

import (
	"net/url"
	"strings"
)

// SanitizeErrorDetail exposes the same bounded credential redaction used by
// structured error logs to small persistence adapters. It keeps the useful
// diagnostic context while removing credentials from URL userinfo and common
// URL query secrets before the value is shown in an operator UI.
func SanitizeErrorDetail(value string) string {
	value = redactErrorDetail(value)
	return realtimeURLPattern.ReplaceAllStringFunc(value, func(raw string) string {
		trailing := ""
		for len(raw) > 0 && strings.ContainsRune(".,);]}", rune(raw[len(raw)-1])) {
			trailing = string(raw[len(raw)-1]) + trailing
			raw = raw[:len(raw)-1]
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return raw + trailing
		}
		if parsed.User != nil {
			parsed.User = url.User("[REDACTED]")
		}
		return redactRealtimeURL(parsed.String()) + trailing
	})
}
