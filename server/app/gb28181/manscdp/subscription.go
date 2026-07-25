package manscdp

import (
	"encoding/xml"
	"fmt"
	"strings"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

type subscriptionQuery struct {
	XMLName  xml.Name `xml:"Query"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Interval *int     `xml:"Interval,omitempty"`
}

// BuildSubscriptionQuery returns the MANSCDP body and SIP Event header for one device-level subscription.
func BuildSubscriptionQuery(kind gbmodels.SubscriptionKind, deviceID string, sn, interval int) ([]byte, string, error) {
	return BuildSubscriptionQueryWithProfile(protocol.ProfileFor(protocol.Version2016), kind, deviceID, sn, interval)
}

// BuildSubscriptionQueryWithProfile builds a subscription query using the
// profile's actual XML charset and declaration.
func BuildSubscriptionQueryWithProfile(profile protocol.Profile, kind gbmodels.SubscriptionKind, deviceID string, sn, interval int) ([]byte, string, error) {
	if !kind.Valid() {
		return nil, "", fmt.Errorf("不支持的订阅类型: %s", kind)
	}
	if strings.TrimSpace(deviceID) == "" {
		return nil, "", fmt.Errorf("设备编码不能为空")
	}
	q := subscriptionQuery{SN: sn, DeviceID: deviceID}
	var event string
	switch kind {
	case gbmodels.SubscriptionKindCatalog:
		q.CmdType, event = CmdCatalog, "Catalog"
	case gbmodels.SubscriptionKindMobilePosition:
		q.CmdType, event = CmdMobilePosition, "presence"
		if interval <= 0 {
			interval = 30
		}
		q.Interval = &interval
	case gbmodels.SubscriptionKindAlarm:
		q.CmdType, event = CmdAlarm, "presence"
	}
	body, err := MarshalProfiledXML(profile, q)
	if err != nil {
		return nil, "", err
	}
	return body, event, nil
}

// ResolveSubscriptionKind accepts standard and common vendor Event-header variants.
func ResolveSubscriptionKind(event string, body []byte) (gbmodels.SubscriptionKind, error) {
	head, err := ParseHead(body)
	if err != nil {
		return "", err
	}
	normalized := strings.ToLower(strings.TrimSpace(strings.Split(event, ";")[0]))
	switch head.CmdType {
	case CmdCatalog:
		if normalized == "catalog" || normalized == "presence" {
			return gbmodels.SubscriptionKindCatalog, nil
		}
	case CmdMobilePosition:
		if normalized == "presence" {
			return gbmodels.SubscriptionKindMobilePosition, nil
		}
	case CmdAlarm:
		if normalized == "presence" || normalized == "alarm" {
			return gbmodels.SubscriptionKindAlarm, nil
		}
	}
	return "", fmt.Errorf("不支持的订阅通知 Event=%q CmdType=%q", event, head.CmdType)
}

type CatalogNotify struct {
	CmdType    string `xml:"CmdType"`
	SN         int    `xml:"SN"`
	DeviceID   string `xml:"DeviceID"`
	DeviceList struct {
		Num   int           `xml:"Num,attr"`
		Items []CatalogItem `xml:"Item"`
	} `xml:"DeviceList"`
}

func ParseCatalogNotify(body []byte) (*CatalogNotify, error) {
	var notify CatalogNotify
	if err := newDecoder(body).Decode(&notify); err != nil {
		return nil, fmt.Errorf("解析 Catalog NOTIFY 失败: %w", err)
	}
	if notify.CmdType != CmdCatalog || notify.DeviceID == "" {
		return nil, fmt.Errorf("非法 Catalog NOTIFY")
	}
	return &notify, nil
}

type MobilePositionNotify struct {
	CmdType   string  `xml:"CmdType"`
	SN        string  `xml:"SN"`
	DeviceID  string  `xml:"DeviceID"`
	Time      string  `xml:"Time"`
	Longitude float64 `xml:"Longitude"`
	Latitude  float64 `xml:"Latitude"`
	Speed     float64 `xml:"Speed"`
	Direction float64 `xml:"Direction"`
	Altitude  float64 `xml:"Altitude"`
}

func ParseMobilePositionNotify(body []byte) (*MobilePositionNotify, error) {
	var notify MobilePositionNotify
	if err := newDecoder(body).Decode(&notify); err != nil {
		return nil, fmt.Errorf("解析 MobilePosition NOTIFY 失败: %w", err)
	}
	if notify.CmdType != CmdMobilePosition || notify.DeviceID == "" {
		return nil, fmt.Errorf("非法 MobilePosition NOTIFY")
	}
	if notify.Longitude < -180 || notify.Longitude > 180 || notify.Latitude < -90 || notify.Latitude > 90 {
		return nil, fmt.Errorf("经纬度超出范围")
	}
	return &notify, nil
}

type AlarmNotify struct {
	CmdType        string  `xml:"CmdType"`
	SN             string  `xml:"SN"`
	DeviceID       string  `xml:"DeviceID"`
	Priority       int     `xml:"AlarmPriority"`
	Method         int     `xml:"AlarmMethod"`
	AlarmType      int     `xml:"AlarmType"`
	AlarmTypeParam string  `xml:"AlarmTypeParam"`
	AlarmTime      string  `xml:"AlarmTime"`
	Description    string  `xml:"AlarmDescription"`
	Longitude      float64 `xml:"Longitude"`
	Latitude       float64 `xml:"Latitude"`
}

func ParseAlarmNotify(body []byte) (*AlarmNotify, error) {
	var notify AlarmNotify
	if err := newDecoder(body).Decode(&notify); err != nil {
		return nil, fmt.Errorf("解析 Alarm NOTIFY 失败: %w", err)
	}
	if notify.CmdType != CmdAlarm || notify.DeviceID == "" {
		return nil, fmt.Errorf("非法 Alarm NOTIFY")
	}
	return &notify, nil
}
