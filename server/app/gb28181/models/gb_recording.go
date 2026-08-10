package models

import "time"

const (
	DefaultRecordingVHost = "__defaultVhost__"
	DefaultRecordingApp   = "rtp"

	CloudRecordingStateDisabled  = "disabled"
	CloudRecordingStateStarting  = "starting"
	CloudRecordingStateRecording = "recording"
	CloudRecordingStateWaiting   = "waiting"
	CloudRecordingStateStopping  = "stopping"
	CloudRecordingStateFailed    = "failed"

	RecordingSessionStateStarting  = "starting"
	RecordingSessionStateRecording = "recording"
	RecordingSessionStateStopping  = "stopping"
	RecordingSessionStateStopped   = "stopped"
	RecordingSessionStateFailed    = "failed"

	RecordingFileSourceHook      = "hook"
	RecordingFileSourceReconcile = "reconcile"

	RecordingMetadataComplete = "complete"
	RecordingMetadataPartial  = "partial"

	RecordingReconcileQueued    = "queued"
	RecordingReconcileRunning   = "running"
	RecordingReconcileSucceeded = "succeeded"
	RecordingReconcilePartial   = "partial"
	RecordingReconcileFailed    = "failed"
)

// GbRecordingSession records the ZLM stream tuple used for one cloud-recording run.
// Stopped rows are retained so delayed on_record_mp4 hooks can still resolve a channel.
type GbRecordingSession struct {
	ID            uint64     `gorm:"primaryKey" json:"id"`
	ChannelID     uint       `gorm:"column:channel_id;not null;index:idx_recording_session_channel_state,priority:1" json:"channelId"`
	DeviceID      string     `gorm:"column:device_id;size:20;not null" json:"deviceId"`
	NodeID        int64      `gorm:"column:node_id;not null;uniqueIndex:uk_recording_session_media,priority:1" json:"nodeId"`
	VHost         string     `gorm:"column:vhost;size:128;not null;default:__defaultVhost__;uniqueIndex:uk_recording_session_media,priority:2" json:"vhost"`
	App           string     `gorm:"column:app;size:64;not null;default:rtp;uniqueIndex:uk_recording_session_media,priority:3" json:"app"`
	Stream        string     `gorm:"column:stream;size:64;not null;uniqueIndex:uk_recording_session_media,priority:4" json:"stream"`
	State         string     `gorm:"column:state;size:20;not null;index:idx_recording_session_channel_state,priority:2" json:"state"`
	StartedAt     *time.Time `gorm:"column:started_at" json:"startedAt"`
	StoppedAt     *time.Time `gorm:"column:stopped_at" json:"stoppedAt"`
	LastCheckedAt *time.Time `gorm:"column:last_checked_at" json:"lastCheckedAt"`
	LastError     string     `gorm:"column:last_error;size:500;not null;default:''" json:"lastError"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

func (GbRecordingSession) TableName() string { return "gb_recording_session" }

// GbRecordingFile is the durable index emitted by ZLM's on_record_mp4 hook.
type GbRecordingFile struct {
	ID                 uint64     `gorm:"primaryKey" json:"-"`
	SessionID          *uint64    `gorm:"column:session_id;index" json:"-"`
	ChannelID          uint       `gorm:"column:channel_id;not null;index:idx_recording_file_channel_start,priority:1" json:"-"`
	DeviceID           string     `gorm:"column:device_id;size:20;not null;index:idx_recording_file_device_start,priority:1" json:"-"`
	ChannelCode        string     `gorm:"column:channel_code;size:20;not null;default:''" json:"-"`
	ChannelName        string     `gorm:"column:channel_name;size:255;not null;default:''" json:"-"`
	DeviceName         string     `gorm:"column:device_name;size:255;not null;default:''" json:"-"`
	OwnerDeptID        uint       `gorm:"column:owner_dept_id;not null;default:0;index" json:"-"`
	NodeID             int64      `gorm:"column:node_id;not null" json:"-"`
	VHost              string     `gorm:"column:vhost;size:128;not null" json:"-"`
	App                string     `gorm:"column:app;size:64;not null" json:"-"`
	Stream             string     `gorm:"column:stream;size:64;not null" json:"-"`
	FileKey            string     `gorm:"column:file_key;size:64;not null;uniqueIndex:uk_recording_file_key" json:"-"`
	FileName           string     `gorm:"column:file_name;size:255;not null" json:"-"`
	FilePath           string     `gorm:"column:file_path;size:1000;not null" json:"-"`
	Folder             string     `gorm:"column:folder;size:1000;not null;default:''" json:"-"`
	URL                string     `gorm:"column:url;size:1000;not null;default:''" json:"-"`
	StartTime          *time.Time `gorm:"column:start_time;index:idx_recording_file_channel_start,priority:2;index:idx_recording_file_device_start,priority:2" json:"-"`
	TimeLen            *float64   `gorm:"column:time_len;type:decimal(12,3)" json:"-"`
	FileSize           *uint64    `gorm:"column:file_size" json:"-"`
	Source             string     `gorm:"column:source;size:16;not null;default:hook" json:"-"`
	MetadataState      string     `gorm:"column:metadata_state;size:16;not null;default:complete" json:"-"`
	RecordDate         *time.Time `gorm:"column:record_date;type:date" json:"-"`
	DiscoveredAt       time.Time  `gorm:"column:discovered_at;not null" json:"-"`
	LastSeenAt         *time.Time `gorm:"column:last_seen_at" json:"-"`
	MissingAt          *time.Time `gorm:"column:missing_at" json:"-"`
	ReconcileMissCount int        `gorm:"column:reconcile_miss_count;not null;default:0" json:"-"`
	CreatedAt          time.Time  `json:"-"`
	UpdatedAt          time.Time  `gorm:"column:updated_at" json:"-"`
}

type GbRecordingReconcileState struct {
	NodeID            int64      `gorm:"column:node_id;primaryKey" json:"nodeId"`
	Status            string     `gorm:"column:status;size:16;not null;default:queued" json:"status"`
	TriggerSource     string     `gorm:"column:trigger_source;size:16;not null;default:scheduled" json:"triggerSource"`
	RequestedStart    *time.Time `gorm:"column:requested_start" json:"requestedStart"`
	RequestedEnd      *time.Time `gorm:"column:requested_end" json:"requestedEnd"`
	EffectiveStart    *time.Time `gorm:"column:effective_start" json:"effectiveStart"`
	EffectiveEnd      *time.Time `gorm:"column:effective_end" json:"effectiveEnd"`
	StartedAt         *time.Time `gorm:"column:started_at" json:"startedAt"`
	FinishedAt        *time.Time `gorm:"column:finished_at" json:"finishedAt"`
	CandidateCount    int        `gorm:"column:candidate_count;not null;default:0" json:"candidateCount"`
	SuccessCount      int        `gorm:"column:success_count;not null;default:0" json:"successCount"`
	FailureCount      int        `gorm:"column:failure_count;not null;default:0" json:"failureCount"`
	DiscoveredCount   int        `gorm:"column:discovered_count;not null;default:0" json:"discoveredCount"`
	InsertedCount     int        `gorm:"column:inserted_count;not null;default:0" json:"insertedCount"`
	UpdatedCount      int        `gorm:"column:updated_count;not null;default:0" json:"updatedCount"`
	MissingCount      int        `gorm:"column:missing_count;not null;default:0" json:"missingCount"`
	UnattributedCount int        `gorm:"column:unattributed_count;not null;default:0" json:"unattributedCount"`
	LastError         string     `gorm:"column:last_error;size:500;not null;default:''" json:"lastError"`
	UpdatedAt         time.Time  `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (GbRecordingReconcileState) TableName() string { return "gb_recording_reconcile_state" }

func (GbRecordingFile) TableName() string { return "gb_recording_file" }
