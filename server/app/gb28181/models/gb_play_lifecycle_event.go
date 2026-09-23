package models

import "time"

// GbPlayLifecycleEvent is an immutable, append-only fact for one playback lifecycle.
type GbPlayLifecycleEvent struct {
	ID            uint64    `gorm:"primaryKey" json:"id"`
	EventID       string    `gorm:"column:event_id;size:64;not null;uniqueIndex:uk_play_lifecycle_event_id" json:"eventId"`
	LifecycleID   string    `gorm:"column:lifecycle_id;size:64;not null;index:idx_play_lifecycle_event_lifecycle_sequence,priority:1;uniqueIndex:uk_play_lifecycle_sequence,priority:1" json:"lifecycleId"`
	Sequence      int64     `gorm:"column:sequence;not null;index:idx_play_lifecycle_event_lifecycle_sequence,priority:2;uniqueIndex:uk_play_lifecycle_sequence,priority:2" json:"sequence"`
	EventAt       time.Time `gorm:"column:event_at;not null;index:idx_play_lifecycle_event_at" json:"eventAt"`
	ElapsedMS     int64     `gorm:"column:elapsed_ms;not null;default:0" json:"elapsedMs"`
	Stage         string    `gorm:"column:stage;size:32;not null" json:"stage"`
	EventName     string    `gorm:"column:event_name;size:64;not null" json:"eventName"`
	FactState     string    `gorm:"column:fact_state;size:32;not null" json:"factState"`
	Source        string    `gorm:"column:source;size:32;not null" json:"source"`
	DeviceCode    string    `gorm:"column:device_code;size:20;not null;index:idx_play_lifecycle_event_device_at,priority:1" json:"deviceCode"`
	ChannelCode   string    `gorm:"column:channel_code;size:20;not null" json:"channelCode"`
	StreamID      string    `gorm:"column:stream_id;size:128;not null;default:''" json:"streamId"`
	NodeID        int64     `gorm:"column:node_id;not null;default:0" json:"nodeId"`
	SSRC          string    `gorm:"column:ssrc;size:32;not null;default:''" json:"ssrc"`
	Reused        bool      `gorm:"column:reused;not null;default:false" json:"reused"`
	CallID        string    `gorm:"column:call_id;size:128;not null;default:''" json:"callId"`
	CSeq          string    `gorm:"column:cseq;size:32;not null;default:''" json:"cseq"`
	ReasonCode    string    `gorm:"column:reason_code;size:64;not null;default:''" json:"reasonCode"`
	ReasonMessage string    `gorm:"column:reason_message;size:256;not null;default:''" json:"reasonMessage"`
	MetadataJSON  []byte    `gorm:"column:metadata_json;type:json" json:"metadata,omitempty"`
	CreatedAt     time.Time `gorm:"column:created_at;not null" json:"createdAt"`
}

func (GbPlayLifecycleEvent) TableName() string { return "gb_play_lifecycle_event" }
