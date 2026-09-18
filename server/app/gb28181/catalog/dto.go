package catalog

import "strings"

// CatalogItem 入库管道的标准 DTO(plan §3 / A3-A4 通用)
//
// 屏蔽 manscdp.CatalogItem 与底层协议差异,handler 层做适配即可。
// 字段子集 + 平铺,不引入 SIP / XML / GORM 依赖,方便单测构造。
type CatalogItem struct {
	DeviceID        string  // 国标 20 位编码(可能是设备/通道/分组/虚拟组织 任一)
	Name            string  // 显示名
	Manufacturer    string  // 厂商
	Model           string  // 型号
	Owner           string  // 持有者
	CivilCode       string  // 国标 Catalog Item 上报的行政区码(GB/T 2260,6 位)
	ParentID        string  // 国标 Catalog Item 上报的父编码
	BusinessGroupID string  // 2022 业务分组归属编码
	Parental        int     // 是否目录节点
	PTZType         int     // 云台类型
	Longitude       float64 // 经度
	Latitude        float64 // 纬度
	StatusOn        bool    // 是否在线
	Address         string  // 物理地址
	Secrecy         int8    // 涉密
	RegisterWay     int8    // 注册方式

	// ---- 通道属性(GB/T 28181 附录 A / §9.3.1)----
	// IPAddress / Port 两版都声明在 Catalog Item 层。
	IPAddress string
	Port      int
	// 以下四项两版都声明在 <Info> 容器内。
	// ⛔ 0 / "" 一律表示"设备本次未上报",不是"该属性为 0" —— 落库时据此判断是否覆盖,
	// 避免一次只带 Status 的 UPDATE 事件把已有属性清零。
	// ⛔ RoomType 两版编码一致(1-室外 2-室内);PTZType 值域已从 1-4 扩到 **1-7**(2022)。
	// ⛔ SupplyLightType 值域也扩过:2016 只到 3(无/红外/白光),2022 加了 4-激光 与 9-其他。
	RoomType        int
	SupplyLightType int
	DirectionType   int
	Resolution      string

	// ---- 版本独有属性(2016 与 2022 各占一半,用来区分设备上报的是哪一版形态)----
	// 2016 独有(DirectionType 之后的位置,2022 已删除;XSD 里是 integer):
	PositionType int // 位置类型 1-省际检查站 … 10-交通干线
	UseType      int // 用途 1-治安 2-交通 3-重点
	// 2022 独有(2016 无此声明;XSD 里是 string):
	PhotoelectricImagingType string // 光电成像类型 1-可见光 2-热成像 3-雷达 4-X光 5-深度光场 9-其他,可多值以 "/" 分隔
	CapturePositionType      string // 采集部位类型,取值见 2022 附录 O
	// StreamNumberList 是设备声明的码流编号列表("0/1"、"0/1/2"),2022 独有的 <Info> 属性。
	// ⭐ 本轮补 DTO 是为视频参数面板服务：面板按"设备支持几段码流"渲染格子，
	// 而这个列表是唯一出处。它本身仍不参与通道属性展示。
	StreamNumberList string

	// ⚠️ 以下字段已在 manscdp 层解析,但本仓尚无对应列,故不进本 DTO(只解析不落库):
	// 2022 独有 SSVCRatioSupportList —— SVAC 码率支持列表,属**点播取流能力**描述,
	// 不属于「通道属性展示」范畴,要落库须另开迁移单。
	// 需要时可从 manscdp.CatalogItem.InfoOrEmpty() 取。
	// (StreamNumberList 原在同批"只解析不落库"清单里,2026-09-18 因视频参数面板需要而补落。)
}

// SplitParentIDs normalizes the GB/T 28181-2022 multi-parent form A/B while
// preserving declaration order and removing empty/duplicate entries.
func SplitParentIDs(raw string) []string {
	parts := strings.Split(raw, "/")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		code := strings.TrimSpace(part)
		if code == "" {
			continue
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out
}

// Sender 入库管道入参元数据
type Sender struct {
	OwnerDeptID    uint   // 业务归属部门
	SourceDeviceID string // 来源设备(NVR / 下级平台)国标编码
}
