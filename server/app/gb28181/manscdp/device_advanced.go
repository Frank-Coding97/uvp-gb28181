package manscdp

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"
)

// XMLCharset is limited to the declarations used by GB28181 devices.
type XMLCharset string

const (
	XMLCharsetGB2312 XMLCharset = "GB2312"
	XMLCharsetUTF8   XMLCharset = "UTF-8"
)

type RecordAction string

const (
	RecordStart RecordAction = "Record"
	RecordStop  RecordAction = "StopRecord"
)

type GuardAction string

const (
	GuardSet   GuardAction = "SetGuard"
	GuardReset GuardAction = "ResetGuard"
)

type DragZoomDirection string

const (
	DragZoomIn  DragZoomDirection = "DragZoomIn"
	DragZoomOut DragZoomDirection = "DragZoomOut"
)

// AlarmResetOptions narrows a reset to a GB28181 alarm method and/or type.
// Empty fields request the device's all-alarm reset semantics.
type AlarmResetOptions struct {
	AlarmMethod string `xml:"AlarmMethod,omitempty" json:"alarmMethod,omitempty"`
	AlarmType   string `xml:"AlarmType,omitempty" json:"alarmType,omitempty"`
}

// DragZoomRegion uses the six integer fields defined by GB28181. Length and
// Width describe the playback window; the remaining fields describe the box.
type DragZoomRegion struct {
	Length    int `xml:"Length" json:"length"`
	Width     int `xml:"Width" json:"width"`
	MidPointX int `xml:"MidPointX" json:"midPointX"`
	MidPointY int `xml:"MidPointY" json:"midPointY"`
	LengthX   int `xml:"LengthX" json:"lengthX"`
	LengthY   int `xml:"LengthY" json:"lengthY"`
}

type DragZoomCommand struct {
	Direction DragZoomDirection `json:"direction"`
	Region    DragZoomRegion    `json:"region"`
}

type advancedDeviceControl struct {
	XMLName     xml.Name           `xml:"Control"`
	CmdType     string             `xml:"CmdType"`
	SN          int                `xml:"SN"`
	DeviceID    string             `xml:"DeviceID"`
	IFameCmd    string             `xml:"IFameCmd,omitempty"`
	RecordCmd   string             `xml:"RecordCmd,omitempty"`
	GuardCmd    string             `xml:"GuardCmd,omitempty"`
	AlarmCmd    string             `xml:"AlarmCmd,omitempty"`
	TeleBoot    string             `xml:"TeleBoot,omitempty"`
	Info        *AlarmResetOptions `xml:"Info,omitempty"`
	DragZoomIn  *DragZoomRegion    `xml:"DragZoomIn,omitempty"`
	DragZoomOut *DragZoomRegion    `xml:"DragZoomOut,omitempty"`
}

func BuildIFrameControl(deviceID string, sn int, charset XMLCharset) ([]byte, error) {
	return marshalAdvancedControl(deviceID, sn, charset, advancedDeviceControl{IFameCmd: "Send"})
}

func BuildRecordControl(deviceID string, sn int, action RecordAction, charset XMLCharset) ([]byte, error) {
	if action != RecordStart && action != RecordStop {
		return nil, fmt.Errorf("不支持的设备录像动作: %q", action)
	}
	return marshalAdvancedControl(deviceID, sn, charset, advancedDeviceControl{RecordCmd: string(action)})
}

func BuildGuardControl(deviceID string, sn int, action GuardAction, charset XMLCharset) ([]byte, error) {
	if action != GuardSet && action != GuardReset {
		return nil, fmt.Errorf("不支持的布撤防动作: %q", action)
	}
	return marshalAdvancedControl(deviceID, sn, charset, advancedDeviceControl{GuardCmd: string(action)})
}

func BuildAlarmResetControl(deviceID string, sn int, options AlarmResetOptions, charset XMLCharset) ([]byte, error) {
	options.AlarmMethod = strings.TrimSpace(options.AlarmMethod)
	options.AlarmType = strings.TrimSpace(options.AlarmType)
	control := advancedDeviceControl{AlarmCmd: "ResetAlarm"}
	if options.AlarmMethod != "" || options.AlarmType != "" {
		control.Info = &options
	}
	return marshalAdvancedControl(deviceID, sn, charset, control)
}

func BuildTeleBootControl(deviceID string, sn int, confirmed bool, charset XMLCharset) ([]byte, error) {
	if !confirmed {
		return nil, fmt.Errorf("远程重启需要显式确认")
	}
	return marshalAdvancedControl(deviceID, sn, charset, advancedDeviceControl{TeleBoot: "Boot"})
}

func BuildDragZoomControl(deviceID string, sn int, command DragZoomCommand, charset XMLCharset) ([]byte, error) {
	if command.Direction != DragZoomIn && command.Direction != DragZoomOut {
		return nil, fmt.Errorf("不支持的 3D 定位动作: %q", command.Direction)
	}
	if err := validateDragZoomRegion(command.Region); err != nil {
		return nil, err
	}
	control := advancedDeviceControl{}
	if command.Direction == DragZoomIn {
		control.DragZoomIn = &command.Region
	} else {
		control.DragZoomOut = &command.Region
	}
	return marshalAdvancedControl(deviceID, sn, charset, control)
}

func marshalAdvancedControl(deviceID string, sn int, charset XMLCharset, fields advancedDeviceControl) ([]byte, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, fmt.Errorf("设备或通道编码不能为空")
	}
	if sn <= 0 {
		return nil, fmt.Errorf("SN 必须为正数")
	}
	if charset != XMLCharsetGB2312 && charset != XMLCharsetUTF8 {
		return nil, fmt.Errorf("不支持的 XML 编码声明: %q", charset)
	}
	fields.CmdType = CmdDeviceControl
	fields.SN = sn
	fields.DeviceID = deviceID
	body, err := xml.Marshal(fields)
	if err != nil {
		return nil, err
	}
	declaration := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"%s\"?>\r\n", charset)
	return append([]byte(declaration), body...), nil
}

func validateDragZoomRegion(region DragZoomRegion) error {
	if region.Length <= 0 || region.Width <= 0 {
		return fmt.Errorf("3D 定位播放窗口尺寸必须为正数")
	}
	if region.LengthX <= 0 || region.LengthY <= 0 {
		return fmt.Errorf("3D 定位矩形尺寸必须为正数")
	}
	if region.MidPointX < 0 || region.MidPointX > region.Length || region.MidPointY < 0 || region.MidPointY > region.Width {
		return fmt.Errorf("3D 定位矩形中心超出播放窗口")
	}
	left := region.LengthX / 2
	right := region.LengthX - left
	top := region.LengthY / 2
	bottom := region.LengthY - top
	if region.LengthX > region.Length || region.LengthY > region.Width ||
		left > region.MidPointX || right > region.Length-region.MidPointX ||
		top > region.MidPointY || bottom > region.Width-region.MidPointY {
		return fmt.Errorf("3D 定位矩形超出播放窗口")
	}
	return nil
}

type CapabilityState string

const (
	CapabilityUnknown     CapabilityState = "unknown"
	CapabilityUnsupported CapabilityState = "unsupported"
	CapabilitySupported   CapabilityState = "supported"
)

type ControlCapability struct {
	State  CapabilityState `json:"state"`
	Reason string          `json:"reason"`
}

type ControlCapabilities struct {
	BasicPTZ   ControlCapability `json:"basicPtz"`
	IFrame     ControlCapability `json:"iFrame"`
	Record     ControlCapability `json:"record"`
	Guard      ControlCapability `json:"guard"`
	AlarmReset ControlCapability `json:"alarmReset"`
	TeleBoot   ControlCapability `json:"teleBoot"`
	DragZoom   ControlCapability `json:"dragZoom"`
}

// ParseControlCapabilities converts explicitly reported booleans into a
// three-state DTO. PTZType is authoritative only for basic PTZ; it never
// promotes an unreported advanced control capability.
func ParseControlCapabilities(raw *string, ptzType int8) ControlCapabilities {
	result := ControlCapabilities{BasicPTZ: basicPTZCapability(ptzType)}
	var reported map[string]json.RawMessage
	missingReason := "设备未上报该能力"
	if raw == nil || strings.TrimSpace(*raw) == "" {
		reported = nil
	} else if err := json.Unmarshal([]byte(*raw), &reported); err != nil {
		reported = nil
		missingReason = "Capabilities JSON 无效"
	}
	result.IFrame = parseReportedCapability(reported, missingReason, "iframe", "iFrame", "i_frame")
	result.Record = parseReportedCapability(reported, missingReason, "recording", "record")
	result.Guard = parseReportedCapability(reported, missingReason, "guard")
	result.AlarmReset = parseReportedCapability(reported, missingReason, "alarm_reset", "alarmReset")
	result.TeleBoot = parseReportedCapability(reported, missingReason, "teleboot", "teleBoot", "tele_boot")
	result.DragZoom = parseReportedCapability(reported, missingReason, "drag_zoom", "dragZoom")
	return result
}

func basicPTZCapability(ptzType int8) ControlCapability {
	switch ptzType {
	case 1, 2, 4:
		return ControlCapability{State: CapabilitySupported, Reason: "PTZType 明确为可控云台"}
	case 3:
		return ControlCapability{State: CapabilityUnsupported, Reason: "PTZType 明确为固定枪机"}
	default:
		return ControlCapability{State: CapabilityUnknown, Reason: "PTZType 未上报或无法识别"}
	}
}

func parseReportedCapability(reported map[string]json.RawMessage, missingReason string, keys ...string) ControlCapability {
	var value json.RawMessage
	for _, key := range keys {
		if candidate, ok := reported[key]; ok {
			value = candidate
			break
		}
	}
	if value == nil || string(value) == "null" {
		return ControlCapability{State: CapabilityUnknown, Reason: missingReason}
	}
	var supported bool
	if err := json.Unmarshal(value, &supported); err != nil {
		return ControlCapability{State: CapabilityUnknown, Reason: "设备上报的能力值不是布尔值"}
	}
	if supported {
		return ControlCapability{State: CapabilitySupported, Reason: "设备明确上报支持"}
	}
	return ControlCapability{State: CapabilityUnsupported, Reason: "设备明确上报不支持"}
}
