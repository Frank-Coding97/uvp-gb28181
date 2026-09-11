package playback

import (
	"context"
	"errors"
	"maps"
	"time"
)

var (
	ErrPlaybackNotFound  = errors.New("playback session not found")
	ErrPlaybackBusy      = errors.New("playback session already active")
	ErrRegistryClosed    = errors.New("playback registry closed")
	ErrInvalidTransition = errors.New("invalid playback state transition")
	ErrInvalidSession    = errors.New("invalid playback session")
)

type ServiceError struct {
	Stage string
	Code  string
	Err   error
}

func (e *ServiceError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Stage + ": " + e.Code
	}
	return e.Stage + ": " + e.Err.Error()
}

func (e *ServiceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type State string

type Mode string

const (
	ModePlayback Mode = "playback"
	ModeDownload Mode = "download"
)

const (
	StateCreating  State = "creating"
	StateBuffering State = "buffering"
	StatePlaying   State = "playing"
	StatePaused    State = "paused"
	StateEnded     State = "ended"
	StateFailed    State = "failed"
	StateStopping  State = "stopping"
	StateStopped   State = "stopped"
)

func (s State) IsTerminal() bool {
	return s == StateEnded || s == StateFailed || s == StateStopped
}

type CleanupResources interface {
	Teardown(context.Context) error
	CloseRTP(context.Context) error
	Unbind(context.Context) error
}

type Session struct {
	ID              string
	OwnerID         string
	DeviceID        string
	ChannelID       string
	RecordKey       string
	Mode            Mode
	DownloadSpeed   uint32
	IdempotencyKey  string
	NodeID          string
	StreamID        string
	SSRC            string
	CallID          string
	MediaURLs       map[string]string
	DefaultProtocol string
	Protocol        string
	URL             string
	ZLMWebRTC       bool
	HasAudio        bool
	SegmentStart    time.Time
	SegmentEnd      time.Time
	PlayFrom        time.Time
	PositionSeconds float64
	Scale           float64
	State           State
	EndReason       string
	Error           error
	ErrorStage      string
	ErrorCode       string
	CreatedAt       time.Time
	LastActivityAt  time.Time
	IdleDeadline    time.Time
	Deadline        time.Time
	Resources       CleanupResources
}

type CreateRequest struct {
	OwnerID, DeviceID, ChannelID, SIPChannelID        string
	RecordKey, IdempotencyKey, Destination, Transport string
	PreferredNodeID                                   int64
	SegmentStart, SegmentEnd                          time.Time
	PlayFrom                                          time.Time
	Mode                                              Mode
	DownloadSpeed                                     uint32
	TCPMode                                           bool
	DefaultProtocol                                   string
	Secure                                            bool
	Now                                               time.Time
	Resources                                         CleanupResources
}

type CreateResult struct {
	Session  *Session
	Existing bool
}

type ActionRequest struct {
	Action          string
	PositionSeconds float64
	Scale           float64
}

type RegistryConfig struct {
	Now         func() time.Time
	IdleTimeout time.Duration
	MaxSession  time.Duration
	// TerminalTTL 终态会话在内存中的保留时长,超龄后从 sessions 表删除
	TerminalTTL time.Duration
}

func (s Session) clone() *Session {
	if s.MediaURLs != nil {
		s.MediaURLs = maps.Clone(s.MediaURLs)
	}
	return &s
}
