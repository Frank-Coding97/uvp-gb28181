package recording

import (
	"strconv"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// FileDTO is the public catalog representation of a recording file.
type FileDTO struct {
	ID            string     `json:"id"`
	FileKey       string     `json:"fileKey"`
	ChannelID     string     `json:"channelId"`
	ChannelCode   string     `json:"channelCode"`
	ChannelName   string     `json:"channelName"`
	DeviceID      string     `json:"deviceId"`
	DeviceName    string     `json:"deviceName"`
	Node          NodeDTO    `json:"node"`
	FileName      string     `json:"fileName"`
	StartTime     *time.Time `json:"startTime"`
	EndTime       *time.Time `json:"endTime"`
	TimeLen       *float64   `json:"timeLen"`
	FileSize      *uint64    `json:"fileSize"`
	Source        string     `json:"source"`
	MetadataState string     `json:"metadataState"`
	Availability  string     `json:"availability"`
	RecordDate    *time.Time `json:"recordDate"`
	DiscoveredAt  *time.Time `json:"discoveredAt"`
	LastSeenAt    *time.Time `json:"lastSeenAt"`
	MissingAt     *time.Time `json:"missingAt"`
}

// NodeDTO is the public node reference embedded in a file response.
type NodeDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	State string `json:"state,omitempty"`
}

// AccessDTO is the public result of a recording access grant.
type AccessDTO struct {
	Mode       string    `json:"mode"`
	Capability string    `json:"capability"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type ActiveCatalogSession struct {
	ID          uint64     `json:"-"`
	ChannelID   uint       `json:"-"`
	ChannelCode string     `json:"channelCode"`
	ChannelName string     `json:"channelName"`
	DeviceID    string     `json:"deviceId"`
	NodeID      int64      `json:"-"`
	State       string     `json:"state"`
	StartedAt   *time.Time `json:"startedAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type ActiveSessionDTO struct {
	ID          string     `json:"id"`
	ChannelID   string     `json:"channelId"`
	ChannelCode string     `json:"channelCode"`
	ChannelName string     `json:"channelName"`
	DeviceID    string     `json:"deviceId"`
	Node        NodeDTO    `json:"node"`
	State       string     `json:"state"`
	StartedAt   *time.Time `json:"startedAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type StopActiveSessionResult struct {
	ID        string `json:"id"`
	ChannelID string `json:"channelId"`
	Stopped   bool   `json:"stopped"`
}

type ReconciliationDTO struct {
	Node              NodeDTO    `json:"node"`
	Status            string     `json:"status"`
	TriggerSource     string     `json:"triggerSource"`
	EffectiveStart    *time.Time `json:"effectiveStart"`
	EffectiveEnd      *time.Time `json:"effectiveEnd"`
	StartedAt         *time.Time `json:"startedAt"`
	FinishedAt        *time.Time `json:"finishedAt"`
	CandidateCount    int        `json:"candidateCount"`
	SuccessCount      int        `json:"successCount"`
	FailureCount      int        `json:"failureCount"`
	DiscoveredCount   int        `json:"discoveredCount"`
	InsertedCount     int        `json:"insertedCount"`
	UpdatedCount      int        `json:"updatedCount"`
	MissingCount      int        `json:"missingCount"`
	UnattributedCount int        `json:"unattributedCount"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type CatalogChannelOptionDTO struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type CatalogDeviceOptionDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CatalogOptionsDTO struct {
	Channels []CatalogChannelOptionDTO `json:"channels"`
	Devices  []CatalogDeviceOptionDTO  `json:"devices"`
	Nodes    []NodeDTO                 `json:"nodes"`
}

// NewFileDTO converts the internal GORM model into the public catalog shape.
// Internal paths and raw ZLM URLs intentionally have no representation here.
func NewFileDTO(file models.GbRecordingFile, node NodeDTO, availability string) FileDTO {
	start := cloneTime(file.StartTime)
	end := endTime(file.StartTime, file.TimeLen)
	return FileDTO{
		ID: strconv.FormatUint(file.ID, 10), FileKey: file.FileKey,
		ChannelID: strconv.FormatUint(uint64(file.ChannelID), 10), ChannelCode: file.ChannelCode, ChannelName: file.ChannelName,
		DeviceID: file.DeviceID, DeviceName: file.DeviceName, Node: node, FileName: file.FileName,
		StartTime: start, EndTime: end, TimeLen: cloneFloat(file.TimeLen), FileSize: cloneUint64(file.FileSize),
		Source: file.Source, MetadataState: file.MetadataState, Availability: availability, RecordDate: cloneTime(file.RecordDate),
		DiscoveredAt: cloneTimeValue(file.DiscoveredAt), LastSeenAt: cloneTime(file.LastSeenAt), MissingAt: cloneTime(file.MissingAt),
	}
}

func endTime(start *time.Time, length *float64) *time.Time {
	if start == nil || length == nil {
		return nil
	}
	value := start.Add(time.Duration(*length * float64(time.Second)))
	return &value
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneTimeValue(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneUint64(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
