package models

import "time"

// GbCustomGroupDevice links stable platform device IDs to custom groups.
type GbCustomGroupDevice struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	GroupID   uint      `gorm:"column:group_id;not null;uniqueIndex:uk_custom_group_device,priority:1;index:idx_custom_group_device_group" json:"groupId"`
	DeviceID  uint      `gorm:"column:device_id;not null;uniqueIndex:uk_custom_group_device,priority:2;index:idx_custom_group_device_device" json:"deviceId"`
	CreatedBy uint      `gorm:"column:created_by;not null" json:"createdBy"`
	CreatedAt time.Time `gorm:"column:created_at;not null" json:"createdAt"`
}

func (GbCustomGroupDevice) TableName() string { return "gb_custom_group_device" }
