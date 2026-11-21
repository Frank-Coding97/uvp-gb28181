package manscdp

import (
	"encoding/xml"
	"fmt"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

const CmdMediaStatus = "MediaStatus"

// MediaStatusNotify represents the historical-media completion event in
// GB/T 28181-2022 A.2.5.4 (also supported by the 2016 profile).
type MediaStatusNotify struct {
	XMLName    xml.Name `xml:"Notify"`
	CmdType    string   `xml:"CmdType"`
	SN         int      `xml:"SN"`
	DeviceID   string   `xml:"DeviceID"`
	NotifyType string   `xml:"NotifyType"`
}

func ParseMediaStatusNotify(body []byte) (MediaStatusNotify, error) {
	var notify MediaStatusNotify
	if err := newDecoder(body).Decode(&notify); err != nil {
		return MediaStatusNotify{}, fmt.Errorf("解析媒体通知失败: %w", err)
	}
	if notify.XMLName.Local != "Notify" || notify.CmdType != CmdMediaStatus || notify.SN <= 0 || strings.TrimSpace(notify.DeviceID) == "" || strings.TrimSpace(notify.NotifyType) == "" {
		return MediaStatusNotify{}, fmt.Errorf("非法媒体通知")
	}
	return notify, nil
}

func BuildMediaStatusFileEnd(profile protocol.Profile, deviceID string, sn int) ([]byte, error) {
	if strings.TrimSpace(deviceID) == "" || sn <= 0 {
		return nil, fmt.Errorf("非法媒体通知目标或 SN")
	}
	return MarshalProfiledXML(profile, MediaStatusNotify{CmdType: CmdMediaStatus, SN: sn, DeviceID: deviceID, NotifyType: "121"})
}
