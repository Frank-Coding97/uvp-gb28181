package manscdp

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

const (
	CmdNotify      = "Notify"
	SubCmdSnapshot = "SnapShot"
)

type SnapshotConfig struct {
	SessionID string `xml:"SessionID" json:"sessionId"`
	UploadURL string `xml:"UploadURL" json:"uploadUrl"`
	SnapNum   int    `xml:"SnapNum" json:"snapNum"`
	Interval  int    `xml:"Interval" json:"interval"`
}

type snapshotControl struct {
	XMLName        xml.Name       `xml:"Control"`
	CmdType        string         `xml:"CmdType"`
	SN             int            `xml:"SN"`
	DeviceID       string         `xml:"DeviceID"`
	SnapshotConfig SnapshotConfig `xml:"SnapShotConfig"`
}

type SnapshotNotify struct {
	XMLName     xml.Name `xml:"Notify"`
	CmdType     string   `xml:"CmdType"`
	SubCmd      string   `xml:"SubCmd"`
	SN          string   `xml:"SN"`
	DeviceID    string   `xml:"DeviceID"`
	SessionID   string   `xml:"SessionID"`
	SnapshotID  string   `xml:"SnapShotID"`
	Time        string   `xml:"Time"`
	StoragePath string   `xml:"StoragePath"`
}

func BuildSnapshotConfigWithProfile(profile protocol.Profile, deviceID string, sn int, config SnapshotConfig) ([]byte, error) {
	if profile.Version != protocol.Version2022 {
		return nil, fmt.Errorf("SnapShotConfig 仅支持 GB/T 28181-2022")
	}
	config.SessionID = strings.TrimSpace(config.SessionID)
	config.UploadURL = strings.TrimSpace(config.UploadURL)
	deviceID = strings.TrimSpace(deviceID)
	parsedURL, err := url.Parse(config.UploadURL)
	if deviceID == "" || config.SessionID == "" || err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return nil, fmt.Errorf("SnapShotConfig 目标或上传地址不合法")
	}
	if config.SnapNum < 1 || config.SnapNum > 10 || config.Interval < 1 || config.Interval > 3600 {
		return nil, fmt.Errorf("SnapShotConfig 抓拍数量或间隔不合法")
	}
	return MarshalProfiledXML(profile, snapshotControl{
		CmdType: CmdDeviceControl, SN: sn, DeviceID: deviceID, SnapshotConfig: config,
	})
}

func ParseSnapshotNotify(body []byte) (SnapshotNotify, error) {
	var notify SnapshotNotify
	if err := DecodeProfiledXML(protocol.ProfileFor(protocol.Version2022), body, &notify); err != nil {
		return notify, err
	}
	notify.CmdType = strings.TrimSpace(notify.CmdType)
	notify.SubCmd = strings.TrimSpace(notify.SubCmd)
	notify.DeviceID = strings.TrimSpace(notify.DeviceID)
	notify.SessionID = strings.TrimSpace(notify.SessionID)
	notify.SnapshotID = strings.TrimSpace(notify.SnapshotID)
	if notify.CmdType != CmdNotify || notify.SubCmd != SubCmdSnapshot || notify.SessionID == "" || notify.SnapshotID == "" {
		return notify, fmt.Errorf("SnapShot Notify 内容不合法")
	}
	return notify, nil
}

func IsSnapshotNotify(body []byte) bool {
	notify, err := ParseSnapshotNotify(body)
	return err == nil && notify.CmdType == CmdNotify && notify.SubCmd == SubCmdSnapshot
}
