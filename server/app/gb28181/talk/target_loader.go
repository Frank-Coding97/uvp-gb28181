package talk

import (
	"context"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type GormTargetLoader struct {
	db *gorm.DB
}

func NewGormTargetLoader(db *gorm.DB) *GormTargetLoader {
	return &GormTargetLoader{db: db}
}

func (l *GormTargetLoader) LoadTalkTarget(ctx context.Context, channelID uint, deviceID string) (*models.GbChannel, *models.GbDevice, error) {
	if l == nil || l.db == nil {
		return nil, nil, ErrTalkActivationUnavailable
	}
	var channel models.GbChannel
	result := l.db.WithContext(ctx).Where("id = ? AND device_id = ?", channelID, deviceID).Limit(1).Find(&channel)
	if result.Error != nil || result.RowsAffected == 0 {
		return nil, nil, result.Error
	}
	var device models.GbDevice
	result = l.db.WithContext(ctx).Where("device_id = ?", deviceID).Limit(1).Find(&device)
	if result.Error != nil || result.RowsAffected == 0 {
		return nil, nil, result.Error
	}
	return &channel, &device, nil
}
