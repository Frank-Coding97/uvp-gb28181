package models

import "time"

type PTZOperationStatus string

const (
	PTZOperationQueued    PTZOperationStatus = "queued"
	PTZOperationSent      PTZOperationStatus = "sent"
	PTZOperationAccepted  PTZOperationStatus = "accepted"
	PTZOperationRejected  PTZOperationStatus = "rejected"
	PTZOperationTimeout   PTZOperationStatus = "timeout"
	PTZOperationCancelled PTZOperationStatus = "cancelled"
	PTZOperationUnknown   PTZOperationStatus = "unknown"
)

// GbPTZOperation is the immutable command audit and correlation record.
type GbPTZOperation struct {
	ID             uint               `gorm:"primaryKey" json:"id"`
	OperationID    string             `gorm:"column:operation_id;size:64;not null;uniqueIndex" json:"operationId"`
	IdempotencyKey string             `gorm:"column:idempotency_key;size:128;not null;uniqueIndex:uk_ptz_operation_idempotency" json:"-"`
	DeviceID       uint               `gorm:"column:device_id;not null;index" json:"deviceId"`
	DeviceCode     string             `gorm:"column:device_code;size:20;not null" json:"deviceCode"`
	ChannelID      uint               `gorm:"column:channel_id;not null;uniqueIndex:uk_ptz_operation_idempotency;index:idx_ptz_operation_channel_time" json:"channelId"`
	ChannelCode    string             `gorm:"column:channel_code;size:20;not null" json:"channelCode"`
	CmdType        string             `gorm:"column:cmd_type;size:64;not null" json:"cmdType"`
	Action         string             `gorm:"column:action;size:64" json:"action"`
	PayloadJSON    string             `gorm:"column:payload_json;type:text" json:"-"`
	SN             int                `gorm:"column:sn;not null;index:idx_ptz_operation_device_sn" json:"sn"`
	CallID         string             `gorm:"column:call_id;size:255;index" json:"-"`
	CSeq           string             `gorm:"column:cseq;size:64" json:"-"`
	SIPStatus      int                `gorm:"column:sip_status" json:"sipStatus"`
	DeviceResult   string             `gorm:"column:device_result;size:32" json:"deviceResult"`
	DeviceError    string             `gorm:"column:device_error;type:text" json:"deviceError"`
	Status         PTZOperationStatus `gorm:"column:status;size:16;not null;index:idx_ptz_operation_status_time" json:"status"`
	Attempt        int                `gorm:"column:attempt;not null;default:1" json:"attempt"`
	ErrorCode      string             `gorm:"column:error_code;size:64" json:"errorCode"`
	ErrorMessage   string             `gorm:"column:error_message;type:text" json:"errorMessage"`
	ActorID        uint               `gorm:"column:actor_id" json:"actorId"`
	ActorDeptID    uint               `gorm:"column:actor_dept_id" json:"actorDeptId"`
	CreatedAt      time.Time          `gorm:"index:idx_ptz_operation_channel_time" json:"createdAt"`
	SentAt         *time.Time         `json:"sentAt"`
	CompletedAt    *time.Time         `json:"completedAt"`
}

func (GbPTZOperation) TableName() string { return "gb_ptz_operation" }

type PTZFreshness string

const (
	PTZFreshnessFresh   PTZFreshness = "fresh"
	PTZFreshnessStale   PTZFreshness = "stale"
	PTZFreshnessUnknown PTZFreshness = "unknown"
)

// GbPTZState stores the most recent valid precise state and home-position data.
type GbPTZState struct {
	ID          uint         `gorm:"primaryKey" json:"id"`
	DeviceID    uint         `gorm:"column:device_id;not null;index" json:"deviceId"`
	ChannelID   uint         `gorm:"column:channel_id;not null;uniqueIndex" json:"channelId"`
	ChannelCode string       `gorm:"column:channel_code;size:20;not null" json:"channelCode"`
	Pan         *float64     `gorm:"column:pan" json:"pan"`
	Tilt        *float64     `gorm:"column:tilt" json:"tilt"`
	Zoom        *float64     `gorm:"column:zoom" json:"zoom"`
	Focus       *float64     `gorm:"column:focus" json:"focus"`
	Iris        *float64     `gorm:"column:iris" json:"iris"`
	HomeEnabled *bool        `gorm:"column:home_enabled" json:"homeEnabled"`
	HomePan     *float64     `gorm:"column:home_pan" json:"homePan"`
	HomeTilt    *float64     `gorm:"column:home_tilt" json:"homeTilt"`
	HomeZoom    *float64     `gorm:"column:home_zoom" json:"homeZoom"`
	DeviceTime  *time.Time   `gorm:"column:device_time" json:"deviceTime"`
	ReceivedAt  time.Time    `gorm:"column:received_at;not null;index" json:"receivedAt"`
	SourceSN    int          `gorm:"column:source_sn" json:"sourceSn"`
	Freshness   PTZFreshness `gorm:"column:freshness;size:16;not null;default:unknown" json:"freshness"`
	DedupeKey   string       `gorm:"column:dedupe_key;size:128" json:"-"`
	RawSummary  string       `gorm:"column:raw_summary;type:text" json:"-"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

func (GbPTZState) TableName() string { return "gb_ptz_state" }

type PTZPresetStatus string

const (
	PTZPresetActive  PTZPresetStatus = "active"
	PTZPresetDeleted PTZPresetStatus = "deleted"
	PTZPresetUnknown PTZPresetStatus = "unknown"
)

type GbPTZPreset struct {
	ID              uint            `gorm:"primaryKey" json:"id"`
	DeviceID        uint            `gorm:"column:device_id;not null;index" json:"deviceId"`
	ChannelID       uint            `gorm:"column:channel_id;not null;uniqueIndex:uk_ptz_preset_channel_number" json:"channelId"`
	PresetID        int             `gorm:"column:preset_id;not null;uniqueIndex:uk_ptz_preset_channel_number" json:"presetId"`
	Name            string          `gorm:"column:name;size:255" json:"name"`
	Status          PTZPresetStatus `gorm:"column:status;size:16;not null;default:unknown" json:"status"`
	LastOperationID string          `gorm:"column:last_operation_id;size:64" json:"lastOperationId"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

func (GbPTZPreset) TableName() string { return "gb_ptz_preset" }

type GbPTZCruiseTrack struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	DeviceID   uint       `gorm:"column:device_id;not null;index" json:"deviceId"`
	ChannelID  uint       `gorm:"column:channel_id;not null;uniqueIndex:uk_ptz_cruise_channel_track" json:"channelId"`
	TrackID    int        `gorm:"column:track_id;not null;uniqueIndex:uk_ptz_cruise_channel_track" json:"trackId"`
	Name       string     `gorm:"column:name;size:255" json:"name"`
	Enabled    *bool      `gorm:"column:enabled" json:"enabled"`
	DetailJSON string     `gorm:"column:detail_json;type:text" json:"detail"`
	RawSummary string     `gorm:"column:raw_summary;type:text" json:"-"`
	DeviceTime *time.Time `gorm:"column:device_time" json:"deviceTime"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

func (GbPTZCruiseTrack) TableName() string { return "gb_ptz_cruise_track" }
