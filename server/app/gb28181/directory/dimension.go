package directory

// NodeType 节点类型常量
const (
	NodeTypeCivilCode   = "civil_code"   // 行政区划节点
	NodeTypeDevice      = "device"       // 设备节点
	NodeTypeChannel     = "channel"      // 通道节点
	NodeTypeBizGroup    = "biz_group"    // 业务分组节点
	NodeTypeVirtualOrg  = "virtual_org"  // 虚拟组织节点
	NodeTypeUnassigned  = "unassigned"   // 未分配桶(特殊节点)
)

// DimensionName 维度名称常量
const (
	DimensionNative    = "native"     // 国标自动注册维度(原始目录树)
	DimensionBizGroup  = "biz_group"  // 业务分组维度
	DimensionCivilCode = "civil_code" // 行政区划维度
)

// Dimension 定义设备目录维度抽象接口
// 三个维度(native/biz_group/civil_code)都实现此接口
//
// 设计原则:
//  - 统一接口:三个维度返回相同的 Node 结构,前端无需感知维度差异
//  - 懒加载:GetRoots 只返回顶层,GetChildren 按需加载子节点
//  - 可选统计:withCounts 控制是否附加挂载数/通道数(性能敏感场景可关闭)
type Dimension interface {
	// GetRoots 获取顶层节点列表
	// withCounts=true 时附加 mount_count / channel_count
	GetRoots(withCounts bool) ([]Node, error)

	// GetChildren 获取指定父节点的子节点列表
	// parentID 为父节点 ID
	// withCounts=true 时附加 mount_count / channel_count
	GetChildren(parentID string, withCounts bool) ([]Node, error)
}

// Node 统一的目录节点结构(跨维度通用)
//
// 字段说明:
//  - ID: 节点唯一标识,格式由具体维度决定(如 civil_code 维度用 6 位行政区划码)
//  - NodeType: 必填,用于前端渲染不同图标
//  - ParentID: 根节点为 nil,其他节点指向父节点 ID
//  - ChannelID/DeviceID: 仅 channel/device 类型节点有值
//  - MountCount/ChannelCount: 仅 withCounts=true 时返回,否则为 0
//  - HasChildren/IsLeaf: 前端判断是否显示展开按钮
type Node struct {
	// 节点唯一标识(格式由具体维度决定)
	ID string `json:"id"`

	// 节点显示名称
	Name string `json:"name"`

	// 节点类型(使用 NodeType* 常量)
	NodeType string `json:"nodeType"`

	// 父节点 ID(根节点为 nil)
	ParentID *string `json:"parentId,omitempty"`

	// 通道 ID(仅 channel 类型节点有值)
	ChannelID *string `json:"channelId,omitempty"`

	// 设备 ID(仅 device/channel 类型节点有值)
	DeviceID *string `json:"deviceId,omitempty"`

	// 行政区划码(civil_code 维度使用)
	CivilCode string `json:"civilCode,omitempty"`

	// 挂载数(该节点被挂载到多少个分组,withCounts=true 时返回)
	MountCount int `json:"mountCount,omitempty"`

	// 通道总数(该节点下递归包含的通道数,withCounts=true 时返回)
	ChannelCount int `json:"channelCount,omitempty"`

	// 是否有子节点(用于前端判断是否显示展开图标)
	HasChildren bool `json:"hasChildren"`

	// 是否为叶子节点(通道节点一定是叶子)
	IsLeaf bool `json:"isLeaf"`
}
