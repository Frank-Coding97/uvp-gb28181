package trace

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/emiago/sipgo/sip"
)

const DefaultMaxFrameBytes = 1024 * 1024

type Frame struct {
	Props     sip.TransportReadProps
	Data      []byte
	Malformed bool
	Error     string
}

type frameStream struct {
	buffer []byte
	resync bool
}

type FrameAssembler struct {
	mu      sync.Mutex
	maxSize int
	streams map[string]*frameStream
}

func NewFrameAssembler(maxSize int) *FrameAssembler {
	if maxSize <= 0 {
		maxSize = DefaultMaxFrameBytes
	}
	return &FrameAssembler{maxSize: maxSize, streams: make(map[string]*frameStream)}
}

func (a *FrameAssembler) Push(props sip.TransportReadProps, data []byte) []Frame {
	if !strings.EqualFold(props.Transport, "TCP") && !strings.EqualFold(props.Transport, "TLS") {
		return []Frame{{Props: props, Data: append([]byte(nil), data...)}}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	key := connectionKey(props)
	stream := a.streams[key]
	if stream == nil {
		stream = &frameStream{}
		a.streams[key] = stream
	}
	stream.buffer = append(stream.buffer, data...)
	frames := make([]Frame, 0, 2)

	for len(stream.buffer) > 0 {
		if stream.resync {
			next := findSIPStart(stream.buffer, 0)
			if next < 0 {
				if len(stream.buffer) > a.maxSize {
					stream.buffer = nil
				}
				break
			}
			stream.buffer = stream.buffer[next:]
			stream.resync = false
		}
		if start := findSIPStart(stream.buffer, 0); start != 0 {
			if start > 0 {
				frames = append(frames, malformedFrame(props, stream.buffer[:start], "invalid SIP start line"))
				stream.buffer = stream.buffer[start:]
				continue
			}
			if bytes.Contains(stream.buffer, []byte("\r\n\r\n")) || len(stream.buffer) > a.maxSize {
				frames = append(frames, malformedFrame(props, stream.buffer, "invalid SIP start line"))
				stream.buffer = nil
				stream.resync = true
			}
			break
		}

		headerEnd := bytes.Index(stream.buffer, []byte("\r\n\r\n"))
		if headerEnd < 0 {
			if len(stream.buffer) > a.maxSize {
				frames = append(frames, malformedFrame(props, stream.buffer, "SIP header exceeds frame limit"))
				stream.buffer = nil
				stream.resync = true
			}
			break
		}
		headerEnd += 4

		contentLength, err := parseContentLength(stream.buffer[:headerEnd])
		totalSize := headerEnd + contentLength
		if err != nil || contentLength > a.maxSize-headerEnd {
			next := findSIPStart(stream.buffer, headerEnd)
			malformedEnd := headerEnd
			if next >= 0 {
				malformedEnd = next
			}
			reason := "invalid Content-Length"
			if err == nil {
				reason = "SIP frame exceeds frame limit"
			}
			frames = append(frames, malformedFrame(props, stream.buffer[:malformedEnd], reason))
			stream.buffer = stream.buffer[malformedEnd:]
			stream.resync = next < 0
			continue
		}
		if len(stream.buffer) < totalSize {
			break
		}

		frames = append(frames, Frame{Props: props, Data: append([]byte(nil), stream.buffer[:totalSize]...)})
		stream.buffer = stream.buffer[totalSize:]
	}

	if len(stream.buffer) == 0 {
		delete(a.streams, key)
	}
	return frames
}

func (a *FrameAssembler) Forget(props sip.TransportReadProps) {
	a.mu.Lock()
	delete(a.streams, connectionKey(props))
	a.mu.Unlock()
}

func connectionKey(props sip.TransportReadProps) string {
	local := ""
	if props.LocalAddr != nil {
		local = props.LocalAddr.String()
	}
	remote := ""
	if props.RemoteAddr != nil {
		remote = props.RemoteAddr.String()
	}
	return strings.ToUpper(props.Transport) + "|" + local + "|" + remote
}

func parseContentLength(header []byte) (int, error) {
	lines := bytes.Split(header, []byte("\r\n"))
	value := -1
	for _, line := range lines[1:] {
		name, rawValue, ok := bytes.Cut(line, []byte(":"))
		if !ok {
			continue
		}
		if !bytes.EqualFold(bytes.TrimSpace(name), []byte("Content-Length")) &&
			!bytes.EqualFold(bytes.TrimSpace(name), []byte("l")) {
			continue
		}
		parsed, err := strconv.Atoi(string(bytes.TrimSpace(rawValue)))
		if err != nil || parsed < 0 {
			return 0, fmt.Errorf("invalid Content-Length")
		}
		if value >= 0 && value != parsed {
			return 0, fmt.Errorf("conflicting Content-Length")
		}
		value = parsed
	}
	if value < 0 {
		return 0, nil
	}
	return value, nil
}

func malformedFrame(props sip.TransportReadProps, data []byte, reason string) Frame {
	return Frame{Props: props, Data: append([]byte(nil), data...), Malformed: true, Error: reason}
}

func findSIPStart(data []byte, from int) int {
	starts := [][]byte{
		[]byte("REGISTER "), []byte("MESSAGE "), []byte("INVITE "), []byte("ACK "),
		[]byte("BYE "), []byte("SUBSCRIBE "), []byte("NOTIFY "), []byte("OPTIONS "),
		[]byte("CANCEL "), []byte("INFO "), []byte("PRACK "), []byte("UPDATE "),
		[]byte("REFER "), []byte("PUBLISH "), []byte("SIP/2.0 "),
	}
	for i := max(from, 0); i < len(data); i++ {
		for _, prefix := range starts {
			if bytes.HasPrefix(data[i:], prefix) {
				return i
			}
		}
	}
	return -1
}
