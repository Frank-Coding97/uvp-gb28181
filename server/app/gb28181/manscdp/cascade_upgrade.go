package manscdp

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

type DeviceUpgradeControl struct {
	SN       int
	DeviceID string
	Command  DeviceUpgradeCommand
}

func ParseDeviceUpgradeControlWithProfile(profile protocol.Profile, body []byte) (*DeviceUpgradeControl, error) {
	if profile.Version != protocol.Version2022 {
		return nil, fmt.Errorf("设备升级仅支持 GB/T 28181-2022 profile")
	}
	if err := validateUpgradeXMLShape(profile, body, "Control"); err != nil {
		return nil, err
	}
	var wire deviceUpgradeControlXML
	if err := DecodeProfiledXML(profile, body, &wire); err != nil {
		return nil, err
	}
	if strings.TrimSpace(wire.CmdType) != CmdDeviceControl {
		return nil, fmt.Errorf("设备升级 CmdType 必须为 DeviceControl")
	}
	// Use the same validation for accepted and emitted commands.
	if _, err := BuildDeviceUpgradeControlWithProfile(profile, wire.DeviceID, wire.SN, wire.DeviceUpgrade); err != nil {
		return nil, err
	}
	command := wire.DeviceUpgrade
	command.Firmware = strings.TrimSpace(command.Firmware)
	command.FileURL = strings.TrimSpace(command.FileURL)
	command.Manufacturer = strings.TrimSpace(command.Manufacturer)
	command.SessionID = strings.TrimSpace(command.SessionID)
	return &DeviceUpgradeControl{SN: wire.SN, DeviceID: strings.TrimSpace(wire.DeviceID), Command: command}, nil
}

func BuildDeviceUpgradeResultWithProfile(profile protocol.Profile, result DeviceUpgradeResultNotify) ([]byte, error) {
	wire := deviceUpgradeResultXML{
		CmdType: CmdDeviceUpgradeResult, SN: result.SN, DeviceID: result.DeviceID,
		SessionID: result.SessionID, UpgradeResult: strings.ToUpper(strings.TrimSpace(result.UpgradeResult)),
		Firmware: result.Firmware, UpgradeFailedReason: result.UpgradeFailedReason,
	}
	if wire.UpgradeResult == "OK" {
		wire.UpgradeFailedReason = ""
	}
	body, err := MarshalProfiledXML(profile, wire)
	if err != nil {
		return nil, err
	}
	if _, err := ParseDeviceUpgradeResultWithProfile(profile, body); err != nil {
		return nil, err
	}
	return body, nil
}

func validateUpgradeFile(command DeviceUpgradeCommand) error {
	if len(strings.TrimSpace(command.Firmware)) > 255 || len(strings.TrimSpace(command.Manufacturer)) > 255 {
		return fmt.Errorf("设备升级 Firmware/Manufacturer 超过 255 字节")
	}
	address := strings.TrimSpace(command.FileURL)
	parsed, err := url.Parse(address)
	if err != nil || len(address) > 2048 || parsed == nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("设备升级 FileURL 必须为不含用户凭据的 HTTP(S) 地址")
	}
	return nil
}

// Upgrade controls have one action and scalar fields. Reject ambiguous XML
// before encoding/xml can silently choose the last duplicate value.
func validateUpgradeXMLShape(profile protocol.Profile, body []byte, root string) error {
	normalized, err := DecodeProfiledXMLBytes(profile, body)
	if err != nil {
		return err
	}
	decoder := xml.NewDecoder(bytes.NewReader(normalized))
	var path []string
	seen := map[string]bool{}
	roots := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if roots != 1 || len(path) != 0 {
				return fmt.Errorf("invalid upgrade XML document")
			}
			for _, field := range []string{"CmdType", "SN", "DeviceID"} {
				if !seen[root+"/"+field] {
					return fmt.Errorf("upgrade XML missing %s", field)
				}
			}
			if root == "Control" {
				for _, field := range []string{"Firmware", "FileURL", "Manufacturer", "SessionID"} {
					if !seen[root+"/DeviceUpgrade/"+field] {
						return fmt.Errorf("upgrade XML missing %s", field)
					}
				}
			}
			return nil
		}
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Space != "" || len(value.Attr) != 0 {
				return fmt.Errorf("upgrade XML namespaces and attributes are unsupported")
			}
			parent := strings.Join(path, "/")
			allowed := false
			switch parent {
			case "":
				roots++
				allowed = roots == 1 && value.Name.Local == root
			case "Control":
				allowed = strings.Contains(" CmdType SN DeviceID DeviceUpgrade ", " "+value.Name.Local+" ")
			case "Control/DeviceUpgrade":
				allowed = strings.Contains(" Firmware FileURL Manufacturer SessionID ", " "+value.Name.Local+" ")
			case "Notify":
				allowed = strings.Contains(" CmdType SN DeviceID SessionID UpgradeResult Firmware UpgradeFailedReason ", " "+value.Name.Local+" ")
			}
			path = append(path, value.Name.Local)
			key := strings.Join(path, "/")
			if !allowed || seen[key] {
				return fmt.Errorf("invalid or duplicate upgrade field %s", key)
			}
			seen[key] = true
		case xml.EndElement:
			path = path[:len(path)-1]
		case xml.CharData:
			if (len(path) < 2 || strings.Join(path, "/") == "Control/DeviceUpgrade") && strings.TrimSpace(string(value)) != "" {
				return fmt.Errorf("unexpected text outside upgrade scalar field")
			}
		case xml.Directive:
			return fmt.Errorf("upgrade XML directives are unsupported")
		}
	}
}
