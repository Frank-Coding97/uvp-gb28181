package models

import "time"

// GbDeviceTrafficSession stores one device/channel media accounting session.
// Byte counters are absolute ZLM values or settled deltas and must not be
// represented as floating point numbers.
type GbDeviceTrafficSession struct {
	ID                 uint64     `gorm:"primaryKey" json:"id"`
	BusinessKey        string     `gorm:"column:business_key;size:255;not null;uniqueIndex:uk_traffic_session_business" json:"businessKey"`
	NodeID             int64      `gorm:"column:node_id;not null;index:idx_traffic_session_node_state" json:"nodeId"`
	MediaServerUUID    string     `gorm:"column:media_server_uuid;size:128;not null;default:''" json:"mediaServerUuid"`
	ZLMSessionID       string     `gorm:"column:zlm_session_id;size:128;not null;default:'';index:idx_traffic_session_zlm" json:"zlmSessionId"`
	Direction          string     `gorm:"column:direction;size:16;not null;index:idx_traffic_session_direction" json:"direction"`
	DeviceCode         string     `gorm:"column:device_code;size:64;not null;index:idx_traffic_session_device_started,priority:1" json:"deviceCode"`
	ChannelCode        string     `gorm:"column:channel_code;size:64;not null;default:'';index:idx_traffic_session_channel_started,priority:1;index:idx_traffic_session_device_started,priority:2" json:"channelCode"`
	OwnerDeptID        uint       `gorm:"column:owner_dept_id;not null;default:0;index" json:"ownerDeptId"`
	MediaKind          string     `gorm:"column:media_kind;size:32;not null;default:''" json:"mediaKind"`
	Schema             string     `gorm:"column:schema;size:32;not null;default:''" json:"schema"`
	VHost              string     `gorm:"column:vhost;size:128;not null;default:''" json:"vhost"`
	App                string     `gorm:"column:app;size:64;not null;default:''" json:"app"`
	Stream             string     `gorm:"column:stream;size:255;not null;default:''" json:"stream"`
	CreateStamp        uint64     `gorm:"column:create_stamp;not null;default:0" json:"createStamp"`
	LastTotalBytes     uint64     `gorm:"column:last_total_bytes;not null;default:0" json:"lastTotalBytes"`
	SettledTotalBytes  uint64     `gorm:"column:settled_total_bytes;not null;default:0" json:"settledTotalBytes"`
	DurationSeconds    int64      `gorm:"column:duration_seconds;not null;default:0" json:"durationSeconds"`
	State              string     `gorm:"column:state;size:16;not null;index:idx_traffic_session_node_state,priority:2" json:"state"`
	StartedAt          *time.Time `gorm:"column:started_at;index:idx_traffic_session_channel_started,priority:2;index:idx_traffic_session_device_started,priority:3" json:"startedAt"`
	LastSeenAt         *time.Time `gorm:"column:last_seen_at" json:"lastSeenAt"`
	EndedAt            *time.Time `gorm:"column:ended_at" json:"endedAt"`
	UnattributedReason string     `gorm:"column:unattributed_reason;size:255;not null;default:''" json:"unattributedReason"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

func (GbDeviceTrafficSession) TableName() string { return "gb_device_traffic_session" }

// GbDeviceTrafficDaily is the durable device/channel daily aggregate.
type GbDeviceTrafficDaily struct {
	ID                        uint64    `gorm:"primaryKey" json:"id"`
	StatDate                  time.Time `gorm:"column:stat_date;type:date;not null;uniqueIndex:uk_traffic_daily_scope,priority:1" json:"statDate"`
	DeviceCode                string    `gorm:"column:device_code;size:64;not null;uniqueIndex:uk_traffic_daily_scope,priority:2;index:idx_traffic_daily_device" json:"deviceCode"`
	ChannelCode               string    `gorm:"column:channel_code;size:64;not null;default:'';uniqueIndex:uk_traffic_daily_scope,priority:3" json:"channelCode"`
	OwnerDeptID               uint      `gorm:"column:owner_dept_id;not null;default:0;index" json:"ownerDeptId"`
	UpstreamBytes             uint64    `gorm:"column:upstream_bytes;not null;default:0" json:"upstreamBytes"`
	DownstreamBytes           uint64    `gorm:"column:downstream_bytes;not null;default:0" json:"downstreamBytes"`
	UpstreamDurationSeconds   int64     `gorm:"column:upstream_duration_seconds;not null;default:0" json:"upstreamDurationSeconds"`
	DownstreamDurationSeconds int64     `gorm:"column:downstream_duration_seconds;not null;default:0" json:"downstreamDurationSeconds"`
	UpstreamSessions          int64     `gorm:"column:upstream_sessions;not null;default:0" json:"upstreamSessions"`
	DownstreamSessions        int64     `gorm:"column:downstream_sessions;not null;default:0" json:"downstreamSessions"`
	CreatedAt                 time.Time `json:"createdAt"`
	UpdatedAt                 time.Time `json:"updatedAt"`
}

func (GbDeviceTrafficDaily) TableName() string { return "gb_device_traffic_daily" }

// GbDeviceTrafficGap records periods where the accounting coverage is not
// trustworthy and must not be rendered as zero traffic.
type GbDeviceTrafficGap struct {
	ID        uint64     `gorm:"primaryKey" json:"id"`
	NodeID    int64      `gorm:"column:node_id;not null;index:idx_traffic_gap_node_state,priority:1" json:"nodeId"`
	Reason    string     `gorm:"column:reason;size:32;not null;index:idx_traffic_gap_node_state,priority:2" json:"reason"`
	State     string     `gorm:"column:state;size:16;not null;index:idx_traffic_gap_node_state,priority:3" json:"state"`
	StartedAt time.Time  `gorm:"column:started_at;not null;index" json:"startedAt"`
	EndedAt   *time.Time `gorm:"column:ended_at" json:"endedAt"`
	Detail    string     `gorm:"column:detail;size:500;not null;default:''" json:"detail"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

func (GbDeviceTrafficGap) TableName() string { return "gb_device_traffic_gap" }
