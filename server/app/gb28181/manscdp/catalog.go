package manscdp

import (
	"encoding/xml"
	"fmt"
)

// CatalogQuery Catalog 目录查询请求(平台→设备)
// GB/T 28181 MANSCDP: <Query><CmdType>Catalog</CmdType>...</Query>
type CatalogQuery struct {
	XMLName  xml.Name `xml:"Query"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
}

// BuildCatalogQuery 构造 Catalog 查询 XML(国标格式,GB2312 声明)
// deviceID = 目标设备国标编码;sn = 查询序列号
func BuildCatalogQuery(deviceID string, sn int) ([]byte, error) {
	q := CatalogQuery{CmdType: CmdCatalog, SN: sn, DeviceID: deviceID}
	body, err := xml.Marshal(q)
	if err != nil {
		return nil, err
	}
	// 国标要求 XML 声明,设备多用 GB2312
	return append([]byte(`<?xml version="1.0" encoding="GB2312"?>`+"\n"), body...), nil
}

// CatalogItem Catalog 应答里的单个通道项
type CatalogItem struct {
	DeviceID     string  `xml:"DeviceID"`
	Name         string  `xml:"Name"`
	Manufacturer string  `xml:"Manufacturer"`
	Model        string  `xml:"Model"`
	Owner        string  `xml:"Owner"`
	CivilCode    string  `xml:"CivilCode"`
	ParentID     string  `xml:"ParentID"`
	PTZType      int     `xml:"PTZType"`
	Longitude    float64 `xml:"Longitude"`
	Latitude     float64 `xml:"Latitude"`
	Status       string  `xml:"Status"` // ON/OFF
}

// CatalogResponse Catalog 应答(设备→平台,可能多条分包)
type CatalogResponse struct {
	XMLName  xml.Name `xml:"Response"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	SumNum   int      `xml:"SumNum"` // 通道总数(用于分包聚合判断)
	DeviceList struct {
		Num   int           `xml:"Num,attr"`
		Items []CatalogItem `xml:"Item"`
	} `xml:"DeviceList"`
}

// ParseCatalogResponse 解析一条 Catalog 应答
func ParseCatalogResponse(body []byte) (*CatalogResponse, error) {
	var r CatalogResponse
	if err := newDecoder(body).Decode(&r); err != nil {
		return nil, fmt.Errorf("解析 Catalog 应答失败: %w", err)
	}
	return &r, nil
}

// IsOnline 通道状态是否在线
func (it *CatalogItem) IsOnline() bool {
	return it.Status == "ON" || it.Status == "On" || it.Status == "ONLINE"
}
