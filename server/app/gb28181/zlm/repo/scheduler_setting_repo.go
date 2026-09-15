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
// UpdateAlgorithm 只更新 algorithm/updated_at;id=1 不存在时创建该行。
type SchedulerSettingRepo struct {
	db *gorm.DB
}

// NewSchedulerSettingRepo 构造
func NewSchedulerSettingRepo(db *gorm.DB) *SchedulerSettingRepo {
	return &SchedulerSettingRepo{db: db}
}

// GetCurrent 取 id=1 的设置(未找到返 nil, nil)
// 注意: 全局 gormhelper hook 关闭了 RaiseErrorOnNotFound,不能只靠 errors.Is 判空
func (r *SchedulerSettingRepo) GetCurrent(ctx context.Context) (*SchedulerSetting, error) {
	var s SchedulerSetting
	result := r.db.WithContext(ctx).Where("id = ?", 1).Take(&s)
	if err := result.Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &s, nil
}

// UpdateAlgorithm 写回 algorithm(M2 暂不暴露 controller,先留方法给 M3 用)
func (r *SchedulerSettingRepo) UpdateAlgorithm(ctx context.Context, name string) error {
	now := time.Now()
	db := r.db.WithContext(ctx)
	result := db.Model(&SchedulerSetting{}).
		Where("id = ?", 1).
		Updates(map[string]interface{}{
			"algorithm":  name,
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}

	// Some drivers (notably MySQL without CLIENT_FOUND_ROWS) report zero when
	// an UPDATE leaves all stored values unchanged. Distinguish that case from
	// a missing singleton before attempting the compatibility insert.
	var existing SchedulerSetting
	lookup := db.Select("id").Where("id = ?", 1).Take(&existing)
	if lookup.Error != nil && !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		return lookup.Error
	}
	if lookup.RowsAffected > 0 {
		return nil
	}

	// Keep Save's historical missing-row behavior: a caller can initialize
	// scheduler_setting id=1 through UpdateAlgorithm without a separate seed.
	return db.Create(&SchedulerSetting{
		ID:        1,
		Algorithm: name,
		CreatedAt: now,
		UpdatedAt: now,
	}).Error
}
