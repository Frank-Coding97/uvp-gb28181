package repo

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// SchedulerSetting gorm 模型,对应 scheduler_setting 单行表(id=1)
//
// 表 schema 见 server/resource/database/gb28181/scheduler_setting.sql。
// algorithm 跟 scheduler.Factory.Build 取值对齐:roundrobin / weighted / leastload。
type SchedulerSetting struct {
	ID         int64     `gorm:"primaryKey;column:id"`
	Algorithm  string    `gorm:"column:algorithm;size:32;not null;default:'roundrobin'"`
	ConfigJSON string    `gorm:"column:config_json;type:text"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

// TableName 显式表名
func (SchedulerSetting) TableName() string { return "scheduler_setting" }

// SchedulerSettingRepo scheduler_setting 表访问
//
// 设计:DB 表单行 id=1。GetCurrent 取该行;未找到返 (nil, nil) 让上层走 fallback。
// UpdateAlgorithm 用 Save 全字段写。
type SchedulerSettingRepo struct {
	db *gorm.DB
}

// NewSchedulerSettingRepo 构造
func NewSchedulerSettingRepo(db *gorm.DB) *SchedulerSettingRepo {
	return &SchedulerSettingRepo{db: db}
}

// GetCurrent 取 id=1 的设置(未找到返 nil, nil)
func (r *SchedulerSettingRepo) GetCurrent(ctx context.Context) (*SchedulerSetting, error) {
	var s SchedulerSetting
	err := r.db.WithContext(ctx).Where("id = ?", 1).Take(&s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// UpdateAlgorithm 写回 algorithm(M2 暂不暴露 controller,先留方法给 M3 用)
func (r *SchedulerSettingRepo) UpdateAlgorithm(ctx context.Context, name string) error {
	s := SchedulerSetting{ID: 1, Algorithm: name, UpdatedAt: time.Now()}
	return r.db.WithContext(ctx).Save(&s).Error
}
