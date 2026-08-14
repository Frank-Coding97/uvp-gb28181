package recordquery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type ErrorCode string

const (
	ErrorCodeInvalidArgument ErrorCode = "invalid_argument"
	ErrorCodeBusy            ErrorCode = "busy"
	ErrorCodeUnavailable     ErrorCode = "unavailable"
	ErrorCodeSendFailed      ErrorCode = "send_failed"
	ErrorCodeTimeout         ErrorCode = "timeout"
	ErrorCodeCapacity        ErrorCode = "capacity"
	ErrorCodeSnapshotMissing ErrorCode = "snapshot_not_found"
	ErrorCodeExpired         ErrorCode = "expired"
	ErrorCodeBoundary        ErrorCode = "boundary"
	ErrorCodeTampered        ErrorCode = "tampered"
)

var (
	ErrInvalidArgument  = errors.New("recordquery: invalid argument")
	ErrBusy             = errors.New("recordquery: busy")
	ErrUnavailable      = errors.New("recordquery: unavailable")
	ErrSendFailed       = errors.New("recordquery: send failed")
	ErrTimeout          = errors.New("recordquery: timeout")
	ErrCapacity         = errors.New("recordquery: capacity exceeded")
	ErrSnapshotNotFound = errors.New("recordquery: snapshot not found")
	ErrSnapshotExpired  = errors.New("recordquery: snapshot expired")
	ErrSnapshotBoundary = errors.New("recordquery: play position outside segment")
	ErrSnapshotTampered = errors.New("recordquery: snapshot tampered")
)

type QueryError struct {
	Code ErrorCode
	Err  error
}

func (e *QueryError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.Err)
	}
	return string(e.Code)
}

func (e *QueryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func queryError(code ErrorCode, err error) error {
	if err == nil {
		err = sentinelFor(code)
	}
	return &QueryError{Code: code, Err: err}
}

func sentinelFor(code ErrorCode) error {
	switch code {
	case ErrorCodeInvalidArgument:
		return ErrInvalidArgument
	case ErrorCodeBusy:
		return ErrBusy
	case ErrorCodeUnavailable:
		return ErrUnavailable
	case ErrorCodeSendFailed:
		return ErrSendFailed
	case ErrorCodeTimeout:
		return ErrTimeout
	case ErrorCodeCapacity:
		return ErrCapacity
	case ErrorCodeSnapshotMissing:
		return ErrSnapshotNotFound
	case ErrorCodeExpired:
		return ErrSnapshotExpired
	case ErrorCodeBoundary:
		return ErrSnapshotBoundary
	case ErrorCodeTampered:
		return ErrSnapshotTampered
	default:
		return errors.New(string(code))
	}
}

type QueryStatus string

const (
	QueryStatusComplete    QueryStatus = "complete"
	QueryStatusEmpty       QueryStatus = "empty"
	QueryStatusPartial     QueryStatus = "partial"
	QueryStatusTimeout     QueryStatus = "timeout"
	QueryStatusCanceled    QueryStatus = "canceled"
	QueryStatusSendFailed  QueryStatus = "send_failed"
	QueryStatusUnavailable QueryStatus = "unavailable"
)

type TrackedSender interface {
	SendMessageTracked(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error)
}

type Options struct {
	Timeout            time.Duration
	CompletionQuiet    time.Duration
	MaxActiveQueries   int
	MaxRecordsPerQuery int
	ResultTTL          time.Duration
	Location           *time.Location
	Now                func() time.Time
}

type QueryRequest struct {
	OwnerUserID uint
	ChannelID   uint
	DeviceCode  string
	ChannelCode string
	Destination string
	Transport   string
	StartTime   time.Time
	EndTime     time.Time
	Type        manscdp.RecordInfoQueryType
	Secrecy     int
	RecorderID  string
}

type Record struct {
	manscdp.RecordInfoItem
	RecordKey string
}

type QueryResult struct {
	QueryID       string
	SN            int
	Status        QueryStatus
	PartialReason ErrorCode
	DeclaredTotal int
	ReceivedCount int
	Incomplete    bool
	Records       []Record
	StartedAt     time.Time
	FinishedAt    time.Time
	// 协议诊断:聚合边界保留的拒绝/警告计数,调用方可区分
	// 协议数据错误与真正的设备超时
	RejectedCount int
	WarningCount  int
	WarningCodes  []string
}
