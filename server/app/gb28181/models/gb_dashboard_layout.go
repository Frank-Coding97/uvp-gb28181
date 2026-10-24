package models

import "time"

type GbDashboardLayout struct {
	ID            uint64    `gorm:"primaryKey" json:"id"`
	UserID        uint      `gorm:"column:user_id;not null;uniqueIndex:uk_dashboard_layout_user_key,priority:1" json:"-"`
	DashboardKey  string    `gorm:"column:dashboard_key;size:32;not null;uniqueIndex:uk_dashboard_layout_user_key,priority:2" json:"dashboardKey"`
	SchemaVersion int       `gorm:"column:schema_version;not null" json:"schemaVersion"`
	Revision      uint64    `gorm:"column:revision;not null;default:1" json:"revision"`
	LayoutJSON    string    `gorm:"column:layout_json;type:text;not null" json:"-"`
	CreatedAt     time.Time `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt     time.Time `gorm:"column:updated_at;not null;index:idx_dashboard_layout_updated" json:"updatedAt"`
}

func (GbDashboardLayout) TableName() string { return "gb_dashboard_layout" }
