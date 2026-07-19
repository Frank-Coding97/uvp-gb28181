package models

import "time"

type SubscriptionKind string

const (
	SubscriptionKindCatalog        SubscriptionKind = "catalog"
	SubscriptionKindMobilePosition SubscriptionKind = "mobile_position"
	SubscriptionKindAlarm          SubscriptionKind = "alarm"
)

func (k SubscriptionKind) Valid() bool {
	switch k {
	case SubscriptionKindCatalog, SubscriptionKindMobilePosition, SubscriptionKindAlarm:
		return true
	default:
		return false
	}
}

type SubscriptionStatus string

const (
	SubscriptionStatusDisabled SubscriptionStatus = "disabled"
	SubscriptionStatusPending  SubscriptionStatus = "pending"
	SubscriptionStatusActive   SubscriptionStatus = "active"
	SubscriptionStatusDegraded SubscriptionStatus = "degraded"
	SubscriptionStatusExpired  SubscriptionStatus = "expired"
)

// GbDeviceSubscription is the durable state for one device-level SIP subscription.
type GbDeviceSubscription struct {
	ID              uint               `gorm:"primaryKey" json:"id"`
	DeviceID        uint               `gorm:"column:device_id;not null;uniqueIndex:uk_device_subscription_kind" json:"deviceId"`
	Kind            SubscriptionKind   `gorm:"column:kind;size:32;not null;uniqueIndex:uk_device_subscription_kind" json:"kind"`
	Enabled         bool               `gorm:"column:enabled;not null;default:false;index:idx_subscription_due,priority:1" json:"enabled"`
	Status          SubscriptionStatus `gorm:"column:status;size:16;not null;default:disabled" json:"status"`
	ExpiresSeconds  int                `gorm:"column:expires_seconds;not null;default:3600" json:"expiresSeconds"`
	IntervalSeconds int                `gorm:"column:interval_seconds;not null;default:0" json:"intervalSeconds"`
	Event           string             `gorm:"column:event;size:32;not null" json:"event"`
	CallID          string             `gorm:"column:call_id;size:255;index:idx_subscription_call_id" json:"-"`
	LocalTag        string             `gorm:"column:local_tag;size:128" json:"-"`
	RemoteTag       string             `gorm:"column:remote_tag;size:128" json:"-"`
	CSeq            uint               `gorm:"column:cseq;not null;default:0" json:"-"`
	ExpiresAt       *time.Time         `gorm:"column:expires_at" json:"expiresAt"`
	LastSubscribeAt *time.Time         `gorm:"column:last_subscribe_at" json:"lastSubscribeAt"`
	LastNotifyAt    *time.Time         `gorm:"column:last_notify_at" json:"lastNotifyAt"`
	NextActionAt    *time.Time         `gorm:"column:next_action_at;index:idx_subscription_due,priority:2" json:"nextActionAt"`
	RetryCount      int                `gorm:"column:retry_count;not null;default:0" json:"retryCount"`
	LastStatusCode  int                `gorm:"column:last_status_code;not null;default:0" json:"lastStatusCode"`
	LastError       string             `gorm:"column:last_error;type:text" json:"lastError"`
	CreatedAt       time.Time          `json:"createdAt"`
	UpdatedAt       time.Time          `json:"updatedAt"`
}

func (GbDeviceSubscription) TableName() string { return "gb_device_subscription" }

// GbMobilePositionLatest stores the most recent valid location reported by one source code.
type GbMobilePositionLatest struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	DeviceID   uint      `gorm:"column:device_id;not null;uniqueIndex:uk_position_device_source" json:"deviceId"`
	SourceCode string    `gorm:"column:source_code;size:20;not null;uniqueIndex:uk_position_device_source" json:"sourceCode"`
	ChannelID  *uint     `gorm:"column:channel_id;index" json:"channelId"`
	EventTime  time.Time `gorm:"column:event_time;not null;index" json:"eventTime"`
	ReceivedAt time.Time `gorm:"column:received_at;not null" json:"receivedAt"`
	Longitude  float64   `gorm:"column:longitude;not null" json:"longitude"`
	Latitude   float64   `gorm:"column:latitude;not null" json:"latitude"`
	Speed      *float64  `gorm:"column:speed" json:"speed"`
	Direction  *float64  `gorm:"column:direction" json:"direction"`
	Altitude   *float64  `gorm:"column:altitude" json:"altitude"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func (GbMobilePositionLatest) TableName() string { return "gb_mobile_position_latest" }

// GbMobilePositionHistory stores one accepted mobile-position notification for later trajectory queries.
type GbMobilePositionHistory struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`
	DeviceID   uint      `gorm:"column:device_id;not null;index:idx_position_history_device_source_time,priority:1" json:"deviceId"`
	SourceCode string    `gorm:"column:source_code;size:20;not null;index:idx_position_history_device_source_time,priority:2" json:"sourceCode"`
	ChannelID  *uint     `gorm:"column:channel_id;index:idx_position_history_channel_time,priority:1" json:"channelId"`
	EventTime  time.Time `gorm:"column:event_time;not null;index:idx_position_history_device_source_time,priority:3;index:idx_position_history_channel_time,priority:2" json:"eventTime"`
	ReceivedAt time.Time `gorm:"column:received_at;not null;index" json:"receivedAt"`
	Longitude  float64   `gorm:"column:longitude;not null" json:"longitude"`
	Latitude   float64   `gorm:"column:latitude;not null" json:"latitude"`
	Speed      *float64  `gorm:"column:speed" json:"speed"`
	Direction  *float64  `gorm:"column:direction" json:"direction"`
	Altitude   *float64  `gorm:"column:altitude" json:"altitude"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func (GbMobilePositionHistory) TableName() string { return "gb_mobile_position_history" }

// GbAlarmEvent is an immutable alarm notification with a deterministic dedupe key.
type GbAlarmEvent struct {
	ID             uint64     `gorm:"primaryKey" json:"id"`
	DeviceID       uint       `gorm:"column:device_id;not null;index:idx_alarm_device_time,priority:1" json:"deviceId"`
	ChannelID      *uint      `gorm:"column:channel_id;index" json:"channelId"`
	SourceCode     string     `gorm:"column:source_code;size:20;not null" json:"sourceCode"`
	SN             string     `gorm:"column:sn;size:64" json:"sn"`
	AlarmTime      *time.Time `gorm:"column:alarm_time;index:idx_alarm_device_time,priority:2" json:"alarmTime"`
	Priority       *int       `gorm:"column:priority" json:"priority"`
	Method         *int       `gorm:"column:method" json:"method"`
	AlarmType      *int       `gorm:"column:alarm_type" json:"alarmType"`
	AlarmTypeParam string     `gorm:"column:alarm_type_param;size:255" json:"alarmTypeParam"`
	Description    string     `gorm:"column:description;type:text" json:"description"`
	Longitude      *float64   `gorm:"column:longitude" json:"longitude"`
	Latitude       *float64   `gorm:"column:latitude" json:"latitude"`
	CallID         string     `gorm:"column:call_id;size:255" json:"-"`
	CSeq           string     `gorm:"column:cseq;size:64" json:"-"`
	DedupeKey      string     `gorm:"column:dedupe_key;size:128;not null;uniqueIndex" json:"-"`
	RawDigest      string     `gorm:"column:raw_digest;size:64" json:"rawDigest"`
	RawSummary     string     `gorm:"column:raw_summary;type:text" json:"rawSummary"`
	ReceivedAt     time.Time  `gorm:"column:received_at;not null;index" json:"receivedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

func (GbAlarmEvent) TableName() string { return "gb_alarm_event" }
