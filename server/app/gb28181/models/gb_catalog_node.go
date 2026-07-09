package models

import (
	"time"

	"gorm.io/gorm"
)

// NodeType 目录节点类型(plan §3.4)
type NodeType string

const (
	NodeTypeCivilCode  NodeType = "civil_code"  // 行政区划
	NodeTypeBizGroup   NodeType = "biz_group"   // 业务分组 (215)
	NodeTypeVirtualOrg NodeType = "virtual_org" // 虚拟组织 (216) / anomaly 兜底落点
	NodeTypeDevice     NodeType = "device"      // 物理设备
	NodeTypeChannel    NodeType = "channel"     // 物理通道
)

// NodeSource 节点来源
type NodeSource string

const (
	NodeSourceCatalog NodeSource = "catalog" // 国标推送
	NodeSourceManual  NodeSource = "manual"  // 人工编辑
	NodeSourceAuto    NodeSource = "auto"    // 识别自动推断
)

// GbCatalogNode 国标多级目录树节点(思路 B+ 核心)
// 承担:多级目录(省/市/区/小区/...) + 节点类型识别 + anomaly 兜底
// 物化路径 path 字段加速子树查询(LIKE 'path%'),代价是 reparent 需要批改
type GbCatalogNode struct {
	ID            uint           `gorm:"primarykey" json:"id"`
	TenantID      uint           `gorm:"column:tenant_id;index:idx_tenant_parent,priority:1;index:idx_tenant_path,priority:1;index:idx_tenant_type,priority:1;index:idx_tenant_anomaly,priority:1;index:idx_civil_code,priority:1" json:"tenantId"`
	NodeType      NodeType       `gorm:"column:node_type;size:16;not null;index:idx_tenant_type,priority:2" json:"nodeType"`
	ParentID      *uint          `gorm:"column:parent_id;index:idx_tenant_parent,priority:2" json:"parentId"`
	Path          string         `gorm:"column:path;size:512;not null;index:idx_tenant_path,priority:2" json:"path"`
	Depth         uint8          `gorm:"column:depth;not null;default:0" json:"depth"`
	Name          string         `gorm:"column:name;size:128;not null" json:"name"`
	Code          string         `gorm:"column:code;size:32;index" json:"code"`
	CivilCode     string         `gorm:"column:civil_code;size:6;index:idx_civil_code,priority:2" json:"civilCode"`
	DeviceID      *uint          `gorm:"column:device_id" json:"deviceId"`
	ChannelID     *uint          `gorm:"column:channel_id" json:"channelId"`
	Source        NodeSource     `gorm:"column:source;size:16;not null;default:catalog" json:"source"`
	SortOrder     int            `gorm:"column:sort_order;default:0" json:"sortOrder"`
	Anomaly       bool           `gorm:"column:anomaly;default:false;index:idx_tenant_anomaly,priority:2" json:"anomaly"`
	AnomalyReason string         `gorm:"column:anomaly_reason;size:255" json:"anomalyReason"`
	RawCode       string         `gorm:"column:raw_code;size:64" json:"rawCode"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"deletedAt"`
}

// TableName 表名
func (GbCatalogNode) TableName() string { return "gb_catalog_node" }
