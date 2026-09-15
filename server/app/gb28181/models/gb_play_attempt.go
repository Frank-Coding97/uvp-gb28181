package models

import "time"

type GbPlayAttempt struct {
	ID            uint64     `gorm:"primaryKey" json:"id"`
	CorrelationID string     `gorm:"column:correlation_id;size:64;not null;uniqueIndex:uk_play_attempt_correlation" json:"correlationId"`
	UserID        uint       `gorm:"column:user_id;not null;index:idx_play_attempt_user_started,priority:1" json:"-"`
	DeviceCode    string     `gorm:"column:device_code;size:20;not null;index:idx_play_attempt_device_started,priority:1" json:"deviceCode"`
	ChannelCode   string     `gorm:"column:channel_code;size:20;not null" json:"channelCode"`
	NodeID        int64      `gorm:"column:node_id;not null;default:0" json:"nodeId"`
	Reused        bool       `gorm:"column:reused;not null;default:false" json:"reused"`
	Outcome       string     `gorm:"column:outcome;size:32;not null;index:idx_play_attempt_outcome_started,priority:1" json:"outcome"`
	FailureStage  string     `gorm:"column:failure_stage;size:32;not null;default:''" json:"failureStage,omitempty"`
	StartedAt     time.Time  `gorm:"column:started_at;not null;index:idx_play_attempt_user_started,priority:2;index:idx_play_attempt_device_started,priority:2;index:idx_play_attempt_outcome_started,priority:2" json:"startedAt"`
	FinishedAt    *time.Time `gorm:"column:finished_at" json:"finishedAt,omitempty"`
	CreatedAt     time.Time  `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (GbPlayAttempt) TableName() string { return "gb_play_attempt" }
