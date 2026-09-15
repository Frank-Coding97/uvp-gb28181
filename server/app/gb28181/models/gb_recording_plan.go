package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	RecordingModeOff        = "off"
	RecordingModeContinuous = "continuous"
	RecordingModeScheduled  = "scheduled"
)

const (
	RecordingDesiredIdle      = "idle"
	RecordingDesiredRecording = "recording"
)

const (
	RecordingStateIdle            = "idle"
	RecordingStateOutsideSchedule = "outside_schedule"
	RecordingStateWaitingDevice   = "waiting_device"
	RecordingStateStartingStream  = "starting_stream"
	RecordingStateWaitingMedia    = "waiting_media"
	RecordingStateStartingRecord  = "starting_record"
	RecordingStateRecording       = "recording"
	RecordingStateRecovering      = "recovering"
	RecordingStateStopping        = "stopping"
	RecordingStateFailed          = "failed"
)

type GbRecordingPlan struct {
	ID          uint64         `gorm:"primaryKey" json:"id"`
	Name        string         `gorm:"size:128;not null" json:"name"`
	Description string         `gorm:"size:500;not null;default:''" json:"description"`
	Status      int8           `gorm:"not null;default:1" json:"status"`
	Version     uint64         `gorm:"not null;default:1" json:"version"`
	OwnerDeptID uint           `gorm:"column:owner_dept_id;not null;index" json:"ownerDeptId"`
	CreatedBy   uint           `gorm:"column:created_by;not null;default:0" json:"createdBy"`
	UpdatedBy   uint           `gorm:"column:updated_by;not null;default:0" json:"updatedBy"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (GbRecordingPlan) TableName() string { return "gb_recording_plan" }

type GbRecordingPlanPeriod struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	PlanID    uint64    `gorm:"column:plan_id;not null;index" json:"planId"`
	Weekday   int8      `gorm:"not null" json:"weekday"`
	StartSlot int16     `gorm:"column:start_slot;not null" json:"startSlot"`
	EndSlot   int16     `gorm:"column:end_slot;not null" json:"endSlot"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (GbRecordingPlanPeriod) TableName() string { return "gb_recording_plan_period" }

type GbRecordingPlanBinding struct {
	ID          uint64    `gorm:"primaryKey" json:"id"`
	PlanID      uint64    `gorm:"column:plan_id;not null;index" json:"planId"`
	ChannelID   uint      `gorm:"column:channel_id;not null;uniqueIndex" json:"channelId"`
	OwnerDeptID uint      `gorm:"column:owner_dept_id;not null;index" json:"ownerDeptId"`
	AssignedBy  uint      `gorm:"column:assigned_by;not null;default:0" json:"assignedBy"`
	AssignedAt  time.Time `gorm:"column:assigned_at;not null" json:"assignedAt"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (GbRecordingPlanBinding) TableName() string { return "gb_recording_plan_binding" }

type GbRecordingPlanChannelState struct {
	ChannelID          uint       `gorm:"column:channel_id;primaryKey" json:"channelId"`
	PlanID             *uint64    `gorm:"column:plan_id;index" json:"planId"`
	PlanVersion        uint64     `gorm:"column:plan_version;not null;default:0" json:"planVersion"`
	DesiredState       string     `gorm:"column:desired_state;size:24;not null" json:"desiredState"`
	ActualState        string     `gorm:"column:actual_state;size:32;not null" json:"actualState"`
	ReasonCode         string     `gorm:"column:reason_code;size:64;not null;default:''" json:"reasonCode"`
	ReasonMessage      string     `gorm:"column:reason_message;size:500;not null;default:''" json:"reasonMessage"`
	NextTransitionAt   *time.Time `gorm:"column:next_transition_at" json:"nextTransitionAt"`
	NextRetryAt        *time.Time `gorm:"column:next_retry_at;index" json:"nextRetryAt"`
	ReconcileAt        time.Time  `gorm:"column:reconcile_at;not null;index" json:"reconcileAt"`
	AttemptCount       int        `gorm:"column:attempt_count;not null;default:0" json:"attemptCount"`
	Generation         uint64     `gorm:"not null;default:0" json:"generation"`
	StreamID           string     `gorm:"column:stream_id;size:64;not null;default:''" json:"streamId"`
	RecordingSessionID *uint64    `gorm:"column:recording_session_id" json:"recordingSessionId"`
	NodeID             string     `gorm:"column:node_id;size:64;not null;default:''" json:"nodeId"`
	LastMediaAt        *time.Time `gorm:"column:last_media_at" json:"lastMediaAt"`
	LastSuccessAt      *time.Time `gorm:"column:last_success_at" json:"lastSuccessAt"`
	LeaseOwner         string     `gorm:"column:lease_owner;size:128;not null;default:''" json:"leaseOwner"`
	LeaseUntil         *time.Time `gorm:"column:lease_until" json:"leaseUntil"`
	StateVersion       uint64     `gorm:"column:state_version;not null;default:0" json:"stateVersion"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

func (GbRecordingPlanChannelState) TableName() string { return "gb_recording_plan_channel_state" }

type GbRecordingPlanExecution struct {
	ID                 uint64     `gorm:"primaryKey" json:"id"`
	PlanID             *uint64    `gorm:"column:plan_id;index" json:"planId"`
	ChannelID          uint       `gorm:"column:channel_id;not null;index" json:"channelId"`
	DeviceID           string     `gorm:"column:device_id;size:20;not null;default:''" json:"deviceId"`
	Action             string     `gorm:"size:32;not null" json:"action"`
	TriggerSource      string     `gorm:"column:trigger_source;size:32;not null" json:"triggerSource"`
	Stage              string     `gorm:"size:32;not null;default:''" json:"stage"`
	Attempt            int        `gorm:"not null;default:1" json:"attempt"`
	Result             string     `gorm:"size:24;not null" json:"result"`
	ReasonCode         string     `gorm:"column:reason_code;size:64;not null;default:''" json:"reasonCode"`
	ReasonMessage      string     `gorm:"column:reason_message;size:500;not null;default:''" json:"reasonMessage"`
	StreamID           string     `gorm:"column:stream_id;size:64;not null;default:''" json:"streamId"`
	NodeID             string     `gorm:"column:node_id;size:64;not null;default:''" json:"nodeId"`
	RecordingSessionID *uint64    `gorm:"column:recording_session_id" json:"recordingSessionId"`
	Generation         uint64     `gorm:"not null;default:0" json:"generation"`
	StartedAt          time.Time  `gorm:"column:started_at;not null" json:"startedAt"`
	EndedAt            *time.Time `gorm:"column:ended_at" json:"endedAt"`
	DurationMs         int64      `gorm:"column:duration_ms;not null;default:0" json:"durationMs"`
	CreatedAt          time.Time  `json:"createdAt"`
}

func (GbRecordingPlanExecution) TableName() string { return "gb_recording_plan_execution" }

type GbRecordingPlanGap struct {
	ID            uint64     `gorm:"primaryKey" json:"id"`
	PlanID        *uint64    `gorm:"column:plan_id;index" json:"planId"`
	ChannelID     uint       `gorm:"column:channel_id;not null;index" json:"channelId"`
	StartedAt     time.Time  `gorm:"column:started_at;not null" json:"startedAt"`
	EndedAt       *time.Time `gorm:"column:ended_at" json:"endedAt"`
	DurationMs    int64      `gorm:"column:duration_ms;not null;default:0" json:"durationMs"`
	ReasonCode    string     `gorm:"column:reason_code;size:64;not null" json:"reasonCode"`
	ReasonMessage string     `gorm:"column:reason_message;size:500;not null;default:''" json:"reasonMessage"`
	Recovered     bool       `gorm:"not null;default:false" json:"recovered"`
	ExecutionID   *uint64    `gorm:"column:execution_id" json:"executionId"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

func (GbRecordingPlanGap) TableName() string { return "gb_recording_plan_gap" }
