package manscdp

import (
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"
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
	Enabled   bool
	ResetTime int
	PresetID  int
}

type homePositionControlXML struct {
	XMLName      xml.Name        `xml:"Control"`
	CmdType      string          `xml:"CmdType"`
	SN           int             `xml:"SN"`
	DeviceID     string          `xml:"DeviceID"`
	HomePosition homePositionXML `xml:"HomePosition"`
}

type homePositionXML struct {
	Enabled     int `xml:"Enabled"`
	ResetTime   int `xml:"ResetTime,omitempty"`
	PresetIndex int `xml:"PresetIndex,omitempty"`
}

func BuildPTZPreciseControl(channelID string, sn int, command PTZPreciseControl) ([]byte, error) {
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
	body, err := xml.Marshal(preciseControlXML{
		CmdType: CmdPTZPreciseCtrl, SN: sn, DeviceID: channelID,
		Pan: command.Pan, Tilt: command.Tilt, Zoom: command.Zoom,
		Focus: command.Focus, Iris: command.Iris, Speed: command.Speed,
	})
	if err != nil {
		return nil, err
	}
	return append([]byte("<?xml version=\"1.0\" encoding=\"GB2312\"?>\r\n"), body...), nil
}

func BuildHomePositionQuery(deviceID string, sn int) ([]byte, error) {
	return buildPTZQuery(CmdHomePositionQuery, deviceID, sn, nil)
}

func BuildPresetQuery(deviceID string, sn int) ([]byte, error) {
	return buildPTZQuery(CmdPresetQuery, deviceID, sn, nil)
}

func BuildHomePositionControl(deviceID string, sn int, command HomePositionControl) ([]byte, error) {
	if err := validatePTZQueryTarget(deviceID, sn); err != nil {
		return nil, err
	}
	if command.Enabled {
		if command.ResetTime <= 0 {
			return nil, fmt.Errorf("看守位自动归位时间必须为正数")
		}
		if command.PresetID <= 0 || command.PresetID > 255 {
			return nil, fmt.Errorf("看守位预置位编号必须在 1-255 之间")
		}
	}
	home := homePositionXML{}
	if command.Enabled {
		home.Enabled = 1
		home.ResetTime = command.ResetTime
		home.PresetIndex = command.PresetID
	}
	body, err := xml.Marshal(homePositionControlXML{
		CmdType: CmdDeviceControl, SN: sn, DeviceID: deviceID, HomePosition: home,
	})
	if err != nil {
		return nil, err
	}
	return append([]byte("<?xml version=\"1.0\" encoding=\"GB2312\"?>\r\n"), body...), nil
}

func BuildCruiseTrackListQuery(deviceID string, sn int) ([]byte, error) {
	return buildPTZQuery(CmdCruiseTrackListQuery, deviceID, sn, nil)
}

func BuildCruiseTrackQuery(deviceID string, sn, trackID int) ([]byte, error) {
	if trackID < 0 || trackID > 255 {
		return nil, fmt.Errorf("巡航轨迹编号必须在 0-255 之间")
	}
	return buildPTZQuery(CmdCruiseTrackQuery, deviceID, sn, &trackID)
}

func BuildPTZPreciseStatusQuery(deviceID string, sn int) ([]byte, error) {
	return buildPTZQuery(CmdPTZPreciseStatusQuery, deviceID, sn, nil)
}

func buildPTZQuery(cmd, deviceID string, sn int, number *int) ([]byte, error) {
	if err := validatePTZQueryTarget(deviceID, sn); err != nil {
		return nil, err
	}
	body, err := xml.Marshal(ptzQueryXML{CmdType: cmd, SN: sn, DeviceID: deviceID, Number: number})
	if err != nil {
		return nil, err
	}
	return append([]byte("<?xml version=\"1.0\" encoding=\"GB2312\"?>\r\n"), body...), nil
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

type HomePositionResponse struct {
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Enabled  *bool    `xml:"Enabled"`
	Pan      *float64 `xml:"Pan"`
	Tilt     *float64 `xml:"Tilt"`
	Zoom     *float64 `xml:"Zoom"`
	Focus    *float64 `xml:"Focus"`
	Iris     *float64 `xml:"Iris"`
	Raw      []byte   `xml:"-"`
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

func ParseHomePositionResponse(body []byte) (*HomePositionResponse, error) {
	var response HomePositionResponse
	if err := newDecoder(body).Decode(&response); err != nil {
		return nil, fmt.Errorf("解析看守位响应失败: %w", err)
	}
	if response.CmdType != CmdHomePositionQuery || response.DeviceID == "" || response.SN <= 0 {
		return nil, fmt.Errorf("非法看守位响应")
	}
	response.Raw = append([]byte(nil), body...)
	return &response, nil
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
