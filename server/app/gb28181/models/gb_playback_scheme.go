package models

import "time"

// GbPlaybackScheme stores a user's reusable multi-screen layout.
type GbPlaybackScheme struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	OwnerUserID uint      `gorm:"column:owner_user_id;not null;uniqueIndex:uk_playback_scheme_owner_name,priority:1;index:idx_playback_scheme_owner_updated,priority:1" json:"-"`
	OwnerDeptID uint      `gorm:"column:owner_dept_id;not null;index:idx_playback_scheme_dept" json:"-"`
	Name        string    `gorm:"column:name;size:64;not null;uniqueIndex:uk_playback_scheme_owner_name,priority:2" json:"name"`
	LayoutSize  int16     `gorm:"column:layout_size;not null" json:"layoutSize"`
	SlotCount   int       `gorm:"column:slot_count;not null;default:0" json:"slotCount"`
	CreatedBy   uint      `gorm:"column:created_by;not null" json:"-"`
	UpdatedBy   uint      `gorm:"column:updated_by;not null" json:"-"`
	CreatedAt   time.Time `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt   time.Time `gorm:"column:updated_at;not null;index:idx_playback_scheme_owner_updated,priority:2" json:"updatedAt"`
}

func (GbPlaybackScheme) TableName() string { return "gb_playback_scheme" }
