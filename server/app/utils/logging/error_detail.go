package logging

import (
	"regexp"
	"strings"
)

var errorCredentialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^\s/@]+@`),
	regexp.MustCompile(`(?i)([^\s:]+:)[^\s@]+(@(?:tcp|unix)\()`),
	regexp.MustCompile(`(?i)(\b(?:authorization|proxy-authorization)\s*[:=]\s*)(?:Bearer|Basic)\s+[^\s,;]+`),
	regexp.MustCompile(`(?i)((?:[\w.-]*(?:password|passwd|token|secret|api[_-]?key|encryption[_-]?key|master[_-]?key)|pwd)\b["']?\s*[:=]\s*)(?:"[^"\n]*"|'[^'\n]*'|[^\s,;&]+)`),
}

// Preserve diagnostic text; redact credentials before applying the event size bound.
func redactErrorDetail(s string) string {
	for i, pattern := range errorCredentialPatterns {
		replacement := "${1}[REDACTED]"
		if i == 0 {
			replacement += "@"
		}
		if i == 1 {
			replacement += "${2}"
		}
		s = pattern.ReplaceAllString(s, replacement)
	}
	s, cut := clipJSON(strings.ToValidUTF8(s, "�"), 8192)
	if cut {
		s += " [truncated]"
	}
	return s
}

func errorDetail(err error) (detail string) {
	if err == nil {
		return ""
	}
	defer func() {
		if recover() != nil {
			detail = "[error formatting failed]"
		}
	}()
	return redactErrorDetail(err.Error())
}
