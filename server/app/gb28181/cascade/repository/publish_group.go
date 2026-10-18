package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
)

// SetPublishGroup changes only the business-group publication setting. ConfigRevision
// protects platform configuration while ProjectionRevision serializes publication
// against catalog/tree edits.
func (r *GormRepository) SetPublishGroup(ctx context.Context, platformID, expectedRevision uint64, publishGroup bool) (*model.GbCascadePlatform, error) {
	if platformID == 0 || expectedRevision == 0 {
		return nil, ErrRevisionConflict
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var platform model.GbCascadePlatform
		result := tx.Limit(1).Find(&platform, platformID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrPlatformNotFound
		}
		if platform.ConfigRevision != expectedRevision {
			return ErrRevisionConflict
		}
		if publishGroup {
			if len(platform.LocalDeviceID) != 20 || !allASCIIDigits(platform.LocalDeviceID) || platform.LocalDeviceID[10:13] != "200" {
				return invalidBusinessGroup("发布业务分组要求本平台设备 ID 为有效的 20 位 200 类型系统编码")
			}
			var rootCount int64
			if err := tx.Model(&model.GbCascadeBusinessNode{}).
				Where("platform_id = ? AND kind = ? AND parent_code = ''", platformID, "group").Count(&rootCount).Error; err != nil {
				return err
			}
			if rootCount != 1 {
				return invalidBusinessGroup("发布业务分组前必须配置唯一的 215 根节点")
			}
			nodes, bindings, err := loadBusinessGroupInputs(tx, platformID)
			if err != nil {
				return err
			}
			if _, err := validatePublishedBusinessGroup(tx, platform, nodes, bindings); err != nil {
				return err
			}
		}

		updates := map[string]any{
			"publish_group":   publishGroup,
			"config_revision": gorm.Expr("config_revision + ?", 1),
			"updated_at":      time.Now().UTC(),
		}
		if platform.PublishGroup != publishGroup {
			updates["projection_revision"] = gorm.Expr("projection_revision + ?", 1)
		}
		write := tx.Model(&model.GbCascadePlatform{}).
			Where("id = ? AND config_revision = ? AND projection_revision = ?", platformID, expectedRevision, platform.ProjectionRevision).
			Updates(updates)
		if write.Error != nil {
			return write.Error
		}
		if write.RowsAffected == 0 {
			return ErrRevisionConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.FindPlatform(ctx, platformID)
}
