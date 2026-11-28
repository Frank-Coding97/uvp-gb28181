package manscdp

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

// NormalizeCascadeConfigTypes covers all twelve A.2.4.7 query types. It is
// independent of the UI's editable configuration groups.
func NormalizeCascadeConfigTypes(types []string) ([]string, error) {
	if len(types) == 0 || len(types) > 64 {
		return nil, fmt.Errorf("invalid configuration type count")
	}
	seen := map[string]bool{}
	var result []string
	for _, value := range types {
		value = strings.TrimSpace(value)
		switch value {
		case ConfigTypeBasicParam, ConfigTypeVideoParamOpt, ConfigTypeSVACEncodeConfig, ConfigTypeSVACDecodeConfig,
			ConfigTypeVideoParamAttribute, ConfigTypeVideoRecordPlan, ConfigTypeVideoAlarmRecord,
			ConfigTypePictureMask, ConfigTypeFrameMirror, ConfigTypeAlarmReport, ConfigTypeOSDConfig, ConfigTypeSnapShotConfig:
		default:
			return nil, fmt.Errorf("unsupported configuration type %q", value)
		}
		if !seen[value] {
			result = append(result, value)
			seen[value] = true
		}
	}
	return result, nil
}

type cascadeConfigReadWire struct {
	XMLName             xml.Name                  `xml:"Response"`
	CmdType             string                    `xml:"CmdType"`
	SN                  int                       `xml:"SN"`
	DeviceID            string                    `xml:"DeviceID"`
	Result              string                    `xml:"Result"`
	BasicParam          *basicParamWire           `xml:"BasicParam,omitempty"`
	VideoParamOpt       *cascadeVideoParamOptWire `xml:"VideoParamOpt,omitempty"`
	SVACEncodeConfig    *cascadeSVACEncodeWire    `xml:"SVACEncodeConfig,omitempty"`
	SVACDecodeConfig    *cascadeSVACDecodeWire    `xml:"SVACDecodeConfig,omitempty"`
	VideoParamAttribute *videoParamAttributeWire  `xml:"VideoParamAttribute,omitempty"`
	VideoRecordPlan     []videoRecordPlanWire     `xml:"VideoRecordPlan,omitempty"`
	VideoAlarmRecord    []videoAlarmRecordWire    `xml:"VideoAlarmRecord,omitempty"`
	PictureMask         *pictureMaskWire          `xml:"PictureMask,omitempty"`
	FrameMirror         *frameMirrorWire          `xml:"FrameMirror,omitempty"`
	AlarmReport         *alarmReportWire          `xml:"AlarmReport,omitempty"`
	OSDConfig           *osdConfigWire            `xml:"OSDConfig,omitempty"`
	SnapShot            *snapShotWire             `xml:"SnapShot,omitempty"`
}

type cascadeVideoParamOptWire struct {
	DownloadSpeed *string `xml:"DownloadSpeed,omitempty"`
	Resolution    *string `xml:"Resolution,omitempty"`
}

// SVAC fields follow A.2.1.21/22. Pointers preserve absence instead of
// manufacturing disabled flags when the device did not return a field.
type cascadeSVACEncodeWire struct {
	ROIParam          *cascadeROIWire                `xml:"ROIParam,omitempty"`
	SVCParam          *cascadeSVCEncodeWire          `xml:"SVCParam,omitempty"`
	SurveillanceParam *cascadeSurveillanceEncodeWire `xml:"SurveillanceParam,omitempty"`
	AudioParam        *cascadeAudioWire              `xml:"AudioParam,omitempty"`
}
type cascadeROIWire struct {
	ROIFlag   *int                 `xml:"ROIFlag,omitempty"`
	ROINumber *int                 `xml:"ROINumber,omitempty"`
	Items     []cascadeROIItemWire `xml:"Item"`
}
type cascadeROIItemWire struct {
	ROISeq      *int `xml:"ROISeq,omitempty"`
	TopLeft     *int `xml:"TopLeft,omitempty"`
	BottomRight *int `xml:"BottomRight,omitempty"`
	ROIQP       *int `xml:"ROIQP,omitempty"`
}
type cascadeSVCEncodeWire struct {
	SVCSpaceDomainMode   *int    `xml:"SVCSpaceDomainMode,omitempty"`
	SVCTimeDomainMode    *int    `xml:"SVCTimeDomainMode,omitempty"`
	SSVCRatioValue       *string `xml:"SSVCRatioValue,omitempty"`
	SVCSpaceSupportMode  *int    `xml:"SVCSpaceSupportMode,omitempty"`
	SVCTimeSupportMode   *int    `xml:"SVCTimeSupportMode,omitempty"`
	SSVCRatioSupportList *string `xml:"SSVCRatioSupportList,omitempty"`
}
type cascadeSurveillanceEncodeWire struct {
	TimeFlag *int `xml:"TimeFlag,omitempty"`
	OSDFlag  *int `xml:"OSDFlag,omitempty"`
	AIFlag   *int `xml:"AIFlag,omitempty"`
	GISFlag  *int `xml:"GISFlag,omitempty"`
}
type cascadeAudioWire struct {
	AudioRecognitionFlag *int `xml:"AudioRecognitionFlag,omitempty"`
}
type cascadeSVACDecodeWire struct {
	SVCParam          *cascadeSVCDecodeWire          `xml:"SVCParam,omitempty"`
	SurveillanceParam *cascadeSurveillanceDecodeWire `xml:"SurveillanceParam,omitempty"`
}
type cascadeSVCDecodeWire struct {
	SVCSTMMode          *int `xml:"SVCSTMMode,omitempty"`
	SVCSpaceSupportMode *int `xml:"SVCSpaceSupportMode,omitempty"`
	SVCTimeSupportMode  *int `xml:"SVCTimeSupportMode,omitempty"`
}
type cascadeSurveillanceDecodeWire struct {
	TimeShowFlag *int `xml:"TimeShowFlag,omitempty"`
	OSDShowFlag  *int `xml:"OSDShowFlag,omitempty"`
	AIShowFlag   *int `xml:"AIShowFlag,omitempty"`
	GISShowFlag  *int `xml:"GISShowFlag,omitempty"`
}

// CascadeConfigReadResponse retains all streams for one requested type, without
// retaining unknown elements, authentication fields or another requested type.
type CascadeConfigReadResponse struct{ wire cascadeConfigReadWire }

func ParseCascadeConfigReadResponse(body []byte, expected ConfigDownloadExpectation, configType string) (*CascadeConfigReadResponse, error) {
	types, err := NormalizeCascadeConfigTypes([]string{configType})
	if err != nil {
		return nil, err
	}
	configType = types[0]
	if len(body) > 1<<20 {
		return nil, fmt.Errorf("configuration response exceeds size limit")
	}
	if err := validateCascadeConfigShape(body, configType); err != nil {
		return nil, err
	}
	var source cascadeConfigReadWire
	if err := DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), body, &source); err != nil {
		return nil, err
	}
	if source.CmdType != CmdConfigDownload || source.SN <= 0 || strings.TrimSpace(source.DeviceID) == "" {
		return nil, fmt.Errorf("invalid configuration response envelope")
	}
	if expected.SN > 0 && source.SN != expected.SN || expected.DeviceID != "" && source.DeviceID != expected.DeviceID {
		return nil, fmt.Errorf("configuration response correlation mismatch")
	}
	result := strings.ToUpper(strings.TrimSpace(source.Result))
	if result == "" {
		result = "OK"
	} // Existing devices omit Result in otherwise valid responses.
	if result != "OK" && result != "ERROR" {
		return nil, fmt.Errorf("invalid configuration result")
	}
	wire := cascadeConfigReadWire{CmdType: source.CmdType, SN: source.SN, DeviceID: source.DeviceID, Result: result}
	if result == "OK" {
		switch configType {
		case ConfigTypeBasicParam:
			wire.BasicParam = source.BasicParam
		case ConfigTypeVideoParamOpt:
			wire.VideoParamOpt = source.VideoParamOpt
		case ConfigTypeSVACEncodeConfig:
			wire.SVACEncodeConfig = source.SVACEncodeConfig
		case ConfigTypeSVACDecodeConfig:
			wire.SVACDecodeConfig = source.SVACDecodeConfig
		case ConfigTypeVideoParamAttribute:
			wire.VideoParamAttribute = source.VideoParamAttribute
		case ConfigTypeVideoRecordPlan:
			wire.VideoRecordPlan = source.VideoRecordPlan
		case ConfigTypeVideoAlarmRecord:
			wire.VideoAlarmRecord = source.VideoAlarmRecord
		case ConfigTypePictureMask:
			wire.PictureMask = source.PictureMask
		case ConfigTypeFrameMirror:
			wire.FrameMirror = source.FrameMirror
		case ConfigTypeAlarmReport:
			wire.AlarmReport = source.AlarmReport
		case ConfigTypeOSDConfig:
			wire.OSDConfig = source.OSDConfig
		case ConfigTypeSnapShotConfig:
			wire.SnapShot = source.SnapShot
		}
		if err := validateCascadeConfigLists(wire); err != nil {
			return nil, err
		}
	}
	return &CascadeConfigReadResponse{wire: wire}, nil
}

func (r *CascadeConfigReadResponse) Accepted() bool { return r != nil && r.wire.Result == "OK" }

func (r *CascadeConfigReadResponse) SnapshotConfig() (*SnapShotBlock, bool) {
	if r == nil || r.wire.SnapShot == nil {
		return nil, false
	}
	return r.wire.SnapShot.toBlock(), true
}

// WithSnapshotRouting leaves the device response unchanged for other readers.
func (r *CascadeConfigReadResponse) WithSnapshotRouting(sessionID, uploadURL string) (*CascadeConfigReadResponse, error) {
	if r == nil || r.wire.SnapShot == nil {
		return nil, fmt.Errorf("snapshot configuration missing")
	}
	wire := r.wire
	snapshot := *wire.SnapShot
	snapshot.SessionID, snapshot.UploadURL = &sessionID, &uploadURL
	wire.SnapShot = &snapshot
	return &CascadeConfigReadResponse{wire: wire}, nil
}

func (r *CascadeConfigReadResponse) Build(profile protocol.Profile, deviceID string, sn int) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("configuration response missing")
	}
	if err := validatePTZQueryTarget(deviceID, sn); err != nil {
		return nil, err
	}
	wire := r.wire
	wire.XMLName, wire.SN, wire.DeviceID = xml.Name{}, sn, deviceID
	if wire.BasicParam != nil && profile.Version == protocol.Version2022 {
		basic := *wire.BasicParam
		basic.PositionCapability, basic.Longitude, basic.Latitude = nil, nil, nil
		wire.BasicParam = &basic
	}
	if wire.SnapShot != nil && wire.SnapShot.UploadURL != nil {
		address, err := url.Parse(*wire.SnapShot.UploadURL)
		if err != nil || address.User != nil {
			return nil, fmt.Errorf("configuration upload URL contains credentials or is invalid")
		}
	}
	return MarshalProfiledXML(profile, wire)
}

func validateCascadeConfigLists(wire cascadeConfigReadWire) error {
	if video := wire.VideoParamAttribute; video != nil {
		if video.Num == nil || *video.Num != len(video.Items) {
			return fmt.Errorf("video parameter list is incomplete")
		}
		parsed, err := parseVideoParamAttributeBlock(video)
		if err != nil {
			return err
		}
		if err := ValidateVideoParamItems(parsed.Items); err != nil {
			return err
		}
	}
	seen := map[int]bool{}
	for _, plan := range wire.VideoRecordPlan {
		if seen[plan.StreamNumber] || plan.RecordScheduleSumNum != len(plan.Schedules) {
			return fmt.Errorf("record plan list is incomplete or duplicated")
		}
		seen[plan.StreamNumber] = true
		for _, day := range plan.Schedules {
			if day.TimeSegmentSumNum != len(day.Segments) {
				return fmt.Errorf("record schedule is incomplete")
			}
		}
		if err := plan.toBlock().validate(); err != nil {
			return err
		}
	}
	seen = map[int]bool{}
	for _, plan := range wire.VideoAlarmRecord {
		if seen[plan.StreamNumber] {
			return fmt.Errorf("duplicate alarm-record stream")
		}
		seen[plan.StreamNumber] = true
		if err := plan.toBlock().validate(); err != nil {
			return err
		}
	}
	if mask := wire.PictureMask; mask != nil {
		count := 0
		if mask.RegionList != nil {
			count = len(mask.RegionList.Items)
			if mask.RegionList.Num != count {
				return fmt.Errorf("mask region count mismatch")
			}
		}
		if mask.SumNum != count || count > MaxPictureMaskRegions {
			return fmt.Errorf("mask region list is incomplete")
		}
	}
	if osd := wire.OSDConfig; osd != nil && osd.SumNum != len(osd.Items) {
		return fmt.Errorf("OSD list is incomplete")
	}
	if svac := wire.SVACEncodeConfig; svac != nil && svac.ROIParam != nil {
		roi := svac.ROIParam
		if roi.ROINumber == nil || *roi.ROINumber != len(roi.Items) || len(roi.Items) > 16 {
			return fmt.Errorf("SVAC ROI list is incomplete")
		}
		seen = map[int]bool{}
		for _, item := range roi.Items {
			if item.ROISeq == nil || *item.ROISeq < 1 || *item.ROISeq > 16 || seen[*item.ROISeq] {
				return fmt.Errorf("invalid SVAC ROI index")
			}
			seen[*item.ROISeq] = true
		}
	}
	return nil
}
