package manscdp

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

// The write schema excludes query-only capability fields and uses SnapShotConfig.
type cascadeConfigWriteWire struct {
	XMLName             xml.Name                 `xml:"Control"`
	CmdType             string                   `xml:"CmdType"`
	SN                  int                      `xml:"SN"`
	DeviceID            string                   `xml:"DeviceID"`
	BasicParam          *basicParamWire          `xml:"BasicParam,omitempty"`
	SVACEncodeConfig    *cascadeSVACEncodeWire   `xml:"SVACEncodeConfig,omitempty"`
	SVACDecodeConfig    *cascadeSVACDecodeWire   `xml:"SVACDecodeConfig,omitempty"`
	VideoParamAttribute *videoParamAttributeWire `xml:"VideoParamAttribute,omitempty"`
	VideoRecordPlan     []videoRecordPlanWire    `xml:"VideoRecordPlan,omitempty"`
	VideoAlarmRecord    []videoAlarmRecordWire   `xml:"VideoAlarmRecord,omitempty"`
	PictureMask         *pictureMaskWire         `xml:"PictureMask,omitempty"`
	FrameMirror         *frameMirrorWire         `xml:"FrameMirror,omitempty"`
	AlarmReport         *alarmReportWire         `xml:"AlarmReport,omitempty"`
	OSDConfig           *osdConfigWire           `xml:"OSDConfig,omitempty"`
	SnapShot            *snapShotWire            `xml:"SnapShotConfig,omitempty"`
}

type CascadeConfigWrite struct{ wire cascadeConfigWriteWire }

func ParseCascadeConfigWrite(body []byte) (*CascadeConfigWrite, error) {
	if len(body) > 1<<20 {
		return nil, fmt.Errorf("configuration command exceeds size limit")
	}
	var source cascadeConfigWriteWire
	if err := DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), body, &source); err != nil {
		return nil, err
	}
	if source.CmdType != CmdDeviceConfig || source.SN <= 0 || strings.TrimSpace(source.DeviceID) == "" {
		return nil, fmt.Errorf("invalid configuration command envelope")
	}
	var envelope struct {
		Children []struct{ XMLName xml.Name } `xml:",any"`
	}
	if err := DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), body, &envelope); err != nil {
		return nil, err
	}
	command := &CascadeConfigWrite{wire: source}
	types := command.Types()
	if len(types) == 0 {
		return nil, fmt.Errorf("configuration command has no writable blocks")
	}
	for _, child := range envelope.Children {
		switch child.XMLName.Local {
		case "CmdType", "SN", "DeviceID", "ExtraInfo":
			continue
		}
		found := false
		for _, kind := range types {
			if child.XMLName.Local == kind {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unsupported configuration block %s", child.XMLName.Local)
		}
	}
	for _, kind := range types {
		if err := validateCascadeConfigShapeFor(body, kind, "Control"); err != nil {
			return nil, err
		}
	}
	if err := command.validate(); err != nil {
		return nil, err
	}
	return command, nil
}

func (c *CascadeConfigWrite) DeviceID() string { return c.wire.DeviceID }
func (c *CascadeConfigWrite) SN() int          { return c.wire.SN }
func (c *CascadeConfigWrite) SnapshotConfig() (*SnapShotBlock, bool) {
	if c == nil || c.wire.SnapShot == nil {
		return nil, false
	}
	return c.wire.SnapShot.toBlock(), true
}

// WithSnapshotRouting copies the command and replaces only cascade routing.
func (c *CascadeConfigWrite) WithSnapshotRouting(sessionID, uploadURL string) (*CascadeConfigWrite, error) {
	if c == nil || c.wire.SnapShot == nil {
		return nil, fmt.Errorf("snapshot configuration missing")
	}
	wire := c.wire
	snapshot := *wire.SnapShot
	snapshot.SessionID, snapshot.UploadURL = &sessionID, &uploadURL
	wire.SnapShot = &snapshot
	copy := &CascadeConfigWrite{wire: wire}
	return copy, copy.validate()
}
func (c *CascadeConfigWrite) Types() []string {
	var kinds []string
	for _, entry := range []struct {
		kind    string
		present bool
	}{
		{ConfigTypeBasicParam, c.wire.BasicParam != nil}, {ConfigTypeSVACEncodeConfig, c.wire.SVACEncodeConfig != nil},
		{ConfigTypeSVACDecodeConfig, c.wire.SVACDecodeConfig != nil}, {ConfigTypeVideoParamAttribute, c.wire.VideoParamAttribute != nil},
		{ConfigTypeVideoRecordPlan, len(c.wire.VideoRecordPlan) > 0}, {ConfigTypeVideoAlarmRecord, len(c.wire.VideoAlarmRecord) > 0},
		{ConfigTypePictureMask, c.wire.PictureMask != nil}, {ConfigTypeFrameMirror, c.wire.FrameMirror != nil},
		{ConfigTypeAlarmReport, c.wire.AlarmReport != nil}, {ConfigTypeOSDConfig, c.wire.OSDConfig != nil}, {ConfigTypeSnapShotConfig, c.wire.SnapShot != nil},
	} {
		if entry.present {
			kinds = append(kinds, entry.kind)
		}
	}
	return kinds
}

// Scoped separates device BasicParam from channel blocks without dropping either.
func (c *CascadeConfigWrite) Scoped(device bool) *CascadeConfigWrite {
	wire := c.wire
	if device {
		wire = cascadeConfigWriteWire{CmdType: wire.CmdType, SN: wire.SN, DeviceID: wire.DeviceID, BasicParam: wire.BasicParam}
	} else {
		wire.BasicParam = nil
	}
	if len((&CascadeConfigWrite{wire: wire}).Types()) == 0 {
		return nil
	}
	return &CascadeConfigWrite{wire: wire}
}

func (c *CascadeConfigWrite) Build(profile protocol.Profile, deviceID string, sn int) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("configuration command missing")
	}
	if err := validatePTZQueryTarget(deviceID, sn); err != nil {
		return nil, err
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	wire := c.wire
	wire.XMLName, wire.SN, wire.DeviceID = xml.Name{}, sn, deviceID
	if wire.BasicParam != nil {
		basic := *wire.BasicParam
		basic.PositionCapability, basic.Longitude, basic.Latitude = nil, nil, nil
		wire.BasicParam = &basic
	}
	return MarshalProfiledXML(profile, wire)
}

func (c *CascadeConfigWrite) validate() error {
	w := c.wire
	if len(c.Types()) == 0 {
		return fmt.Errorf("configuration command empty")
	}
	if w.BasicParam != nil {
		b := w.BasicParam
		if err := (&BasicParamBlock{Name: b.Name, Expiration: b.Expiration, HeartBeatInterval: b.HeartBeatInterval, HeartBeatCount: b.HeartBeatCount}).validate(); err != nil {
			return err
		}
	}
	for _, block := range []interface{ validate() error }{w.PictureMask.toBlock(), w.FrameMirror.toBlock(), w.AlarmReport.toBlock(), w.OSDConfig.toBlock(), w.SnapShot.toBlock()} {
		if err := block.validate(); err != nil {
			return err
		}
	}
	// Validate complete multi-stream lists using the same standard wire as reads.
	if err := validateCascadeConfigLists(cascadeConfigReadWire{VideoParamAttribute: w.VideoParamAttribute, VideoRecordPlan: w.VideoRecordPlan, VideoAlarmRecord: w.VideoAlarmRecord, PictureMask: w.PictureMask, OSDConfig: w.OSDConfig}); err != nil {
		return err
	}
	if w.SnapShot != nil && w.SnapShot.UploadURL != nil {
		address, err := url.Parse(*w.SnapShot.UploadURL)
		if err != nil || address.User != nil {
			return fmt.Errorf("configuration upload URL contains credentials or is invalid")
		}
	}
	return validateCascadeSVACWrite(w.SVACEncodeConfig, w.SVACDecodeConfig)
}

func validateCascadeSVACWrite(encode *cascadeSVACEncodeWire, decode *cascadeSVACDecodeWire) error {
	check := func(value *int, min, max int) error {
		if value != nil && (*value < min || *value > max) {
			return fmt.Errorf("SVAC value outside %d..%d", min, max)
		}
		return nil
	}
	if encode != nil {
		if encode.SurveillanceParam != nil {
			return fmt.Errorf("SVAC encode surveillance parameters are query-only")
		}
		if svc := encode.SVCParam; svc != nil {
			if svc.SVCSpaceSupportMode != nil || svc.SVCTimeSupportMode != nil || svc.SSVCRatioSupportList != nil {
				return fmt.Errorf("SVAC capability fields are query-only")
			}
			for _, value := range []*int{svc.SVCSpaceDomainMode, svc.SVCTimeDomainMode} {
				if value == nil {
					return fmt.Errorf("SVAC encoding modes required")
				}
				if err := check(value, 0, 3); err != nil {
					return err
				}
			}
		}
		if audio := encode.AudioParam; audio != nil {
			if err := check(audio.AudioRecognitionFlag, 0, 1); err != nil {
				return err
			}
		}
		if roi := encode.ROIParam; roi != nil {
			if err := check(roi.ROIFlag, 0, 1); err != nil {
				return err
			}
			if err := check(roi.ROINumber, 0, 16); err != nil {
				return err
			}
			if len(roi.Items) > 16 || roi.ROINumber != nil && *roi.ROINumber != len(roi.Items) {
				return fmt.Errorf("SVAC ROI count mismatch")
			}
			seen := map[int]bool{}
			for _, item := range roi.Items {
				for _, field := range []struct {
					value    *int
					min, max int
				}{{item.ROISeq, 1, 16}, {item.TopLeft, 0, 1024}, {item.BottomRight, 0, 1024}, {item.ROIQP, 0, 3}} {
					if err := check(field.value, field.min, field.max); err != nil {
						return err
					}
				}
				if item.ROISeq != nil {
					if seen[*item.ROISeq] {
						return fmt.Errorf("duplicate SVAC ROI index")
					}
					seen[*item.ROISeq] = true
				}
			}
		}
	}
	if decode != nil {
		if svc := decode.SVCParam; svc != nil {
			if svc.SVCSpaceSupportMode != nil || svc.SVCTimeSupportMode != nil {
				return fmt.Errorf("SVAC decoding capabilities are query-only")
			}
			if svc.SVCSTMMode == nil {
				return fmt.Errorf("SVAC display mode required")
			}
			if err := check(svc.SVCSTMMode, 0, 3); err != nil {
				return err
			}
		}
		if surveillance := decode.SurveillanceParam; surveillance != nil {
			for _, value := range []*int{surveillance.TimeShowFlag, surveillance.OSDShowFlag, surveillance.AIShowFlag, surveillance.GISShowFlag} {
				if err := check(value, 0, 1); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ParseCascadeConfigWriteResponse validates the actual device result; it does
// not infer application success from transport acceptance or a cached value.
func ParseCascadeConfigWriteResponse(body []byte, expected ConfigDownloadExpectation) (*DeviceConfigAck, error) {
	if len(body) > 1<<20 {
		return nil, fmt.Errorf("configuration result exceeds size limit")
	}
	if err := validateCascadeConfigShape(body, ""); err != nil {
		return nil, err
	}
	ack, err := ParseDeviceConfigResponseFor(body, expected)
	if err != nil {
		return nil, err
	}
	if ack.Result != "OK" && ack.Result != "ERROR" {
		return nil, fmt.Errorf("invalid configuration write result")
	}
	return ack, nil
}
