package sdp

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
)

type BroadcastSenderMode string

const (
	BroadcastSenderUDP        BroadcastSenderMode = "udp"
	BroadcastSenderTCPActive  BroadcastSenderMode = "tcp_active"
	BroadcastSenderTCPPassive BroadcastSenderMode = "tcp_passive"
)

type BroadcastMedia struct {
	RemoteIP   string
	RemotePort int
	Transport  string
	SenderMode BroadcastSenderMode
	SSRC       string
}

type BroadcastAnswerParams struct {
	ServerID   string
	LocalIP    string
	LocalPort  int
	SSRC       string
	SenderMode BroadcastSenderMode
}

func ParseBroadcastOffer(raw string) (BroadcastMedia, error) {
	var sessionIP string
	var mediaIP string
	var port int
	var transport string
	var setup string
	var ssrc string
	codecOK := false
	inAudio := false

	scanner := bufio.NewScanner(strings.NewReader(strings.ReplaceAll(raw, "\r\n", "\n")))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "m="):
			fields := strings.Fields(strings.TrimPrefix(line, "m="))
			inAudio = len(fields) >= 4 && strings.EqualFold(fields[0], "audio")
			if inAudio {
				parsedPort, err := strconv.Atoi(fields[1])
				if err != nil || parsedPort <= 0 || parsedPort > 65535 {
					return BroadcastMedia{}, fmt.Errorf("Broadcast SDP audio 端口不合法")
				}
				if !containsField(fields[3:], "8") {
					return BroadcastMedia{}, fmt.Errorf("Broadcast SDP 未提供 payload 8")
				}
				port = parsedPort
				transport = strings.ToUpper(fields[2])
			}
		case strings.HasPrefix(line, "c=IN IP4 "):
			ip := strings.TrimSpace(strings.TrimPrefix(line, "c=IN IP4 "))
			if inAudio {
				mediaIP = ip
			} else if sessionIP == "" {
				sessionIP = ip
			}
		case inAudio && strings.EqualFold(line, "a=rtpmap:8 PCMA/8000"):
			codecOK = true
		case inAudio && strings.HasPrefix(strings.ToLower(line), "a=setup:"):
			setup = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "a=setup:")))
		case strings.HasPrefix(line, "y="):
			ssrc = strings.TrimSpace(strings.TrimPrefix(line, "y="))
		}
	}
	if err := scanner.Err(); err != nil {
		return BroadcastMedia{}, err
	}
	if port == 0 || !codecOK {
		return BroadcastMedia{}, fmt.Errorf("Broadcast SDP 缺少 PCMA/8000 audio")
	}
	if mediaIP == "" {
		mediaIP = sessionIP
	}
	parsedIP := net.ParseIP(mediaIP)
	if parsedIP == nil || parsedIP.To4() == nil || parsedIP.IsUnspecified() {
		return BroadcastMedia{}, fmt.Errorf("Broadcast SDP 媒体地址不合法")
	}
	mode := BroadcastSenderUDP
	switch transport {
	case "RTP/AVP":
		mode = BroadcastSenderUDP
	case "TCP/RTP/AVP":
		switch setup {
		case "active":
			mode = BroadcastSenderTCPPassive
		case "passive":
			mode = BroadcastSenderTCPActive
		default:
			return BroadcastMedia{}, fmt.Errorf("Broadcast TCP SDP 缺少受支持的 setup")
		}
	default:
		return BroadcastMedia{}, fmt.Errorf("Broadcast SDP 传输协议不支持: %s", transport)
	}
	return BroadcastMedia{RemoteIP: parsedIP.String(), RemotePort: port, Transport: transport, SenderMode: mode, SSRC: ssrc}, nil
}

func BuildBroadcastAnswer(in BroadcastAnswerParams) (string, error) {
	if strings.TrimSpace(in.ServerID) == "" || net.ParseIP(in.LocalIP) == nil || in.LocalPort <= 0 || in.LocalPort > 65535 {
		return "", fmt.Errorf("Broadcast answer 缺少平台编码、地址或端口")
	}
	transport := "RTP/AVP"
	setup := ""
	if in.SenderMode == BroadcastSenderTCPActive || in.SenderMode == BroadcastSenderTCPPassive {
		transport = "TCP/RTP/AVP"
		if in.SenderMode == BroadcastSenderTCPPassive {
			setup = "a=setup:passive\r\na=connection:new\r\n"
		} else {
			setup = "a=setup:active\r\na=connection:new\r\n"
		}
	}
	var b strings.Builder
	b.WriteString("v=0\r\n")
	b.WriteString(fmt.Sprintf("o=%s 0 0 IN IP4 %s\r\n", in.ServerID, in.LocalIP))
	b.WriteString("s=Talk\r\n")
	b.WriteString(fmt.Sprintf("c=IN IP4 %s\r\n", in.LocalIP))
	b.WriteString("t=0 0\r\n")
	b.WriteString(fmt.Sprintf("m=audio %d %s 8\r\n", in.LocalPort, transport))
	b.WriteString(setup)
	b.WriteString("a=sendonly\r\n")
	b.WriteString("a=rtpmap:8 PCMA/8000\r\n")
	if strings.TrimSpace(in.SSRC) != "" {
		b.WriteString(fmt.Sprintf("y=%s\r\n", strings.TrimSpace(in.SSRC)))
	}
	b.WriteString("f=v/////a/1/8/1\r\n")
	return b.String(), nil
}

func containsField(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
