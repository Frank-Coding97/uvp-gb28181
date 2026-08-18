package service

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
)

const (
	LoginResultSuccess = "success"
	LoginResultFailure = "failure"

	LoginFailureCaptchaInvalid    = "captcha_invalid"
	LoginFailureUserNotFound      = "user_not_found"
	LoginFailureUserDisabled      = "user_disabled"
	LoginFailureAccountLocked     = "account_locked"
	LoginFailurePasswordIncorrect = "password_incorrect"
	LoginFailureSessionCreate     = "session_create_failed"
	LoginFailureServerError       = "server_error"
	loginLogWriteTimeout          = 500 * time.Millisecond
)

// RecordLoginAttempt isolates audit failures from authentication responses.
func RecordLoginAttempt(parent context.Context, recorder app.LoginLogRecorderInterface, event app.LoginLogEvent) {
	if recorder == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, loginLogWriteTimeout)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil && app.ZapLog != nil {
			app.ZapLog.Warn("登录日志记录异常", zap.Any("panic", recovered))
		}
	}()
	if err := recorder.RecordLogin(ctx, event); err != nil {
		recordLoginFailure(event, err)
	}
}

// LoginLogService persists privacy-filtered login attempts.
type LoginLogService struct {
	db *gorm.DB
}

func NewLoginLogService(db *gorm.DB) *LoginLogService { return &LoginLogService{db: db} }

func (s *LoginLogService) RecordLogin(parent context.Context, event app.LoginLogEvent) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("login log database is unavailable")
	}
	ctx, cancel := context.WithTimeout(parent, loginLogWriteTimeout)
	defer cancel()
	result := s.db.WithContext(ctx).Create(&models.SysLoginLog{
		UserID: event.UserID, Username: event.Username, Result: event.Result,
		FailureReason: event.FailureReason, IP: event.IP, Location: event.Location,
		UserAgent: event.UserAgent, Browser: event.Browser, OS: event.OS,
	})
	return result.Error
}

func recordLoginFailure(event app.LoginLogEvent, err error) {
	if err == nil {
		return
	}
	if app.ZapLog != nil {
		app.ZapLog.Warn("登录日志记录失败", zap.Error(err), zap.String("result", event.Result), zap.String("username", event.Username))
	}
}
