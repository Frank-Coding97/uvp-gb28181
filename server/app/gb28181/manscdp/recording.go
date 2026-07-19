package manscdp

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	CmdRecordInfo = "RecordInfo"

	RecordTypeAll    = "all"
	RecordTypeTime   = "time"
	RecordTypeAlarm  = "alarm"
	RecordTypeManual = "manual"
)

const gbTimeLayout = "2006-01-02T15:04:05"

// RecordInfoQuery describes one GB28181 RecordInfo query.
type RecordInfoQuery struct {
	SN         int
	DeviceID   string
	StartTime  time.Time
	EndTime    time.Time
	Type       string
	Secrecy    int
	RecorderID string
}

type recordInfoQueryXML struct {
	XMLName    xml.Name `xml:"Query"`
	CmdType    string   `xml:"CmdType"`
	SN         int      `xml:"SN"`
	DeviceID   string   `xml:"DeviceID"`
	StartTime  string   `xml:"StartTime"`
	EndTime    string   `xml:"EndTime"`
	Secrecy    int      `xml:"Secrecy"`
	Type       string   `xml:"Type"`
	RecorderID string   `xml:"RecorderID,omitempty"`
}

// BuildRecordInfoQuery builds the MESSAGE body for a recording query.
func BuildRecordInfoQuery(query RecordInfoQuery) ([]byte, error) {
	if query.SN <= 0 {
		return nil, fmt.Errorf("SN 必须为正数")
	}
	if strings.TrimSpace(query.DeviceID) == "" {
		return nil, fmt.Errorf("通道编码不能为空")
	}
	if query.StartTime.IsZero() || query.EndTime.IsZero() || !query.EndTime.After(query.StartTime) {
		return nil, fmt.Errorf("录像查询时间范围非法")
	}
	if query.Secrecy < 0 || query.Secrecy > 1 {
		return nil, fmt.Errorf("保密属性必须为 0 或 1")
	}
	recordType := strings.ToLower(strings.TrimSpace(query.Type))
	if recordType == "" {
		recordType = RecordTypeAll
	}
	if !validRecordType(recordType) {
		return nil, fmt.Errorf("不支持的录像类型: %q", query.Type)
	}
	body, err := xml.Marshal(recordInfoQueryXML{
		CmdType: CmdRecordInfo, SN: query.SN, DeviceID: strings.TrimSpace(query.DeviceID),
		StartTime: formatGBTime(query.StartTime), EndTime: formatGBTime(query.EndTime),
		Secrecy: query.Secrecy, Type: recordType, RecorderID: strings.TrimSpace(query.RecorderID),
	})
	if err != nil {
		return nil, err
	}
	return withGB2312Declaration(body), nil
}

func validRecordType(recordType string) bool {
	switch recordType {
	case RecordTypeAll, RecordTypeTime, RecordTypeAlarm, RecordTypeManual:
		return true
	default:
		return false
	}
}

func formatGBTime(value time.Time) string {
	return value.Format(gbTimeLayout)
}

func withGB2312Declaration(body []byte) []byte {
	return append([]byte("<?xml version=\"1.0\" encoding=\"GB2312\"?>\r\n"), body...)
}

// RecordInfoItem is one device-provided recording segment.
type RecordInfoItem struct {
	DeviceID  string
	Name      string
	FilePath  string
	Address   string
	StartTime string
	EndTime   string
	Secrecy   int
	Type      string
}

// RecordInfoResponse is one possibly partial RecordInfo MESSAGE response.
type RecordInfoResponse struct {
	SN       int
	DeviceID string
	Sum      int
	Num      int
	Records  []RecordInfoItem
}

type recordInfoResponseXML struct {
	CmdType    string `xml:"CmdType"`
	SN         int    `xml:"SN"`
	DeviceID   string `xml:"DeviceID"`
	Sum        int    `xml:"SumNum"`
	RecordList struct {
		Num   int              `xml:"Num,attr"`
		Items []RecordInfoItem `xml:"Item"`
	} `xml:"RecordList"`
}

// ParseRecordInfoResponse parses a device RecordInfo response without applying business aggregation.
func ParseRecordInfoResponse(body []byte) (*RecordInfoResponse, error) {
	var payload recordInfoResponseXML
	if err := newDecoder(body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("解析 RecordInfo 响应失败: %w", err)
	}
	if payload.CmdType != CmdRecordInfo {
		return nil, fmt.Errorf("不是 RecordInfo 响应: %q", payload.CmdType)
	}
	if payload.SN <= 0 || strings.TrimSpace(payload.DeviceID) == "" {
		return nil, fmt.Errorf("RecordInfo 响应缺少 SN 或 DeviceID")
	}
	num := payload.RecordList.Num
	if num == 0 && len(payload.RecordList.Items) > 0 {
		num = len(payload.RecordList.Items)
	}
	return &RecordInfoResponse{
		SN: payload.SN, DeviceID: payload.DeviceID, Sum: payload.Sum,
		Num: num, Records: payload.RecordList.Items,
	}, nil
}

type PlaybackAction string

const (
	PlaybackActionPlay        PlaybackAction = "Play"
	PlaybackActionPause       PlaybackAction = "Pause"
	PlaybackActionFastForward PlaybackAction = "FastForward"
	PlaybackActionSlowForward PlaybackAction = "SlowForward"
)

// PlaybackCommand represents a GB28181 playback control INFO payload.
type PlaybackCommand struct {
	Action     PlaybackAction
	Scale      float64
	RangeStart time.Time
	RangeEnd   time.Time
}

type playbackControlXML struct {
	XMLName     xml.Name `xml:"Control"`
	CmdType     string   `xml:"CmdType"`
	SN          int      `xml:"SN"`
	DeviceID    string   `xml:"DeviceID"`
	PlaybackCmd string   `xml:"PlaybackCmd"`
	Range       string   `xml:"Range,omitempty"`
	Scale       string   `xml:"Scale,omitempty"`
}

// BuildPlaybackControl builds the SIP INFO MANSCDP body for a playback command.
func BuildPlaybackControl(deviceID string, sn int, command PlaybackCommand) ([]byte, error) {
	if strings.TrimSpace(deviceID) == "" {
		return nil, fmt.Errorf("通道编码不能为空")
	}
	if sn <= 0 {
		return nil, fmt.Errorf("SN 必须为正数")
	}
	if !validPlaybackAction(command.Action) {
		return nil, fmt.Errorf("不支持的回放动作: %q", command.Action)
	}
	if command.Scale != 0 && !validPlaybackScale(command.Scale) {
		return nil, fmt.Errorf("不支持的回放速度: %g", command.Scale)
	}
	rangeValue, err := playbackRange(command.RangeStart, command.RangeEnd)
	if err != nil {
		return nil, err
	}
	scale := ""
	if command.Scale != 0 {
		scale = strconv.FormatFloat(command.Scale, 'f', -1, 64)
	}
	body, err := xml.Marshal(playbackControlXML{
		CmdType: CmdDeviceControl, SN: sn, DeviceID: strings.TrimSpace(deviceID),
		PlaybackCmd: string(command.Action), Range: rangeValue, Scale: scale,
	})
	if err != nil {
		return nil, err
	}
	return withGB2312Declaration(body), nil
}

func validPlaybackAction(action PlaybackAction) bool {
	switch action {
	case PlaybackActionPlay, PlaybackActionPause, PlaybackActionFastForward, PlaybackActionSlowForward:
		return true
	default:
		return false
	}
}

func validPlaybackScale(scale float64) bool {
	return scale == 0.5 || scale == 1 || scale == 2 || scale == 4
}

func playbackRange(start, end time.Time) (string, error) {
	if start.IsZero() && end.IsZero() {
		return "", nil
	}
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return "", fmt.Errorf("回放跳转时间范围非法")
	}
	return formatGBTime(start) + "/" + formatGBTime(end), nil
}
