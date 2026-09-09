package sdp

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

// CascadeVideoOffer contains the media facts needed to start a PS-over-RTP
// sender towards an upstream GB28181 platform.
type CascadeVideoOffer struct {
	RemoteIP    string
	RemotePort  int
	SSRC        string
	PayloadType int
	Transport   zlm.GBSendRTPTransport
}

type cascadeVideoConnection struct {
	address string
	seen    bool
	valid   bool
}

type cascadeVideoMedia struct {
	kind       string
	port       int
	transport  string
	pt         []int
	invalid    bool
	rtpmap     map[int]string
	connection cascadeVideoConnection
	ssrc       string
	ssrcSeen   bool
	direction  string
	setup      string
	format     string
}

// ParseCascadeVideoOffer parses the SDP body of an upstream real-time Play
// offer. The returned transport is expressed in terms of the ZLM sender: a
// TCP offer's active/passive role is therefore reversed.
func ParseCascadeVideoOffer(body []byte) (CascadeVideoOffer, error) {
	var sessionConnection cascadeVideoConnection
	var sessionSSRC string
	var sessionSSRCSeen bool
	var sessionDirection string
	var sessionFormat string
	var sessionName string
	var current *cascadeVideoMedia
	var video *cascadeVideoMedia

	normalized := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(string(body))
	scanner := bufio.NewScanner(strings.NewReader(normalized))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "m="):
			media := parseCascadeVideoMedia(strings.TrimPrefix(line, "m="))
			current = media
			if media.kind == "video" && video == nil {
				video = media
			}
		case strings.HasPrefix(line, "s="):
			sessionName = strings.TrimSpace(strings.TrimPrefix(line, "s="))
		case strings.HasPrefix(line, "c="):
			connection := parseCascadeVideoConnection(strings.TrimPrefix(line, "c="))
			if current != nil {
				if current == video {
					current.connection = connection
				}
			} else {
				sessionConnection = connection
			}
		case strings.HasPrefix(line, "y="):
			ssrc := strings.TrimSpace(strings.TrimPrefix(line, "y="))
			if current != nil {
				if current == video {
					current.ssrc = ssrc
					current.ssrcSeen = true
				}
			} else {
				sessionSSRC = ssrc
				sessionSSRCSeen = true
			}
		case strings.HasPrefix(line, "f="):
			format := strings.TrimSpace(strings.TrimPrefix(line, "f="))
			if current == video {
				current.format = format
			} else if current == nil {
				sessionFormat = format
			}
		default:
			if value, ok := cascadeVideoAttributeValue(line, "a=rtpmap:"); ok && current == video {
				fields := strings.Fields(value)
				if len(fields) >= 2 {
					payloadType, err := strconv.Atoi(fields[0])
					if err == nil {
						current.rtpmap[payloadType] = strings.TrimSpace(fields[1])
					}
				}
				continue
			}
			if value, ok := cascadeVideoAttributeValue(line, "a=setup:"); ok && current == video {
				current.setup = strings.ToLower(value)
				continue
			}
			if direction, ok := cascadeVideoDirection(line); ok {
				if current == video {
					current.direction = direction
				} else if current == nil {
					sessionDirection = direction
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return CascadeVideoOffer{}, err
	}

	if !strings.EqualFold(strings.TrimSpace(sessionName), "Play") {
		return CascadeVideoOffer{}, fmt.Errorf("cascade video SDP session must be Play")
	}
	if video == nil {
		return CascadeVideoOffer{}, fmt.Errorf("cascade video SDP missing video media")
	}
	if video.invalid || video.port <= 0 || video.port > 65535 {
		return CascadeVideoOffer{}, fmt.Errorf("cascade video SDP video port is invalid")
	}
	direction := strings.ToLower(strings.TrimSpace(video.direction))
	if direction == "" {
		direction = strings.ToLower(strings.TrimSpace(sessionDirection))
	}
	if direction == "" {
		direction = "sendrecv"
	}
	if direction != "recvonly" && direction != "sendrecv" {
		return CascadeVideoOffer{}, fmt.Errorf("cascade video SDP direction is not supported")
	}

	connection := video.connection
	if !connection.seen {
		connection = sessionConnection
	}
	if !connection.seen || !connection.valid {
		return CascadeVideoOffer{}, fmt.Errorf("cascade video SDP media address is invalid")
	}
	parsedIP := net.ParseIP(connection.address)
	if parsedIP == nil || parsedIP.To4() == nil || parsedIP.IsUnspecified() {
		return CascadeVideoOffer{}, fmt.Errorf("cascade video SDP media address is invalid")
	}

	ssrc := video.ssrc
	if !video.ssrcSeen && sessionSSRCSeen {
		ssrc = sessionSSRC
	}
	if !validCascadeVideoSSRC(ssrc) {
		return CascadeVideoOffer{}, fmt.Errorf("cascade video SDP SSRC is missing or invalid")
	}

	format := video.format
	if format == "" {
		format = sessionFormat
	}
	payloadType := 0
	for _, candidate := range video.pt {
		if candidate < 96 || candidate > 127 {
			continue
		}
		codec, ok := video.rtpmap[candidate]
		if ok {
			if strings.EqualFold(strings.TrimSpace(codec), "PS/90000") {
				payloadType = candidate
				break
			}
			continue
		}
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(format)), "V/PS/") {
			payloadType = candidate
			break
		}
	}
	if payloadType == 0 {
		return CascadeVideoOffer{}, fmt.Errorf("cascade video SDP missing dynamic PS/90000 payload")
	}

	var transport zlm.GBSendRTPTransport
	switch strings.ToUpper(strings.TrimSpace(video.transport)) {
	case "RTP/AVP":
		transport = zlm.GBSendRTPUDP
	case "TCP/RTP/AVP":
		switch video.setup {
		case "active":
			transport = zlm.GBSendRTPTCPPassive
		case "passive":
			transport = zlm.GBSendRTPTCPActive
		default:
			return CascadeVideoOffer{}, fmt.Errorf("cascade video TCP SDP setup is not supported")
		}
	default:
		return CascadeVideoOffer{}, fmt.Errorf("cascade video SDP transport is not supported: %s", video.transport)
	}

	return CascadeVideoOffer{
		RemoteIP: parsedIP.To4().String(), RemotePort: video.port, SSRC: ssrc,
		PayloadType: payloadType, Transport: transport,
	}, nil
}

// BuildCascadeVideoAnswer builds the local answer for a parsed upstream Play
// offer. The local endpoint sends the negotiated PS stream to the upstream.
func BuildCascadeVideoAnswer(offer CascadeVideoOffer, localID, localIP string, localPort int) ([]byte, error) {
	localID = strings.TrimSpace(localID)
	if localID == "" {
		return nil, fmt.Errorf("cascade video answer local ID is required")
	}
	parsedIP := net.ParseIP(strings.TrimSpace(localIP))
	if parsedIP == nil || parsedIP.To4() == nil || parsedIP.IsUnspecified() {
		return nil, fmt.Errorf("cascade video answer local address is invalid")
	}
	if localPort <= 0 || localPort > 65535 {
		return nil, fmt.Errorf("cascade video answer local port is invalid")
	}
	ssrc := strings.TrimSpace(offer.SSRC)
	if !validCascadeVideoSSRC(ssrc) {
		return nil, fmt.Errorf("cascade video answer SSRC is missing or invalid")
	}
	if offer.PayloadType < 96 || offer.PayloadType > 127 {
		return nil, fmt.Errorf("cascade video answer payload type is invalid")
	}

	transport := ""
	setup := ""
	switch offer.Transport {
	case zlm.GBSendRTPUDP:
		transport = "RTP/AVP"
	case zlm.GBSendRTPTCPActive:
		transport = "TCP/RTP/AVP"
		setup = "a=setup:active\r\na=connection:new\r\n"
	case zlm.GBSendRTPTCPPassive:
		transport = "TCP/RTP/AVP"
		setup = "a=setup:passive\r\na=connection:new\r\n"
	default:
		return nil, fmt.Errorf("cascade video answer transport is not supported: %s", offer.Transport)
	}

	var answer strings.Builder
	answer.WriteString("v=0\r\n")
	answer.WriteString(fmt.Sprintf("o=%s 0 0 IN IP4 %s\r\n", localID, parsedIP.To4().String()))
	answer.WriteString("s=Play\r\n")
	answer.WriteString(fmt.Sprintf("c=IN IP4 %s\r\n", parsedIP.To4().String()))
	answer.WriteString("t=0 0\r\n")
	answer.WriteString(fmt.Sprintf("m=video %d %s %d\r\n", localPort, transport, offer.PayloadType))
	answer.WriteString(setup)
	answer.WriteString("a=sendonly\r\n")
	answer.WriteString(fmt.Sprintf("a=rtpmap:%d PS/90000\r\n", offer.PayloadType))
	answer.WriteString(fmt.Sprintf("y=%s\r\n", ssrc))
	return []byte(answer.String()), nil
}

func parseCascadeVideoMedia(raw string) *cascadeVideoMedia {
	fields := strings.Fields(raw)
	media := &cascadeVideoMedia{rtpmap: make(map[int]string)}
	if len(fields) < 4 {
		media.invalid = true
		return media
	}
	media.kind = strings.ToLower(fields[0])
	var err error
	media.port, err = strconv.Atoi(fields[1])
	if err != nil {
		media.invalid = true
	}
	media.transport = fields[2]
	for _, value := range fields[3:] {
		payloadType, parseErr := strconv.Atoi(value)
		if parseErr != nil || payloadType < 0 || payloadType > 127 {
			media.invalid = true
			continue
		}
		media.pt = append(media.pt, payloadType)
	}
	return media
}

func parseCascadeVideoConnection(raw string) cascadeVideoConnection {
	fields := strings.Fields(raw)
	if len(fields) != 3 {
		return cascadeVideoConnection{seen: true}
	}
	return cascadeVideoConnection{
		address: fields[2], seen: true,
		valid: strings.EqualFold(fields[0], "IN") && strings.EqualFold(fields[1], "IP4"),
	}
}

func cascadeVideoAttributeValue(line, prefix string) (string, bool) {
	if len(line) < len(prefix) || !strings.EqualFold(line[:len(prefix)], prefix) {
		return "", false
	}
	return strings.TrimSpace(line[len(prefix):]), true
}

func cascadeVideoDirection(line string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(line))
	switch value {
	case "a=recvonly", "a=sendrecv", "a=sendonly", "a=inactive":
		return strings.TrimPrefix(value, "a="), true
	default:
		return "", false
	}
}

func validCascadeVideoSSRC(value string) bool {
	if len(value) != 10 || value[0] != '0' {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
