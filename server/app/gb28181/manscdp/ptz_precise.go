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
	TrackID  int      `xml:"TrackID,omitempty"`
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
	return buildPTZQuery(CmdHomePositionQuery, deviceID, sn, 0)
}

func BuildCruiseTrackListQuery(deviceID string, sn int) ([]byte, error) {
	return buildPTZQuery(CmdCruiseTrackListQuery, deviceID, sn, 0)
}

func BuildCruiseTrackQuery(deviceID string, sn, trackID int) ([]byte, error) {
	if trackID <= 0 {
		return nil, fmt.Errorf("巡航轨迹编号必须为正数")
	}
	return buildPTZQuery(CmdCruiseTrackQuery, deviceID, sn, trackID)
}

func BuildPTZPreciseStatusQuery(deviceID string, sn int) ([]byte, error) {
	return buildPTZQuery(CmdPTZPreciseStatusQuery, deviceID, sn, 0)
}

func buildPTZQuery(cmd, deviceID string, sn, trackID int) ([]byte, error) {
	if err := validatePTZQueryTarget(deviceID, sn); err != nil {
		return nil, err
	}
	body, err := xml.Marshal(ptzQueryXML{CmdType: cmd, SN: sn, DeviceID: deviceID, TrackID: trackID})
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
	ID      int    `xml:"TrackID" json:"trackId"`
	Name    string `xml:"Name" json:"name"`
	Enabled *bool  `xml:"Enabled" json:"enabled"`
}

type CruiseTrackListResponse struct {
	CmdType  string        `xml:"CmdType"`
	SN       int           `xml:"SN"`
	DeviceID string        `xml:"DeviceID"`
	Tracks   []CruiseTrack `xml:"TrackList>Track"`
	Raw      []byte        `xml:"-"`
}

func ParseCruiseTrackListResponse(body []byte) (*CruiseTrackListResponse, error) {
	var response CruiseTrackListResponse
	if err := newDecoder(body).Decode(&response); err != nil {
		return nil, fmt.Errorf("解析巡航轨迹列表失败: %w", err)
	}
	if response.CmdType != CmdCruiseTrackListQuery || response.DeviceID == "" || response.SN <= 0 {
		return nil, fmt.Errorf("非法巡航轨迹列表响应")
	}
	response.Raw = append([]byte(nil), body...)
	return &response, nil
}

type CruiseTrackResponse struct {
	CmdType  string      `xml:"CmdType"`
	SN       int         `xml:"SN"`
	DeviceID string      `xml:"DeviceID"`
	Track    CruiseTrack `xml:"Track"`
	Raw      []byte      `xml:"-"`
}

func ParseCruiseTrackResponse(body []byte) (*CruiseTrackResponse, error) {
	var response CruiseTrackResponse
	if err := newDecoder(body).Decode(&response); err != nil {
		return nil, fmt.Errorf("解析巡航轨迹响应失败: %w", err)
	}
	if response.CmdType != CmdCruiseTrackQuery || response.DeviceID == "" || response.SN <= 0 {
		return nil, fmt.Errorf("非法巡航轨迹响应")
	}
	response.Raw = append([]byte(nil), body...)
	return &response, nil
}
