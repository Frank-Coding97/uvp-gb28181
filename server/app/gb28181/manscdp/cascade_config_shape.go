package manscdp

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

// Existing UI wire structs use values for some required integers. Check their
// presence before decoding so a missing value cannot become a fabricated zero.
func validateCascadeConfigShape(body []byte, configType string) error {
	return validateCascadeConfigShapeFor(body, configType, "Response")
}

func validateCascadeConfigShapeFor(body []byte, configType, root string) error {
	normalized, err := DecodeProfiledXMLBytes(protocol.ProfileFor(protocol.Version2016), body)
	if err != nil {
		return err
	}
	if configType == ConfigTypeSnapShotConfig && root == "Response" {
		configType = "SnapShot"
	}
	type frame struct {
		name          string
		children      map[string]int
		numAttributes int
	}
	var frames []frame
	var path []string
	roots := 0
	decoder := xml.NewDecoder(bytes.NewReader(normalized))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if roots != 1 || len(frames) != 0 {
				return fmt.Errorf("invalid configuration XML document")
			}
			return nil
		}
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.CharData:
			if len(frames) == 0 && strings.TrimSpace(string(value)) != "" {
				return fmt.Errorf("unexpected text outside configuration XML root")
			}
		case xml.StartElement:
			if len(frames) == 0 {
				roots++
				if roots != 1 || value.Name.Local != root {
					return fmt.Errorf("invalid configuration XML root")
				}
			}
			if value.Name.Space != "" {
				return fmt.Errorf("configuration XML namespaces are unsupported")
			}
			if len(frames) >= 64 {
				return fmt.Errorf("configuration XML nesting limit exceeded")
			}
			if len(frames) > 0 {
				frames[len(frames)-1].children[value.Name.Local]++
			}
			entry := frame{name: value.Name.Local, children: map[string]int{}}
			for _, attr := range value.Attr {
				if attr.Name.Local == "Num" {
					entry.numAttributes++
				}
			}
			frames = append(frames, entry)
			path = append(path, entry.name)
		case xml.EndElement:
			if len(frames) == 0 {
				return fmt.Errorf("invalid configuration XML structure")
			}
			current := frames[len(frames)-1]
			location := strings.Join(path, "/")
			if root == "Control" {
				location = "Response" + strings.TrimPrefix(location, "Control")
				location = strings.Replace(location, "/SnapShotConfig", "/SnapShot", 1)
			}
			if len(path) == 1 || len(path) > 1 && path[1] == configType {
				for _, field := range strings.Fields(cascadeConfigShapeFields(location, root)) {
					if current.children[field] != 1 {
						return fmt.Errorf("configuration %s requires exactly one %s", location, field)
					}
				}
				if root == "Control" && (len(path) == 1 || len(path) > 1 && path[1] == configType) {
					for name, count := range current.children {
						if count > 1 && name != "Item" && name != "RecordSchedule" && name != "TimeSegment" && name != "VideoRecordPlan" && name != "VideoAlarmRecord" && name != "ExtraInfo" {
							return fmt.Errorf("duplicate configuration field %s", name)
						}
					}
				}
				if location == "Response" {
					if current.children["Result"] > 1 || configType != ConfigTypeVideoRecordPlan && configType != ConfigTypeVideoAlarmRecord && current.children[configType] > 1 {
						return fmt.Errorf("duplicate configuration block or result")
					}
				}
				if location == "Response/PictureMask/RegionList" && current.numAttributes != 1 {
					return fmt.Errorf("mask list requires one Num attribute")
				}
			}
			frames = frames[:len(frames)-1]
			path = path[:len(path)-1]
		}
	}
}

func cascadeConfigRequiredFields(path string) string {
	switch path {
	case "Response":
		return "CmdType SN DeviceID"
	case "Response/VideoRecordPlan":
		return "RecordEnable RecordScheduleSumNum StreamNumber"
	case "Response/VideoRecordPlan/RecordSchedule":
		return "WeekDayNum TimeSegmentSumNum"
	case "Response/VideoRecordPlan/RecordSchedule/TimeSegment":
		return "StartHour StartMin StartSec StopHour StopMin StopSec"
	case "Response/VideoAlarmRecord":
		return "RecordEnable StreamNumber"
	case "Response/PictureMask":
		return "On SumNum"
	case "Response/PictureMask/RegionList/Item":
		return "Seq Point"
	case "Response/OSDConfig":
		return "Length Width TimeX TimeY SumNum"
	case "Response/OSDConfig/Item":
		return "Text X Y"
	case "Response/VideoParamAttribute/Item":
		return "StreamNumber VideoFormat Resolution FrameRate BitRateType"
	case "Response/AlarmReport":
		return "MotionDetection FieldDetection"
	case "Response/SVACEncodeConfig/ROIParam":
		return "ROIFlag ROINumber"
	case "Response/SVACEncodeConfig/ROIParam/Item":
		return "ROISeq TopLeft BottomRight ROIQP"
	case "Response/SVACEncodeConfig/SVCParam":
		return "SVCSpaceDomainMode SVCTimeDomainMode SVCSpaceSupportMode SVCTimeSupportMode"
	case "Response/SVACEncodeConfig/AudioParam":
		return "AudioRecognitionFlag"
	case "Response/SVACDecodeConfig/SVCParam":
		return "SVCSpaceSupportMode SVCTimeSupportMode"
	case "Response/SnapShot":
		return "SnapNum UploadURL SessionID"
	}
	return ""
}

func cascadeConfigShapeFields(path, root string) string {
	if root == "Control" {
		switch path {
		case "Response/SVACEncodeConfig/ROIParam", "Response/SVACEncodeConfig/ROIParam/Item":
			return ""
		case "Response/SVACEncodeConfig/SVCParam":
			return "SVCSpaceDomainMode SVCTimeDomainMode"
		case "Response/SVACDecodeConfig/SVCParam":
			return "SVCSTMMode"
		}
	}
	return cascadeConfigRequiredFields(path)
}
