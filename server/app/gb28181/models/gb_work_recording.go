package models

import "time"

// GbWorkRecording is one immutable work identity, unlike the media-tuple session
// index which is reused by consecutive recordings of the same stream.
type GbWorkRecording struct {
	ID            string `gorm:"size:36;primaryKey"`
	ChannelID     uint   `gorm:"not null;index:idx_work_recording_channel"`
	CreatedBy     uint   `gorm:"not null;uniqueIndex:uk_work_recording_request,priority:1"`
	RequestID     string `gorm:"size:128;not null;uniqueIndex:uk_work_recording_request,priority:2"`
	State         string `gorm:"size:20;not null;index:idx_work_recording_state"`
	DesiredAction string `gorm:"size:20;not null"`
	Version       uint64 `gorm:"not null"`
	NodeID        int64  `gorm:"not null;default:0"`
	VHost         string `gorm:"size:128;not null;default:''"`
	App           string `gorm:"size:64;not null;default:''"`
	Stream        string `gorm:"size:64;not null;default:''"`
	Generation    uint64 `gorm:"not null;default:0"`
	StartedAt     *time.Time
	StoppedAt     *time.Time
	LastCheckedAt *time.Time
	LastError     string `gorm:"size:500;not null;default:''"`
	FileState     string `gorm:"size:20;not null;default:pending"`
	FormState     string `gorm:"size:20;not null;default:draft"`
	FormVersion   uint64 `gorm:"not null;default:0"`
	SchemaVersion uint   `gorm:"not null;default:1"`
	DeviceID      string `gorm:"size:20;not null;default:''"`
	FormJSON      string `gorm:"column:form_json;type:text;not null"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (GbWorkRecording) TableName() string { return "gb_work_recording" }

// GbRecorderClaim holds a channel or media MP4 resource until its owner has
// positively resolved stopping/finalization. No TTL transfers an unknown owner.
type GbRecorderClaim struct {
	ResourceKey string `gorm:"size:64;primaryKey"`
	OwnerKind   string `gorm:"size:20;not null;index:idx_recorder_claim_owner,priority:1"`
	OwnerID     string `gorm:"size:128;not null;index:idx_recorder_claim_owner,priority:2"`
	State       string `gorm:"size:20;not null"`
	Version     uint64 `gorm:"not null"`
	Generation  uint64 `gorm:"not null;default:0"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (GbRecorderClaim) TableName() string { return "gb_recorder_claim" }

type GbWorkRecordingFile struct {
	FileID          uint64 `gorm:"primaryKey;autoIncrement:false"`
	WorkRecordingID string `gorm:"size:36;not null;index:idx_work_recording_file_job"`
	Evidence        string `gorm:"size:500;not null;default:''"`
	CreatedAt       time.Time
}

func (GbWorkRecordingFile) TableName() string { return "gb_work_recording_file" }
