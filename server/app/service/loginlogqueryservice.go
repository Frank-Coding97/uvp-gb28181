package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/models"
)

var ErrLoginLogNotFound = errors.New("login log not found")

type LoginLogFilter struct {
	PageNum       int
	PageSize      int
	Username      string
	Result        string
	FailureReason string
	IP            string
	StartTime     *time.Time
	EndTime       *time.Time
}

func (f LoginLogFilter) Validate() error {
	if f.PageNum < 0 || f.PageSize < 0 || f.PageSize > 100 {
		return fmt.Errorf("invalid login log pagination")
	}
	if f.Result != "" && f.Result != LoginResultSuccess && f.Result != LoginResultFailure {
		return fmt.Errorf("invalid login result")
	}
	if f.FailureReason != "" && !validLoginFailure(f.FailureReason) {
		return fmt.Errorf("invalid login failure reason")
	}
	if f.StartTime != nil && f.EndTime != nil && f.StartTime.After(*f.EndTime) {
		return fmt.Errorf("invalid login log time range")
	}
	return nil
}

type LoginLogListItem struct {
	ID            uint      `json:"id"`
	UserID        *uint     `json:"userId,omitempty"`
	Username      string    `json:"username"`
	Result        string    `json:"result"`
	FailureReason string    `json:"failureReason,omitempty"`
	IP            string    `json:"ip"`
	Location      string    `json:"location"`
	Browser       string    `json:"browser"`
	OS            string    `json:"os"`
	CreatedAt     time.Time `json:"createdAt"`
}

type LoginLogDetail struct {
	LoginLogListItem
	UserAgent string `json:"userAgent"`
}

type LoginLogQueryService struct{ db *gorm.DB }

func NewLoginLogQueryService(db *gorm.DB) *LoginLogQueryService {
	return &LoginLogQueryService{db: db}
}

func (s *LoginLogQueryService) List(ctx context.Context, filter LoginLogFilter) ([]LoginLogListItem, int64, error) {
	if s == nil || s.db == nil {
		return nil, 0, fmt.Errorf("login log database is unavailable")
	}
	if err := filter.Validate(); err != nil {
		return nil, 0, err
	}
	if filter.PageNum == 0 {
		filter.PageNum = 1
	}
	if filter.PageSize == 0 {
		filter.PageSize = 20
	}
	query := applyLoginLogFilter(s.db.WithContext(ctx).Model(&models.SysLoginLog{}), filter)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]LoginLogListItem, 0)
	err := query.Select("id", "user_id", "username", "result", "failure_reason", "ip", "location", "browser", "os", "created_at").
		Order("created_at DESC, id DESC").Offset((filter.PageNum - 1) * filter.PageSize).Limit(filter.PageSize).Scan(&items).Error
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *LoginLogQueryService) Detail(ctx context.Context, id uint) (*LoginLogDetail, error) {
	if s == nil || s.db == nil || id == 0 {
		return nil, ErrLoginLogNotFound
	}
	var detail LoginLogDetail
	result := s.db.WithContext(ctx).Model(&models.SysLoginLog{}).
		Select("id", "user_id", "username", "result", "failure_reason", "ip", "location", "browser", "os", "created_at", "user_agent").
		Where("id = ?", id).Scan(&detail)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrLoginLogNotFound
	}
	return &detail, nil
}

func applyLoginLogFilter(db *gorm.DB, filter LoginLogFilter) *gorm.DB {
	if value := strings.TrimSpace(filter.Username); value != "" {
		db = db.Where("username LIKE ?", "%"+value+"%")
	}
	if filter.Result != "" {
		db = db.Where("result = ?", filter.Result)
	}
	if filter.FailureReason != "" {
		db = db.Where("failure_reason = ?", filter.FailureReason)
	}
	if value := strings.TrimSpace(filter.IP); value != "" {
		db = db.Where("ip LIKE ?", "%"+value+"%")
	}
	if filter.StartTime != nil {
		db = db.Where("created_at >= ?", *filter.StartTime)
	}
	if filter.EndTime != nil {
		db = db.Where("created_at <= ?", *filter.EndTime)
	}
	return db
}

func validLoginFailure(value string) bool {
	switch value {
	case LoginFailureCaptchaInvalid, LoginFailureUserNotFound, LoginFailureUserDisabled,
		LoginFailureAccountLocked, LoginFailurePasswordIncorrect, LoginFailureSessionCreate, LoginFailureServerError:
		return true
	default:
		return false
	}
}
