package manscdp

import (
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

var (
	ErrInvalidPTZCommand         = errors.New("manscdp: invalid PTZ command")
	ErrUnsupportedPTZInstruction = errors.New("manscdp: unsupported PTZ instruction")
)

type ParsedPTZCommand struct {
	Raw    string
	Action string
}

type PTZControl struct {
	SN       int
	DeviceID string
	Command  ParsedPTZCommand
}

type ptzControlXML struct {
	XMLName  xml.Name `xml:"Control"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	PTZCmd   string   `xml:"PTZCmd"`
}

func ParsePTZControlWithProfile(profile protocol.Profile, body []byte) (PTZControl, error) {
	var wire ptzControlXML
	if err := DecodeProfiledXML(profile, body, &wire); err != nil {
		return PTZControl{}, fmt.Errorf("%w: decode DeviceControl: %v", ErrInvalidPTZCommand, err)
	}
	if wire.XMLName.Local != "Control" || strings.TrimSpace(wire.CmdType) != CmdDeviceControl ||
		wire.SN <= 0 || strings.TrimSpace(wire.DeviceID) == "" {
		return PTZControl{}, fmt.Errorf("%w: invalid DeviceControl envelope", ErrInvalidPTZCommand)
	}
	command, err := ParsePTZCommand(wire.PTZCmd)
	if err != nil {
		return PTZControl{}, err
	}
	return PTZControl{SN: wire.SN, DeviceID: strings.TrimSpace(wire.DeviceID), Command: command}, nil
}

// ParsePTZCommand validates the standard eight-byte front-end command from
// GB/T 28181 Annex A. Basic PTZ and FI commands are accepted; extended
// preset/cruise/scan instructions remain outside the first cascade release.
func ParsePTZCommand(raw string) (ParsedPTZCommand, error) {
	raw = strings.TrimSpace(raw)
	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) != 8 {
		return ParsedPTZCommand{}, fmt.Errorf("%w: PTZCmd must contain 8 hex bytes", ErrInvalidPTZCommand)
	}
	if decoded[0] != 0xA5 || decoded[1] != 0x0F || decoded[2] != 0x01 {
		return ParsedPTZCommand{}, fmt.Errorf("%w: invalid header", ErrInvalidPTZCommand)
	}
	var checksum byte
	for _, value := range decoded[:7] {
		checksum += value
	}
	if checksum != decoded[7] {
		return ParsedPTZCommand{}, fmt.Errorf("%w: checksum mismatch", ErrInvalidPTZCommand)
	}

	action, err := classifyPTZInstruction(decoded[3])
	if err != nil {
		return ParsedPTZCommand{}, err
	}
	return ParsedPTZCommand{Raw: raw, Action: action}, nil
}

func classifyPTZInstruction(instruction byte) (string, error) {
	if instruction&0x80 != 0 {
		return "", fmt.Errorf("%w: 0x%02X", ErrUnsupportedPTZInstruction, instruction)
	}
	if instruction&0x40 != 0 {
		return classifyFIInstruction(instruction)
	}
	if instruction&0xC0 != 0 || instruction&0x03 == 0x03 || instruction&0x0C == 0x0C || instruction&0x30 == 0x30 {
		return "", fmt.Errorf("%w: conflicting PTZ direction bits", ErrInvalidPTZCommand)
	}
	switch instruction {
	case 0x00:
		return "stop", nil
	case 0x01:
		return "right", nil
	case 0x02:
		return "left", nil
	case 0x04:
		return "down", nil
	case 0x05:
		return "right_down", nil
	case 0x06:
		return "left_down", nil
	case 0x08:
		return "up", nil
	case 0x09:
		return "right_up", nil
	case 0x0A:
		return "left_up", nil
	case 0x10:
		return "zoom_in", nil
	case 0x20:
		return "zoom_out", nil
	default:
		return "", fmt.Errorf("%w: 0x%02X", ErrUnsupportedPTZInstruction, instruction)
	}
}

func classifyFIInstruction(instruction byte) (string, error) {
	if instruction&0x30 != 0 || instruction&0x03 == 0x03 || instruction&0x0C == 0x0C {
		return "", fmt.Errorf("%w: conflicting FI bits", ErrInvalidPTZCommand)
	}
	switch instruction {
	case 0x40:
		return "fi_stop", nil
	case 0x41:
		return "focus_far", nil
	case 0x42:
		return "focus_near", nil
	case 0x44:
		return "iris_open", nil
	case 0x48:
		return "iris_close", nil
	default:
		return "", fmt.Errorf("%w: 0x%02X", ErrUnsupportedPTZInstruction, instruction)
	}
}

func BuildRawPTZControlWithProfile(profile protocol.Profile, channelID string, sn int, raw string) ([]byte, error) {
	if strings.TrimSpace(channelID) == "" {
		return nil, fmt.Errorf("通道编码不能为空")
	}
	if sn <= 0 {
		return nil, fmt.Errorf("SN 必须为正数")
	}
	command, err := ParsePTZCommand(raw)
	if err != nil {
		return nil, err
	}
	return MarshalProfiledXML(profile, deviceControl{
		CmdType: CmdDeviceControl, SN: sn, DeviceID: strings.TrimSpace(channelID),
		PTZCmd: command.Raw, Info: controlInfo{ControlPriority: 5},
	})
}
