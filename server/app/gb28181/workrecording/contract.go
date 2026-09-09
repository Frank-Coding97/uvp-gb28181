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
