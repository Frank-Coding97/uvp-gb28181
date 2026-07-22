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
	ID        uint64    `gorm:"primaryKey" json:"id"`
	SessionID *uint64   `gorm:"column:session_id;index" json:"sessionId"`
	ChannelID uint      `gorm:"column:channel_id;not null;index:idx_recording_file_channel_start,priority:1" json:"channelId"`
	DeviceID  string    `gorm:"column:device_id;size:20;not null;index:idx_recording_file_device_start,priority:1" json:"deviceId"`
	NodeID    int64     `gorm:"column:node_id;not null;uniqueIndex:uk_recording_file_node_path,priority:1" json:"nodeId"`
	VHost     string    `gorm:"column:vhost;size:128;not null" json:"vhost"`
	App       string    `gorm:"column:app;size:64;not null" json:"app"`
	Stream    string    `gorm:"column:stream;size:64;not null" json:"stream"`
	FileName  string    `gorm:"column:file_name;size:255;not null" json:"fileName"`
	FilePath  string    `gorm:"column:file_path;size:1000;not null;uniqueIndex:uk_recording_file_node_path,priority:2" json:"filePath"`
	Folder    string    `gorm:"column:folder;size:1000;not null;default:''" json:"folder"`
	URL       string    `gorm:"column:url;size:1000;not null;default:''" json:"url"`
	StartTime time.Time `gorm:"column:start_time;not null;index:idx_recording_file_channel_start,priority:2;index:idx_recording_file_device_start,priority:2" json:"startTime"`
	TimeLen   float64   `gorm:"column:time_len;type:decimal(12,3);not null;default:0" json:"timeLen"`
	FileSize  uint64    `gorm:"column:file_size;not null;default:0" json:"fileSize"`
	CreatedAt time.Time `json:"createdAt"`
}

func (GbRecordingFile) TableName() string { return "gb_recording_file" }
