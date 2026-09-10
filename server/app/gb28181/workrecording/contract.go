// Package workrecording owns customer work recordings independently of continuous cloud recording.
package workrecording

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidRequest     = errors.New("作业录像请求不合法")
	ErrRequestConflict    = errors.New("请求标识已用于其他作业参数")
	ErrOwnerConflict      = errors.New("录像资源已被其他任务占用")
	ErrVersionConflict    = errors.New("作业录像状态已变化，请重新查询")
	ErrAttributionUnknown = errors.New("录像文件归属待核实")
	ErrBatchInvalid       = errors.New("录像批次请求不合法")
	ErrBatchIncomplete    = errors.New("录像批次尚未全部完成")
)

const (
	StateIdle      = "idle"
	StateStarting  = "starting"
	StateRecording = "recording"
	StateStopping  = "stopping"
	StateStopped   = "stopped"
	StateFailed    = "failed"
	StateUnknown   = "unknown"
	FilePending    = "pending"
	FileFinalizing = "finalizing"
	FileReady      = "ready"
	FileUnknown    = "unknown"
	FormDraft      = "draft"
	FormSubmitted  = "submitted"
)

type StartRequest struct {
	ChannelID uint   `json:"channelId"`
	RequestID string `json:"requestId"`
}

// BatchMaxCameraCount is a defensive bound, not a business limit: a work order
// records every playing channel, which the multi-screen layout caps at 16. It
// exists so a malformed request cannot fan out into an unbounded number of
// recorder claims on one ZLM node.
const BatchMaxCameraCount = 16


type BatchStartRequest struct {
	ChannelIDs []uint `json:"channelIds"`
	RequestID  string `json:"requestId"`
}

// OrderStartRequest is the "fill the work order first, then record" entry point
// and the only creation path the UI uses. The legacy BatchStartRequest stays
// behind /work-recordings/batches until that route is retired.
type OrderStartRequest struct {
	RequestID  string `json:"requestId"`
	ChannelIDs []uint `json:"channelIds"`
	Form       Form   `json:"form"`
}

// Validate rejects the request unless the work order form carries every
// business-required field, because nothing may start recording before the
// operator has filled the order in.
func (r OrderStartRequest) Validate() error {
	if err := (BatchStartRequest{ChannelIDs: r.ChannelIDs, RequestID: r.RequestID}).Validate(); err != nil {
		return err
	}
	return r.Form.Validate()
}


func (r BatchStartRequest) Validate() error {
	if len(r.ChannelIDs) < 1 || len(r.ChannelIDs) > BatchMaxCameraCount || strings.TrimSpace(r.RequestID) == "" || len(r.RequestID) > 128 || strings.TrimSpace(r.RequestID) != r.RequestID {
		return ErrBatchInvalid
	}
	seen := make(map[uint]struct{}, len(r.ChannelIDs))
	for _, id := range r.ChannelIDs {
		if id == 0 {
			return ErrBatchInvalid
		}
		if _, ok := seen[id]; ok {
			return ErrBatchInvalid
		}
		seen[id] = struct{}{}
	}
	return nil
}

type BatchCameraSnapshot struct {
	ChannelID   uint   `json:"channelId"`
	ChannelName string `json:"channelName,omitempty"`
	// ChannelCode and DeviceID are the GB28181 20-digit codes an operator reads
	// off the camera, DeviceName the human label of the owning device. They are
	// resolved in the same batched reads as ChannelName so a camera row can be
	// explained without a per-row lookup.
	ChannelCode string              `json:"channelCode,omitempty"`
	DeviceID    string              `json:"deviceId,omitempty"`
	DeviceName  string              `json:"deviceName,omitempty"`

	JobID       string              `json:"jobId"`
	State       string              `json:"state"`
	FileState   string              `json:"fileState"`
	StartedAt   *time.Time          `json:"startedAt"`
	StoppedAt   *time.Time          `json:"stoppedAt"`
	LastError   string              `json:"lastError,omitempty"`
	Files       []BatchFileSnapshot `json:"files,omitempty"`
}

type BatchFileSnapshot struct {
	ID        uint64     `json:"id"`
	ChannelID uint       `json:"channelId"`
	FileName  string     `json:"fileName"`
	StartTime *time.Time `json:"startTime,omitempty"`
	TimeLen   *float64   `json:"timeLen,omitempty"`
	FileSize  *uint64    `json:"fileSize,omitempty"`
	State     string     `json:"state"`
}

type BatchSnapshot struct {
	ID          string                `json:"id"`
	RequestID   string                `json:"requestId"`
	State       string                `json:"state"`
	FormState   string                `json:"formState"`
	FormVersion uint64                `json:"formVersion"`
	ProjectName string                `json:"projectName,omitempty"`

	Cameras     []BatchCameraSnapshot `json:"cameras"`
	LastError   string                `json:"lastError,omitempty"`
}

// BatchDeleteMaxIDs bounds one delete request so a malformed body cannot ask for
// an unbounded number of row deletions in a single transaction.
const BatchDeleteMaxIDs = 100

type BatchDeleteRequest struct {
	IDs []string `json:"ids"`
}

func (r BatchDeleteRequest) Validate() error {
	if len(r.IDs) < 1 || len(r.IDs) > BatchDeleteMaxIDs {
		return ErrBatchInvalid
	}
	seen := make(map[string]struct{}, len(r.IDs))
	for _, id := range r.IDs {
		if !validJobID(id) {
			return ErrBatchInvalid
		}
		if _, ok := seen[id]; ok {
			return ErrBatchInvalid
		}
		seen[id] = struct{}{}
	}
	return nil
}

// BatchDeleteResult reports what a delete actually did. Recording work orders
// are skipped rather than failed, so a batch that mixes running and finished
// rows still removes everything the operator could safely remove.
type BatchDeleteResult struct {
	Deleted uint64   `json:"deleted"`
	Skipped []string `json:"skipped,omitempty"`
	Missing []string `json:"missing,omitempty"`
}


func (r StartRequest) Validate() error {
	if r.ChannelID == 0 || strings.TrimSpace(r.RequestID) == "" || len(r.RequestID) > 128 || strings.TrimSpace(r.RequestID) != r.RequestID {
		return ErrInvalidRequest
	}
	return nil
}

func (r StartRequest) Matches(other StartRequest) error {
	if r.RequestID != other.RequestID || r.ChannelID != other.ChannelID {
		return ErrRequestConflict
	}
	return nil
}

type Snapshot struct {
	ID            string     `json:"id"`
	ChannelID     uint       `json:"channelId"`
	State         string     `json:"state"`
	Version       uint64     `json:"version"`
	LastCheckedAt *time.Time `json:"lastCheckedAt"`
	StartedAt     *time.Time `json:"startedAt"`
	StoppedAt     *time.Time `json:"stoppedAt"`
	FileState     string     `json:"fileState"`
	FormState     string     `json:"formState"`
	LastError     string     `json:"lastError,omitempty"`
}

func (s Snapshot) CanStart() bool { return s.State == StateIdle }

// FormHistoryEntry 是作业单表单历史值的一条记录。
// 前端 a-auto-complete 的下拉只展示 Value；UseCount/LastUsedAt 留给运维后台排查「为什么这条会被推荐」。
type FormHistoryEntry struct {
	Value      string    `json:"value"`
	UseCount   int       `json:"useCount"`
	LastUsedAt time.Time `json:"lastUsedAt"`
}
