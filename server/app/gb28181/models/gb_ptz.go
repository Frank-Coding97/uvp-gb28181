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
	ID                   uint               `gorm:"primaryKey;index:idx_ptz_operation_channel_cmd_id,priority:3" json:"id"`
	OperationID          string             `gorm:"column:operation_id;size:64;not null;uniqueIndex:uk_ptz_operation_id" json:"operationId"`
	IdempotencyKey       string             `gorm:"column:idempotency_key;size:128;not null;uniqueIndex:uk_ptz_operation_idempotency,priority:2" json:"-"`
	DeviceID             uint               `gorm:"column:device_id;not null;index:idx_ptz_operation_device_sn,priority:1" json:"deviceId"`
	DeviceCode           string             `gorm:"column:device_code;size:20;not null" json:"deviceCode"`
	ChannelID            uint               `gorm:"column:channel_id;not null;uniqueIndex:uk_ptz_operation_idempotency,priority:1;index:idx_ptz_operation_channel_time,priority:1;index:idx_ptz_operation_channel_cmd_id,priority:1" json:"channelId"`
	ChannelCode          string             `gorm:"column:channel_code;size:20;not null" json:"channelCode"`
	CmdType              string             `gorm:"column:cmd_type;size:64;not null;index:idx_ptz_operation_channel_cmd_id,priority:2" json:"cmdType"`
	Action               string             `gorm:"column:action;size:64" json:"action"`
	PayloadJSON          string             `gorm:"column:payload_json;type:text" json:"-"`
	SN                   int                `gorm:"column:sn;not null;index:idx_ptz_operation_device_sn,priority:2" json:"sn"`
	CallID               string             `gorm:"column:call_id;size:255;index:idx_ptz_operation_call_id" json:"-"`
	CSeq                 string             `gorm:"column:cseq;size:64" json:"-"`
	SIPStatus            int                `gorm:"column:sip_status;not null;default:0" json:"sipStatus"`
	DeviceResult         string             `gorm:"column:device_result;size:32" json:"deviceResult"`
	DeviceError          string             `gorm:"column:device_error;type:text" json:"deviceError"`
	Status               PTZOperationStatus `gorm:"column:status;size:16;not null;index:idx_ptz_operation_status_time,priority:1;index:idx_ptz_operation_status_next_attempt,priority:1;index:idx_ptz_operation_status_queue_deadline,priority:1;index:idx_ptz_operation_status_transport_deadline,priority:1;index:idx_ptz_operation_status_deadline,priority:1" json:"status"`
	Attempt              int                `gorm:"column:attempt;not null;default:1" json:"attempt"`
	ResponseRequired     bool               `gorm:"column:response_required;not null;default:false" json:"responseRequired"`
	MaxAttempts          int                `gorm:"column:max_attempts;not null;default:1" json:"maxAttempts"`
	ErrorCode            string             `gorm:"column:error_code;size:64" json:"errorCode"`
	ErrorMessage         string             `gorm:"column:error_message;type:text" json:"errorMessage"`
	ActorID              uint               `gorm:"column:actor_id;not null;default:0" json:"actorId"`
	ActorDeptID          uint               `gorm:"column:actor_dept_id;not null;default:0" json:"actorDeptId"`
	CreatedAt            time.Time          `gorm:"column:created_at;not null;index:idx_ptz_operation_channel_time,priority:2;index:idx_ptz_operation_status_time,priority:2" json:"createdAt"`
	SentAt               *time.Time         `json:"sentAt"`
	CompletedAt          *time.Time         `json:"completedAt"`
	QueueDeadlineAt      *time.Time         `gorm:"column:queue_deadline_at;index:idx_ptz_operation_status_queue_deadline,priority:2" json:"queueDeadlineAt"`
	DispatchStartedAt    *time.Time         `gorm:"column:dispatch_started_at" json:"dispatchStartedAt"`
	TransportDeadlineAt  *time.Time         `gorm:"column:transport_deadline_at;index:idx_ptz_operation_status_transport_deadline,priority:2" json:"transportDeadlineAt"`
	DeadlineAt           *time.Time         `gorm:"column:deadline_at;index:idx_ptz_operation_status_deadline,priority:2" json:"deadlineAt"`
	NextAttemptAt        *time.Time         `gorm:"column:next_attempt_at;index:idx_ptz_operation_status_next_attempt,priority:2" json:"nextAttemptAt"`
	ResponseCallID       *string            `gorm:"column:response_call_id;size:255" json:"responseCallId"`
	ResponseCSeq         *string            `gorm:"column:response_cseq;size:64" json:"responseCseq"`
	ResponseAt           *time.Time         `gorm:"column:response_at" json:"responseAt"`
	ResponseHasData      *bool              `gorm:"column:response_has_data" json:"responseHasData"`
	TriggerOperationID   *string            `gorm:"column:trigger_operation_id;size:64" json:"triggerOperationId"`
	ReconcileOperationID *string            `gorm:"column:reconcile_operation_id;size:64" json:"reconcileOperationId"`
}

func (GbPTZOperation) TableName() string { return "gb_ptz_operation" }

type PTZFreshness string

const (
	PTZFreshnessFresh   PTZFreshness = "fresh"
	PTZFreshnessStale   PTZFreshness = "stale"
	PTZFreshnessUnknown PTZFreshness = "unknown"
)

// GbPTZState stores the most recent valid precise state.
type GbPTZState struct {
	ID          uint         `gorm:"primaryKey" json:"id"`
	DeviceID    uint         `gorm:"column:device_id;not null;index:idx_ptz_state_device" json:"deviceId"`
	ChannelID   uint         `gorm:"column:channel_id;not null;uniqueIndex:uk_ptz_state_channel" json:"channelId"`
	ChannelCode string       `gorm:"column:channel_code;size:20;not null" json:"channelCode"`
	Pan         *float64     `gorm:"column:pan" json:"pan"`
	Tilt        *float64     `gorm:"column:tilt" json:"tilt"`
	Zoom        *float64     `gorm:"column:zoom" json:"zoom"`
	Focus       *float64     `gorm:"column:focus" json:"focus"`
	Iris        *float64     `gorm:"column:iris" json:"iris"`
	DeviceTime  *time.Time   `gorm:"column:device_time" json:"deviceTime"`
	ReceivedAt  time.Time    `gorm:"column:received_at;not null;index:idx_ptz_state_received" json:"receivedAt"`
	SourceSN    int          `gorm:"column:source_sn;not null;default:0" json:"sourceSn"`
	Freshness   PTZFreshness `gorm:"column:freshness;size:16;not null;default:unknown" json:"freshness"`
	DedupeKey   string       `gorm:"column:dedupe_key;size:128" json:"-"`
	RawSummary  string       `gorm:"column:raw_summary;type:text" json:"-"`
	CreatedAt   time.Time    `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt   time.Time    `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (GbPTZState) TableName() string { return "gb_ptz_state" }

type PTZHomePositionEnabledEncoding string

const (
	PTZHomePositionEnabledNumeric           PTZHomePositionEnabledEncoding = "numeric"
	PTZHomePositionEnabledCompatBooleanText PTZHomePositionEnabledEncoding = "compat_boolean_text"
)

type PTZHomePositionSource string

const (
	PTZHomePositionSourceDeviceQuery   PTZHomePositionSource = "device_query"
	PTZHomePositionSourceControlACK    PTZHomePositionSource = "control_ack"
	PTZHomePositionSourceLegacyProfile PTZHomePositionSource = "legacy_profile"
)

type PTZHomePositionVerification string

const (
	PTZHomePositionVerificationVerified   PTZHomePositionVerification = "verified"
	PTZHomePositionVerificationUnverified PTZHomePositionVerification = "unverified"
)

type GbPTZHomePosition struct {
	ID                 uint                           `gorm:"primaryKey" json:"id"`
	DeviceID           uint                           `gorm:"column:device_id;not null;index:idx_ptz_home_position_device" json:"deviceId"`
	ChannelID          uint                           `gorm:"column:channel_id;not null;uniqueIndex:uk_ptz_home_position_channel" json:"channelId"`
	ChannelCode        string                         `gorm:"column:channel_code;size:20;not null" json:"channelCode"`
	Enabled            bool                           `gorm:"column:enabled;not null" json:"enabled"`
	ResetTime          *int                           `gorm:"column:reset_time" json:"resetTime"`
	PresetID           *int                           `gorm:"column:preset_id" json:"presetId"`
	EnabledEncoding    PTZHomePositionEnabledEncoding `gorm:"column:enabled_encoding;size:32;not null;default:numeric" json:"enabledEncoding"`
	ConfirmedAt        time.Time                      `gorm:"column:confirmed_at;not null" json:"confirmedAt"`
	Source             PTZHomePositionSource          `gorm:"column:source;size:32;not null" json:"source"`
	Verification       PTZHomePositionVerification    `gorm:"column:verification;size:16;not null" json:"verification"`
	SourceSN           int                            `gorm:"column:source_sn;not null;default:0" json:"sourceSn"`
	SourceOperationID  *string                        `gorm:"column:source_operation_id;size:64" json:"sourceOperationId"`
	SourceOperationSeq uint                           `gorm:"column:source_operation_seq;not null;default:0" json:"sourceOperationSeq"`
	RawSummary         string                         `gorm:"column:raw_summary;type:text" json:"-"`
	CreatedAt          time.Time                      `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt          time.Time                      `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (GbPTZHomePosition) TableName() string { return "gb_ptz_home_position" }

type PTZOperationAttemptStatus string

const (
	PTZOperationAttemptDispatching PTZOperationAttemptStatus = "dispatching"
	PTZOperationAttemptSent        PTZOperationAttemptStatus = "sent"
	PTZOperationAttemptFailed      PTZOperationAttemptStatus = "failed"
	PTZOperationAttemptUnknown     PTZOperationAttemptStatus = "unknown"
)

type GbPTZOperationAttempt struct {
	ID           uint                      `gorm:"primaryKey" json:"id"`
	OperationID  uint                      `gorm:"column:operation_id;not null;uniqueIndex:uk_ptz_operation_attempt,priority:1" json:"operationId"`
	AttemptNo    int                       `gorm:"column:attempt_no;not null;uniqueIndex:uk_ptz_operation_attempt,priority:2" json:"attemptNo"`
	SN           int                       `gorm:"column:sn;not null" json:"sn"`
	Status       PTZOperationAttemptStatus `gorm:"column:status;size:16;not null;index:idx_ptz_attempt_status_lease,priority:1" json:"status"`
	CallID       string                    `gorm:"column:call_id;size:255" json:"callId"`
	CSeq         string                    `gorm:"column:cseq;size:64" json:"cseq"`
	SIPStatus    int                       `gorm:"column:sip_status;not null;default:0" json:"sipStatus"`
	StartedAt    time.Time                 `gorm:"column:started_at;not null" json:"startedAt"`
	LeaseUntil   time.Time                 `gorm:"column:lease_until;not null;index:idx_ptz_attempt_status_lease,priority:2" json:"leaseUntil"`
	SentAt       *time.Time                `gorm:"column:sent_at" json:"sentAt"`
	CompletedAt  *time.Time                `gorm:"column:completed_at" json:"completedAt"`
	ErrorCode    string                    `gorm:"column:error_code;size:64" json:"errorCode"`
	ErrorMessage string                    `gorm:"column:error_message;type:text" json:"errorMessage"`
	CreatedAt    time.Time                 `gorm:"column:created_at;not null" json:"createdAt"`
}

func (GbPTZOperationAttempt) TableName() string { return "gb_ptz_operation_attempt" }

type PTZPresetStatus string

const (
	PTZPresetActive  PTZPresetStatus = "active"
	PTZPresetDeleted PTZPresetStatus = "deleted"
	PTZPresetUnknown PTZPresetStatus = "unknown"
)

type GbPTZPreset struct {
	ID              uint            `gorm:"primaryKey" json:"id"`
	DeviceID        uint            `gorm:"column:device_id;not null;index:idx_ptz_preset_device" json:"deviceId"`
	ChannelID       uint            `gorm:"column:channel_id;not null;uniqueIndex:uk_ptz_preset_channel_number" json:"channelId"`
	PresetID        int             `gorm:"column:preset_id;not null;uniqueIndex:uk_ptz_preset_channel_number" json:"presetId"`
	Name            string          `gorm:"column:name;size:255" json:"name"`
	Status          PTZPresetStatus `gorm:"column:status;size:16;not null;default:unknown" json:"status"`
	LastOperationID string          `gorm:"column:last_operation_id;size:64" json:"lastOperationId"`
	CreatedAt       time.Time       `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt       time.Time       `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (GbPTZPreset) TableName() string { return "gb_ptz_preset" }

type GbPTZCruiseTrack struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	DeviceID   uint       `gorm:"column:device_id;not null;index:idx_ptz_cruise_device" json:"deviceId"`
	ChannelID  uint       `gorm:"column:channel_id;not null;uniqueIndex:uk_ptz_cruise_channel_track" json:"channelId"`
	TrackID    int        `gorm:"column:track_id;not null;uniqueIndex:uk_ptz_cruise_channel_track" json:"trackId"`
	Name       string     `gorm:"column:name;size:255" json:"name"`
	Enabled    *bool      `gorm:"column:enabled" json:"enabled"`
	DetailJSON string     `gorm:"column:detail_json;type:text" json:"detail"`
	RawSummary string     `gorm:"column:raw_summary;type:text" json:"-"`
	DeviceTime *time.Time `gorm:"column:device_time" json:"deviceTime"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;not null" json:"updatedAt"`
	CreatedAt  time.Time  `gorm:"column:created_at;not null" json:"createdAt"`
}

func (GbPTZCruiseTrack) TableName() string { return "gb_ptz_cruise_track" }
