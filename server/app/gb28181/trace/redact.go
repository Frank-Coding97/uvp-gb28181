package trace

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
)

var (
	ErrSensitiveAccessDenied = errors.New("sensitive SIP trace access denied")
	ErrAuditContextRequired  = errors.New("sensitive SIP trace audit context required")
	passwordElementPattern   = regexp.MustCompile(`(?is)(<(?:Password|DevicePassword)(?:\s[^>]*)?>)(.*?)(</(?:Password|DevicePassword)\s*>)`)
)

type DisclosureContext struct {
	IsAdmin bool
	UserID  string
	Purpose string
}

func RedactSIP(raw []byte) []byte {
	headerEnd, separatorLength := sipHeaderEnd(raw)
	if headerEnd < 0 {
		return redactPasswordElements(raw)
	}
	var output bytes.Buffer
	redactHeaderLines(&output, raw[:headerEnd])
	output.Write(raw[headerEnd : headerEnd+separatorLength])
	output.Write(redactPasswordElements(raw[headerEnd+separatorLength:]))
	return output.Bytes()
}

func RenderForDisplay(raw []byte, includeSensitive bool, audit DisclosureContext) ([]byte, error) {
	if !includeSensitive {
		return RedactSIP(raw), nil
	}
	if !audit.IsAdmin {
		return nil, ErrSensitiveAccessDenied
	}
	if strings.TrimSpace(audit.UserID) == "" || strings.TrimSpace(audit.Purpose) == "" {
		return nil, ErrAuditContextRequired
	}
	return append([]byte(nil), raw...), nil
}

func redactHeaderLines(output *bytes.Buffer, header []byte) {
	redactingContinuation := false
	for len(header) > 0 {
		lineEnd := bytes.IndexByte(header, '\n')
		line := header
		if lineEnd >= 0 {
			line = header[:lineEnd+1]
			header = header[lineEnd+1:]
		} else {
			header = nil
		}
		content := bytes.TrimRight(line, "\r\n")
		ending := line[len(content):]
		if len(content) > 0 && (content[0] == ' ' || content[0] == '\t') {
			if redactingContinuation {
				output.WriteString(" [REDACTED]")
				output.Write(ending)
				continue
			}
			output.Write(line)
			continue
		}
		name, _, ok := bytes.Cut(content, []byte(":"))
		redactingContinuation = ok && sensitiveHeader(string(bytes.TrimSpace(name)))
		if redactingContinuation {
			output.Write(name)
			output.WriteString(": [REDACTED]")
			output.Write(ending)
			continue
		}
		output.Write(line)
	}
}

func sensitiveHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization", "authentication-info", "proxy-authentication-info", "www-authenticate", "proxy-authenticate", "x-password":
		return true
	default:
		return false
	}
}

func redactPasswordElements(body []byte) []byte {
	return passwordElementPattern.ReplaceAll(body, []byte("$1[REDACTED]$3"))
}

func sipHeaderEnd(raw []byte) (int, int) {
	if index := bytes.Index(raw, []byte("\r\n\r\n")); index >= 0 {
		return index, 4
	}
	if index := bytes.Index(raw, []byte("\n\n")); index >= 0 {
		return index, 2
	}
	return -1, 0
}
