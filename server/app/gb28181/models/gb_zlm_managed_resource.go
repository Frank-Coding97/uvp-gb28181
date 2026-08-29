package models

import "time"

// GbZLMManagedResource 记录管理台创建过的 ZLM 资源来源。
//
// 这张表只保留来源审计所需的安全摘要。ZLM 的实时列表才是资源是否存在
// 的事实来源；因此模型没有 online/status、desired state 或原始响应字段。
// TombstonedAt 表示最近一次已确认资源消失，nil 表示尚未标记为消失。
type GbZLMManagedResource struct {
	ID                  uint64     `gorm:"primaryKey;column:id" json:"-"`
	NodeID              int64      `gorm:"column:node_id;not null;uniqueIndex:uk_gb_zlm_managed_resource_identity,priority:1;index:idx_gb_zlm_managed_resource_observed,priority:1;index:idx_gb_zlm_managed_resource_tombstone,priority:1" json:"nodeId"`
	ResourceType        string     `gorm:"column:resource_type;size:32;not null;uniqueIndex:uk_gb_zlm_managed_resource_identity,priority:2" json:"resourceType"`
	ResourceKey         string     `gorm:"column:resource_key;size:255;not null;uniqueIndex:uk_gb_zlm_managed_resource_identity,priority:3" json:"-"`
	App                 string     `gorm:"column:app;size:64;not null;default:''" json:"app"`
	Stream              string     `gorm:"column:stream;size:255;not null;default:''" json:"stream"`
	IdentityFingerprint string     `gorm:"column:identity_fingerprint;type:char(64);not null" json:"-"`
	Summary             string     `gorm:"column:summary;size:512;not null;default:''" json:"summary"`
	CreatedBy           uint64     `gorm:"column:created_by;not null;default:0" json:"createdBy"`
	CreatedAt           time.Time  `gorm:"column:created_at;not null" json:"createdAt"`
	LastObservedAt      *time.Time `gorm:"column:last_observed_at;index:idx_gb_zlm_managed_resource_observed,priority:2" json:"lastObservedAt"`
	TombstonedAt        *time.Time `gorm:"column:tombstoned_at;index:idx_gb_zlm_managed_resource_tombstone,priority:2" json:"tombstonedAt"`
	UpdatedAt           time.Time  `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (GbZLMManagedResource) TableName() string { return "gb_zlm_managed_resource" }
