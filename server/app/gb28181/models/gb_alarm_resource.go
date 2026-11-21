package models

import (
	"time"

	"gorm.io/gorm"
)

type AlarmResourceType string

const (
	AlarmResourceInput  AlarmResourceType = "alarm_input"
	AlarmResourceOutput AlarmResourceType = "alarm_output"
)

// GbAlarmResource stores non-video alarm resources reported by Catalog.
// DeviceCode identifies the registering physical device even when its local
// database row is not available yet; DeviceID is backfilled when possible.
type GbAlarmResource struct {
	ID           uint              `gorm:"primaryKey" json:"id"`
	OwnerDeptID  uint              `gorm:"column:owner_dept_id;not null;uniqueIndex:uk_alarm_resource_code,priority:1;index:idx_alarm_resource_device,priority:1" json:"ownerDeptId"`
	DeviceID     uint              `gorm:"column:device_id;not null;default:0;index:idx_alarm_resource_device_id" json:"deviceId"`
	DeviceCode   string            `gorm:"column:device_code;size:20;not null;uniqueIndex:uk_alarm_resource_code,priority:2;index:idx_alarm_resource_device,priority:2" json:"deviceCode"`
	AlarmCode    string            `gorm:"column:alarm_code;size:20;not null;uniqueIndex:uk_alarm_resource_code,priority:3;index:idx_alarm_resource_alarm_code" json:"alarmCode"`
	ResourceType AlarmResourceType `gorm:"column:resource_type;size:16;not null;index:idx_alarm_resource_type" json:"resourceType"`
	TypeCode     string            `gorm:"column:type_code;size:3;not null" json:"typeCode"`
	Name         string            `gorm:"column:name;size:255;not null" json:"name"`
	RawParentIDs string            `gorm:"column:raw_parent_ids;size:512;not null;default:''" json:"rawParentIds"`
	Status       int8              `gorm:"column:status;not null;default:0" json:"status"`
	CreatedAt    time.Time         `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt    time.Time         `gorm:"column:updated_at;not null" json:"updatedAt"`
	DeletedAt    gorm.DeletedAt    `gorm:"column:deleted_at;index:idx_alarm_resource_deleted_at" json:"-"`
}

func (GbAlarmResource) TableName() string { return "gb_alarm_resource" }

// GbAlarmResourceParent normalizes Catalog ParentID. GB/T 28181-2022 permits
// multiple parent codes separated by '/', so one raw string is not queryable.
type GbAlarmResourceParent struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	AlarmResourceID uint      `gorm:"column:alarm_resource_id;not null;uniqueIndex:uk_alarm_resource_parent,priority:1;index:idx_alarm_parent_resource" json:"alarmResourceId"`
	ParentCode      string    `gorm:"column:parent_code;size:20;not null;uniqueIndex:uk_alarm_resource_parent,priority:2;index:idx_alarm_parent_code" json:"parentCode"`
	CreatedAt       time.Time `gorm:"column:created_at;not null" json:"createdAt"`
}

func (GbAlarmResourceParent) TableName() string { return "gb_alarm_resource_parent" }

type AlarmBindingSource string

const AlarmBindingSourceManual AlarmBindingSource = "manual"

// GbAlarmBinding is an explicit operator-selected video-to-alarm mapping. It
// has higher priority than Catalog-derived heuristics.
type GbAlarmBinding struct {
	ID              uint               `gorm:"primaryKey" json:"id"`
	DeviceID        uint               `gorm:"column:device_id;not null;uniqueIndex:uk_alarm_binding_channel,priority:1;index:idx_alarm_binding_device" json:"deviceId"`
	ChannelCode     string             `gorm:"column:channel_code;size:20;not null;uniqueIndex:uk_alarm_binding_channel,priority:2" json:"channelCode"`
	AlarmResourceID uint               `gorm:"column:alarm_resource_id;not null;index:idx_alarm_binding_resource" json:"alarmResourceId"`
	Source          AlarmBindingSource `gorm:"column:source;size:16;not null;default:manual" json:"source"`
	CreatedAt       time.Time          `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt       time.Time          `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (GbAlarmBinding) TableName() string { return "gb_alarm_binding" }
