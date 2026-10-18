package mansrtsp

import (
	"fmt"
	"strconv"
	"strings"
)

type Request struct {
	RangeValue      string
	Range           *PlaybackRange
	CSeq            uint32
	Action          string
	PositionSeconds float64
	Scale           float64
}

// ParseRequest decodes the basic GB28181 playback controls carried in SIP INFO.
// Unsupported range formats and speeds are rejected rather than silently changed.
func ParseRequest(body []byte) (Request, error) {
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	parts := strings.Fields(lines[0])
	if len(parts) != 2 || parts[1] != protocolVersion {
		return Request{}, protocolError(lines[0], "invalid request line")
	}
	headers := map[string]string{}
	for _, raw := range lines[1:] {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return Request{}, protocolError(line, "malformed header")
		}
		name = strings.ToLower(strings.TrimSpace(name))
		if _, duplicate := headers[name]; duplicate {
			return Request{}, protocolError(line, "duplicate header")
		}
		headers[name] = strings.TrimSpace(value)
	}
	cseq, err := strconv.ParseUint(headers["cseq"], 10, 32)
	if err != nil || cseq == 0 {
		return Request{}, protocolError("", "missing or invalid CSeq")
	}
	request := Request{CSeq: uint32(cseq)}
	switch parts[0] {
	case "PAUSE":
		if headers["pausetime"] != "now" || headers["range"] != "" || headers["scale"] != "" {
			return Request{}, protocolError("", "PAUSE requires PauseTime now")
		}
		request.Action = "pause"
	case "TEARDOWN":
		request.Action = "stop"
	case "PLAY":
		rangeValue, scaleValue := headers["range"], headers["scale"]
		if rangeValue != "" && scaleValue != "" {
			return Request{}, protocolError("", "Range and Scale cannot be combined")
		}
		switch {
		case scaleValue != "":
			scale, err := strconv.ParseFloat(scaleValue, 64)
			if _, supported := playbackScale(scale); err != nil || !supported {
				return Request{}, protocolError(scaleValue, "unsupported Scale")
			}
			request.Action, request.Scale = "scale", scale
		case rangeValue == "" || rangeValue == "npt=now-":
			request.Action = "resume"
		default:
			parsedRange, err := ParseRange(rangeValue)
			if err != nil {
				return Request{}, err
			}
			request.Action, request.PositionSeconds = "seek", parsedRange.StartSeconds
			request.RangeValue, request.Range = rangeValue, &parsedRange
		}
	default:
		return Request{}, protocolError(lines[0], "unsupported method")
	}
	return request, nil
}

func BuildResponse(cseq uint32, status int, reason string) []byte {
	return BuildResponseWithHeaders(cseq, status, reason)
}

// ResponseHeader is an optional MANSRTSP response header. Headers are kept
// explicit so callers cannot accidentally manufacture RTP metadata.
type ResponseHeader struct {
	Name  string
	Value string
}

// RTPInfo is the real first packet position after the current PLAY action.
// It is intentionally optional: callers must omit the header when the sender
// has not observed the packet rather than inventing seq/rtptime values.
type RTPInfo struct {
	URL     string
	Seq     uint16
	RTPTime uint32
}

func (info RTPInfo) Header() ResponseHeader {
	value := fmt.Sprintf("url=%s;seq=%d;rtptime=%d", info.URL, info.Seq, info.RTPTime)
	return ResponseHeader{Name: "RTP-Info", Value: value}
}

func BuildResponseWithHeaders(cseq uint32, status int, reason string, headers ...ResponseHeader) []byte {
	var builder strings.Builder
	fmt.Fprintf(&builder, "RTSP/1.0 %d %s\r\nCSeq: %d\r\n", status, reason, cseq)
	for _, header := range headers {
		if strings.TrimSpace(header.Name) == "" || strings.TrimSpace(header.Value) == "" {
			continue
		}
		fmt.Fprintf(&builder, "%s: %s\r\n", header.Name, header.Value)
	}
	builder.WriteString("\r\n")
	return []byte(builder.String())
}
