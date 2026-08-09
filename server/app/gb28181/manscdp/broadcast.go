package manscdp

import (
	"encoding/xml"
	"fmt"
	"strings"
)

type BroadcastNotify struct {
	SN       int
	SourceID string
	TargetID string
}

type broadcastNotifyWire struct {
	XMLName  xml.Name `xml:"Notify"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	SourceID string   `xml:"SourceID"`
	TargetID string   `xml:"TargetID"`
}

type BroadcastResponse struct {
	CmdType  string
	SN       int
	DeviceID string
	TargetID string
	Result   string
	Info     string
}

type broadcastResponseWire struct {
	XMLName  xml.Name `xml:"Response"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	TargetID string   `xml:"TargetID"`
	Result   string   `xml:"Result"`
	Info     string   `xml:"Info"`
}

func BuildBroadcastNotify(in BroadcastNotify) ([]byte, error) {
	in.SourceID = strings.TrimSpace(in.SourceID)
	in.TargetID = strings.TrimSpace(in.TargetID)
	if in.SN <= 0 || in.SourceID == "" || in.TargetID == "" {
		return nil, fmt.Errorf("Broadcast Notify 缺少 SN、SourceID 或 TargetID")
	}
	body, err := xml.Marshal(broadcastNotifyWire{CmdType: CmdBroadcast, SN: in.SN, SourceID: in.SourceID, TargetID: in.TargetID})
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}

func ParseBroadcastResponse(body []byte) (BroadcastResponse, error) {
	var wire broadcastResponseWire
	if err := newDecoder(body).Decode(&wire); err != nil {
		return BroadcastResponse{}, fmt.Errorf("解析 Broadcast Response 失败: %w", err)
	}
	if wire.XMLName.Local != "Response" || strings.TrimSpace(wire.CmdType) != CmdBroadcast {
		return BroadcastResponse{}, fmt.Errorf("不是 Broadcast Response")
	}
	wire.DeviceID = strings.TrimSpace(wire.DeviceID)
	wire.TargetID = strings.TrimSpace(wire.TargetID)
	if wire.SN <= 0 || wire.DeviceID == "" {
		return BroadcastResponse{}, fmt.Errorf("Broadcast Response 缺少 SN 或 DeviceID")
	}
	return BroadcastResponse{
		CmdType: CmdBroadcast, SN: wire.SN, DeviceID: wire.DeviceID, TargetID: wire.TargetID,
		Result: strings.TrimSpace(wire.Result), Info: strings.TrimSpace(wire.Info),
	}, nil
}

func (r BroadcastResponse) Success() bool {
	result := strings.ToUpper(strings.TrimSpace(r.Result))
	return result == "OK" || result == "SUCCESS"
}
