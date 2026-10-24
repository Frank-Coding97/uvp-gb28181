package models

import "time"

type SIPTraceCaptureEndReason string

const (
	SIPTraceCaptureEndManual  SIPTraceCaptureEndReason = "manual"
	SIPTraceCaptureEndTimeout SIPTraceCaptureEndReason = "timeout"
)

type GbSIPTraceCapture struct {
	ID           string                   `gorm:"column:id;type:char(36);primaryKey" json:"id"`
	DeviceID     uint                     `gorm:"column:device_id;not null;index:idx_sip_trace_capture_device_started,priority:1" json:"deviceId"`
	DeviceCode   string                   `gorm:"column:device_code;size:20;not null;index" json:"deviceCode"`
	CreatedBy    uint                     `gorm:"column:created_by;not null;index" json:"createdBy"`
	StartedAt    time.Time                `gorm:"column:started_at;not null;index:idx_sip_trace_capture_device_started,priority:2" json:"startedAt"`
	PlannedEndAt time.Time                `gorm:"column:planned_end_at;not null;index" json:"plannedEndAt"`
	EndedAt      *time.Time               `gorm:"column:ended_at" json:"endedAt"`
	EndReason    SIPTraceCaptureEndReason `gorm:"column:end_reason;size:16" json:"endReason"`
	ActiveKey    *string                  `gorm:"column:active_key;size:64;uniqueIndex:uk_sip_trace_capture_active" json:"-"`
	CreatedAt    time.Time                `json:"createdAt"`
	UpdatedAt    time.Time                `json:"updatedAt"`
}

func (GbSIPTraceCapture) TableName() string { return "gb_sip_trace_capture" }
