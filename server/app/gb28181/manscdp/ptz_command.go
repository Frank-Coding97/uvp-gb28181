package manscdp

import (
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
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
	Params   *PTZCommandParams
}

type PTZCommandParams struct {
	PresetName      string `xml:"PresetName,omitempty"`
	CruiseTrackName string `xml:"CruiseTrackName,omitempty"`
}

type ptzControlXML struct {
	XMLName  xml.Name          `xml:"Control"`
	CmdType  string            `xml:"CmdType"`
	SN       int               `xml:"SN"`
	DeviceID string            `xml:"DeviceID"`
	PTZCmd   string            `xml:"PTZCmd"`
	Params   *PTZCommandParams `xml:"PTZCmdParams"`
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
	if err := validatePTZCommandParams(profile, command, wire.Params); err != nil {
		return PTZControl{}, err
	}
	return PTZControl{SN: wire.SN, DeviceID: strings.TrimSpace(wire.DeviceID), Command: command, Params: wire.Params}, nil
}

// ParsePTZCommand validates the standard eight-byte front-end command from
// GB/T 28181 Annex A, including preset, cruise, scan and auxiliary commands.
func ParsePTZCommand(raw string) (ParsedPTZCommand, error) {
	raw = strings.TrimSpace(raw)
	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) != 8 {
		return ParsedPTZCommand{}, fmt.Errorf("%w: PTZCmd must contain 8 hex bytes", ErrInvalidPTZCommand)
	}
	if decoded[0] != 0xA5 || decoded[1] != 0x0F {
		return ParsedPTZCommand{}, fmt.Errorf("%w: invalid header", ErrInvalidPTZCommand)
	}
	var checksum byte
	for _, value := range decoded[:7] {
		checksum += value
	}
	if checksum != decoded[7] {
		return ParsedPTZCommand{}, fmt.Errorf("%w: checksum mismatch", ErrInvalidPTZCommand)
	}

	var action string
	if decoded[3]&0x80 != 0 {
		action, err = classifyExtendedPTZInstruction(decoded[3], decoded[4], decoded[5], decoded[6])
	} else {
		action, err = classifyPTZInstruction(decoded[3])
	}
	if err != nil {
		return ParsedPTZCommand{}, err
	}
	return ParsedPTZCommand{Raw: raw, Action: action}, nil
}

func classifyExtendedPTZInstruction(instruction, data1, data2, data3 byte) (string, error) {
	invalid := func() (string, error) {
		return "", fmt.Errorf("%w: invalid parameters for 0x%02X", ErrInvalidPTZCommand, instruction)
	}
	switch instruction {
	case 0x81, 0x82, 0x83:
		if data1 != 0 || data2 == 0 {
			return invalid()
		}
		return map[byte]string{0x81: "preset_set", 0x82: "preset_call", 0x83: "preset_delete"}[instruction], nil
	case 0x84:
		if data2 == 0 {
			return invalid()
		}
		return "cruise_add_stop", nil
	case 0x85:
		if data2 == 0 {
			return "cruise_delete_path", nil
		}
		return "cruise_delete_stop", nil
	case 0x86, 0x87, 0x8A:
		if data2 == 0 && data3>>4 == 0 {
			return invalid()
		}
		return map[byte]string{0x86: "cruise_set_speed", 0x87: "cruise_set_dwell", 0x8A: "scan_set_speed"}[instruction], nil
	case 0x88:
		if data2 != 0 {
			return invalid()
		}
		return "cruise_start", nil
	case 0x89:
		if data2 > 2 {
			return invalid()
		}
		return []string{"scan_start", "scan_set_left", "scan_set_right"}[data2], nil
	case 0x8C:
		return "aux_on", nil
	case 0x8D:
		return "aux_off", nil
	default:
		return "", fmt.Errorf("%w: 0x%02X", ErrUnsupportedPTZInstruction, instruction)
	}
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
		return "ptz_combined", nil
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
		return "fi_combined", nil
	}
}

func BuildRawPTZControlWithProfile(profile protocol.Profile, channelID string, sn int, raw string) ([]byte, error) {
	return BuildRawPTZControlParamsWithProfile(profile, channelID, sn, raw, nil)
}

func BuildRawPTZControlParamsWithProfile(profile protocol.Profile, channelID string, sn int, raw string, params *PTZCommandParams) ([]byte, error) {
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
	if err := validatePTZCommandParams(profile, command, params); err != nil {
		return nil, err
	}
	return MarshalProfiledXML(profile, deviceControl{
		CmdType: CmdDeviceControl, SN: sn, DeviceID: strings.TrimSpace(channelID),
		PTZCmd: command.Raw, PTZCmdParams: params, Info: controlInfo{ControlPriority: 5},
	})
}

func validatePTZCommandParams(profile protocol.Profile, command ParsedPTZCommand, params *PTZCommandParams) error {
	if params == nil {
		return nil
	}
	if (params.PresetName != "" && command.Action != "preset_set") ||
		(params.CruiseTrackName != "" && !strings.HasPrefix(command.Action, "cruise_")) {
		return fmt.Errorf("%w: PTZCmdParams does not match instruction", ErrInvalidPTZCommand)
	}
	if params.CruiseTrackName != "" {
		_, encoder, err := profileCharset(profile)
		if err != nil {
			return err
		}
		encoded := []byte(params.CruiseTrackName)
		if encoder != nil {
			encoded, err = encoder.NewEncoder().Bytes(encoded)
		}
		if err != nil || len(encoded) > 32 {
			return fmt.Errorf("%w: CruiseTrackName exceeds wire limit or cannot be encoded", ErrInvalidPTZCommand)
		}
	}
	return nil
}
