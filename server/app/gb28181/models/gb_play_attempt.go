package models

import "time"

type GbPlayAttempt struct {
	ID                 uint64     `gorm:"primaryKey" json:"id"`
	CorrelationID      string     `gorm:"column:correlation_id;size:64;not null;uniqueIndex:uk_play_attempt_correlation" json:"correlationId"`
	UserID             uint       `gorm:"column:user_id;not null;index:idx_play_attempt_user_started,priority:1" json:"-"`
	DeviceCode         string     `gorm:"column:device_code;size:20;not null;index:idx_play_attempt_device_started,priority:1" json:"deviceCode"`
	ChannelCode        string     `gorm:"column:channel_code;size:20;not null" json:"channelCode"`
	NodeID             int64      `gorm:"column:node_id;not null;default:0" json:"nodeId"`
	Reused             bool       `gorm:"column:reused;not null;default:false" json:"reused"`
	StreamID           string     `gorm:"column:stream_id;size:128;not null;default:''" json:"streamId"`
	SSRC               string     `gorm:"column:ssrc;size:32;not null;default:''" json:"ssrc"`
	CallID             string     `gorm:"column:call_id;size:128;not null;default:''" json:"callId"`
	CSeq               string     `gorm:"column:cseq;size:32;not null;default:''" json:"cseq"`
	CurrentStage       string     `gorm:"column:current_stage;size:32;not null;default:'unknown'" json:"currentStage"`
	MediaState         string     `gorm:"column:media_state;size:32;not null;default:'unknown'" json:"mediaState"`
	ClientState        string     `gorm:"column:client_state;size:32;not null;default:'unknown'" json:"clientState"`
	LifecycleState     string     `gorm:"column:lifecycle_state;size:32;not null;default:'in_progress'" json:"lifecycleState"`
	Outcome            string     `gorm:"column:outcome;size:32;not null;index:idx_play_attempt_outcome_started,priority:1" json:"outcome"`
	FailureStage       string     `gorm:"column:failure_stage;size:32;not null;default:''" json:"failureStage,omitempty"`
	ReasonCode         string     `gorm:"column:reason_code;size:64;not null;default:''" json:"reasonCode,omitempty"`
	ReasonMessage      string     `gorm:"column:reason_message;size:256;not null;default:''" json:"reasonMessage,omitempty"`
	ClientFirstFrameAt *time.Time `gorm:"column:client_first_frame_at" json:"clientFirstFrameAt,omitempty"`
	ClientErrorAt      *time.Time `gorm:"column:client_error_at" json:"clientErrorAt,omitempty"`
	ClientErrorCode    string     `gorm:"column:client_error_code;size:64;not null;default:''" json:"clientErrorCode,omitempty"`
	StartedAt          time.Time  `gorm:"column:started_at;not null;index:idx_play_attempt_user_started,priority:2;index:idx_play_attempt_device_started,priority:2;index:idx_play_attempt_outcome_started,priority:2" json:"startedAt"`
	LastEventAt        *time.Time `gorm:"column:last_event_at" json:"lastEventAt,omitempty"`
	FinishedAt         *time.Time `gorm:"column:finished_at" json:"finishedAt,omitempty"`
	CreatedAt          time.Time  `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (GbPlayAttempt) TableName() string { return "gb_play_attempt" }
