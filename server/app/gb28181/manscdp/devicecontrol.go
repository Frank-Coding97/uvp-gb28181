package manscdp

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// PTZAction is the small, stable action vocabulary exposed by the HTTP API.
type PTZAction string

const (
	PTZActionStop      PTZAction = "stop"
	PTZActionLeft      PTZAction = "left"
	PTZActionRight     PTZAction = "right"
	PTZActionUp        PTZAction = "up"
	PTZActionDown      PTZAction = "down"
	PTZActionLeftUp    PTZAction = "left_up"
	PTZActionRightUp   PTZAction = "right_up"
	PTZActionLeftDown  PTZAction = "left_down"
	PTZActionRightDown PTZAction = "right_down"
	PTZActionZoomIn    PTZAction = "zoom_in"
	PTZActionZoomOut   PTZAction = "zoom_out"
)

// PTZCommand is encoded as the GB28181 front-end PTZ command string.
type PTZCommand struct {
	Action PTZAction
	Speed  int
}

type deviceControl struct {
	XMLName  xml.Name    `xml:"Control"`
	CmdType  string      `xml:"CmdType"`
	SN       int         `xml:"SN"`
	DeviceID string      `xml:"DeviceID"`
	PTZCmd   string      `xml:"PTZCmd"`
	Info     controlInfo `xml:"Info"`
}

type controlInfo struct {
	ControlPriority int `xml:"ControlPriority"`
}

// ParsePTZAction accepts canonical API names plus common UI spellings.
func ParsePTZAction(value string) (PTZAction, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	switch normalized {
	case "stop", "停止":
		return PTZActionStop, nil
	case "left", "左":
		return PTZActionLeft, nil
	case "right", "右":
		return PTZActionRight, nil
	case "up", "上":
		return PTZActionUp, nil
	case "down", "下":
		return PTZActionDown, nil
	case "left_up", "左上":
		return PTZActionLeftUp, nil
	case "right_up", "右上":
		return PTZActionRightUp, nil
	case "left_down", "左下":
		return PTZActionLeftDown, nil
	case "right_down", "右下":
		return PTZActionRightDown, nil
	case "zoom_in", "in", "放大":
		return PTZActionZoomIn, nil
	case "zoom_out", "out", "缩小":
		return PTZActionZoomOut, nil
	default:
		return "", fmt.Errorf("不支持的 PTZ 动作: %q", value)
	}
}

// BuildPTZControl builds a GB28181 DeviceControl MESSAGE body.
// The PTZ command is the standard 8-byte A5 front-end command with a modulo-256 checksum.
func BuildPTZControl(channelID string, sn int, command PTZCommand) ([]byte, error) {
	if strings.TrimSpace(channelID) == "" {
		return nil, fmt.Errorf("通道编码不能为空")
	}
	if sn <= 0 {
		return nil, fmt.Errorf("SN 必须为正数")
	}
	if command.Speed < 1 || command.Speed > 255 {
		return nil, fmt.Errorf("PTZ 速度必须在 1-255 之间")
	}
	ptz, err := encodePTZ(command)
	if err != nil {
		return nil, err
	}
	body, err := xml.Marshal(deviceControl{
		CmdType:  CmdDeviceControl,
		SN:       sn,
		DeviceID: channelID,
		PTZCmd:   ptz,
		Info:     controlInfo{ControlPriority: 5},
	})
	if err != nil {
		return nil, err
	}
	return append([]byte("<?xml version=\"1.0\" encoding=\"GB2312\"?>\r\n"), body...), nil
}

func encodePTZ(command PTZCommand) (string, error) {
	var instruction byte
	switch command.Action {
	case PTZActionStop:
	case PTZActionLeft:
		instruction = 0x02
	case PTZActionRight:
		instruction = 0x01
	case PTZActionUp:
		instruction = 0x08
	case PTZActionDown:
		instruction = 0x04
	case PTZActionLeftUp:
		instruction = 0x0A
	case PTZActionRightUp:
		instruction = 0x09
	case PTZActionLeftDown:
		instruction = 0x06
	case PTZActionRightDown:
		instruction = 0x05
	case PTZActionZoomIn:
		instruction = 0x10
	case PTZActionZoomOut:
		instruction = 0x20
	default:
		return "", fmt.Errorf("不支持的 PTZ 动作: %q", command.Action)
	}

	moveSpeed := byte(command.Speed)
	zoomSpeed := byte(0)
	if command.Action == PTZActionZoomIn || command.Action == PTZActionZoomOut {
		// The zoom nibble occupies the high half-byte. Values below 0x10
		// are accepted by the API but normalized to the protocol minimum.
		zoom := command.Speed
		if zoom < 0x10 {
			zoom = 0x10
		}
		zoomSpeed = byte(zoom & 0xF0)
		moveSpeed = 0
	}

	bytes := [8]byte{0xA5, 0x0F, 0x01, instruction, moveSpeed, moveSpeed, zoomSpeed, 0}
	for i := 0; i < len(bytes)-1; i++ {
		bytes[7] += bytes[i]
	}
	return fmt.Sprintf("%02X%02X%02X%02X%02X%02X%02X%02X", bytes[0], bytes[1], bytes[2], bytes[3], bytes[4], bytes[5], bytes[6], bytes[7]), nil
}
