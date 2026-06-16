package manscdp

import "encoding/xml"

// MANSCDP 消息 CmdType 常量
const (
	CmdKeepalive = "Keepalive"
	CmdCatalog   = "Catalog"
	CmdDeviceInfo = "DeviceInfo"
	CmdAlarm     = "Alarm"
)

// Notify MANSCDP 通知/查询消息的通用结构(本期只关心 Keepalive)
// 国标 MESSAGE body 是 XML,根元素可能是 Notify / Response / Query
type Notify struct {
	XMLName  xml.Name `xml:"Notify"`
	CmdType  string   `xml:"CmdType"`
	SN       string   `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Status   string   `xml:"Status"`
}

// MessageHead 仅解析根元素的 CmdType,用于分发(不关心根标签名)
type MessageHead struct {
	CmdType  string `xml:"CmdType"`
	SN       string `xml:"SN"`
	DeviceID string `xml:"DeviceID"`
}

// ParseHead 解析消息头,提取 CmdType/SN/DeviceID
// 兼容 Notify/Response/Query 等不同根标签(只取公共字段)
func ParseHead(body []byte) (*MessageHead, error) {
	var h MessageHead
	// GB18030/GBK 编码兼容:国标设备常用 GB2312/GB18030,XML 声明可能标注
	decoder := newDecoder(body)
	if err := decoder.Decode(&h); err != nil {
		return nil, err
	}
	return &h, nil
}

// IsKeepalive 判断是否心跳消息
func (h *MessageHead) IsKeepalive() bool {
	return h.CmdType == CmdKeepalive
}
