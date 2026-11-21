package model

// GbCascadeBusinessNode stores one root business group or virtual organization
// for a single upstream cascade platform.
type GbCascadeBusinessNode struct {
	ID         uint64 `gorm:"primaryKey" json:"id"`
	PlatformID uint64 `gorm:"column:platform_id;not null;uniqueIndex:uk_cascade_business_node_code,priority:1" json:"platformId"`
	Code       string `gorm:"column:code;size:20;not null;uniqueIndex:uk_cascade_business_node_code,priority:2" json:"code"`
	Name       string `gorm:"column:name;size:128;not null" json:"name"`
	Kind       string `gorm:"column:kind;size:16;not null" json:"kind"`
	ParentCode string `gorm:"column:parent_code;size:20;not null;default:''" json:"parentCode"`
	Sort       int    `gorm:"column:sort;not null;default:0" json:"sort"`
}

func (GbCascadeBusinessNode) TableName() string { return "gb_cascade_business_node" }

// GbCascadeBusinessBinding assigns one shared source channel to an
// organization node for one upstream platform.
type GbCascadeBusinessBinding struct {
	ID              uint64 `gorm:"primaryKey" json:"id"`
	PlatformID      uint64 `gorm:"column:platform_id;not null;uniqueIndex:uk_cascade_business_binding_channel,priority:1" json:"platformId"`
	SourceChannelID uint64 `gorm:"column:source_channel_id;not null;uniqueIndex:uk_cascade_business_binding_channel,priority:2" json:"sourceChannelId"`
	NodeCode        string `gorm:"column:node_code;size:20;not null" json:"nodeCode"`
}

func (GbCascadeBusinessBinding) TableName() string { return "gb_cascade_business_binding" }
