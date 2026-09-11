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
