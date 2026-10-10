package models

import "time"

// GbSipTraceSessionDiagnosis stores the latest durable diagnosis for one
// logical SIP attempt. Raw SIP messages remain in GbSipTraceMessage.
type GbSipTraceSessionDiagnosis struct {
	ID             uint64     `gorm:"column:id;primaryKey" json:"id"`
	SessionDay     time.Time  `gorm:"column:session_day;type:date;not null;uniqueIndex:uk_sip_trace_diagnosis_session,priority:1;index:idx_sip_trace_diagnosis_category_state_observed,priority:1" json:"sessionDay"`
	ObservedAt     time.Time  `gorm:"column:observed_at;not null;index:idx_sip_trace_diagnosis_category_state_observed,priority:4;index:idx_sip_trace_diagnosis_device_observed,priority:2" json:"observedAt"`
	CorrelationKey string     `gorm:"column:correlation_key;size:128;not null;uniqueIndex:uk_sip_trace_diagnosis_session,priority:3" json:"correlationKey"`
	State          string     `gorm:"column:state;size:16;not null;default:active;index:idx_sip_trace_diagnosis_category_state_observed,priority:3" json:"state"`
	Category       string     `gorm:"column:category;size:32;not null;uniqueIndex:uk_sip_trace_diagnosis_session,priority:2;index:idx_sip_trace_diagnosis_category_state_observed,priority:2" json:"category"`
	Code           string     `gorm:"column:code;size:64;not null" json:"code"`
	Stage          string     `gorm:"column:stage;size:32;not null" json:"stage"`
	Source         string     `gorm:"column:source;size:32;not null" json:"source"`
	DeviceID       string     `gorm:"column:device_id;size:64;not null;default:'';index:idx_sip_trace_diagnosis_device_observed,priority:1" json:"deviceId"`
	ChannelID      string     `gorm:"column:channel_id;size:64;not null;default:''" json:"channelId"`
	CallID         string     `gorm:"column:call_id;size:255;not null;default:'';index:idx_sip_trace_diagnosis_call_cseq,priority:1" json:"callId"`
	CSeq           uint32     `gorm:"column:cseq;not null;default:0;index:idx_sip_trace_diagnosis_call_cseq,priority:2" json:"cseq"`
	Method         string     `gorm:"column:method;size:32;not null;default:''" json:"method"`
	StatusCode     uint16     `gorm:"column:status_code;not null;default:0" json:"statusCode"`
	StreamID       string     `gorm:"column:stream_id;size:255;not null;default:''" json:"streamId"`
	ResolvedAt     *time.Time `gorm:"column:resolved_at" json:"resolvedAt"`
}

func (GbSipTraceSessionDiagnosis) TableName() string { return "gb_sip_trace_session_diagnosis" }
