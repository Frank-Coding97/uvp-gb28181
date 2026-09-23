package manscdp

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// ControlState is the fact value exposed by DeviceStatus. Missing or unknown
// wire values deliberately remain unknown rather than being coerced to off.
type ControlState string

const (
	ControlStateOn      ControlState = "on"
	ControlStateOff     ControlState = "off"
	ControlStateUnknown ControlState = "unknown"
)

type DutyStatus string

const (
	DutyStatusOnDuty  DutyStatus = "ONDUTY"
	DutyStatusOffDuty DutyStatus = "OFFDUTY"
	DutyStatusAlarm   DutyStatus = "ALARM"
	DutyStatusUnknown DutyStatus = "UNKNOWN"
)

// DeviceOnlineState 是设备自报在线状态（<Online>）规范化后的取值。
type DeviceOnlineState string

const (
	DeviceOnlineStateOnline  DeviceOnlineState = "online"
	DeviceOnlineStateOffline DeviceOnlineState = "offline"
	DeviceOnlineStateUnknown DeviceOnlineState = "unknown"
)

// DeviceSelfTestState 是设备自检结果（<Status>）规范化后的取值。
// 未识别的原文一律归入 unknown，不做臆测。
type DeviceSelfTestState string

const (
	DeviceSelfTestOK      DeviceSelfTestState = "ok"
	DeviceSelfTestError   DeviceSelfTestState = "error"
	DeviceSelfTestUnknown DeviceSelfTestState = "unknown"
)

type DeviceStatusExpectation struct {
	SN       int
	DeviceID string
}

type DeviceStatusAlarmItem struct {
	Num        int
	DeviceID   string
	DutyStatus DutyStatus
}

type DeviceStatus struct {
	CmdType    string
	SN         int
	DeviceID   string
	Result     string
	Record     ControlState
	Encode     ControlState
	Online     DeviceOnlineState
	SelfTest   DeviceSelfTestState
	DeviceTime string
	// AlarmNumKnown 为 true 表示设备在应答里明确给出了报警输入数量。
	// 它必须与 AlarmNum 分开看：已知的 0（设备声明自己没有报警输入）和
	// "设备什么都没说"是两种不同的事实，混成一个 0 会让"没有报警能力"
	// 这件事永远停留在未知。
	AlarmNumKnown bool
	AlarmNum      int
	AlarmItems    []DeviceStatusAlarmItem
}

type DeviceStatusErrorCode string

const (
	DeviceStatusErrorMalformed   DeviceStatusErrorCode = "malformed"
	DeviceStatusErrorCmdType     DeviceStatusErrorCode = "cmd_type"
	DeviceStatusErrorResult      DeviceStatusErrorCode = "result"
	DeviceStatusErrorCorrelation DeviceStatusErrorCode = "correlation"
)

type DeviceStatusParseError struct {
	Code DeviceStatusErrorCode
	Msg  string
}

func (e *DeviceStatusParseError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("DeviceStatus parse %s: %s", e.Code, e.Msg)
}

type deviceStatusQuery struct {
	XMLName  xml.Name `xml:"Query"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
}

type deviceStatusWire struct {
	XMLName     xml.Name              `xml:"Response"`
	CmdType     string                `xml:"CmdType"`
	SN          int                   `xml:"SN"`
	DeviceID    string                `xml:"DeviceID"`
	Result      string                `xml:"Result"`
	Online      string                `xml:"Online"`
	Status      string                `xml:"Status"`
	Encode      string                `xml:"Encode"`
	Record      string                `xml:"Record"`
	DeviceTime  string                `xml:"DeviceTime"`
	Alarmstatus *deviceStatusListWire `xml:"Alarmstatus"`
	AlarmStatus *deviceStatusListWire `xml:"AlarmStatus"`
}

type deviceStatusListWire struct {
	NumLower string                 `xml:"num,attr"`
	NumUpper string                 `xml:"Num,attr"`
	NumText  string                 `xml:"Num"`
	Items    []deviceStatusItemWire `xml:"Item"`
}

type deviceStatusItemWire struct {
	NumLower  string `xml:"num,attr"`
	NumUpper  string `xml:"Num,attr"`
	DeviceID  string `xml:"DeviceID"`
	DutyState string `xml:"DutyStatus"`
}

func BuildDeviceStatusQuery(deviceID string, sn int) ([]byte, error) {
	return BuildDeviceStatusQueryWithProfile(protocol.ProfileFor(protocol.Version2016), deviceID, sn)
}

func BuildDeviceStatusQueryWithProfile(profile protocol.Profile, deviceID string, sn int) ([]byte, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, fmt.Errorf("设备编码不能为空")
	}
	if sn <= 0 {
		return nil, fmt.Errorf("SN 必须为正数")
	}
	return MarshalProfiledXML(profile, deviceStatusQuery{CmdType: CmdDeviceStatus, SN: sn, DeviceID: deviceID})
}

func ParseDeviceStatusResponse(body []byte) (DeviceStatus, error) {
	var wire deviceStatusWire
	if err := DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), body, &wire); err != nil {
		return DeviceStatus{}, &DeviceStatusParseError{Code: DeviceStatusErrorMalformed, Msg: err.Error()}
	}
	if wire.CmdType != CmdDeviceStatus {
		return DeviceStatus{}, &DeviceStatusParseError{Code: DeviceStatusErrorCmdType, Msg: fmt.Sprintf("got %q", wire.CmdType)}
	}
	if wire.SN <= 0 || strings.TrimSpace(wire.DeviceID) == "" {
		return DeviceStatus{}, &DeviceStatusParseError{Code: DeviceStatusErrorMalformed, Msg: "SN 或 DeviceID 缺失"}
	}
	result := strings.ToUpper(strings.TrimSpace(wire.Result))
	if result != "OK" {
		return DeviceStatus{}, &DeviceStatusParseError{Code: DeviceStatusErrorResult, Msg: fmt.Sprintf("got %q", wire.Result)}
	}
	status := DeviceStatus{
		CmdType:    wire.CmdType,
		SN:         wire.SN,
		DeviceID:   strings.TrimSpace(wire.DeviceID),
		Result:     result,
		Record:     parseControlState(wire.Record),
		Encode:     parseControlState(wire.Encode),
		Online:     parseOnlineState(wire.Online),
		SelfTest:   parseSelfTestState(wire.Status),
		DeviceTime: strings.TrimSpace(wire.DeviceTime),
	}
	status.AlarmNumKnown, status.AlarmNum = parseAlarmInputCount(&wire)
	for _, list := range []*deviceStatusListWire{wire.Alarmstatus, wire.AlarmStatus} {
		if list == nil {
			continue
		}
		for _, item := range list.Items {
			num := parseNum(item.NumLower)
			if num == 0 {
				num = parseNum(item.NumUpper)
			}
			status.AlarmItems = append(status.AlarmItems, DeviceStatusAlarmItem{
				Num: num, DeviceID: strings.TrimSpace(item.DeviceID), DutyStatus: parseDutyStatus(item.DutyState),
			})
		}
	}
	return status, nil
}

// parseAlarmInputCount 读设备声明的报警输入数量。
// 实测两种形态并存：<Alarmstatus Num="0"> 用属性，<Alarmstatus><Num>1</Num> 用子元素，
// 所以三级都要试。返回值第一项表示"设备到底有没有给出这个数字" —— 设备明确写了 0
// 与设备整段没写是两回事，不能都塌成 0。
func parseAlarmInputCount(wire *deviceStatusWire) (bool, int) {
	for _, list := range []*deviceStatusListWire{wire.Alarmstatus, wire.AlarmStatus} {
		if list == nil {
			continue
		}
		for _, raw := range []string{list.NumText, list.NumUpper, list.NumLower} {
			if declared, ok := parseDeclaredCount(raw); ok {
				return true, declared
			}
		}
		return false, 0
	}
	return false, 0
}

// parseDeclaredCount 与 parseNum 的差别在于 0 是合法取值。
func parseDeclaredCount(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// ParseDeviceClock 解析应答里的设备时间。报文不带时区，按本地时区解释。
func ParseDeviceClock(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
	} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func parseOnlineState(value string) DeviceOnlineState {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "ONLINE":
		return DeviceOnlineStateOnline
	case "OFFLINE":
		return DeviceOnlineStateOffline
	default:
		return DeviceOnlineStateUnknown
	}
}

func parseSelfTestState(value string) DeviceSelfTestState {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "OK":
		return DeviceSelfTestOK
	case "ERROR":
		return DeviceSelfTestError
	default:
		return DeviceSelfTestUnknown
	}
}

func ParseDeviceStatusResponseFor(body []byte, expectation DeviceStatusExpectation) (DeviceStatus, error) {
	status, err := ParseDeviceStatusResponse(body)
	if err != nil {
		return DeviceStatus{}, err
	}
	if expectation.SN > 0 && status.SN != expectation.SN {
		return DeviceStatus{}, &DeviceStatusParseError{Code: DeviceStatusErrorCorrelation, Msg: fmt.Sprintf("SN got %d want %d", status.SN, expectation.SN)}
	}
	if want := strings.TrimSpace(expectation.DeviceID); want != "" && status.DeviceID != want {
		return DeviceStatus{}, &DeviceStatusParseError{Code: DeviceStatusErrorCorrelation, Msg: fmt.Sprintf("DeviceID got %q want %q", status.DeviceID, want)}
	}
	return status, nil
}

func parseControlState(value string) ControlState {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "ON":
		return ControlStateOn
	case "OFF":
		return ControlStateOff
	default:
		return ControlStateUnknown
	}
}

func parseDutyStatus(value string) DutyStatus {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case string(DutyStatusOnDuty):
		return DutyStatusOnDuty
	case string(DutyStatusOffDuty):
		return DutyStatusOffDuty
	case string(DutyStatusAlarm):
		return DutyStatusAlarm
	default:
		return DutyStatusUnknown
	}
}

func parseNum(value string) int {
	if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n > 0 {
		return n
	}
	return 0
}
