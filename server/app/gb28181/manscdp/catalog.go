package manscdp

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
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
	return BuildCatalogQueryWithProfile(protocol.ProfileFor(protocol.Version2016), deviceID, sn)
}

// BuildCatalogQueryWithProfile builds a Catalog query using the profile's
// actual XML charset and declaration.
func BuildCatalogQueryWithProfile(profile protocol.Profile, deviceID string, sn int) ([]byte, error) {
	q := CatalogQuery{CmdType: CmdCatalog, SN: sn, DeviceID: deviceID}
	return MarshalProfiledXML(profile, q)
}

// CatalogInfo 是 Catalog Item 的 <Info> 容器(GB/T 28181 附录 A / §9.3.1)。
//
// ⛔ 为什么必须单独建它:通道属性(云台类型/室内外/补光方式/分辨率…)在标准里都在
// <Info> 容器内,而本仓原实现把 PTZType 直接声明为 CatalogItem 的 Item 层字段。
// Go 的 encoding/xml **只匹配当前元素的直接子元素、不递归** → 合规设备上报的
// `<Info><PTZType>3</PTZType></Info>` 永远解不出来,gb_channel.ptz_type 恒为 0;
// 2016 的 BusinessGroupID 同理读不到,业务分组挂载静默失效。
//
// ⛔ 两版差异(必须并存,**不得按版本删字段**):
//   - 2016:PTZType 为 integer(取值 1-4);含 PositionType / UseType;
//     BusinessGroupID **在本容器内**
//   - 2022:PTZType 改为 string(取值扩到 **1-7**);PositionType / UseType **已删除**;
//     新增 PhotoelectricImagingType / CapturePositionType / StreamNumberList /
//     SSVCRatioSupportList;BusinessGroupID **上提到 Item 层**
//
// 因此这里一律用 string 承载 —— 2016 的 integer 文本与 2022 的 string 都能解出,
// 由上层按需转数字。**两个版本的字段全读,不做版本分支**:设备上报什么就收什么,
// 不认识的字段留空即可。按版本过滤会把另一版的真实数据丢掉,而设备的
// `effective_version` 对历史行有 `default:2016` 兜底,靠它分支会误伤 2022 设备。
type CatalogInfo struct {
	// 两版共有(都在 <Info> 内)
	PTZType         string `xml:"PTZType"`         // 云台类型:2016 1-4 / 2022 1-7
	RoomType        string `xml:"RoomType"`        // 室内外:1-室外 2-室内
	SupplyLightType string `xml:"SupplyLightType"` // 补光方式
	DirectionType   string `xml:"DirectionType"`   // 方向
	Resolution      string `xml:"Resolution"`      // 分辨率
	// 仅 2016(2022 标准已删除这两个元素)
	PositionType string `xml:"PositionType"`
	UseType      string `xml:"UseType"`
	// 仅 2022 新增
	PhotoelectricImagingType string `xml:"PhotoelectricImagingType"`
	CapturePositionType      string `xml:"CapturePositionType"`
	StreamNumberList         string `xml:"StreamNumberList"`
	SSVCRatioSupportList     string `xml:"SSVCRatioSupportList"`
	// BusinessGroupID 仅 2016 在此(2022 在 Item 层)
	BusinessGroupID string `xml:"BusinessGroupID"`
}

// CatalogItem Catalog 应答里的单个通道项
type CatalogItem struct {
	DeviceID     string `xml:"DeviceID"`
	Name         string `xml:"Name"`
	Manufacturer string `xml:"Manufacturer"`
	Model        string `xml:"Model"`
	Owner        string `xml:"Owner"`
	CivilCode    string `xml:"CivilCode"`
	ParentID     string `xml:"ParentID"`
	// BusinessGroupID 是 **2022 的位置**(Item 层)。2016 把它放在 <Info> 内,
	// 由 normalize() 回填 → 消费方不必知道它来自哪一层。
	BusinessGroupID string `xml:"BusinessGroupID"`
	Address         string `xml:"Address"`
	Parental        int    `xml:"Parental"`
	RegisterWay     int    `xml:"RegisterWay"`
	Secrecy         int    `xml:"Secrecy"`
	// PTZTypeAtItem 兼容把 PTZType 直接写在 Item 层的非合规设备(旧实现只认这里,
	// 而标准要求写在 <Info> 内 —— 取值优先级见 normalize())。
	PTZTypeAtItem int     `xml:"PTZType"`
	Longitude     float64 `xml:"Longitude"`
	Latitude      float64 `xml:"Latitude"`
	Status        string  `xml:"Status"` // ON/OFF
	Event         string  `xml:"Event"`  // ADD/UPDATE/DEL in Catalog NOTIFY
	// IPAddress / Port 两版都在 Item 层(附录 A)
	IPAddress string `xml:"IPAddress"`
	Port      int    `xml:"Port"`
	// Info 是 <Info> 容器,minOccurs=0 → 可能为 nil
	Info *CatalogInfo `xml:"Info,omitempty"`

	// PTZType 是**归一化后**的云台类型(0 = 未上报/非法)。
	// ⛔ 刻意不带 xml tag:它由 normalize() 从 <Info> 或 Item 层择一填充,
	// 让消费方(handler / subscribe / upsert / controllers)无需改动即可拿到标准位置的值。
	PTZType int `xml:"-"`
}

// normalize 把 <Info> 内的属性回填到归一化的对外字段上。
//
// ⛔ 取值优先级(两版并存,不做版本分支):
//   - PTZType:`<Info>` 优先(两版标准位置)→ 回落 Item 层(非合规设备)
//   - BusinessGroupID:Item 层优先(2022 位置,保持既有行为不变)→ 回落 `<Info>`(2016 位置)
//
// normalize 是幂等的,可安全重复调用。
func (it *CatalogItem) normalize() {
	info := it.Info
	if info == nil {
		it.PTZType = it.PTZTypeAtItem
		return
	}
	// 属性文本统一去空白:设备常在元素内带换行/缩进(国标示例报文就是多行缩进),
	// 不去掉会把 "\n  2\n" 这类值直接写进库。
	info.PTZType = strings.TrimSpace(info.PTZType)
	info.RoomType = strings.TrimSpace(info.RoomType)
	info.SupplyLightType = strings.TrimSpace(info.SupplyLightType)
	info.DirectionType = strings.TrimSpace(info.DirectionType)
	info.Resolution = strings.TrimSpace(info.Resolution)
	info.PositionType = strings.TrimSpace(info.PositionType)
	info.UseType = strings.TrimSpace(info.UseType)
	info.PhotoelectricImagingType = strings.TrimSpace(info.PhotoelectricImagingType)
	info.CapturePositionType = strings.TrimSpace(info.CapturePositionType)
	info.StreamNumberList = strings.TrimSpace(info.StreamNumberList)
	info.SSVCRatioSupportList = strings.TrimSpace(info.SSVCRatioSupportList)
	info.BusinessGroupID = strings.TrimSpace(info.BusinessGroupID)

	if v := ParseAttrInt(info.PTZType); v != 0 {
		it.PTZType = v
	} else {
		it.PTZType = it.PTZTypeAtItem
	}
	if it.BusinessGroupID == "" {
		it.BusinessGroupID = info.BusinessGroupID
	}
}

// InfoOrEmpty 返回 <Info> 内容;容器不存在(minOccurs=0)时返回零值,
// 省去调用方逐处 nil 判断。
func (it *CatalogItem) InfoOrEmpty() CatalogInfo {
	if it.Info == nil {
		return CatalogInfo{}
	}
	return *it.Info
}

// ParseAttrInt 把 Catalog 数字属性的文本转成整数;空串 / 非数字返回 0(表示未上报)。
//
// 导出是因为 <Info> 里的属性一律用 string 承载(2016 的 integer 与 2022 的 string
// 都要能解),上层(handler / subscribe 适配器)需要自己转数字。
func ParseAttrInt(raw string) int {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0
	}
	v, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0
	}
	return v
}

// CatalogResponse Catalog 应答(设备→平台,可能多条分包)
type CatalogResponse struct {
	XMLName    xml.Name `xml:"Response"`
	CmdType    string   `xml:"CmdType"`
	SN         int      `xml:"SN"`
	DeviceID   string   `xml:"DeviceID"`
	SumNum     int      `xml:"SumNum"` // 通道总数(用于分包聚合判断)
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
	// ⛔ 必须逐项 normalize:PTZType / BusinessGroupID 在标准里位于 <Info> 内,
	// 解码阶段(Go encoding/xml 不递归)只能拿到 Item 层的值。
	for i := range r.DeviceList.Items {
		r.DeviceList.Items[i].normalize()
	}
	return &r, nil
}

// IsOnline 通道状态是否在线
func (it *CatalogItem) IsOnline() bool {
	return it.Status == "ON" || it.Status == "On" || it.Status == "ONLINE"
}
