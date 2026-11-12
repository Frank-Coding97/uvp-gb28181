package ptz

import "fmt"

type ErrorCode string

const (
	ErrorCodeHomePositionInvalidArgument     ErrorCode = "HOME_POSITION_INVALID_ARGUMENT"
	ErrorCodeHomePositionNotFound            ErrorCode = "HOME_POSITION_NOT_FOUND"
	ErrorCodeHomePositionDeviceOffline       ErrorCode = "HOME_POSITION_DEVICE_OFFLINE"
	ErrorCodeHomePositionIdempotencyConflict ErrorCode = "HOME_POSITION_IDEMPOTENCY_CONFLICT"
	ErrorCodeHomePositionUnavailable         ErrorCode = "HOME_POSITION_UNAVAILABLE"
	ErrorCodeHomePositionInternal            ErrorCode = "HOME_POSITION_INTERNAL_ERROR"
)

type OperationError struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *OperationError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" && e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.Err)
	}
	return string(e.Code)
}

func (e *OperationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func operationError(code ErrorCode, message string, err error) error {
	return &OperationError{Code: code, Message: message, Err: err}
}
