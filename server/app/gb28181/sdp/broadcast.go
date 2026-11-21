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
	// Codecs 是设备 offer 里按 m 行顺序声明的、平台支持的 G.711 编码。
	// Codec 是其中第一个（设备首选），仅用于展示/兜底；真正发什么由会话
	// 配置决定，调用方必须先确认 Supports(session.AudioCodec)。
	Codecs []AudioCodec
	Codec  AudioCodec
}

// Supports 回答「设备这路 offer 是否接受该编码」。
func (m BroadcastMedia) Supports(codec AudioCodec) bool {
	for _, candidate := range m.Codecs {
		if candidate == codec {
			return true
		}
	}
	return false
}

type BroadcastAnswerParams struct {
	ServerID   string
	LocalIP    string
	LocalPort  int
	SSRC       string
	SenderMode BroadcastSenderMode
	// Codec 零值按 G.711A 处理。
	Codec AudioCodec
}

func ParseBroadcastOffer(raw string) (BroadcastMedia, error) {
	var sessionIP string
	var mediaIP string
	var port int
	var transport string
	var setup string
	var ssrc string
	var audioPayloads []string
	var codecs []AudioCodec
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
					return BroadcastMedia{}, fmt.Errorf("broadcast SDP audio 端口不合法")
				}
				audioPayloads = fields[3:]
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
		case inAudio && strings.HasPrefix(strings.ToLower(line), "a=rtpmap:"):
			fields := strings.Fields(line)
			if len(fields) != 2 {
				continue
			}
			codec, ok := ParseG711RTPMap(fields[0], fields[1])
			// 载荷类型必须真的出现在本段 m 行的列表里：设备写了一条
			// rtpmap 却在 m 行没列该 PT 的情况并不少见，按「声明了就算支持」
			// 会让我们发出设备根本没准备接收的包。
			if !ok || !containsField(audioPayloads, strconv.Itoa(codec.PayloadType())) || containsCodec(codecs, codec) {
				continue
			}
			codecs = append(codecs, codec)
		case inAudio && strings.HasPrefix(strings.ToLower(line), "a=setup:"):
			setup = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "a=setup:")))
		case strings.HasPrefix(line, "y="):
			ssrc = strings.TrimSpace(strings.TrimPrefix(line, "y="))
		}
	}
	if err := scanner.Err(); err != nil {
		return BroadcastMedia{}, err
	}
	if port == 0 || len(codecs) == 0 {
		return BroadcastMedia{}, fmt.Errorf("broadcast SDP 缺少 G.711 音频（PCMA/8000 或 PCMU/8000）")
	}
	if mediaIP == "" {
		mediaIP = sessionIP
	}
	parsedIP := net.ParseIP(mediaIP)
	if parsedIP == nil || parsedIP.To4() == nil || parsedIP.IsUnspecified() {
		return BroadcastMedia{}, fmt.Errorf("broadcast SDP 媒体地址不合法")
	}
	var mode BroadcastSenderMode
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
			return BroadcastMedia{}, fmt.Errorf("broadcast TCP SDP 缺少受支持的 setup")
		}
	default:
		return BroadcastMedia{}, fmt.Errorf("broadcast SDP 传输协议不支持: %s", transport)
	}
	return BroadcastMedia{RemoteIP: parsedIP.String(), RemotePort: port, Transport: transport, SenderMode: mode, SSRC: ssrc, Codecs: codecs, Codec: codecs[0]}, nil
}

func BuildBroadcastAnswer(in BroadcastAnswerParams) (string, error) {
	if strings.TrimSpace(in.ServerID) == "" || net.ParseIP(in.LocalIP) == nil || in.LocalPort <= 0 || in.LocalPort > 65535 {
		return "", fmt.Errorf("broadcast answer 缺少平台编码、地址或端口")
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
	codec := in.Codec.normalize()
	var b strings.Builder
	b.WriteString("v=0\r\n")
	fmt.Fprintf(&b, "o=%s 0 0 IN IP4 %s\r\n", in.ServerID, in.LocalIP)
	b.WriteString("s=Talk\r\n")
	fmt.Fprintf(&b, "c=IN IP4 %s\r\n", in.LocalIP)
	b.WriteString("t=0 0\r\n")
	fmt.Fprintf(&b, "m=audio %d %s %d\r\n", in.LocalPort, transport, codec.PayloadType())
	b.WriteString(setup)
	b.WriteString("a=sendonly\r\n")
	fmt.Fprintf(&b, "a=rtpmap:%d %s\r\n", codec.PayloadType(), codec.RTPMap())
	if strings.TrimSpace(in.SSRC) != "" {
		fmt.Fprintf(&b, "y=%s\r\n", strings.TrimSpace(in.SSRC))
	}
	fmt.Fprintf(&b, "f=%s\r\n", codec.FLine())
	return b.String(), nil
}

func containsCodec(values []AudioCodec, want AudioCodec) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsField(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
