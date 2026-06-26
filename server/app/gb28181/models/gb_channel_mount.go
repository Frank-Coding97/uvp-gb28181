package models

import "time"

// MountSource 挂载来源
type MountSource string

const (
	MountSourceCatalog MountSource = "catalog"
	MountSourceManual  MountSource = "manual"
)

// GbChannelMount 通道与目录树节点的 N:N 挂载关系(plan §3.5)
// 同一通道可挂多处(主挂载 is_primary=1 唯一);主键 + 业务约束保证
// 唯一约束 (channel_id, parent_node_id) 防同节点重复挂载
type GbChannelMount struct {
	ID           uint        `gorm:"primarykey" json:"id"`
	TenantID     uint        `gorm:"column:tenant_id;index" json:"tenantId"`
	ChannelID    uint        `gorm:"column:channel_id;not null;uniqueIndex:uk_channel_parent,priority:1;index" json:"channelId"`
	ParentNodeID uint        `gorm:"column:parent_node_id;not null;uniqueIndex:uk_channel_parent,priority:2;index:idx_parent_sort,priority:1" json:"parentNodeId"`
	DisplayName  string      `gorm:"column:display_name;size:128" json:"displayName"`
	IsPrimary    bool        `gorm:"column:is_primary;default:false" json:"isPrimary"`
	MountSource  MountSource `gorm:"column:mount_source;size:16;not null;default:catalog" json:"mountSource"`
	SortOrder    int         `gorm:"column:sort_order;default:0;index:idx_parent_sort,priority:2" json:"sortOrder"`
	CreatedAt    time.Time   `json:"createdAt"`
	UpdatedAt    time.Time   `json:"updatedAt"`
}

// TableName 表名
func (GbChannelMount) TableName() string { return "gb_channel_mount" }
