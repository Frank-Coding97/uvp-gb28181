package app

import "context"

// LoginLogEvent is the privacy-filtered payload accepted by the login audit recorder.
// It deliberately contains no request body, password, captcha, or token fields.
type LoginLogEvent struct {
	UserID        *uint
	Username      string
	Result        string
	FailureReason string
	IP            string
	Location      string
	UserAgent     string
	Browser       string
	OS            string
}

// LoginLogRecorderInterface persists one login attempt without coupling global state to the service package.
type LoginLogRecorderInterface interface {
	RecordLogin(context.Context, LoginLogEvent) error
}
