package manscdp

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

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
	AlarmNum   int
	AlarmItems []DeviceStatusAlarmItem
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
	Record      string                `xml:"Record"`
	Alarmstatus *deviceStatusListWire `xml:"Alarmstatus"`
	AlarmStatus *deviceStatusListWire `xml:"AlarmStatus"`
}

type deviceStatusListWire struct {
	NumLower string                 `xml:"num,attr"`
	NumUpper string                 `xml:"Num,attr"`
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
		CmdType:  wire.CmdType,
		SN:       wire.SN,
		DeviceID: strings.TrimSpace(wire.DeviceID),
		Result:   result,
		Record:   parseControlState(wire.Record),
	}
	for _, list := range []*deviceStatusListWire{wire.Alarmstatus, wire.AlarmStatus} {
		if list == nil {
			continue
		}
		listNum := parseNum(list.NumUpper)
		if listNum == 0 {
			listNum = parseNum(list.NumLower)
		}
		if listNum > 0 && status.AlarmNum == 0 {
			status.AlarmNum = listNum
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
