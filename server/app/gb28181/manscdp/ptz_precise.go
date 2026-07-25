package manscdp

import (
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

const (
	CmdPTZPreciseCtrl        = "PTZPreciseCtrl"
	CmdPresetQuery           = "PresetQuery"
	CmdHomePositionQuery     = "HomePositionQuery"
	CmdCruiseTrackListQuery  = "CruiseTrackListQuery"
	CmdCruiseTrackQuery      = "CruiseTrackQuery"
	CmdPTZPreciseStatusQuery = "PTZPreciseStatusQuery"
)

type PTZPreciseControl struct {
	Pan   *float64
	Tilt  *float64
	Zoom  *float64
	Focus *float64
	Iris  *float64
	Speed int
}

type preciseControlXML struct {
	XMLName  xml.Name `xml:"Control"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Pan      *float64 `xml:"Pan,omitempty"`
	Tilt     *float64 `xml:"Tilt,omitempty"`
	Zoom     *float64 `xml:"Zoom,omitempty"`
	Focus    *float64 `xml:"Focus,omitempty"`
	Iris     *float64 `xml:"Iris,omitempty"`
	Speed    int      `xml:"Speed,omitempty"`
}

type ptzQueryXML struct {
	XMLName  xml.Name `xml:"Query"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Number   *int     `xml:"Number,omitempty"`
}

type HomePositionControl struct {
	Enabled     bool
	ResetTime   *int
	PresetIndex *int
}

type homePositionControlXML struct {
	XMLName      xml.Name        `xml:"Control"`
	CmdType      string          `xml:"CmdType"`
	SN           int             `xml:"SN"`
	DeviceID     string          `xml:"DeviceID"`
	HomePosition homePositionXML `xml:"HomePosition"`
}

type homePositionXML struct {
	Enabled     int  `xml:"Enabled"`
	ResetTime   *int `xml:"ResetTime,omitempty"`
	PresetIndex *int `xml:"PresetIndex,omitempty"`
}

func BuildPTZPreciseControl(channelID string, sn int, command PTZPreciseControl) ([]byte, error) {
	return BuildPTZPreciseControlWithProfile(protocol.ProfileFor(protocol.Version2016), channelID, sn, command)
}

// BuildPTZPreciseControlWithProfile builds a precise PTZ control body using
// the profile's actual XML charset and declaration.
func BuildPTZPreciseControlWithProfile(profile protocol.Profile, channelID string, sn int, command PTZPreciseControl) ([]byte, error) {
	if err := validatePTZQueryTarget(channelID, sn); err != nil {
		return nil, err
	}
	for name, value := range map[string]*float64{"Pan": command.Pan, "Tilt": command.Tilt, "Zoom": command.Zoom, "Focus": command.Focus, "Iris": command.Iris} {
		if value != nil {
			if math.IsNaN(*value) || math.IsInf(*value, 0) {
				return nil, fmt.Errorf("%s 数值非法", name)
			}
			text := strconv.FormatFloat(*value, 'f', -1, 64)
			if dot := strings.IndexByte(text, '.'); dot >= 0 && len(text)-dot-1 > 6 {
				return nil, fmt.Errorf("%s 精度超过 6 位", name)
			}
		}
	}
	if command.Speed < 0 || command.Speed > 255 {
		return nil, fmt.Errorf("PTZ 速度必须在 0-255 之间")
	}
	return MarshalProfiledXML(profile, preciseControlXML{
		CmdType: CmdPTZPreciseCtrl, SN: sn, DeviceID: channelID,
		Pan: command.Pan, Tilt: command.Tilt, Zoom: command.Zoom,
		Focus: command.Focus, Iris: command.Iris, Speed: command.Speed,
	})
}

func BuildHomePositionQuery(deviceID string, sn int) ([]byte, error) {
	return BuildHomePositionQueryWithProfile(protocol.ProfileFor(protocol.Version2016), deviceID, sn)
}

func BuildHomePositionQueryWithProfile(profile protocol.Profile, deviceID string, sn int) ([]byte, error) {
	return buildPTZQuery(profile, CmdHomePositionQuery, deviceID, sn, nil)
}

func BuildPresetQuery(deviceID string, sn int) ([]byte, error) {
	return BuildPresetQueryWithProfile(protocol.ProfileFor(protocol.Version2016), deviceID, sn)
}

func BuildPresetQueryWithProfile(profile protocol.Profile, deviceID string, sn int) ([]byte, error) {
	return buildPTZQuery(profile, CmdPresetQuery, deviceID, sn, nil)
}

func BuildHomePositionControl(deviceID string, sn int, command HomePositionControl) ([]byte, error) {
	return BuildHomePositionControlWithProfile(protocol.ProfileFor(protocol.Version2016), deviceID, sn, command)
}

func BuildHomePositionControlWithProfile(profile protocol.Profile, deviceID string, sn int, command HomePositionControl) ([]byte, error) {
	if err := validatePTZQueryTarget(deviceID, sn); err != nil {
		return nil, err
	}
	if command.Enabled {
		if command.ResetTime != nil && *command.ResetTime < 0 {
			return nil, fmt.Errorf("看守位自动归位时间不能为负数")
		}
		if command.PresetIndex != nil && (*command.PresetIndex < 0 || *command.PresetIndex > 255) {
			return nil, fmt.Errorf("看守位预置位编号必须在 0-255 之间")
		}
	}
	home := homePositionXML{Enabled: 0}
	if command.Enabled {
		home.Enabled = 1
		home.ResetTime = command.ResetTime
		home.PresetIndex = command.PresetIndex
	}
	return MarshalProfiledXML(profile, homePositionControlXML{
		CmdType: CmdDeviceControl, SN: sn, DeviceID: deviceID, HomePosition: home,
	})
}

func BuildCruiseTrackListQuery(deviceID string, sn int) ([]byte, error) {
	return BuildCruiseTrackListQueryWithProfile(protocol.ProfileFor(protocol.Version2016), deviceID, sn)
}

func BuildCruiseTrackListQueryWithProfile(profile protocol.Profile, deviceID string, sn int) ([]byte, error) {
	return buildPTZQuery(profile, CmdCruiseTrackListQuery, deviceID, sn, nil)
}

func BuildCruiseTrackQuery(deviceID string, sn, trackID int) ([]byte, error) {
	return BuildCruiseTrackQueryWithProfile(protocol.ProfileFor(protocol.Version2016), deviceID, sn, trackID)
}

func BuildCruiseTrackQueryWithProfile(profile protocol.Profile, deviceID string, sn, trackID int) ([]byte, error) {
	if trackID < 0 || trackID > 255 {
		return nil, fmt.Errorf("巡航轨迹编号必须在 0-255 之间")
	}
	return buildPTZQuery(profile, CmdCruiseTrackQuery, deviceID, sn, &trackID)
}

func BuildPTZPreciseStatusQuery(deviceID string, sn int) ([]byte, error) {
	return BuildPTZPreciseStatusQueryWithProfile(protocol.ProfileFor(protocol.Version2016), deviceID, sn)
}

func BuildPTZPreciseStatusQueryWithProfile(profile protocol.Profile, deviceID string, sn int) ([]byte, error) {
	return buildPTZQuery(profile, CmdPTZPreciseStatusQuery, deviceID, sn, nil)
}

func buildPTZQuery(profile protocol.Profile, cmd, deviceID string, sn int, number *int) ([]byte, error) {
	if err := validatePTZQueryTarget(deviceID, sn); err != nil {
		return nil, err
	}
	return MarshalProfiledXML(profile, ptzQueryXML{CmdType: cmd, SN: sn, DeviceID: deviceID, Number: number})
}

func validatePTZQueryTarget(deviceID string, sn int) error {
	if strings.TrimSpace(deviceID) == "" {
		return fmt.Errorf("设备/通道编码不能为空")
	}
	if sn <= 0 {
		return fmt.Errorf("SN 必须为正数")
	}
	return nil
}

type PTZPreciseStatusResponse struct {
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Pan      *float64 `xml:"Pan"`
	Tilt     *float64 `xml:"Tilt"`
	Zoom     *float64 `xml:"Zoom"`
	Focus    *float64 `xml:"Focus"`
	Iris     *float64 `xml:"Iris"`
	Raw      []byte   `xml:"-"`
}

func ParsePTZPreciseStatusResponse(body []byte) (*PTZPreciseStatusResponse, error) {
	var response PTZPreciseStatusResponse
	if err := newDecoder(body).Decode(&response); err != nil {
		return nil, fmt.Errorf("解析 PTZ 精准状态失败: %w", err)
	}
	if response.CmdType != CmdPTZPreciseStatusQuery || response.DeviceID == "" || response.SN <= 0 {
		return nil, fmt.Errorf("非法 PTZ 精准状态响应")
	}
	response.Raw = append([]byte(nil), body...)
	return &response, nil
}

type PTZPrecisePositionNotify struct {
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Time     string   `xml:"Time"`
	Pan      *float64 `xml:"Pan"`
	Tilt     *float64 `xml:"Tilt"`
	Zoom     *float64 `xml:"Zoom"`
	Focus    *float64 `xml:"Focus"`
	Iris     *float64 `xml:"Iris"`
}

func ParsePTZPrecisePositionNotify(body []byte) (*PTZPrecisePositionNotify, error) {
	var notify PTZPrecisePositionNotify
	if err := newDecoder(body).Decode(&notify); err != nil {
		return nil, fmt.Errorf("解析 PTZ 精准位置通知失败: %w", err)
	}
	if (notify.CmdType != CmdPTZPrecisePosition && notify.CmdType != CmdPTZPreciseStatusQuery) || notify.DeviceID == "" || notify.SN <= 0 {
		return nil, fmt.Errorf("非法 PTZ 精准位置通知")
	}
	return &notify, nil
}

var ErrInvalidResponse = errors.New("manscdp: invalid response")

// ParseError identifies a syntactically or semantically invalid MANSCDP response.
type ParseError struct {
	Field  string
	Value  string
	Reason string
	Err    error
}

func (e *ParseError) Error() string {
	message := "manscdp: invalid response"
	if e.Field != "" {
		message += " field " + e.Field
	}
	if e.Value != "" {
		message += "=" + strconv.Quote(e.Value)
	}
	if e.Reason != "" {
		message += ": " + e.Reason
	}
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

func (e *ParseError) Unwrap() error {
	return e.Err
}

func (e *ParseError) Is(target error) bool {
	return target == ErrInvalidResponse
}

func invalidResponse(field, value, reason string, err error) error {
	return &ParseError{Field: field, Value: value, Reason: reason, Err: err}
}

type HomePositionEnabledEncoding string

const (
	HomePositionEnabledEncodingNumeric           HomePositionEnabledEncoding = "numeric"
	HomePositionEnabledEncodingCompatBooleanText HomePositionEnabledEncoding = "compat_boolean_text"
)

type HomePositionParseOptions struct {
	AllowBooleanEnabled bool
}

type HomePositionConfig struct {
	Enabled         bool
	ResetTime       *int
	PresetIndex     *int
	EnabledEncoding HomePositionEnabledEncoding
}

type HomePositionResponse struct {
	XMLName      xml.Name
	CmdType      string
	SN           int
	DeviceID     string
	HomePosition *HomePositionConfig
	Raw          []byte
}

type homePositionResponseXML struct {
	XMLName      xml.Name               `xml:"Response"`
	CmdType      string                 `xml:"CmdType"`
	SN           int                    `xml:"SN"`
	DeviceID     string                 `xml:"DeviceID"`
	HomePosition *homePositionConfigXML `xml:"HomePosition"`
}

type homePositionConfigXML struct {
	Enabled     *string `xml:"Enabled"`
	ResetTime   *string `xml:"ResetTime"`
	PresetIndex *string `xml:"PresetIndex"`
}

type Preset struct {
	ID   int    `xml:"PresetID" json:"presetId"`
	Name string `xml:"PresetName" json:"name"`
}

type PresetResponse struct {
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	SumNum   int      `xml:"SumNum"`
	Presets  []Preset `xml:"PresetList>Item"`
	Raw      []byte   `xml:"-"`
}

func ParsePresetResponse(body []byte) (*PresetResponse, error) {
	var response PresetResponse
	if err := newDecoder(body).Decode(&response); err != nil {
		return nil, fmt.Errorf("解析预置位响应失败: %w", err)
	}
	if response.CmdType != CmdPresetQuery || response.DeviceID == "" || response.SN <= 0 {
		return nil, fmt.Errorf("非法预置位响应")
	}
	for _, preset := range response.Presets {
		if preset.ID <= 0 || preset.ID > 255 {
			return nil, fmt.Errorf("预置位编号超出 1-255 范围")
		}
	}
	response.Raw = append([]byte(nil), body...)
	return &response, nil
}

func ParseHomePositionResponse(body []byte, options HomePositionParseOptions) (*HomePositionResponse, error) {
	var wire homePositionResponseXML
	if err := newDecoder(body).Decode(&wire); err != nil {
		return nil, invalidResponse("XML", "", "decode failed", err)
	}
	if wire.XMLName.Local != "Response" {
		return nil, invalidResponse("XMLName", wire.XMLName.Local, "root element must be Response", nil)
	}
	if wire.CmdType != CmdHomePositionQuery {
		return nil, invalidResponse("CmdType", wire.CmdType, "must be HomePositionQuery", nil)
	}
	if wire.SN <= 0 {
		return nil, invalidResponse("SN", strconv.Itoa(wire.SN), "must be positive", nil)
	}
	if strings.TrimSpace(wire.DeviceID) == "" {
		return nil, invalidResponse("DeviceID", wire.DeviceID, "must not be empty", nil)
	}

	response := &HomePositionResponse{
		XMLName:  wire.XMLName,
		CmdType:  wire.CmdType,
		SN:       wire.SN,
		DeviceID: wire.DeviceID,
		Raw:      append([]byte(nil), body...),
	}
	if wire.HomePosition == nil {
		return response, nil
	}

	config, err := parseHomePositionConfig(wire.HomePosition, options)
	if err != nil {
		return nil, err
	}
	response.HomePosition = config
	return response, nil
}

func parseHomePositionConfig(wire *homePositionConfigXML, options HomePositionParseOptions) (*HomePositionConfig, error) {
	if wire.Enabled == nil {
		return nil, invalidResponse("HomePosition.Enabled", "", "field is required", nil)
	}
	enabledText := strings.TrimSpace(*wire.Enabled)
	config := &HomePositionConfig{EnabledEncoding: HomePositionEnabledEncodingNumeric}
	switch enabledText {
	case "0":
		config.Enabled = false
	case "1":
		config.Enabled = true
	case "false":
		if !options.AllowBooleanEnabled {
			return nil, invalidResponse("HomePosition.Enabled", enabledText, "boolean text requires an explicit compatibility option", nil)
		}
		config.EnabledEncoding = HomePositionEnabledEncodingCompatBooleanText
	case "true":
		if !options.AllowBooleanEnabled {
			return nil, invalidResponse("HomePosition.Enabled", enabledText, "boolean text requires an explicit compatibility option", nil)
		}
		config.Enabled = true
		config.EnabledEncoding = HomePositionEnabledEncodingCompatBooleanText
	default:
		return nil, invalidResponse("HomePosition.Enabled", enabledText, "must be 0 or 1", nil)
	}

	var err error
	config.ResetTime, err = parseOptionalNonNegativeInt("HomePosition.ResetTime", wire.ResetTime, -1)
	if err != nil {
		return nil, err
	}
	config.PresetIndex, err = parseOptionalNonNegativeInt("HomePosition.PresetIndex", wire.PresetIndex, 255)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func parseOptionalNonNegativeInt(field string, raw *string, max int) (*int, error) {
	if raw == nil {
		return nil, nil
	}
	text := strings.TrimSpace(*raw)
	value, err := strconv.Atoi(text)
	if err != nil {
		return nil, invalidResponse(field, text, "must be an integer", err)
	}
	if value < 0 || (max >= 0 && value > max) {
		reason := "must be non-negative"
		if max >= 0 {
			reason = fmt.Sprintf("must be between 0 and %d", max)
		}
		return nil, invalidResponse(field, text, reason, nil)
	}
	return &value, nil
}

type CruiseTrack struct {
	ID        int             `xml:"Number" json:"trackId"`
	Name      string          `xml:"Name" json:"name"`
	Enabled   *bool           `xml:"Enabled" json:"enabled,omitempty"`
	SumNum    int             `xml:"SumNum" json:"sumNum,omitempty"`
	PointList CruisePointList `xml:"CruisePointList" json:"-"`
}

type CruisePointList struct {
	Num    int           `xml:"Num,attr" json:"num"`
	Points []CruisePoint `xml:"CruisePoint" json:"cruisePoints"`
}

type CruisePoint struct {
	PresetIndex int `xml:"PresetIndex" json:"presetIndex"`
	StayTime    int `xml:"StayTime" json:"stayTime"`
	Speed       int `xml:"Speed" json:"speed"`
}

type CruiseTrackList struct {
	Num    int           `xml:"Num,attr"`
	Tracks []CruiseTrack `xml:"CruiseTrack"`
}

type CruiseTrackListResponse struct {
	CmdType  string          `xml:"CmdType"`
	SN       int             `xml:"SN"`
	DeviceID string          `xml:"DeviceID"`
	SumNum   int             `xml:"SumNum"`
	List     CruiseTrackList `xml:"CruiseTrackList"`
	Raw      []byte          `xml:"-"`
}

func ParseCruiseTrackListResponse(body []byte) (*CruiseTrackListResponse, error) {
	var response CruiseTrackListResponse
	if err := newDecoder(body).Decode(&response); err != nil {
		return nil, fmt.Errorf("解析巡航轨迹列表失败: %w", err)
	}
	if response.CmdType != CmdCruiseTrackListQuery || response.DeviceID == "" || response.SN <= 0 {
		return nil, fmt.Errorf("非法巡航轨迹列表响应")
	}
	if response.SumNum < 0 || response.List.Num != len(response.List.Tracks) || response.SumNum < response.List.Num {
		return nil, fmt.Errorf("巡航轨迹列表数量不一致")
	}
	for _, track := range response.List.Tracks {
		if track.ID < 0 || track.ID > 255 {
			return nil, fmt.Errorf("巡航轨迹编号超出 0-255 范围")
		}
	}
	response.Raw = append([]byte(nil), body...)
	return &response, nil
}

type CruiseTrackResponse struct {
	CmdType  string `xml:"CmdType"`
	SN       int    `xml:"SN"`
	DeviceID string `xml:"DeviceID"`
	CruiseTrack
	Raw []byte `xml:"-"`
}

func ParseCruiseTrackResponse(body []byte) (*CruiseTrackResponse, error) {
	var response CruiseTrackResponse
	if err := newDecoder(body).Decode(&response); err != nil {
		return nil, fmt.Errorf("解析巡航轨迹响应失败: %w", err)
	}
	if response.CmdType != CmdCruiseTrackQuery || response.DeviceID == "" || response.SN <= 0 {
		return nil, fmt.Errorf("非法巡航轨迹响应")
	}
	if response.CruiseTrack.ID < 0 || response.CruiseTrack.ID > 255 || response.CruiseTrack.SumNum < 0 ||
		response.CruiseTrack.PointList.Num != len(response.CruiseTrack.PointList.Points) ||
		response.CruiseTrack.SumNum < response.CruiseTrack.PointList.Num {
		return nil, fmt.Errorf("巡航轨迹详情不合法")
	}
	for _, point := range response.CruiseTrack.PointList.Points {
		if point.PresetIndex <= 0 || point.PresetIndex > 255 || point.StayTime < 0 || point.StayTime > 4095 || point.Speed < 0 || point.Speed > 4095 {
			return nil, fmt.Errorf("巡航点参数不合法")
		}
	}
	response.Raw = append([]byte(nil), body...)
	return &response, nil
}
