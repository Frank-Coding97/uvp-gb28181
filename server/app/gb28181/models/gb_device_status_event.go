package models

import (
	"time"

	"gorm.io/gorm"
)

type DeviceStatusEventType string

const (
	DeviceEventRegisterOnline     DeviceStatusEventType = "register_online"
	DeviceEventUnregisterOffline  DeviceStatusEventType = "unregister_offline"
	DeviceEventHeartbeatTimeout   DeviceStatusEventType = "heartbeat_timeout"
	DeviceEventHeartbeatRecovered DeviceStatusEventType = "heartbeat_recovered"
	DeviceEventRegisterRenewed    DeviceStatusEventType = "register_renewed"
)

func (t DeviceStatusEventType) Valid() bool {
	switch t {
	case DeviceEventRegisterOnline, DeviceEventUnregisterOffline, DeviceEventHeartbeatTimeout, DeviceEventHeartbeatRecovered, DeviceEventRegisterRenewed:
		return true
	default:
		return false
	}
}

func (t DeviceStatusEventType) DisplayName() string {
	switch t {
	case DeviceEventRegisterOnline:
		return "注册上线"
	case DeviceEventUnregisterOffline:
		return "正常注销"
	case DeviceEventHeartbeatTimeout:
		return "心跳超时"
	case DeviceEventHeartbeatRecovered:
		return "心跳恢复"
	case DeviceEventRegisterRenewed:
		return "注册续订"
	default:
		return string(t)
	}
}

type DeviceStatusEventSource string

const (
	DeviceEventSourceRegister       DeviceStatusEventSource = "register"
	DeviceEventSourceUnregister     DeviceStatusEventSource = "unregister"
	DeviceEventSourceKeepalive      DeviceStatusEventSource = "keepalive"
	DeviceEventSourceOfflineScanner DeviceStatusEventSource = "offline_scanner"
)

// GbDeviceStatusEvent records meaningful device lifecycle transitions.
type GbDeviceStatusEvent struct {
	ID                uint64                  `gorm:"primaryKey;index:idx_device_occurred,priority:3" json:"id"`
	DeviceID          uint                    `gorm:"column:device_id;not null;index:idx_device_occurred,priority:1" json:"deviceId"`
	DeviceCode        string                  `gorm:"column:device_code;size:20;not null;index:idx_device_code" json:"deviceCode"`
	EventType         DeviceStatusEventType   `gorm:"column:event_type;size:32;not null;index:idx_event_type_occurred,priority:1" json:"eventType"`
	FromStatus        *int8                   `gorm:"column:from_status" json:"fromStatus"`
	ToStatus          int8                    `gorm:"column:to_status;not null" json:"toStatus"`
	OccurredAt        time.Time               `gorm:"column:occurred_at;not null;index:idx_device_occurred,priority:2;index:idx_event_type_occurred,priority:2" json:"occurredAt"`
	Source            DeviceStatusEventSource `gorm:"column:source;size:32;not null" json:"source"`
	RegisterExpires   *int                    `gorm:"column:register_expires" json:"registerExpires"`
	KeepaliveInterval *int                    `gorm:"column:keepalive_interval" json:"keepaliveInterval"`
	IP                string                  `gorm:"column:ip;size:64" json:"ip"`
	Port              int                     `gorm:"column:port" json:"port"`
	Transport         string                  `gorm:"column:transport;size:8" json:"transport"`
	Detail            string                  `gorm:"column:detail;type:text" json:"-"`
	CreatedAt         time.Time               `json:"createdAt"`
	UpdatedAt         time.Time               `json:"updatedAt"`
	DeletedAt         gorm.DeletedAt          `gorm:"index" json:"deletedAt"`
}

func (GbDeviceStatusEvent) TableName() string { return "gb_device_status_event" }
