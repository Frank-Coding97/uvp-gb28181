package diagnosis

import (
	"errors"
	"fmt"
	"time"
)

type Category string

const (
	CategoryRegisterFailure Category = "register_failure"
	CategoryPlayStuck       Category = "play_stuck"
)

type Code string

const (
	CodeDigestFailure         Code = "digest_failure"
	CodeNonceInvalid          Code = "nonce_invalid"
	CodeNonceExpired          Code = "nonce_expired"
	CodeNonceReplay           Code = "nonce_replay"
	CodeNonceStale            Code = "nonce_stale"
	CodeServerIDMismatch      Code = "server_id_mismatch"
	CodeDeviceNotPreallocated Code = "device_not_preallocated"
	CodeInvalidRequest        Code = "invalid_request"
	CodeInternalError         Code = "internal_error"
	CodeRegisterTimeout       Code = "timeout"
	CodeUndetermined          Code = "undetermined"
	CodeSignalingTimeout      Code = "signaling_timeout"
	CodeMediaTimeout          Code = "media_timeout"
)

type Stage string

const (
	StageRegister  Stage = "register"
	StageSignaling Stage = "signaling"
	StageACK       Stage = "ack"
	StageMedia     Stage = "media"
)

type State string

const (
	StateActive   State = "active"
	StateResolved State = "resolved"
)

type Source string

const (
	SourceRuntime       Source = "runtime"
	SourceReconstructed Source = "reconstructed"
)

type Event struct {
	ObservedAt     time.Time
	CorrelationKey string
	State          State
	Category       Category
	Code           Code
	Stage          Stage
	Source         Source
	DeviceID       string
	ChannelID      string
	CallID         string
	CSeq           uint32
	Method         string
	StatusCode     uint16
	StreamID       string
	ResolvedAt     *time.Time
}

type Record struct {
	ID             uint64
	SessionDay     time.Time
	ObservedAt     time.Time
	CorrelationKey string
	State          State
	Category       Category
	Code           Code
	Stage          Stage
	Source         Source
	DeviceID       string
	ChannelID      string
	CallID         string
	CSeq           uint32
	Method         string
	StatusCode     uint16
	StreamID       string
	ResolvedAt     *time.Time
}

func ValidateCategoryCode(category Category, code Code) error {
	valid := false
	switch category {
	case CategoryRegisterFailure:
		switch code {
		case CodeDigestFailure, CodeNonceInvalid, CodeNonceExpired, CodeNonceReplay, CodeNonceStale,
			CodeServerIDMismatch, CodeDeviceNotPreallocated, CodeInvalidRequest,
			CodeInternalError, CodeRegisterTimeout, CodeUndetermined:
			valid = true
		}
	case CategoryPlayStuck:
		valid = code == CodeSignalingTimeout || code == CodeMediaTimeout
	}
	if !valid {
		return fmt.Errorf("invalid diagnosis category/code combination: %q/%q", category, code)
	}
	return nil
}

func (event Event) Validate() error {
	if event.Category == "" {
		return errors.New("category is required")
	}
	if event.Code == "" {
		return errors.New("code is required")
	}
	if err := ValidateCategoryCode(event.Category, event.Code); err != nil {
		return err
	}
	if event.ObservedAt.IsZero() {
		return errors.New("observedAt is required")
	}
	if event.CorrelationKey == "" {
		return errors.New("correlationKey is required")
	}
	if event.State != StateActive && event.State != StateResolved {
		return fmt.Errorf("invalid state %q", event.State)
	}
	if event.Stage == "" {
		return errors.New("stage is required")
	}
	if event.Source != SourceRuntime && event.Source != SourceReconstructed {
		return fmt.Errorf("invalid source %q", event.Source)
	}
	return nil
}

func (record Record) Validate() error {
	return record.Event().Validate()
}

func (record Record) Event() Event {
	return Event{
		ObservedAt: record.ObservedAt, CorrelationKey: record.CorrelationKey,
		State: record.State, Category: record.Category, Code: record.Code,
		Stage: record.Stage, Source: record.Source, DeviceID: record.DeviceID,
		ChannelID: record.ChannelID, CallID: record.CallID, CSeq: record.CSeq,
		Method: record.Method, StatusCode: record.StatusCode, StreamID: record.StreamID,
		ResolvedAt: record.ResolvedAt,
	}
}
