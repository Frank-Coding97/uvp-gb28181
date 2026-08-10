// Package protocol provides read-only golden-fixture codecs for the cascade
// boundary. It is deliberately not a production SIP/MANSCDP builder.
package protocol

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	baseprotocol "uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

var errInvalidWire = errors.New("cascade protocol: invalid wire fixture")

// SIPMessage is the minimal, header-preserving representation needed to
// inspect fixture messages. It is not a replacement for the SIP transaction
// codec used by production paths.
type SIPMessage struct {
	StartLine string
	headers   map[string]string
	Body      []byte
}

func (m SIPMessage) Header(name string) string { return m.headers[strings.ToLower(name)] }

// ParseRegister parses a REGISTER request or its response fixture.
func ParseRegister(raw []byte) (SIPMessage, error) {
	message, err := parseSIP(raw)
	if err != nil {
		return SIPMessage{}, err
	}
	if !strings.HasPrefix(message.StartLine, "REGISTER ") && !strings.HasPrefix(message.StartLine, "SIP/2.0 ") {
		return SIPMessage{}, fmt.Errorf("%w: expected REGISTER request or response", errInvalidWire)
	}
	return message, nil
}

// ValidateRegisterProfile keeps the T1 version-header boundary explicit:
// the task baseline uses no 2016 version header and 2022 uses exactly 3.0.
func ValidateRegisterProfile(profile baseprotocol.Version, message SIPMessage) error {
	switch profile {
	case baseprotocol.Version2016:
		if got := message.Header("X-GB-Ver"); got != "" {
			return fmt.Errorf("%w: 2016 X-GB-Ver = %q", errInvalidWire, got)
		}
	case baseprotocol.Version2022:
		if got := message.Header("X-GB-Ver"); got != "3.0" {
			return fmt.Errorf("%w: 2022 X-GB-Ver = %q, want 3.0", errInvalidWire, got)
		}
	default:
		return fmt.Errorf("%w: unsupported profile %q", errInvalidWire, profile)
	}
	return nil
}

// MANSCDPMessage exposes only common fields that T1 fixtures can support
// from the cited standard pages. Unknown XML remains outside this helper.
type MANSCDPMessage struct {
	Encoding       baseprotocol.Charset
	Root           string
	CmdType        string
	SN             int
	DeviceID       string
	SumNum         *int
	DeviceListNum  string
	DeviceListnum  string
	DeviceListSize int
	PTZCmd         string
}

type manscdpEnvelope struct {
	XMLName    xml.Name `xml:""`
	CmdType    string   `xml:"CmdType"`
	SN         int      `xml:"SN"`
	DeviceID   string   `xml:"DeviceID"`
	SumNum     *int     `xml:"SumNum"`
	DeviceList *struct {
		Num       string `xml:"Num,attr"`
		LegacyNum string `xml:"num,attr"`
		Item      []struct {
			DeviceID string `xml:"DeviceID"`
		} `xml:"Item"`
	} `xml:"DeviceList"`
	PTZCmd string `xml:"PTZCmd"`
}

// ParseMANSCDP reads an ASCII-only fixture. The CharsetReader deliberately
// treats fixture bytes as ASCII after the declaration has been checked; it is
// not an XML transcoder and must not be used for device traffic.
func ParseMANSCDP(profile baseprotocol.Version, raw []byte) (MANSCDPMessage, error) {
	encoding, err := declaredEncoding(raw)
	if err != nil {
		return MANSCDPMessage{}, err
	}
	wantEncoding := baseprotocol.CharsetFor(profile)
	if encoding != wantEncoding {
		return MANSCDPMessage{}, fmt.Errorf("%w: encoding = %q, want %q", errInvalidWire, encoding, wantEncoding)
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	decoder.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }
	var envelope manscdpEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return MANSCDPMessage{}, fmt.Errorf("%w: XML: %v", errInvalidWire, err)
	}
	message := MANSCDPMessage{
		Encoding: encoding,
		Root:     envelope.XMLName.Local,
		CmdType:  strings.TrimSpace(envelope.CmdType),
		SN:       envelope.SN,
		DeviceID: strings.TrimSpace(envelope.DeviceID),
		SumNum:   envelope.SumNum,
		PTZCmd:   strings.TrimSpace(envelope.PTZCmd),
	}
	if envelope.DeviceList != nil {
		message.DeviceListNum = envelope.DeviceList.Num
		message.DeviceListnum = envelope.DeviceList.LegacyNum
		message.DeviceListSize = len(envelope.DeviceList.Item)
	}
	return message, nil
}

// ValidateMANSCDPFixture performs only the common T1 assertions. In
// particular it does not interpret PTZ control bits or unknown response data.
func ValidateMANSCDPFixture(message MANSCDPMessage) error {
	if message.CmdType == "" || message.SN < 1 || !validID(message.DeviceID) {
		return fmt.Errorf("%w: missing CmdType, valid SN, or DeviceID", errInvalidWire)
	}
	switch message.CmdType {
	case "Keepalive":
		if message.Root != "Notify" {
			return fmt.Errorf("%w: Keepalive root = %q", errInvalidWire, message.Root)
		}
	case "Catalog":
		if message.Root != "Response" || message.SumNum == nil || *message.SumNum < 0 {
			return fmt.Errorf("%w: invalid Catalog response", errInvalidWire)
		}
		if *message.SumNum == 0 && message.DeviceListSize != 0 {
			return fmt.Errorf("%w: empty Catalog contains items", errInvalidWire)
		}
	case "DeviceInfo", "DeviceStatus":
		if message.Root != "Response" {
			return fmt.Errorf("%w: %s root = %q", errInvalidWire, message.CmdType, message.Root)
		}
	case "DeviceControl":
		if message.Root != "Control" {
			return fmt.Errorf("%w: DeviceControl root = %q", errInvalidWire, message.Root)
		}
		decoded, err := hex.DecodeString(message.PTZCmd)
		if err != nil || len(decoded) != 8 {
			return fmt.Errorf("%w: PTZCmd must be 8 hex bytes", errInvalidWire)
		}
	default:
		return fmt.Errorf("%w: unsupported CmdType %q", errInvalidWire, message.CmdType)
	}
	return nil
}

// PlayInvite combines the INVITE headers and its SDP fixture body.
type PlayInvite struct {
	SIPMessage
	SenderID       string
	SenderSSRC     string
	ReceiverID     string
	ReceiverStream string
	SessionName    string
	ConnectionIP   string
	MediaPort      int
	Transport      string
	SSRC           string
	Format         string
}

func ParsePlayInvite(raw []byte) (PlayInvite, error) {
	message, err := parseSIP(raw)
	if err != nil {
		return PlayInvite{}, err
	}
	if !strings.HasPrefix(message.StartLine, "INVITE ") || !strings.EqualFold(message.Header("Content-Type"), "application/sdp") {
		return PlayInvite{}, fmt.Errorf("%w: expected SDP INVITE", errInvalidWire)
	}
	invite := PlayInvite{SIPMessage: message}
	if invite.SenderID, invite.SenderSSRC, invite.ReceiverID, invite.ReceiverStream, err = parseSubject(message.Header("Subject")); err != nil {
		return PlayInvite{}, err
	}
	for _, line := range strings.Split(string(message.Body), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "s="):
			invite.SessionName = strings.TrimPrefix(line, "s=")
		case strings.HasPrefix(line, "c="):
			parts := strings.Fields(strings.TrimPrefix(line, "c="))
			if len(parts) == 3 {
				invite.ConnectionIP = parts[2]
			}
		case strings.HasPrefix(line, "m="):
			parts := strings.Fields(strings.TrimPrefix(line, "m="))
			if len(parts) >= 3 && parts[0] == "video" {
				invite.MediaPort, _ = strconv.Atoi(parts[1])
				invite.Transport = parts[2]
			}
		case strings.HasPrefix(line, "y="):
			invite.SSRC = strings.TrimPrefix(line, "y=")
		case strings.HasPrefix(line, "f="):
			invite.Format = strings.TrimPrefix(line, "f=")
		}
	}
	return invite, nil
}

// ValidatePlayInvite checks the shared 2016/2022 Play boundary. The two
// profiles retain the same fixture field set here; profile remains explicit
// to prevent a later caller from validating an unspecified generation.
func ValidatePlayInvite(profile baseprotocol.Version, invite PlayInvite) error {
	if profile != baseprotocol.Version2016 && profile != baseprotocol.Version2022 {
		return fmt.Errorf("%w: unsupported profile %q", errInvalidWire, profile)
	}
	if invite.SessionName != "Play" || !validID(invite.SenderID) || !validID(invite.ReceiverID) ||
		!validRealtimeSSRC(invite.SenderSSRC) || invite.ReceiverStream == "" {
		return fmt.Errorf("%w: invalid Play Subject/session", errInvalidWire)
	}
	if net.ParseIP(invite.ConnectionIP) == nil || invite.MediaPort < 1 || invite.MediaPort > 65535 ||
		(invite.Transport != "RTP/AVP" && invite.Transport != "TCP/RTP/AVP") ||
		!validRealtimeSSRC(invite.SSRC) || invite.Format == "" {
		return fmt.Errorf("%w: invalid Play SDP", errInvalidWire)
	}
	return nil
}

func parseSIP(raw []byte) (SIPMessage, error) {
	sections := bytes.SplitN(raw, []byte("\n\n"), 2)
	if len(sections) != 2 {
		return SIPMessage{}, fmt.Errorf("%w: missing SIP body separator", errInvalidWire)
	}
	scanner := bufio.NewScanner(bytes.NewReader(sections[0]))
	if !scanner.Scan() {
		return SIPMessage{}, fmt.Errorf("%w: missing start line", errInvalidWire)
	}
	message := SIPMessage{StartLine: strings.TrimSpace(scanner.Text()), headers: map[string]string{}, Body: sections[1]}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return SIPMessage{}, fmt.Errorf("%w: malformed header %q", errInvalidWire, line)
		}
		message.headers[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.TrimSpace(parts[1])
	}
	if err := scanner.Err(); err != nil {
		return SIPMessage{}, err
	}
	return message, nil
}

func declaredEncoding(raw []byte) (baseprotocol.Charset, error) {
	firstLine, _, _ := bytes.Cut(raw, []byte("\n"))
	line := strings.TrimSpace(string(firstLine))
	const prefix = "<?xml version=\"1.0\" encoding=\""
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, "\"?>") {
		return "", fmt.Errorf("%w: unsupported XML declaration", errInvalidWire)
	}
	return baseprotocol.Charset(strings.TrimSuffix(strings.TrimPrefix(line, prefix), "\"?>")), nil
}

func parseSubject(raw string) (string, string, string, string, error) {
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return "", "", "", "", fmt.Errorf("%w: invalid Subject", errInvalidWire)
	}
	left := strings.Split(strings.TrimSpace(parts[0]), ":")
	right := strings.Split(strings.TrimSpace(parts[1]), ":")
	if len(left) != 2 || len(right) != 2 {
		return "", "", "", "", fmt.Errorf("%w: invalid Subject", errInvalidWire)
	}
	return left[0], left[1], right[0], right[1], nil
}

func validID(value string) bool {
	if len(value) != 20 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validRealtimeSSRC(value string) bool {
	if len(value) != 10 || value[0] != '0' {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
