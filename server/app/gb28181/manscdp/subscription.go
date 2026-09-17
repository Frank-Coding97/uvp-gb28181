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
	case gbmodels.SubscriptionKindPTZPrecisePosition:
		q.CmdType, event = CmdPTZPosition, CmdPTZPosition
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
	case CmdPTZPrecisePosition, CmdPTZPosition:
		if strings.Contains(normalized, "ptzprecise") || strings.Contains(normalized, "ptzposition") {
			return gbmodels.SubscriptionKindPTZPrecisePosition, nil
		}
	}
	return "", fmt.Errorf("不支持的订阅通知 Event=%q CmdType=%q", event, head.CmdType)
}

type CatalogNotify struct {
	CmdType    string `xml:"CmdType"`
	SN         int    `xml:"SN"`
	DeviceID   string `xml:"DeviceID"`
	SumNum     int    `xml:"SumNum"`
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

// MobilePositionItem 是 GB/T 28181-2022 A.2.1.14 itemMobilePositionType —— 列表形态里的单条位置。
//
// 字段序与标准 sequence 一致（DeviceID / CaptureTime / Longitude / Latitude /
// Speed? / Direction? / Altitude? / Height?），方便与附录 A 逐行对照。
//
// ⚠️ Speed / Direction / Altitude / Height 在标准里是 minOccurs=0 可选字段，这里用裸 float64
// 承载 —— 因此「未上报」与「上报了 0」在解析结果里不可区分，两者都落成 0。落地列本来就允许 0，
// 故不引入指针（指针化会让 2016 扁平形态的根字段与 item 字段类型不一致）。
type MobilePositionItem struct {
	DeviceID    string  `xml:"DeviceID"`
	CaptureTime string  `xml:"CaptureTime"`
	Longitude   float64 `xml:"Longitude"`
	Latitude    float64 `xml:"Latitude"`
	Speed       float64 `xml:"Speed"`
	Direction   float64 `xml:"Direction"`
	Altitude    float64 `xml:"Altitude"`
	// Height 是 2022 新增的「地面高度」，区别于 Altitude（海拔高度）。
	// ⚠️ 当前只解析、不落库：gb_mobile_position_latest / history 没有对应列。
	// 解析在此是为了让它「可见」（不静默丢弃）+ 可被单测锁住；持久化需另开迁移单。
	Height float64 `xml:"Height"`
}

// MobilePositionDeviceList 是 A.2.5.6 的 DeviceList（带 Num 属性的 Item 列表）。
type MobilePositionDeviceList struct {
	Num   int                  `xml:"Num,attr"`
	Items []MobilePositionItem `xml:"Item"`
}

// MobilePositionNotify 同时承载 MobilePosition NOTIFY 的**两种形态**，两者都必须认。
//
//	GB/T 28181-2016（扁平）：
//	  Time / Longitude / Latitude / Speed / Direction / Altitude 直挂 <Notify>；
//	  <Time> 即**位置采集时间**；一次只报一台设备。
//	GB/T 28181-2022 A.2.5.6（列表）：
//	  <Time> 语义变为**上报通知时间**，采集时间下沉到 Item/CaptureTime；
//	  位置走 SumNum + DeviceList@Num + Item(itemMobilePositionType)。
//
// 这**不是「新写法 vs 老写法」，而是两次结构不同的报文**，且现网 2016 设备仍在网，
// 所以两种都要认、都要能落地。跨版本字段合并在解析层完成，调用方只看
// [MobilePositionNotify.Positions] 的归一化结果 —— 与 [ParseAlarmNotify] 的
// 「根字段 + 嵌套 Info 双读」是同一思路。
type MobilePositionNotify struct {
	CmdType  string `xml:"CmdType"`
	SN       string `xml:"SN"`
	DeviceID string `xml:"DeviceID"`
	// Time 是两形态共有的元素，但**语义不同**：2016 = 采集时间；2022 = 上报通知时间。
	Time string `xml:"Time"`

	// —— 2016 扁平形态 ——
	Longitude float64 `xml:"Longitude"`
	Latitude  float64 `xml:"Latitude"`
	Speed     float64 `xml:"Speed"`
	Direction float64 `xml:"Direction"`
	Altitude  float64 `xml:"Altitude"`

	// —— 2022 列表形态 ——
	//
	// SumNum / DeviceList 用指针而非值类型：**「元素不存在」与「元素值为 0 / 空」必须能区分**，
	// 否则 2022 的 SumNum=0 空列表会被误判成 2016 扁平形态，进而从根上的 0 值坐标
	// 合成出一条 (0,0) 的**假位置**。
	SumNum     *int                      `xml:"SumNum"`
	DeviceList *MobilePositionDeviceList `xml:"DeviceList"`
}

// Positions 把两种形态归一化成同一种 item 列表，供处理器逐条落地。
//
//   - 2022 列表形态（SumNum 或 DeviceList 在场）：逐条返回；Item 缺 CaptureTime 时用根 Time 兜底。
//   - 2016 扁平形态：把根上那一组坐标合成一条，DeviceID / CaptureTime 取根值。
//
// ⛔ 一旦判定为列表形态就在列表语义里走到底：**DeviceList 在场但 Item 为空时返回空切片**，
// 而不是回落到扁平字段。A.2.5.6 允许 SumNum=0（本次无位置上报），那是合法 no-op；
// 回落则会拿根上的 0 值坐标造出一条 (0,0) 假位置，前端地图上会出现一个漂到几内亚湾的设备。
func (n *MobilePositionNotify) Positions() []MobilePositionItem {
	if n.SumNum != nil || n.DeviceList != nil {
		items := n.DeviceList
		if items == nil {
			return nil
		}
		out := make([]MobilePositionItem, 0, len(items.Items))
		for _, item := range items.Items {
			if strings.TrimSpace(item.CaptureTime) == "" {
				item.CaptureTime = n.Time
			}
			out = append(out, item)
		}
		return out
	}
	return []MobilePositionItem{{
		DeviceID:    n.DeviceID,
		CaptureTime: n.Time,
		Longitude:   n.Longitude,
		Latitude:    n.Latitude,
		Speed:       n.Speed,
		Direction:   n.Direction,
		Altitude:    n.Altitude,
	}}
}

func ParseMobilePositionNotify(body []byte) (*MobilePositionNotify, error) {
	var notify MobilePositionNotify
	if err := newDecoder(body).Decode(&notify); err != nil {
		return nil, fmt.Errorf("解析 MobilePosition NOTIFY 失败: %w", err)
	}
	if notify.CmdType != CmdMobilePosition || notify.DeviceID == "" {
		return nil, fmt.Errorf("非法 MobilePosition NOTIFY")
	}
	// 两形态的坐标都在归一化后统一做范围校验。2022 列表里任一项越界即整包非法 ——
	// 与 Catalog / Alarm 的「先整体校验再落地」一致，不给下游留半截脏数据。
	for _, item := range notify.Positions() {
		if item.Longitude < -180 || item.Longitude > 180 || item.Latitude < -90 || item.Latitude > 90 {
			return nil, fmt.Errorf("经纬度超出范围")
		}
	}
	return &notify, nil
}

type AlarmNotify struct {
	CmdType        string  `xml:"CmdType"`
	SN             string  `xml:"SN"`
	DeviceID       string  `xml:"DeviceID"`
	Priority       int     `xml:"AlarmPriority"`
	Method         int     `xml:"AlarmMethod"`
	AlarmType      *int    `xml:"AlarmType"`
	AlarmTypeParam string  `xml:"AlarmTypeParam"`
	AlarmTime      string  `xml:"AlarmTime"`
	Description    string  `xml:"AlarmDescription"`
	Longitude      float64 `xml:"Longitude"`
	Latitude       float64 `xml:"Latitude"`
	Info           struct {
		AlarmType      *int `xml:"AlarmType"`
		AlarmTypeParam struct {
			EventType *int `xml:"EventType"`
		} `xml:"AlarmTypeParam"`
	} `xml:"Info"`
}

func ParseAlarmNotify(body []byte) (*AlarmNotify, error) {
	var notify AlarmNotify
	if err := newDecoder(body).Decode(&notify); err != nil {
		return nil, fmt.Errorf("解析 Alarm NOTIFY 失败: %w", err)
	}
	if notify.CmdType != CmdAlarm || notify.DeviceID == "" {
		return nil, fmt.Errorf("非法 Alarm NOTIFY")
	}
	// GB/T 28181-2022 A.2.5.3 puts AlarmType and AlarmTypeParam inside Info.
	// Keep the root fields for compatibility with legacy vendor notifications.
	if notify.Info.AlarmType != nil {
		notify.AlarmType = notify.Info.AlarmType
	}
	if notify.Info.AlarmTypeParam.EventType != nil {
		notify.AlarmTypeParam = fmt.Sprintf("EventType=%d", *notify.Info.AlarmTypeParam.EventType)
	}
	return &notify, nil
}
