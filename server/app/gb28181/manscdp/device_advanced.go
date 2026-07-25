package manscdp

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// XMLCharset is limited to the declarations used by GB28181 devices.
type XMLCharset string

const (
	XMLCharsetGB2312  XMLCharset = "GB2312"
	XMLCharsetGB18030 XMLCharset = "GB18030"
	XMLCharsetUTF8    XMLCharset = "UTF-8"
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

// AlarmMethod is the standard GB/T 28181 AlarmMethod code. AlarmMethodAll
// (zero) requests a reset for every alarm method; the remaining values may be
// combined with "/" (for example, "1/5") in AlarmResetOptions.
type AlarmMethod uint8

const (
	AlarmMethodAll         AlarmMethod = 0
	AlarmMethodPhone       AlarmMethod = 1
	AlarmMethodDevice      AlarmMethod = 2
	AlarmMethodSMS         AlarmMethod = 3
	AlarmMethodGPS         AlarmMethod = 4
	AlarmMethodVideo       AlarmMethod = 5
	AlarmMethodDeviceFault AlarmMethod = 6
	AlarmMethodManual      AlarmMethod = 7
)

// AlarmType is the standard GB/T 28181 AlarmType code used by AlarmCmd. The
// current standard set is deliberately finite; unknown vendor values must be
// rejected before a control operation is persisted or sent.
type AlarmType uint8

const (
	AlarmTypeVideoLost    AlarmType = 1
	AlarmTypeDeviceTamper AlarmType = 2
	AlarmTypeStorageFull  AlarmType = 3
	AlarmTypeDeviceFault  AlarmType = 4
	AlarmTypeOther        AlarmType = 5
)

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
	IFrameCmd   string             `xml:"IFrameCmd,omitempty"`
	RecordCmd   string             `xml:"RecordCmd,omitempty"`
	GuardCmd    string             `xml:"GuardCmd,omitempty"`
	AlarmCmd    string             `xml:"AlarmCmd,omitempty"`
	TeleBoot    string             `xml:"TeleBoot,omitempty"`
	Info        *AlarmResetOptions `xml:"Info,omitempty"`
	DragZoomIn  *DragZoomRegion    `xml:"DragZoomIn,omitempty"`
	DragZoomOut *DragZoomRegion    `xml:"DragZoomOut,omitempty"`
}

func BuildIFrameControl(deviceID string, sn int, charset XMLCharset) ([]byte, error) {
	profile, err := advancedProfileForCharset(charset)
	if err != nil {
		return nil, err
	}
	return BuildIFrameControlWithProfile(profile, deviceID, sn)
}

func BuildRecordControl(deviceID string, sn int, action RecordAction, charset XMLCharset) ([]byte, error) {
	if action != RecordStart && action != RecordStop {
		return nil, fmt.Errorf("不支持的设备录像动作: %q", action)
	}
	profile, err := advancedProfileForCharset(charset)
	if err != nil {
		return nil, err
	}
	return BuildRecordControlWithProfile(profile, deviceID, sn, action)
}

func BuildGuardControl(deviceID string, sn int, action GuardAction, charset XMLCharset) ([]byte, error) {
	if action != GuardSet && action != GuardReset {
		return nil, fmt.Errorf("不支持的布撤防动作: %q", action)
	}
	profile, err := advancedProfileForCharset(charset)
	if err != nil {
		return nil, err
	}
	return BuildGuardControlWithProfile(profile, deviceID, sn, action)
}

func BuildAlarmResetControl(deviceID string, sn int, options AlarmResetOptions, charset XMLCharset) ([]byte, error) {
	profile, err := advancedProfileForCharset(charset)
	if err != nil {
		return nil, err
	}
	return BuildAlarmResetControlWithProfile(profile, deviceID, sn, options)
}

func BuildTeleBootControl(deviceID string, sn int, confirmed bool, charset XMLCharset) ([]byte, error) {
	if !confirmed {
		return nil, fmt.Errorf("远程重启需要显式确认")
	}
	profile, err := advancedProfileForCharset(charset)
	if err != nil {
		return nil, err
	}
	return BuildTeleBootControlWithProfile(profile, deviceID, sn, confirmed)
}

func BuildDragZoomControl(deviceID string, sn int, command DragZoomCommand, charset XMLCharset) ([]byte, error) {
	if command.Direction != DragZoomIn && command.Direction != DragZoomOut {
		return nil, fmt.Errorf("不支持的 3D 定位动作: %q", command.Direction)
	}
	if err := validateDragZoomRegion(command.Region); err != nil {
		return nil, err
	}
	profile, err := advancedProfileForCharset(charset)
	if err != nil {
		return nil, err
	}
	return BuildDragZoomControlWithProfile(profile, deviceID, sn, command)
}

func marshalAdvancedControl(deviceID string, sn int, charset XMLCharset, fields advancedDeviceControl) ([]byte, error) {
	profile, err := advancedProfileForCharset(charset)
	if err != nil {
		return nil, err
	}
	return marshalAdvancedControlWithProfile(profile, deviceID, sn, fields)
}

// BuildIFrameControlWithProfile builds the force-key-frame command using the
// profile's field spelling: deployed 2016 devices commonly require the
// historical IFameCmd typo, while 2022 uses IFrameCmd.
func BuildIFrameControlWithProfile(profile protocol.Profile, deviceID string, sn int) ([]byte, error) {
	return marshalAdvancedControlWithProfile(profile, deviceID, sn, advancedDeviceControl{IFameCmd: "Send"})
}

// BuildRecordControlWithProfile builds a device-side recording command.
func BuildRecordControlWithProfile(profile protocol.Profile, deviceID string, sn int, action RecordAction) ([]byte, error) {
	if action != RecordStart && action != RecordStop {
		return nil, fmt.Errorf("不支持的设备录像动作: %q", action)
	}
	return marshalAdvancedControlWithProfile(profile, deviceID, sn, advancedDeviceControl{RecordCmd: string(action)})
}

// BuildGuardControlWithProfile builds a guard/unguard command.
func BuildGuardControlWithProfile(profile protocol.Profile, deviceID string, sn int, action GuardAction) ([]byte, error) {
	if action != GuardSet && action != GuardReset {
		return nil, fmt.Errorf("不支持的布撤防动作: %q", action)
	}
	return marshalAdvancedControlWithProfile(profile, deviceID, sn, advancedDeviceControl{GuardCmd: string(action)})
}

// BuildAlarmResetControlWithProfile builds AlarmCmd=ResetAlarm. Empty
// options intentionally mean all alarms; non-empty values are validated
// against the standard AlarmMethod/AlarmType enumerations first.
func BuildAlarmResetControlWithProfile(profile protocol.Profile, deviceID string, sn int, options AlarmResetOptions) ([]byte, error) {
	options.AlarmMethod = strings.TrimSpace(options.AlarmMethod)
	options.AlarmType = strings.TrimSpace(options.AlarmType)
	if err := ValidateAlarmResetOptions(options); err != nil {
		return nil, err
	}
	control := advancedDeviceControl{AlarmCmd: "ResetAlarm"}
	if options.AlarmMethod != "" || options.AlarmType != "" {
		control.Info = &options
	}
	return marshalAdvancedControlWithProfile(profile, deviceID, sn, control)
}

// BuildTeleBootControlWithProfile builds a confirmed remote reboot command.
func BuildTeleBootControlWithProfile(profile protocol.Profile, deviceID string, sn int, confirmed bool) ([]byte, error) {
	if !confirmed {
		return nil, fmt.Errorf("远程重启需要显式确认")
	}
	return marshalAdvancedControlWithProfile(profile, deviceID, sn, advancedDeviceControl{TeleBoot: "Boot"})
}

// BuildDragZoomControlWithProfile builds a structured 3D drag command with
// actual playback-window pixels from the caller.
func BuildDragZoomControlWithProfile(profile protocol.Profile, deviceID string, sn int, command DragZoomCommand) ([]byte, error) {
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
	return marshalAdvancedControlWithProfile(profile, deviceID, sn, control)
}

func advancedProfileForCharset(charset XMLCharset) (protocol.Profile, error) {
	profile := protocol.ProfileFor(protocol.Version2016)
	switch charset {
	case XMLCharsetGB2312:
		profile.Charset = protocol.CharsetGB2312
	case XMLCharsetGB18030:
		profile.Charset = protocol.CharsetGB18030
	case XMLCharsetUTF8:
		profile.Charset = protocol.CharsetUTF8
	default:
		return protocol.Profile{}, fmt.Errorf("不支持的 XML 编码声明: %q", charset)
	}
	return profile, nil
}

func marshalAdvancedControlWithProfile(profile protocol.Profile, deviceID string, sn int, fields advancedDeviceControl) ([]byte, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, fmt.Errorf("设备或通道编码不能为空")
	}
	if sn <= 0 {
		return nil, fmt.Errorf("SN 必须为正数")
	}
	if profile.Version == "" {
		profile = protocol.ProfileFor(protocol.Version2016)
	}
	if profile.IFrameElement == "" {
		profile.IFrameElement = protocol.ProfileFor(profile.Version).IFrameElement
	}
	fields.CmdType = CmdDeviceControl
	fields.SN = sn
	fields.DeviceID = deviceID
	if value := strings.TrimSpace(fields.IFameCmd); value != "" || strings.TrimSpace(fields.IFrameCmd) != "" {
		if value == "" {
			value = strings.TrimSpace(fields.IFrameCmd)
		}
		fields.IFameCmd = ""
		fields.IFrameCmd = ""
		if profile.IFrameElement == "IFrameCmd" {
			fields.IFrameCmd = value
		} else {
			fields.IFameCmd = value
		}
	}
	return MarshalProfiledXML(profile, fields)
}

// ParseAlarmMethod parses the optional slash-separated AlarmMethod selector.
// Zero means all methods and therefore cannot be combined with another code.
func ParseAlarmMethod(raw string) ([]AlarmMethod, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, "/")
	result := make([]AlarmMethod, 0, len(parts))
	seen := make(map[AlarmMethod]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("AlarmMethod 包含空枚举值")
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < int(AlarmMethodAll) || value > int(AlarmMethodManual) {
			return nil, fmt.Errorf("AlarmMethod %q 不在 0-7 标准范围内", raw)
		}
		method := AlarmMethod(value)
		if _, exists := seen[method]; exists {
			return nil, fmt.Errorf("AlarmMethod %q 包含重复枚举值", raw)
		}
		if method == AlarmMethodAll && len(parts) != 1 {
			return nil, fmt.Errorf("AlarmMethod=0 不能与其他报警方式组合")
		}
		seen[method] = struct{}{}
		result = append(result, method)
	}
	return result, nil
}

// ParseAlarmType parses the standard AlarmType selector. AlarmType does not
// have the AlarmMethod slash-combination syntax; an empty value is omitted.
func ParseAlarmType(raw string) (AlarmType, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < int(AlarmTypeVideoLost) || value > int(AlarmTypeOther) {
		return 0, fmt.Errorf("AlarmType %q 不在 1-5 标准范围内", raw)
	}
	return AlarmType(value), nil
}

// ValidateAlarmResetOptions validates both optional selectors before XML is
// built. This keeps malformed alarm combinations out of persisted operations.
func ValidateAlarmResetOptions(options AlarmResetOptions) error {
	if _, err := ParseAlarmMethod(options.AlarmMethod); err != nil {
		return err
	}
	if _, err := ParseAlarmType(options.AlarmType); err != nil {
		return err
	}
	return nil
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
