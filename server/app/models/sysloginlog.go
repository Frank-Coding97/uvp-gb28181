package models

import (
	"context"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"

	"gorm.io/gorm"
)

// SysLoginLog is an immutable audit event for one backend login attempt.
type SysLoginLog struct {
	ID            uint       `gorm:"column:id;primaryKey" json:"id"`
	UserID        *uint      `gorm:"column:user_id;index" json:"userId,omitempty"`
	Username      string     `gorm:"column:username;size:100;not null;index" json:"username"`
	Result        string     `gorm:"column:result;size:16;not null;index" json:"result"`
	FailureReason string     `gorm:"column:failure_reason;size:48;index" json:"failureReason,omitempty"`
	IP            string     `gorm:"column:ip;size:50;not null;index" json:"ip"`
	Location      string     `gorm:"column:location;size:100;not null" json:"location"`
	UserAgent     string     `gorm:"column:user_agent;size:500;not null" json:"userAgent,omitempty"`
	Browser       string     `gorm:"column:browser;size:100;not null" json:"browser"`
	OS            string     `gorm:"column:os;size:100;not null" json:"os"`
	CreatedAt     time.Time  `gorm:"column:created_at;index" json:"createdAt"`
	UpdatedAt     time.Time  `gorm:"column:updated_at" json:"updatedAt"`
	DeletedAt     *time.Time `gorm:"column:deleted_at;index" json:"-"`
}

func (SysLoginLog) TableName() string { return "sys_login_logs" }

type SysLoginLogList []*SysLoginLog

func (list *SysLoginLogList) Find(ctx context.Context, funcs ...func(*gorm.DB) *gorm.DB) error {
	return app.DB().WithContext(ctx).Scopes(funcs...).Find(list).Error
}

func (list *SysLoginLogList) GetTotal(ctx context.Context, query ...func(*gorm.DB) *gorm.DB) (int64, error) {
	var total int64
	err := app.DB().WithContext(ctx).Model(&SysLoginLog{}).Scopes(query...).Count(&total).Error
	return total, err
}
