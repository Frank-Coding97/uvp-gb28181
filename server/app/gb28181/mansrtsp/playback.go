package mansrtsp

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const ContentType = "Application/MANSRTSP"

var (
	ErrInvalidArgument = errors.New("mansrtsp: invalid argument")
	ErrProtocol        = errors.New("mansrtsp: protocol error")
)

type ArgumentError struct {
	Field string
	Err   error
}

func (e *ArgumentError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s %s: %v", ErrInvalidArgument, e.Field, e.Err)
}

func (e *ArgumentError) Unwrap() error { return ErrInvalidArgument }

type ProtocolError struct {
	Line   string
	Reason string
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return ""
	}
	if e.Line == "" {
		return fmt.Sprintf("%s: %s", ErrProtocol, e.Reason)
	}
	return fmt.Sprintf("%s at %q: %s", ErrProtocol, e.Line, e.Reason)
}

func (e *ProtocolError) Unwrap() error { return ErrProtocol }

type ResultStatus string

const (
	ResultAccepted ResultStatus = "accepted"
	ResultRejected ResultStatus = "rejected"
)

type Result struct {
	Status     ResultStatus
	StatusCode int
	Reason     string
	CSeq       uint32
}

func BuildPlay(cseq uint32) ([]byte, error) {
	return buildCommand("PLAY", cseq)
}

func BuildResume(cseq uint32) ([]byte, error) {
	return buildCommand("PLAY", cseq, "Range: npt=now-")
}

func BuildPause(cseq uint32) ([]byte, error) {
	return buildCommand("PAUSE", cseq, "PauseTime: now")
}

func BuildSeek(cseq uint32, position, segmentDuration time.Duration) ([]byte, error) {
	if segmentDuration <= 0 || position < 0 || position >= segmentDuration {
		return nil, invalidArgument("position", "must be inside [0, segment duration)")
	}
	rangeStart := strconv.FormatFloat(position.Seconds(), 'f', -1, 64)
	return buildCommand("PLAY", cseq, "Range: npt="+rangeStart+"-")
}

func BuildScale(cseq uint32, scale float64) ([]byte, error) {
	wireScale, ok := playbackScale(scale)
	if !ok {
		return nil, invalidArgument("scale", "must be one of 0.25, 0.5, 1, 2, or 4")
	}
	return buildCommand("PLAY", cseq, "Scale: "+wireScale)
}

func BuildTeardown(cseq uint32) ([]byte, error) {
	return buildCommand("TEARDOWN", cseq)
}

func buildCommand(method string, cseq uint32, headers ...string) ([]byte, error) {
	if cseq == 0 {
		return nil, invalidArgument("cseq", "must be positive")
	}
	var body strings.Builder
	body.WriteString(method)
	body.WriteString(" MANSRTSP/1.0\r\nCSeq: ")
	body.WriteString(strconv.FormatUint(uint64(cseq), 10))
	body.WriteString("\r\n")
	for _, header := range headers {
		body.WriteString(header)
		body.WriteString("\r\n")
	}
	body.WriteString("\r\n")
	return []byte(body.String()), nil
}

func playbackScale(scale float64) (string, bool) {
	switch scale {
	case 0.25:
		return "0.25", true
	case 0.5:
		return "0.5", true
	case 1:
		return "1.0", true
	case 2:
		return "2.0", true
	case 4:
		return "4.0", true
	default:
		return "", false
	}
}

// ParseResponse parses the MANSRTSP response carried by a SIP INFO response.
// A syntactically valid non-2xx response is a rejected Result, not a parse error.
func ParseResponse(body []byte) (Result, error) {
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return Result{}, protocolError("", "missing status line")
	}

	statusLine := strings.TrimSpace(lines[0])
	statusParts := strings.Fields(statusLine)
	if len(statusParts) < 2 || statusParts[0] != "MANSRTSP/1.0" {
		return Result{}, protocolError(statusLine, "invalid status line")
	}
	statusCode, err := strconv.Atoi(statusParts[1])
	if err != nil || len(statusParts[1]) != 3 || statusCode < 100 || statusCode > 599 {
		return Result{}, protocolError(statusLine, "invalid status code")
	}

	var cseq uint64
	foundCSeq := false
	for _, rawLine := range lines[1:] {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return Result{}, protocolError(line, "malformed header")
		}
		if !strings.EqualFold(strings.TrimSpace(name), "CSeq") {
			continue
		}
		if foundCSeq {
			return Result{}, protocolError(line, "duplicate CSeq")
		}
		cseq, err = strconv.ParseUint(strings.TrimSpace(value), 10, 32)
		if err != nil || cseq == 0 {
			return Result{}, protocolError(line, "invalid CSeq")
		}
		foundCSeq = true
	}
	if !foundCSeq {
		return Result{}, protocolError("", "missing CSeq")
	}

	status := ResultRejected
	if statusCode >= 200 && statusCode < 300 {
		status = ResultAccepted
	}
	return Result{
		Status:     status,
		StatusCode: statusCode,
		Reason:     strings.Join(statusParts[2:], " "),
		CSeq:       uint32(cseq),
	}, nil
}

func invalidArgument(field, reason string) error {
	return &ArgumentError{Field: field, Err: errors.New(reason)}
}

func protocolError(line, reason string) error {
	return &ProtocolError{Line: line, Reason: reason}
}
