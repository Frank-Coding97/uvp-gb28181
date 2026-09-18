package grant

import (
	"context"
	"errors"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// ErrDeviceNotFound 共享目标设备不存在
var ErrDeviceNotFound = errors.New("设备不存在")

// Repo 共享授权数据访问(幂等创建:同键已存在则跳过/恢复)。
type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// Create 创建一条共享授权(幂等):
//   - 同键(device_id+target_type+target_id)有效记录已存在 → created=false,跳过
//   - 同键记录曾被软删 → 恢复(deleted_at 清空),created=true
//   - 不存在 → 插入,created=true
//   - 设备不存在 → ErrDeviceNotFound
func (r *Repo) Create(ctx context.Context, grant *gbmodels.GbDeviceGrant) (bool, error) {
	db := r.db.WithContext(ctx)

	var deviceCount int64
	if err := db.Table("gb_device").Where("id = ? AND deleted_at IS NULL", grant.DeviceID).Count(&deviceCount).Error; err != nil {
		return false, err
	}
	if deviceCount == 0 {
		return false, ErrDeviceNotFound
	}

	var existing gbmodels.GbDeviceGrant
	err := db.Unscoped().
		Where("device_id = ? AND target_type = ? AND target_id = ?", grant.DeviceID, grant.TargetType, grant.TargetID).
		First(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return true, db.Create(grant).Error
	case err != nil:
		return false, err
	}

	if !existing.DeletedAt.Valid {
		// 有效记录已存在 → 跳过(不覆盖原操作人)
		return false, nil
	}

	// 软删记录 → 恢复并刷新操作人
	return true, db.Unscoped().Model(&gbmodels.GbDeviceGrant{}).
		Where("id = ?", existing.ID).
		Updates(map[string]interface{}{"deleted_at": nil, "created_by": grant.CreatedBy}).Error
}

// CreateBatch 批量创建(逐条幂等),返回 added/skipped;设备不存在视为跳过。
func (r *Repo) CreateBatch(ctx context.Context, deviceID uint, targets []GrantTarget) (added, skipped int, err error) {
	for _, t := range targets {
		created, createErr := r.Create(ctx, &gbmodels.GbDeviceGrant{
			DeviceID:   deviceID,
			TargetType: t.Type,
			TargetID:   t.ID,
			CreatedBy:  t.CreatedBy,
		})
		if createErr != nil {
			if errors.Is(createErr, ErrDeviceNotFound) {
				skipped++
				continue
			}
			return added, skipped, createErr
		}
		if created {
			added++
		} else {
			skipped++
		}
	}
	return added, skipped, nil
}

// ListByDevice 查询设备当前有效的共享授权。
func (r *Repo) ListByDevice(ctx context.Context, deviceID uint) ([]gbmodels.GbDeviceGrant, error) {
	var list []gbmodels.GbDeviceGrant
	err := r.db.WithContext(ctx).
		Where("device_id = ?", deviceID).
		Order("id ASC").
		Find(&list).Error
	return list, err
}

// ListDeviceIDsByTarget 查询某目标(部门或用户)被授权的设备 ID 集合(VisibilityScope 用)。
func (r *Repo) ListDeviceIDsByTarget(ctx context.Context, targetType string, targetIDs []uint) ([]uint, error) {
	if len(targetIDs) == 0 {
		return nil, nil
	}
	var ids []uint
	err := r.db.WithContext(ctx).Model(&gbmodels.GbDeviceGrant{}).
		Where("target_type = ? AND target_id IN ?", targetType, targetIDs).
		Distinct().
		Pluck("device_id", &ids).Error
	return ids, err
}

// Remove 软删一条授权(校验归属设备,防越权删除)。
func (r *Repo) Remove(ctx context.Context, deviceID, grantID uint) error {
	res := r.db.WithContext(ctx).
		Where("id = ? AND device_id = ?", grantID, deviceID).
		Delete(&gbmodels.GbDeviceGrant{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// GrantTarget 共享目标(批量创建入参)。
type GrantTarget struct {
	Type      string // dept / user
	ID        uint
	CreatedBy uint
}
