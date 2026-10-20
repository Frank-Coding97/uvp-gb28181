package subscribe

import (
	"context"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

const (
	defaultPositionHistoryRetentionDays = 7
	maxPositionHistoryRetentionDays     = 365
)

// PositionHistoryEnabled defaults to false (2026-10-20 交付包口径:
// 移动位置历史轨迹默认不保存，仅更新最新位置).
func PositionHistoryEnabled() bool {
	if app.ConfigYml == nil || app.ConfigYml.Get("gb28181.position_history.enabled") == nil {
		return false
	}
	return app.ConfigYml.GetBool("gb28181.position_history.enabled")
}

func PositionHistoryRetentionDays() int {
	if app.ConfigYml == nil {
		return defaultPositionHistoryRetentionDays
	}
	days := app.ConfigYml.GetInt("gb28181.position_history.retention_days")
	if days < 1 || days > maxPositionHistoryRetentionDays {
		return defaultPositionHistoryRetentionDays
	}
	return days
}

// PrunePositionHistory deletes records by server receive time, independent of device clock correctness.
func PrunePositionHistory(ctx context.Context, db *gorm.DB, now time.Time, retentionDays int) (int64, error) {
	if db == nil || retentionDays < 1 {
		return 0, nil
	}
	result := db.WithContext(ctx).
		Where("received_at < ?", now.AddDate(0, 0, -retentionDays)).
		Delete(&gbmodels.GbMobilePositionHistory{})
	return result.RowsAffected, result.Error
}
