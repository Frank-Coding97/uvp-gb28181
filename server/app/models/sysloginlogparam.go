package models

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type SysLoginLogListRequest struct {
	BasePaging
	Validator
	Username      string `form:"username"`
	Result        string `form:"result"`
	FailureReason string `form:"failureReason"`
	IP            string `form:"ip"`
	StartTime     string `form:"startTime"`
	EndTime       string `form:"endTime"`
}

func (r *SysLoginLogListRequest) Validate(c *gin.Context) error {
	if err := r.Check(c, r); err != nil {
		return err
	}
	if r.PageNum < 0 || r.PageSize < 0 || r.PageSize > 100 {
		return gorm.ErrInvalidData
	}
	if r.PageNum == 0 {
		r.PageNum = 1
	}
	if r.PageSize == 0 {
		r.PageSize = 20
	}
	if r.Result != "" && r.Result != "success" && r.Result != "failure" {
		return gorm.ErrInvalidData
	}
	if r.FailureReason != "" && !validLoginFailureReason(r.FailureReason) {
		return gorm.ErrInvalidData
	}
	return nil
}

func validLoginFailureReason(value string) bool {
	switch value {
	case "captcha_invalid", "user_not_found", "user_disabled", "account_locked", "password_incorrect", "session_create_failed", "server_error":
		return true
	default:
		return false
	}
}

func (r *SysLoginLogListRequest) Handle() func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if value := strings.TrimSpace(r.Username); value != "" {
			db = db.Where("username LIKE ?", "%"+value+"%")
		}
		if r.Result != "" {
			db = db.Where("result = ?", r.Result)
		}
		if r.FailureReason != "" {
			db = db.Where("failure_reason = ?", r.FailureReason)
		}
		if value := strings.TrimSpace(r.IP); value != "" {
			db = db.Where("ip LIKE ?", "%"+value+"%")
		}
		if r.StartTime != "" {
			db = db.Where("created_at >= ?", r.StartTime)
		}
		if r.EndTime != "" {
			db = db.Where("created_at <= ?", r.EndTime)
		}
		return db
	}
}

func (r *SysLoginLogListRequest) Paginate() func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Offset((r.PageNum - 1) * r.PageSize).Limit(r.PageSize)
	}
}

func (r *SysLoginLogListRequest) Cutoff(now time.Time) time.Time {
	return now.Add(-180 * 24 * time.Hour)
}

type SysLoginLogDeleteRequest struct {
	Validator
	IDs []uint `json:"ids" form:"ids"`
}

func (r *SysLoginLogDeleteRequest) Validate(c *gin.Context) error {
	if err := r.Check(c, r); err != nil {
		return err
	}
	if len(r.IDs) == 0 || len(r.IDs) > 100 {
		return gorm.ErrInvalidData
	}
	for _, id := range r.IDs {
		if id == 0 {
			return gorm.ErrInvalidData
		}
	}
	return nil
}

type SysLoginLogUnlockRequest struct {
	Validator
	ID uint `json:"id" form:"id"`
}

func (r *SysLoginLogUnlockRequest) Validate(c *gin.Context) error {
	if err := r.Check(c, r); err != nil {
		return err
	}
	if r.ID == 0 {
		return gorm.ErrInvalidData
	}
	return nil
}
