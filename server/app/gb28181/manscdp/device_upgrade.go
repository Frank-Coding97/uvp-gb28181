package manscdp

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// CmdDeviceUpgradeResult is the MANSCDP command used by the device's final
// upgrade notification. The upgrade request itself remains DeviceControl.
const CmdDeviceUpgradeResult = "DeviceUpgradeResult"

// DeviceUpgradeCommand contains the four fields defined by GB/T 28181-2022
// A.2.3.1.12. Firmware is interpreted as the target version by this product.
type DeviceUpgradeCommand struct {
	Firmware     string
	FileURL      string
	Manufacturer string
	SessionID    string
}

type deviceUpgradeControlXML struct {
	XMLName       xml.Name             `xml:"Control"`
	CmdType       string               `xml:"CmdType"`
	SN            int                  `xml:"SN"`
	DeviceID      string               `xml:"DeviceID"`
	DeviceUpgrade DeviceUpgradeCommand `xml:"DeviceUpgrade"`
}

// BuildDeviceUpgradeControl creates a GB/T 28181-2022 DeviceControl body.
func BuildDeviceUpgradeControl(deviceID string, sn int, command DeviceUpgradeCommand) ([]byte, error) {
	return BuildDeviceUpgradeControlWithProfile(protocol.ProfileFor(protocol.Version2022), deviceID, sn, command)
}

// BuildDeviceUpgradeControlWithProfile creates a profile encoded request.
// The controller restricts this operation to the 2022 profile; the builder
// remains profile-aware so wire charset handling is consistent with the rest
// of MANSCDP.
func BuildDeviceUpgradeControlWithProfile(profile protocol.Profile, deviceID string, sn int, command DeviceUpgradeCommand) ([]byte, error) {
	if profile.Version != protocol.Version2022 {
		return nil, fmt.Errorf("设备升级仅支持 GB/T 28181-2022 profile")
	}
	if strings.TrimSpace(deviceID) == "" {
		return nil, fmt.Errorf("设备升级 DeviceID 不能为空")
	}
	if sn <= 0 {
		return nil, fmt.Errorf("设备升级 SN 必须为正数")
	}
	if strings.TrimSpace(command.Firmware) == "" {
		return nil, fmt.Errorf("设备升级 Firmware 不能为空")
	}
	if strings.TrimSpace(command.FileURL) == "" {
		return nil, fmt.Errorf("设备升级 FileURL 不能为空")
	}
	if strings.TrimSpace(command.Manufacturer) == "" {
		return nil, fmt.Errorf("设备升级 Manufacturer 不能为空")
	}
	if err := validateDeviceUpgradeSessionID(command.SessionID); err != nil {
		return nil, err
	}
	command.Firmware = strings.TrimSpace(command.Firmware)
	command.FileURL = strings.TrimSpace(command.FileURL)
	command.Manufacturer = strings.TrimSpace(command.Manufacturer)
	command.SessionID = strings.TrimSpace(command.SessionID)
	return MarshalProfiledXML(profile, deviceUpgradeControlXML{
		CmdType: CmdDeviceControl, SN: sn, DeviceID: deviceID, DeviceUpgrade: command,
	})
}

func validateDeviceUpgradeSessionID(value string) error {
	value = strings.TrimSpace(value)
	if len(value) < 32 || len(value) > 128 {
		return fmt.Errorf("设备升级 SessionID 长度必须在 32-128 字节之间")
	}
	for _, ch := range value {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '-' {
			return fmt.Errorf("设备升级 SessionID 只能包含字母、数字和短划线")
		}
	}
	return nil
}

// DeviceUpgradeResultNotify is the final result notification from a device.
// Firmware is the current version reported by the device at the end of the
// attempt, including ERROR responses.
type DeviceUpgradeResultNotify struct {
	XMLName             xml.Name
	CmdType             string
	SN                  int
	DeviceID            string
	SessionID           string
	UpgradeResult       string
	Firmware            string
	UpgradeFailedReason string
	Raw                 []byte
}

type deviceUpgradeResultXML struct {
	XMLName             xml.Name `xml:"Notify"`
	CmdType             string   `xml:"CmdType"`
	SN                  int      `xml:"SN"`
	DeviceID            string   `xml:"DeviceID"`
	SessionID           string   `xml:"SessionID"`
	UpgradeResult       string   `xml:"UpgradeResult"`
	Firmware            string   `xml:"Firmware"`
	UpgradeFailedReason string   `xml:"UpgradeFailedReason"`
}

// ParseDeviceUpgradeResult parses and validates A.2.5.9.
func ParseDeviceUpgradeResult(body []byte) (*DeviceUpgradeResultNotify, error) {
	return ParseDeviceUpgradeResultWithProfile(protocol.ProfileFor(protocol.Version2022), body)
}

// ParseDeviceUpgradeResultWithProfile parses an upgrade result using the
// device operation's XML profile.
func ParseDeviceUpgradeResultWithProfile(profile protocol.Profile, body []byte) (*DeviceUpgradeResultNotify, error) {
	if profile.Version != protocol.Version2022 {
		return nil, fmt.Errorf("设备升级结果仅支持 GB/T 28181-2022 profile")
	}
	var wire deviceUpgradeResultXML
	if err := DecodeProfiledXML(profile, body, &wire); err != nil {
		return nil, fmt.Errorf("解析 DeviceUpgradeResult XML 失败: %w", err)
	}
	if wire.XMLName.Local != "Notify" {
		return nil, fmt.Errorf("DeviceUpgradeResult 根元素必须为 Notify")
	}
	if strings.TrimSpace(wire.CmdType) != CmdDeviceUpgradeResult {
		return nil, fmt.Errorf("DeviceUpgradeResult CmdType 不合法: %q", wire.CmdType)
	}
	if wire.SN <= 0 {
		return nil, fmt.Errorf("DeviceUpgradeResult SN 必须为正数: %s", strconv.Itoa(wire.SN))
	}
	if strings.TrimSpace(wire.DeviceID) == "" {
		return nil, fmt.Errorf("DeviceUpgradeResult DeviceID 不能为空")
	}
	if err := validateDeviceUpgradeSessionID(wire.SessionID); err != nil {
		return nil, err
	}
	wire.UpgradeResult = strings.ToUpper(strings.TrimSpace(wire.UpgradeResult))
	if wire.UpgradeResult != "OK" && wire.UpgradeResult != "ERROR" {
		return nil, fmt.Errorf("DeviceUpgradeResult UpgradeResult 必须为 OK 或 ERROR")
	}
	wire.Firmware = strings.TrimSpace(wire.Firmware)
	if wire.Firmware == "" {
		return nil, fmt.Errorf("DeviceUpgradeResult Firmware 不能为空")
	}
	wire.UpgradeFailedReason = strings.TrimSpace(wire.UpgradeFailedReason)
	if wire.UpgradeResult == "ERROR" {
		switch wire.UpgradeFailedReason {
		case "01", "02", "03", "99":
		default:
			return nil, fmt.Errorf("DeviceUpgradeResult UpgradeFailedReason 不合法: %q", wire.UpgradeFailedReason)
		}
	}
	return &DeviceUpgradeResultNotify{
		XMLName: wire.XMLName, CmdType: wire.CmdType, SN: wire.SN, DeviceID: strings.TrimSpace(wire.DeviceID),
		SessionID: strings.TrimSpace(wire.SessionID), UpgradeResult: wire.UpgradeResult, Firmware: wire.Firmware,
		UpgradeFailedReason: wire.UpgradeFailedReason, Raw: append([]byte(nil), body...),
	}, nil
}
