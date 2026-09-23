package snapshot

import (
	"context"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// Repo 屏蔽存储层,方便单测替换。
type Repo interface {
	UpdateSnapshot(ctx context.Context, deviceID, channelID, url string, at time.Time) error
}

// GormRepo 基于 gorm 的 Repo 实现
type GormRepo struct {
	db *gorm.DB
}

// NewGormRepo 构造 gorm 实现
func NewGormRepo(db *gorm.DB) *GormRepo {
	return &GormRepo{db: db}
}

// UpdateSnapshot 按 (device_id, channel_id) 唯一键 UPDATE 两列
func (r *GormRepo) UpdateSnapshot(ctx context.Context, deviceID, channelID, url string, at time.Time) error {
	return r.db.WithContext(ctx).
		Model(&gbmodels.GbChannel{}).
		Where("device_id = ? AND channel_id = ?", deviceID, channelID).
		Updates(map[string]interface{}{
			"snapshot_url": url,
			"snapshot_at":  at,
		}).Error
}
