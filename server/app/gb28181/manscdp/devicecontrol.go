package manscdp

import (
	"encoding/xml"
	"fmt"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// PTZAction is the small, stable action vocabulary exposed by the HTTP API.
type PTZAction string

const (
	PTZActionStop      PTZAction = "stop"
	PTZActionLeft      PTZAction = "left"
	PTZActionRight     PTZAction = "right"
	PTZActionUp        PTZAction = "up"
	PTZActionDown      PTZAction = "down"
	PTZActionLeftUp    PTZAction = "left_up"
	PTZActionRightUp   PTZAction = "right_up"
	PTZActionLeftDown  PTZAction = "left_down"
	PTZActionRightDown PTZAction = "right_down"
	PTZActionZoomIn    PTZAction = "zoom_in"
	PTZActionZoomOut   PTZAction = "zoom_out"
)

// PTZCommand is encoded as the GB28181 front-end PTZ command string.
type PTZCommand struct {
	Action PTZAction
	Speed  int
}

// PTZExtendedAction covers standard device-control operations beyond the
// directional A5 command. Lens operations require an explicit device profile.
type PTZExtendedAction string

const (
	PTZActionFocusNear        PTZExtendedAction = "focus_near"
	PTZActionFocusFar         PTZExtendedAction = "focus_far"
	PTZActionIrisOpen         PTZExtendedAction = "iris_open"
	PTZActionIrisClose        PTZExtendedAction = "iris_close"
	PTZActionSetPreset        PTZExtendedAction = "preset_set"
	PTZActionCallPreset       PTZExtendedAction = "preset_call"
	PTZActionDeletePreset     PTZExtendedAction = "preset_delete"
	PTZActionCruiseStart      PTZExtendedAction = "cruise_start"
	PTZActionCruiseStop       PTZExtendedAction = "cruise_stop"
	PTZActionCruisePause      PTZExtendedAction = "cruise_pause"
	PTZActionCruiseResume     PTZExtendedAction = "cruise_resume"
	PTZActionCruiseDelete     PTZExtendedAction = "cruise_delete"
	PTZActionCruiseAddStop    PTZExtendedAction = "cruise_add_stop"
	PTZActionCruiseDeleteStop PTZExtendedAction = "cruise_delete_stop"
	PTZActionCruiseSetSpeed   PTZExtendedAction = "cruise_set_speed"
	PTZActionCruiseSetDwell   PTZExtendedAction = "cruise_set_dwell"
	PTZActionCruiseDeletePath PTZExtendedAction = "cruise_delete_path"
	PTZActionScanStart        PTZExtendedAction = "scan_start"
	PTZActionScanStop         PTZExtendedAction = "scan_stop"
)

// PTZProfile supplies vendor-specific lens instruction bytes. A nil field
// means the device has not declared that operation and it must be rejected.
type PTZProfile struct {
	FocusNearInstruction *byte
	FocusFarInstruction  *byte
	IrisOpenInstruction  *byte
	IrisCloseInstruction *byte
}

type PTZExtendedCommand struct {
	Action  PTZExtendedAction
	ID      int
	Speed   int
	SubID   int // 0x84/0x85 的预置位号;0x85 中为 0 表示删除整条巡航
	Value16 int // 0x86/0x87 的 12 bit 值:低 8 位放 P2,高 4 位放 P3 高半字节
	Profile *PTZProfile
}

type deviceControl struct {
	XMLName  xml.Name    `xml:"Control"`
	CmdType  string      `xml:"CmdType"`
	SN       int         `xml:"SN"`
	DeviceID string      `xml:"DeviceID"`
	PTZCmd   string      `xml:"PTZCmd"`
	Info     controlInfo `xml:"Info"`
}

type controlInfo struct {
	ControlPriority int `xml:"ControlPriority"`
}

type DeviceControlResult string

const (
	DeviceControlResultOK    DeviceControlResult = "OK"
	DeviceControlResultError DeviceControlResult = "ERROR"
)

type DeviceControlResponse struct {
	XMLName  xml.Name
	CmdType  string
	SN       int
	DeviceID string
	Result   DeviceControlResult
	Raw      []byte
}

type deviceControlResponseXML struct {
	XMLName  xml.Name `xml:"Response"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Result   *string  `xml:"Result"`
}

func ParseDeviceControlResponse(body []byte) (*DeviceControlResponse, error) {
	return ParseDeviceControlResponseWithProfile(protocol.ProfileFor(protocol.Version2016), body)
}

// ParseAdvancedControlResponse is the semantic alias used by the advanced
// control scheduler. DeviceControl responses share the same strict wire
// envelope as PTZ DeviceControl responses.
func ParseAdvancedControlResponse(body []byte) (*DeviceControlResponse, error) {
	return ParseDeviceControlResponse(body)
}

// ParseAdvancedControlResponseWithProfile decodes a response using the
// operation's profile and validates every correlation field. A declaration in
// the XML takes precedence over the profile charset, while the profile still
// handles devices that omit the declaration.
func ParseAdvancedControlResponseWithProfile(profile protocol.Profile, body []byte) (*DeviceControlResponse, error) {
	return ParseDeviceControlResponseWithProfile(profile, body)
}

// ParseDeviceControlResponseWithProfile is the profile-aware implementation
// shared by advanced and ordinary DeviceControl response consumers.
func ParseDeviceControlResponseWithProfile(profile protocol.Profile, body []byte) (*DeviceControlResponse, error) {
	var wire deviceControlResponseXML
	if err := DecodeProfiledXML(profile, body, &wire); err != nil {
		return nil, invalidResponse("XML", "", "decode failed", err)
	}
	if wire.XMLName.Local != "Response" {
		return nil, invalidResponse("XMLName", wire.XMLName.Local, "root element must be Response", nil)
	}
	if wire.CmdType != CmdDeviceControl {
		return nil, invalidResponse("CmdType", wire.CmdType, "must be DeviceControl", nil)
	}
	if wire.SN <= 0 {
		return nil, invalidResponse("SN", fmt.Sprint(wire.SN), "must be positive", nil)
	}
	if strings.TrimSpace(wire.DeviceID) == "" {
		return nil, invalidResponse("DeviceID", wire.DeviceID, "must not be empty", nil)
	}
	if wire.Result == nil {
		return nil, invalidResponse("Result", "", "field is required", nil)
	}
	result := DeviceControlResult(strings.TrimSpace(*wire.Result))
	if result != DeviceControlResultOK && result != DeviceControlResultError {
		return nil, invalidResponse("Result", string(result), "must be OK or ERROR", nil)
	}
	return &DeviceControlResponse{
		XMLName:  wire.XMLName,
		CmdType:  wire.CmdType,
		SN:       wire.SN,
		DeviceID: wire.DeviceID,
		Result:   result,
		Raw:      append([]byte(nil), body...),
	}, nil
}

// ParsePTZAction accepts canonical API names plus common UI spellings.
func ParsePTZAction(value string) (PTZAction, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	switch normalized {
	case "stop", "停止":
		return PTZActionStop, nil
	case "left", "左":
		return PTZActionLeft, nil
	case "right", "右":
		return PTZActionRight, nil
	case "up", "上":
		return PTZActionUp, nil
	case "down", "下":
		return PTZActionDown, nil
	case "left_up", "左上":
		return PTZActionLeftUp, nil
	case "right_up", "右上":
		return PTZActionRightUp, nil
	case "left_down", "左下":
		return PTZActionLeftDown, nil
	case "right_down", "右下":
		return PTZActionRightDown, nil
	case "zoom_in", "in", "放大":
		return PTZActionZoomIn, nil
	case "zoom_out", "out", "缩小":
		return PTZActionZoomOut, nil
	default:
		return "", fmt.Errorf("不支持的 PTZ 动作: %q", value)
	}
}

// BuildPTZControl builds a GB28181 DeviceControl MESSAGE body.
// The PTZ command is the standard 8-byte A5 front-end command with a modulo-256 checksum.
func BuildPTZControl(channelID string, sn int, command PTZCommand) ([]byte, error) {
	return BuildPTZControlWithProfile(protocol.ProfileFor(protocol.Version2016), channelID, sn, command)
}

// BuildPTZControlWithProfile builds a directional PTZ DeviceControl body
// using the profile's actual XML charset and declaration.
func BuildPTZControlWithProfile(profile protocol.Profile, channelID string, sn int, command PTZCommand) ([]byte, error) {
	if strings.TrimSpace(channelID) == "" {
		return nil, fmt.Errorf("通道编码不能为空")
	}
	if sn <= 0 {
		return nil, fmt.Errorf("SN 必须为正数")
	}
	if command.Speed < 1 || command.Speed > 255 {
		return nil, fmt.Errorf("PTZ 速度必须在 1-255 之间")
	}
	ptz, err := encodePTZ(command)
	if err != nil {
		return nil, err
	}
	return MarshalProfiledXML(profile, deviceControl{
		CmdType:  CmdDeviceControl,
		SN:       sn,
		DeviceID: channelID,
		PTZCmd:   ptz,
		Info:     controlInfo{ControlPriority: 5},
	})
}

// BuildExtendedPTZControl builds standard DeviceControl operations such as
// preset, cruise and scan commands. Focus/iris use profile bytes
// because their encoding is not interoperable across vendor families.
func BuildExtendedPTZControl(channelID string, sn int, command PTZExtendedCommand) ([]byte, error) {
	return BuildExtendedPTZControlWithProfile(protocol.ProfileFor(protocol.Version2016), channelID, sn, command)
}

// BuildExtendedPTZControlWithProfile builds an extended PTZ DeviceControl
// body using the profile's actual XML charset and declaration.
func BuildExtendedPTZControlWithProfile(profile protocol.Profile, channelID string, sn int, command PTZExtendedCommand) ([]byte, error) {
	if strings.TrimSpace(channelID) == "" {
		return nil, fmt.Errorf("通道编码不能为空")
	}
	if sn <= 0 {
		return nil, fmt.Errorf("SN 必须为正数")
	}
	if command.Speed < 0 || command.Speed > 255 {
		return nil, fmt.Errorf("PTZ 速度必须在 0-255 之间")
	}
	if command.ID < 0 || command.ID > 255 {
		return nil, fmt.Errorf("PTZ 编号必须在 0-255 之间")
	}

	var instruction, parameter1, parameter2, parameter3 byte
	switch command.Action {
	case PTZActionSetPreset:
		instruction = 0x81
		parameter2 = byte(command.ID)
	case PTZActionCallPreset:
		instruction = 0x82
		parameter2 = byte(command.ID)
	case PTZActionDeletePreset:
		instruction = 0x83
		parameter2 = byte(command.ID)
	case PTZActionCruiseStart:
		instruction = 0x88
		parameter1 = byte(command.ID)
	case PTZActionCruiseStop:
		// GB/T 28181 defines no dedicated cruise-stop instruction. The
		// standard front-end stop command is the all-zero instruction.
	case PTZActionCruisePause, PTZActionCruiseResume:
		return nil, fmt.Errorf("PTZ action %q has no standard front-end instruction", command.Action)
	case PTZActionCruiseAddStop:
		if command.SubID <= 0 || command.SubID > 255 {
			return nil, fmt.Errorf("巡航加站的预置位号必须在 1-255 之间")
		}
		instruction = 0x84
		parameter1 = byte(command.ID)
		parameter2 = byte(command.SubID)
	case PTZActionCruiseDeleteStop:
		if command.SubID < 0 || command.SubID > 255 {
			return nil, fmt.Errorf("巡航删点的预置位号必须在 0-255 之间")
		}
		instruction = 0x85
		parameter1 = byte(command.ID)
		parameter2 = byte(command.SubID)
	case PTZActionCruiseSetSpeed:
		if command.Value16 <= 0 || command.Value16 > 4095 {
			return nil, fmt.Errorf("巡航速度必须在 1-4095 之间")
		}
		instruction = 0x86
		parameter1 = byte(command.ID)
		parameter2 = byte(command.Value16 & 0xFF)
		parameter3 = byte((command.Value16>>8)&0x0F) << 4
	case PTZActionCruiseSetDwell:
		if command.Value16 <= 0 || command.Value16 > 4095 {
			return nil, fmt.Errorf("巡航停留时间必须在 1-4095 秒之间")
		}
		instruction = 0x87
		parameter1 = byte(command.ID)
		parameter2 = byte(command.Value16 & 0xFF)
		parameter3 = byte((command.Value16>>8)&0x0F) << 4
	case PTZActionCruiseDelete, PTZActionCruiseDeletePath:
		instruction = 0x85
		parameter1 = byte(command.ID)
	case PTZActionScanStart:
		instruction = 0x89
		parameter1 = byte(command.ID)
	case PTZActionScanStop:
		// As with cruise stop, stop scanning uses the standard stop command.
	case PTZActionFocusNear, PTZActionFocusFar, PTZActionIrisOpen, PTZActionIrisClose:
		if command.Profile == nil {
			return nil, fmt.Errorf("PTZ lens operation requires profile")
		}
		var code *byte
		switch command.Action {
		case PTZActionFocusNear:
			code = command.Profile.FocusNearInstruction
		case PTZActionFocusFar:
			code = command.Profile.FocusFarInstruction
		case PTZActionIrisOpen:
			code = command.Profile.IrisOpenInstruction
		case PTZActionIrisClose:
			code = command.Profile.IrisCloseInstruction
		}
		if code == nil {
			return nil, fmt.Errorf("PTZ lens operation requires profile instruction")
		}
		instruction = *code
		parameter1 = byte(command.Speed)
		parameter2 = byte(command.Speed)
	default:
		return nil, fmt.Errorf("不支持的 PTZ 扩展动作: %q", command.Action)
	}

	bytes := [8]byte{0xA5, 0x0F, 0x01, instruction, parameter1, parameter2, parameter3, 0}
	for i := 0; i < len(bytes)-1; i++ {
		bytes[7] += bytes[i]
	}
	ptz := fmt.Sprintf("%02X%02X%02X%02X%02X%02X%02X%02X", bytes[0], bytes[1], bytes[2], bytes[3], bytes[4], bytes[5], bytes[6], bytes[7])
	return MarshalProfiledXML(profile, deviceControl{CmdType: CmdDeviceControl, SN: sn, DeviceID: channelID, PTZCmd: ptz, Info: controlInfo{ControlPriority: 5}})
}

func encodePTZ(command PTZCommand) (string, error) {
	var instruction byte
	switch command.Action {
	case PTZActionStop:
	case PTZActionLeft:
		instruction = 0x02
	case PTZActionRight:
		instruction = 0x01
	case PTZActionUp:
		instruction = 0x08
	case PTZActionDown:
		instruction = 0x04
	case PTZActionLeftUp:
		instruction = 0x0A
	case PTZActionRightUp:
		instruction = 0x09
	case PTZActionLeftDown:
		instruction = 0x06
	case PTZActionRightDown:
		instruction = 0x05
	case PTZActionZoomIn:
		instruction = 0x10
	case PTZActionZoomOut:
		instruction = 0x20
	default:
		return "", fmt.Errorf("不支持的 PTZ 动作: %q", command.Action)
	}

	moveSpeed := byte(command.Speed)
	zoomSpeed := byte(0)
	if command.Action == PTZActionZoomIn || command.Action == PTZActionZoomOut {
		// The zoom nibble occupies the high half-byte. Values below 0x10
		// are accepted by the API but normalized to the protocol minimum.
		zoom := command.Speed
		if zoom < 0x10 {
			zoom = 0x10
		}
		zoomSpeed = byte(zoom & 0xF0)
		moveSpeed = 0
	}

	bytes := [8]byte{0xA5, 0x0F, 0x01, instruction, moveSpeed, moveSpeed, zoomSpeed, 0}
	for i := 0; i < len(bytes)-1; i++ {
		bytes[7] += bytes[i]
	}
	return fmt.Sprintf("%02X%02X%02X%02X%02X%02X%02X%02X", bytes[0], bytes[1], bytes[2], bytes[3], bytes[4], bytes[5], bytes[6], bytes[7]), nil
}
