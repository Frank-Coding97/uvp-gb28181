package manscdp

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

// ParseCascadeBroadcastNotify accepts one unambiguous broadcast notification.
// The caller must still authenticate SourceID and authorize the published target.
func ParseCascadeBroadcastNotify(profile protocol.Profile, body []byte) (BroadcastNotify, error) {
	normalized, err := DecodeProfiledXMLBytes(profile, body)
	if err != nil {
		return BroadcastNotify{}, err
	}
	decoder := xml.NewDecoder(bytes.NewReader(normalized))
	fields := map[string]string{}
	var path []string
	roots := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return BroadcastNotify{}, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Space != "" || len(value.Attr) != 0 {
				return BroadcastNotify{}, fmt.Errorf("broadcast XML namespaces and attributes are unsupported")
			}
			if len(path) == 0 {
				roots++
				if roots != 1 || value.Name.Local != "Notify" {
					return BroadcastNotify{}, fmt.Errorf("invalid broadcast XML root")
				}
			} else {
				if len(path) != 1 {
					return BroadcastNotify{}, fmt.Errorf("nested broadcast field")
				}
				switch value.Name.Local {
				case "CmdType", "SN", "SourceID", "TargetID":
				default:
					return BroadcastNotify{}, fmt.Errorf("unsupported broadcast field %s", value.Name.Local)
				}
				if _, seen := fields[value.Name.Local]; seen {
					return BroadcastNotify{}, fmt.Errorf("duplicate broadcast field %s", value.Name.Local)
				}
				fields[value.Name.Local] = ""
			}
			path = append(path, value.Name.Local)
		case xml.EndElement:
			path = path[:len(path)-1]
		case xml.CharData:
			if len(path) == 2 {
				fields[path[1]] += string(value)
			} else if strings.TrimSpace(string(value)) != "" {
				return BroadcastNotify{}, fmt.Errorf("unexpected broadcast XML text")
			}
		case xml.Directive:
			return BroadcastNotify{}, fmt.Errorf("broadcast XML directives are unsupported")
		}
	}
	if roots != 1 || len(path) != 0 || len(fields) != 4 {
		return BroadcastNotify{}, fmt.Errorf("incomplete broadcast notification")
	}
	var wire broadcastNotifyWire
	if err := DecodeProfiledXML(profile, normalized, &wire); err != nil {
		return BroadcastNotify{}, err
	}
	source, target := strings.TrimSpace(wire.SourceID), strings.TrimSpace(wire.TargetID)
	if strings.TrimSpace(wire.CmdType) != CmdBroadcast || wire.SN <= 0 || !validMANSCDPDeviceID(source) || !validMANSCDPDeviceID(target) {
		return BroadcastNotify{}, fmt.Errorf("invalid broadcast command or correlation")
	}
	return BroadcastNotify{SN: wire.SN, SourceID: source, TargetID: target}, nil
}

func BuildBroadcastResponseWithProfile(profile protocol.Profile, sn int, deviceID, result string) ([]byte, error) {
	if sn <= 0 || !validMANSCDPDeviceID(deviceID) || result != "OK" && result != "ERROR" {
		return nil, fmt.Errorf("invalid broadcast response correlation or result")
	}
	return MarshalProfiledXML(profile, struct {
		XMLName  xml.Name `xml:"Response"`
		CmdType  string   `xml:"CmdType"`
		SN       int      `xml:"SN"`
		DeviceID string   `xml:"DeviceID"`
		Result   string   `xml:"Result"`
	}{CmdType: CmdBroadcast, SN: sn, DeviceID: strings.TrimSpace(deviceID), Result: result})
}

func BuildBroadcastNotifyWithProfile(profile protocol.Profile, in BroadcastNotify) ([]byte, error) {
	in.SourceID = strings.TrimSpace(in.SourceID)
	in.TargetID = strings.TrimSpace(in.TargetID)
	if in.SN <= 0 || !validMANSCDPDeviceID(in.SourceID) || !validMANSCDPDeviceID(in.TargetID) {
		return nil, fmt.Errorf("invalid broadcast notify correlation")
	}
	return MarshalProfiledXML(profile, broadcastNotifyWire{CmdType: CmdBroadcast, SN: in.SN, SourceID: in.SourceID, TargetID: in.TargetID})
}
