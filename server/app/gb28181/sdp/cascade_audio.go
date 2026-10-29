package sdp

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// CascadeAudioOffer describes a single PCMA media section. Transport roles
// are expressed locally, so an active peer requires a passive local socket.
type CascadeAudioOffer struct {
	SessionName string
	RemoteIP    string
	RemotePort  int
	RTCPIP      string
	RTCPPort    int
	Transport   string
	Setup       string
	Direction   string
	SSRC        string
}

func ParseCascadeAudioOffer(body []byte) (CascadeAudioOffer, error) {
	return parseCascadeAudioOffer(body, false)
}

// ParseCascadeBroadcastPullOffer accepts the receiver's initial Play request.
// 2022 9.12.1.2 adds y when the platform establishes the source media leg.
// Other audio offers and all answers continue to require a negotiated SSRC.
func ParseCascadeBroadcastPullOffer(body []byte) (CascadeAudioOffer, error) {
	offer, err := parseCascadeAudioOffer(body, true)
	if err == nil && (offer.SessionName != "Play" || offer.Direction != "recvonly") {
		err = fmt.Errorf("cascade broadcast pull requires Play/recvonly")
	}
	return offer, err
}

func validCascadeAudioSSRC(value string) bool {
	if !validCascadeVideoSSRC(value) {
		return false
	}
	n, err := strconv.ParseUint(value, 10, 32)
	return err == nil && n != 0
}

func parseCascadeAudioOffer(body []byte, allowMissingSSRC bool) (CascadeAudioOffer, error) {
	var offer CascadeAudioOffer
	media := false
	sessionIP, mediaIP, sessionDirection, mediaDirection := "", "", "", ""
	seen := map[string]bool{}
	claim := func(key string) error {
		if seen[key] {
			return fmt.Errorf("duplicate cascade audio SDP %s", key)
		}
		seen[key] = true
		return nil
	}
	scanner := bufio.NewScanner(strings.NewReader(strings.ReplaceAll(string(body), "\r\n", "\n")))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		key := ""
		switch {
		case strings.HasPrefix(line, "s="):
			key = "name"
			if line != "s=Talk" && line != "s=Play" {
				return offer, fmt.Errorf("unsupported cascade audio session")
			}
			offer.SessionName = line[2:]
		case strings.HasPrefix(line, "t="):
			key = "timing"
			if strings.Join(strings.Fields(line[2:]), " ") != "0 0" {
				return offer, fmt.Errorf("scheduled cascade audio is unsupported")
			}
		case strings.HasPrefix(line, "m="):
			key = "media"
			f := strings.Fields(line[2:])
			if len(f) < 4 || f[0] != "audio" || !containsField(f[3:], "8") {
				return offer, fmt.Errorf("cascade audio requires one PCMA audio section")
			}
			port, err := strconv.Atoi(f[1])
			if err != nil || port < 1 || port > 65535 {
				return offer, fmt.Errorf("invalid cascade audio port")
			}
			offer.RemotePort, offer.Transport, media = port, f[2], true
			if f[2] == "RTP/AVP" {
				offer.RTCPPort = port + 1
			}
		case strings.HasPrefix(line, "c="):
			c := parseCascadeVideoConnection(line[2:])
			ip := net.ParseIP(c.address)
			if !c.valid || ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.Equal(net.IPv4bcast) {
				return offer, fmt.Errorf("invalid cascade audio address")
			}
			if media {
				key, mediaIP = "media connection", ip.String()
			} else {
				key, sessionIP = "session connection", ip.String()
			}
		case strings.HasPrefix(line, "y="):
			key, offer.SSRC = "SSRC", strings.TrimSpace(line[2:])
			if !validCascadeAudioSSRC(offer.SSRC) {
				return offer, fmt.Errorf("invalid cascade audio SSRC")
			}
		default:
			if direction, ok := cascadeVideoDirection(line); ok {
				if direction == "inactive" {
					return offer, fmt.Errorf("inactive cascade audio")
				}
				if media {
					key, mediaDirection = "media direction", direction
				} else {
					key, sessionDirection = "session direction", direction
				}
			} else if mapping, ok := cascadeVideoAttributeValue(line, "a=rtpmap:"); ok && media {
				f := strings.Fields(mapping)
				if len(f) != 2 {
					return offer, fmt.Errorf("invalid cascade audio mapping")
				}
				if f[0] == "8" {
					key = "PCMA mapping"
					if !strings.EqualFold(f[1], "PCMA/8000") && !strings.EqualFold(f[1], "PCMA/8000/1") {
						return offer, fmt.Errorf("unsupported cascade audio codec")
					}
				}
			} else if setup, ok := cascadeVideoAttributeValue(line, "a=setup:"); ok {
				key = "setup"
				switch strings.ToLower(setup) {
				case "active":
					offer.Setup = "passive"
				case "passive":
					offer.Setup = "active"
				default:
					return offer, fmt.Errorf("unsupported cascade audio TCP role")
				}
			} else if rtcpValue, ok := cascadeVideoAttributeValue(line, "a=rtcp:"); ok && media {
				key = "RTCP endpoint"
				f := strings.Fields(rtcpValue)
				if len(f) != 1 && len(f) != 4 {
					return offer, fmt.Errorf("invalid cascade audio RTCP endpoint")
				}
				port, parseErr := strconv.Atoi(f[0])
				if parseErr != nil || port < 1 || port > 65535 {
					return offer, fmt.Errorf("invalid cascade audio RTCP port")
				}
				offer.RTCPPort = port
				if len(f) == 4 {
					ip := net.ParseIP(f[3])
					if f[1] != "IN" || f[2] != "IP4" || ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.Equal(net.IPv4bcast) {
						return offer, fmt.Errorf("invalid cascade audio RTCP address")
					}
					offer.RTCPIP = ip.String()
				}
			}
		}
		if key != "" {
			if err := claim(key); err != nil {
				return offer, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return offer, err
	}
	if !seen["media"] || !seen["name"] || !seen["timing"] || (!seen["SSRC"] && !allowMissingSSRC) || !seen["PCMA mapping"] {
		return offer, fmt.Errorf("incomplete cascade audio SDP")
	}
	offer.RemoteIP = mediaIP
	if offer.RemoteIP == "" {
		offer.RemoteIP = sessionIP
	}
	if offer.RemoteIP == "" {
		return offer, fmt.Errorf("missing cascade audio address")
	}
	if offer.Transport == "RTP/AVP" {
		if offer.RTCPPort < 1 || offer.RTCPPort > 65535 {
			return offer, fmt.Errorf("invalid cascade audio RTCP port")
		}
		if offer.RTCPIP == "" {
			offer.RTCPIP = offer.RemoteIP
		}
	}
	offer.Direction = mediaDirection
	if offer.Direction == "" {
		offer.Direction = sessionDirection
	}
	if offer.Direction == "" {
		offer.Direction = "sendrecv"
	}
	switch offer.Transport {
	case "RTP/AVP":
		if seen["setup"] {
			return offer, fmt.Errorf("TCP setup on UDP audio")
		}
	case "TCP/RTP/AVP":
		if offer.Setup == "" {
			return offer, fmt.Errorf("missing cascade audio TCP role")
		}
	default:
		return offer, fmt.Errorf("unsupported cascade audio transport")
	}
	return offer, nil
}

func BuildCascadeAudioAnswer(offer CascadeAudioOffer, localID, localIP string, localPort int, rtcpPorts ...int) ([]byte, error) {
	if len(localID) != 20 || strings.Trim(localID, "0123456789") != "" || !validCascadeAudioSSRC(offer.SSRC) {
		return nil, fmt.Errorf("invalid cascade audio answer identity")
	}
	ip := net.ParseIP(localIP)
	if ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.Equal(net.IPv4bcast) || localPort < 1 || localPort > 65535 {
		return nil, fmt.Errorf("invalid cascade audio answer endpoint")
	}
	direction := "sendrecv"
	switch offer.Direction {
	case "sendonly":
		direction = "recvonly"
	case "recvonly":
		direction = "sendonly"
	case "sendrecv":
	default:
		return nil, fmt.Errorf("invalid cascade audio direction")
	}
	setup := ""
	rtcpLine := ""
	switch offer.Transport {
	case "RTP/AVP":
		rtcpPort := localPort + 1
		if len(rtcpPorts) > 0 {
			rtcpPort = rtcpPorts[0]
		}
		if rtcpPort < 1 || rtcpPort > 65535 {
			return nil, fmt.Errorf("invalid cascade audio RTCP answer port")
		}
		rtcpLine = fmt.Sprintf("a=rtcp:%d IN IP4 %s\r\n", rtcpPort, ip.String())
	case "TCP/RTP/AVP":
		if offer.Setup != "active" && offer.Setup != "passive" {
			return nil, fmt.Errorf("invalid cascade audio TCP role")
		}
		setup = "a=setup:" + offer.Setup + "\r\na=connection:new\r\n"
	default:
		return nil, fmt.Errorf("invalid cascade audio transport")
	}
	name := offer.SessionName
	if name == "" {
		name = "Talk"
	}
	if name != "Play" && name != "Talk" {
		return nil, fmt.Errorf("invalid cascade audio session")
	}
	return []byte(fmt.Sprintf("v=0\r\no=%s 0 0 IN IP4 %s\r\ns=%s\r\nc=IN IP4 %s\r\nt=0 0\r\nm=audio %d %s 8\r\n%s%sa=%s\r\na=rtpmap:8 PCMA/8000\r\ny=%s\r\nf=v/////a/1/8/1\r\n", localID, ip.String(), name, ip.String(), localPort, offer.Transport, setup, rtcpLine, direction, offer.SSRC)), nil
}

// BuildCascadeAudioPullOffer advertises a locally reserved receiver for the
// original upstream broadcast audio. setup is the local RFC 4145 role.
func BuildCascadeAudioPullOffer(localID, localIP string, localPort int, transport, setup, ssrc string, rtcpPorts ...int) ([]byte, error) {
	if len(localID) != 20 || strings.Trim(localID, "0123456789") != "" || !validCascadeAudioSSRC(ssrc) {
		return nil, fmt.Errorf("invalid cascade audio pull identity")
	}
	ip := net.ParseIP(localIP)
	if ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.Equal(net.IPv4bcast) || localPort < 1 || localPort > 65535 {
		return nil, fmt.Errorf("invalid cascade audio pull endpoint")
	}
	setupLines := ""
	rtcpLine := ""
	switch transport {
	case "RTP/AVP":
		if setup != "" {
			return nil, fmt.Errorf("TCP role on UDP audio pull")
		}
		rtcpPort := localPort + 1
		if len(rtcpPorts) > 0 {
			rtcpPort = rtcpPorts[0]
		}
		if rtcpPort < 1 || rtcpPort > 65535 {
			return nil, fmt.Errorf("invalid cascade audio pull RTCP port")
		}
		rtcpLine = fmt.Sprintf("a=rtcp:%d IN IP4 %s\r\n", rtcpPort, ip.To4().String())
	case "TCP/RTP/AVP":
		if setup != "active" && setup != "passive" {
			return nil, fmt.Errorf("invalid cascade audio pull TCP role")
		}
		setupLines = "a=setup:" + setup + "\r\na=connection:new\r\n"
	default:
		return nil, fmt.Errorf("unsupported cascade audio pull transport")
	}
	return []byte(fmt.Sprintf("v=0\r\no=%s 0 0 IN IP4 %s\r\ns=Play\r\nc=IN IP4 %s\r\nt=0 0\r\nm=audio %d %s 8\r\n%s%sa=recvonly\r\na=rtpmap:8 PCMA/8000\r\ny=%s\r\nf=v/////a/1/8/1\r\n", localID, ip.To4().String(), ip.To4().String(), localPort, transport, setupLines, rtcpLine, ssrc)), nil
}

// ValidateCascadeAudioAnswer checks the negotiated endpoint without allowing an
// answer to add a media direction or change the offered transport and SSRC.
func ValidateCascadeAudioAnswer(offer CascadeAudioOffer, body []byte) (CascadeAudioOffer, error) {
	answer, err := ParseCascadeAudioOffer(body)
	if err != nil {
		return CascadeAudioOffer{}, err
	}
	if answer.Transport != offer.Transport || answer.SSRC != offer.SSRC {
		return CascadeAudioOffer{}, fmt.Errorf("cascade audio answer changed transport or SSRC")
	}
	if offer.SessionName != "" && answer.SessionName != offer.SessionName {
		return CascadeAudioOffer{}, fmt.Errorf("cascade audio answer changed session name")
	}
	if offer.Transport == "TCP/RTP/AVP" && answer.Setup == offer.Setup {
		return CascadeAudioOffer{}, fmt.Errorf("cascade audio answer has conflicting TCP roles")
	}
	expected := "sendrecv"
	switch offer.Direction {
	case "sendonly":
		expected = "recvonly"
	case "recvonly":
		expected = "sendonly"
	case "sendrecv":
	default:
		return CascadeAudioOffer{}, fmt.Errorf("invalid offered cascade audio direction")
	}
	if offer.Direction != "sendrecv" && answer.Direction != expected {
		return CascadeAudioOffer{}, fmt.Errorf("cascade audio answer changed media direction")
	}
	return answer, nil
}
