package service

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
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
	logger := app.Log(parent).Named("audit")
	ctx, cancel := context.WithTimeout(parent, loginLogWriteTimeout)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Warn("登录日志记录异常",
				zap.String("event", "auth.login_audit.panic"),
				zap.String("panic_type", logging.TypeName(recovered)))
		}
	}()
	if err := recorder.RecordLogin(ctx, event); err != nil {
		recordLoginFailure(parent, event, err)
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

// CleanupBefore hard-deletes immutable audit rows in bounded batches.
func (s *LoginLogService) CleanupBefore(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("login log database is unavailable")
	}
	if batchSize <= 0 || batchSize > 1000 {
		batchSize = 1000
	}
	var deleted int64
	for {
		var ids []uint
		if err := s.db.WithContext(ctx).Model(&models.SysLoginLog{}).
			Where("created_at < ?", cutoff).Order("id").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
			return deleted, err
		}
		if len(ids) == 0 {
			return deleted, nil
		}
		result := s.db.WithContext(ctx).Where("id IN ?", ids).Delete(&models.SysLoginLog{})
		if result.Error != nil {
			return deleted, result.Error
		}
		deleted += result.RowsAffected
		if len(ids) < batchSize {
			return deleted, nil
		}
	}
}

func recordLoginFailure(ctx context.Context, event app.LoginLogEvent, err error) {
	if err == nil {
		return
	}
	app.Log(ctx).Named("audit").Warn("登录日志记录失败",
		zap.String("event", "auth.login_audit.persist_failed"), logging.Error(err),
		zap.String("result", event.Result), zap.String("username", event.Username))
}
