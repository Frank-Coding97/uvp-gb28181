package catalog

// CatalogItem 入库管道的标准 DTO(plan §3 / A3-A4 通用)
//
// 屏蔽 manscdp.CatalogItem 与底层协议差异,handler 层做适配即可。
// 字段子集 + 平铺,不引入 SIP / XML / GORM 依赖,方便单测构造。
type CatalogItem struct {
	DeviceID     string  // 国标 20 位编码(可能是设备/通道/分组/虚拟组织 任一)
	Name         string  // 显示名
	Manufacturer string  // 厂商
	Model        string  // 型号
	Owner        string  // 持有者
	CivilCode    string  // 国标 Catalog Item 上报的行政区码(GB/T 2260,6 位)
	ParentID     string  // 国标 Catalog Item 上报的父编码
	PTZType      int     // 云台类型
	Longitude    float64 // 经度
	Latitude     float64 // 纬度
	StatusOn     bool    // 是否在线
	Address      string  // 物理地址
	Secrecy      int8    // 涉密
	RegisterWay  int8    // 注册方式
}

// Sender 入库管道入参元数据
type Sender struct {
	TenantID       uint   // 多租户
	SourceDeviceID string // 来源设备(NVR / 下级平台)国标编码
}
