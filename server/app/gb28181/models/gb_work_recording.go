package models

import "time"

// GbWorkRecording is one immutable work identity, unlike the media-tuple session
// index which is reused by consecutive recordings of the same stream.
type GbWorkRecording struct {
	ID                   string `gorm:"size:36;primaryKey"`
	BatchID              string `gorm:"column:batch_id;size:36;not null;default:'';index:idx_work_recording_batch"`
	ChannelID            uint   `gorm:"not null;index:idx_work_recording_channel"`
	CreatedBy            uint   `gorm:"not null;uniqueIndex:uk_work_recording_request,priority:1"`
	RequestID            string `gorm:"size:128;not null;uniqueIndex:uk_work_recording_request,priority:2"`
	State                string `gorm:"size:20;not null;index:idx_work_recording_state"`
	DesiredAction        string `gorm:"size:20;not null"`
	Version              uint64 `gorm:"not null"`
	RecorderClaimVersion uint64 `gorm:"column:recorder_claim_version;not null;default:0"`
	NodeID               int64  `gorm:"not null;default:0"`
	VHost                string `gorm:"size:128;not null;default:''"`
	App                  string `gorm:"size:64;not null;default:''"`
	Stream               string `gorm:"size:64;not null;default:''"`
	RecordingRoot        string `gorm:"column:recording_root;size:1024;not null;default:''"`
	Generation           uint64 `gorm:"not null;default:0"`
	StartedAt            *time.Time
	StoppedAt            *time.Time
	LastCheckedAt        *time.Time
	LastError            string `gorm:"size:500;not null;default:''"`
	FileState            string `gorm:"size:20;not null;default:pending"`
	FormState            string `gorm:"size:20;not null;default:draft"`
	FormVersion          uint64 `gorm:"not null;default:0"`
	SchemaVersion        uint   `gorm:"not null;default:1"`
	DeviceID             string `gorm:"size:20;not null;default:''"`
	FormJSON             string `gorm:"column:form_json;type:text;not null"`
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (GbWorkRecording) TableName() string { return "gb_work_recording" }

// GbWorkRecordingBatch is the local ledger for a one-to-four-camera work session.
// The child GbWorkRecording rows retain the existing per-camera recorder
// lifecycle and point back here through BatchID.
type GbWorkRecordingBatch struct {
	ID            string `gorm:"size:36;primaryKey"`
	CreatedBy     uint   `gorm:"not null;uniqueIndex:uk_work_recording_batch_request,priority:1"`
	RequestID     string `gorm:"size:128;not null;uniqueIndex:uk_work_recording_batch_request,priority:2"`
	State         string `gorm:"size:20;not null;index:idx_work_recording_batch_state"`
	Version       uint64 `gorm:"not null;default:1"`
	FormState     string `gorm:"size:20;not null;default:draft"`
	FormVersion   uint64 `gorm:"not null;default:0"`
	SchemaVersion uint   `gorm:"not null;default:1"`
	DeviceID      string `gorm:"size:20;not null;default:''"`
	FormJSON      string `gorm:"column:form_json;type:text;not null"`
	LastError     string `gorm:"size:500;not null;default:''"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (GbWorkRecordingBatch) TableName() string { return "gb_work_recording_batch" }

// GbRecorderClaim holds a channel or media MP4 resource until its owner has
// positively confirmed stopping. File finalization is separate; no TTL
// transfers an unknown owner.
type GbRecorderClaim struct {
	ChannelID     uint   `gorm:"not null;default:0"`
	NodeID        int64  `gorm:"not null;default:0"`
	VHost         string `gorm:"size:128;not null;default:''"`
	App           string `gorm:"size:64;not null;default:''"`
	Stream        string `gorm:"size:64;not null;default:''"`
	RecordingRoot string `gorm:"column:recording_root;size:1024;not null;default:''"`
	ResourceKey   string `gorm:"size:64;primaryKey"`
	OwnerKind     string `gorm:"size:20;not null;index:idx_recorder_claim_owner,priority:1"`
	OwnerID       string `gorm:"size:128;not null;index:idx_recorder_claim_owner,priority:2"`
	State         string `gorm:"size:20;not null"`
	Version       uint64 `gorm:"not null"`
	Generation    uint64 `gorm:"not null;default:0"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (GbRecorderClaim) TableName() string { return "gb_recorder_claim" }

type GbWorkRecordingFile struct {
	FileID          uint64 `gorm:"primaryKey;autoIncrement:false"`
	WorkRecordingID string `gorm:"size:36;not null;index:idx_work_recording_file_job"`
	Evidence        string `gorm:"size:500;not null;default:''"`
	CreatedAt       time.Time
}

func (GbWorkRecordingFile) TableName() string { return "gb_work_recording_file" }

// GbWorkOrderFormHistory 保存作业单表单 4 个字段（项目名称/站区/作业负责人/作业人员）
// 的历史值。提交作业单时 upsert（同一 field+value 累计 use_count + 1），
// 列表接口按 last_used_at desc 拉取，喂给前端的 a-auto-complete 下拉。
type GbWorkOrderFormHistory struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	FieldKey   string    `gorm:"size:64;not null;uniqueIndex:uk_gb_work_order_form_history_field_value" json:"fieldKey"`
	Value      string    `gorm:"size:256;not null;uniqueIndex:uk_gb_work_order_form_history_field_value" json:"value"`
	UseCount   int       `gorm:"not null;default:1" json:"useCount"`
	LastUsedAt time.Time `gorm:"not null" json:"lastUsedAt"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func (GbWorkOrderFormHistory) TableName() string { return "gb_work_order_form_history" }

// FormHistoryField 限定 FieldKey 的合法取值，
// 防止脏数据进入历史库后污染前端的字段白名单。
const (
	FormHistoryFieldProjectName   = "projectName"
	FormHistoryFieldStationArea   = "stationArea"
	FormHistoryFieldWorkLeader    = "workLeader"
	FormHistoryFieldWorkPersonnel = "workPersonnel"
)

// IsValidFormHistoryField 判断 value 是否在 4 个白名单内。
func IsValidFormHistoryField(value string) bool {
	switch value {
	case FormHistoryFieldProjectName, FormHistoryFieldStationArea,
		FormHistoryFieldWorkLeader, FormHistoryFieldWorkPersonnel:
		return true
	}
	return false
}
