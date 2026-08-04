package playback

import (
	"context"
	"errors"
	"time"
)

var (
	ErrPlaybackNotFound  = errors.New("playback session not found")
	ErrPlaybackBusy      = errors.New("playback session already active")
	ErrRegistryClosed    = errors.New("playback registry closed")
	ErrInvalidTransition = errors.New("invalid playback state transition")
	ErrInvalidSession    = errors.New("invalid playback session")
)

type State string

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
	ChannelID       string
	RecordKey       string
	IdempotencyKey  string
	SegmentStart    time.Time
	SegmentEnd      time.Time
	PositionSeconds float64
	Scale           float64
	State           State
	EndReason       string
	Error           error
	CreatedAt       time.Time
	LastActivityAt  time.Time
	IdleDeadline    time.Time
	Deadline        time.Time
	Resources       CleanupResources
}

type CreateRequest struct {
	OwnerID, ChannelID, RecordKey, IdempotencyKey string
	SegmentStart, SegmentEnd                      time.Time
	Now                                           time.Time
	Resources                                     CleanupResources
}

type CreateResult struct {
	Session  *Session
	Existing bool
}

type RegistryConfig struct {
	Now         func() time.Time
	IdleTimeout time.Duration
	MaxSession  time.Duration
}

func (s Session) clone() *Session {
	return &s
}
