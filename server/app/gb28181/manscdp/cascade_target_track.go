package manscdp

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

type CascadeTargetTrackControl struct {
	SN       int
	DeviceID string
	Command  TargetTrackCommand
}

func ParseTargetTrackControlWithProfile(profile protocol.Profile, body []byte) (CascadeTargetTrackControl, error) {
	if profile.Version != protocol.Version2022 {
		return CascadeTargetTrackControl{}, fmt.Errorf("TargetTrack 仅支持 GB/T 28181-2022")
	}
	normalized, err := DecodeProfiledXMLBytes(profile, body)
	if err != nil {
		return CascadeTargetTrackControl{}, err
	}
	decoder := xml.NewDecoder(bytes.NewReader(normalized))
	allowed := map[string]bool{
		"Control/CmdType": true, "Control/SN": true, "Control/DeviceID": true,
		"Control/TargetTrack": true, "Control/DeviceID2": true,
		"Control/TargetArea/Length": true, "Control/TargetArea/Width": true,
		"Control/TargetArea/MidPointX": true, "Control/TargetArea/MidPointY": true,
		"Control/TargetArea/LengthX": true, "Control/TargetArea/LengthY": true,
	}
	path := []string{}
	seen := map[string]int{}
	roots := 0
	for {
		token, tokenErr := decoder.Token()
		if tokenErr == io.EOF {
			break
		}
		if tokenErr != nil {
			return CascadeTargetTrackControl{}, tokenErr
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Space != "" || len(value.Attr) != 0 {
				return CascadeTargetTrackControl{}, fmt.Errorf("TargetTrack 不支持 XML namespace 或属性")
			}
			if len(path) == 0 {
				roots++
				if roots != 1 || value.Name.Local != "Control" {
					return CascadeTargetTrackControl{}, fmt.Errorf("TargetTrack XML 根节点无效")
				}
			} else if len(path) == 1 && value.Name.Local == "TargetArea" {
				seen["Control/TargetArea"]++
				if seen["Control/TargetArea"] != 1 {
					return CascadeTargetTrackControl{}, fmt.Errorf("TargetArea 重复")
				}
			} else {
				key := strings.Join(append(append([]string(nil), path...), value.Name.Local), "/")
				if !allowed[key] {
					return CascadeTargetTrackControl{}, fmt.Errorf("TargetTrack 包含不支持字段 %s", key)
				}
				seen[key]++
				if seen[key] != 1 {
					return CascadeTargetTrackControl{}, fmt.Errorf("TargetTrack 字段重复 %s", key)
				}
			}
			path = append(path, value.Name.Local)
		case xml.EndElement:
			if len(path) == 0 || path[len(path)-1] != value.Name.Local {
				return CascadeTargetTrackControl{}, fmt.Errorf("TargetTrack XML 结构无效")
			}
			path = path[:len(path)-1]
		case xml.CharData:
			if len(path) == 0 {
				if strings.TrimSpace(string(value)) != "" {
					return CascadeTargetTrackControl{}, fmt.Errorf("TargetTrack 根节点外存在文本")
				}
				continue
			}
			key := strings.Join(path, "/")
			if _, scalar := allowed[key]; !scalar && strings.TrimSpace(string(value)) != "" {
				return CascadeTargetTrackControl{}, fmt.Errorf("TargetTrack 标量字段结构无效")
			}
		case xml.Directive:
			return CascadeTargetTrackControl{}, fmt.Errorf("TargetTrack XML directive unsupported")
		}
	}
	if roots != 1 || len(path) != 0 {
		return CascadeTargetTrackControl{}, fmt.Errorf("TargetTrack XML 文档无效")
	}
	for _, key := range []string{"Control/CmdType", "Control/SN", "Control/DeviceID", "Control/TargetTrack"} {
		if seen[key] != 1 {
			return CascadeTargetTrackControl{}, fmt.Errorf("TargetTrack 缺少 %s", key)
		}
	}
	var wire struct {
		CmdType   string           `xml:"CmdType"`
		SN        int              `xml:"SN"`
		DeviceID  string           `xml:"DeviceID"`
		Mode      string           `xml:"TargetTrack"`
		DeviceID2 string           `xml:"DeviceID2"`
		Area      *TargetTrackArea `xml:"TargetArea"`
	}
	if err := DecodeProfiledXML(profile, normalized, &wire); err != nil {
		return CascadeTargetTrackControl{}, err
	}
	if strings.TrimSpace(wire.CmdType) != CmdDeviceControl || wire.SN <= 0 || !validMANSCDPDeviceID(wire.DeviceID) {
		return CascadeTargetTrackControl{}, fmt.Errorf("TargetTrack 控制头无效")
	}
	if wire.Area != nil {
		for _, field := range []string{"Length", "Width", "MidPointX", "MidPointY", "LengthX", "LengthY"} {
			if seen["Control/TargetArea/"+field] != 1 {
				return CascadeTargetTrackControl{}, fmt.Errorf("TargetArea 缺少 %s", field)
			}
		}
	}
	mode, err := ParseTargetTrackMode(wire.Mode)
	if err != nil {
		return CascadeTargetTrackControl{}, err
	}
	command := TargetTrackCommand{Mode: mode, DeviceID2: strings.TrimSpace(wire.DeviceID2), Area: wire.Area}
	if command.DeviceID2 != "" && !validMANSCDPDeviceID(command.DeviceID2) {
		return CascadeTargetTrackControl{}, fmt.Errorf("TargetTrack DeviceID2 无效")
	}
	if err := ValidateTargetTrackCommand(command); err != nil {
		return CascadeTargetTrackControl{}, err
	}
	return CascadeTargetTrackControl{SN: wire.SN, DeviceID: strings.TrimSpace(wire.DeviceID), Command: command}, nil
}

func validMANSCDPDeviceID(value string) bool {
	if len(strings.TrimSpace(value)) != 20 {
		return false
	}
	for _, digit := range strings.TrimSpace(value) {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
