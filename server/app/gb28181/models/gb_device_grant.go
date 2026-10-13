package models

import (
	"time"

	"gorm.io/gorm"
)

// GrantTargetTypeDept / GrantTargetTypeUser 共享目标类型
const (
	GrantTargetTypeDept = "dept"
	GrantTargetTypeUser = "user"
)

// GbDeviceGrant 设备共享授权(设备级可见性共享,不改变 owner_dept_id 归属)。
type GbDeviceGrant struct {
	ID         uint           `gorm:"primarykey" json:"id"`
	DeviceID   uint           `gorm:"column:device_id;not null;index:idx_device;uniqueIndex:uk_device_target,priority:1;comment:设备ID(gb_device.id)" json:"deviceId"`
	TargetType string         `gorm:"column:target_type;size:16;not null;default:'';index:idx_target,priority:1;uniqueIndex:uk_device_target,priority:2;comment:共享目标类型 dept/user" json:"targetType"`
	TargetID   uint           `gorm:"column:target_id;not null;default:0;index:idx_target,priority:2;uniqueIndex:uk_device_target,priority:3;comment:部门ID或用户ID" json:"targetId"`
	CreatedBy  uint           `gorm:"column:created_by;default:0;comment:操作人" json:"createdBy"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"deletedAt"`
}

func (GbDeviceGrant) TableName() string {
	return "gb_device_grant"
}
