package models

import "time"

// GbCustomGroup stores user-managed device groups independently from Catalog.
type GbCustomGroup struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	OwnerDeptID uint      `gorm:"column:owner_dept_id;not null;uniqueIndex:uk_custom_group_sibling_name,priority:1;index:idx_custom_group_dept_path,priority:1" json:"ownerDeptId"`
	ParentID    uint      `gorm:"column:parent_id;not null;default:0;uniqueIndex:uk_custom_group_sibling_name,priority:2;index:idx_custom_group_parent" json:"parentId"`
	Path        string    `gorm:"column:path;size:1024;not null;index:idx_custom_group_dept_path,priority:2" json:"path"`
	Depth       uint8     `gorm:"column:depth;not null;default:0" json:"depth"`
	Name        string    `gorm:"column:name;size:64;not null;uniqueIndex:uk_custom_group_sibling_name,priority:3" json:"name"`
	CreatedBy   uint      `gorm:"column:created_by;not null" json:"createdBy"`
	CreatedAt   time.Time `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt   time.Time `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (GbCustomGroup) TableName() string { return "gb_custom_group" }
