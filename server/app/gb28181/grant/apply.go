package grant

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

type ApplyMode string

const (
	ApplyModeAdd    ApplyMode = "add"
	ApplyModeRemove ApplyMode = "remove"
)

var ErrApplyRequestInvalid = errors.New("共享授权请求不合法")

var (
	errApplyStale       = errors.New("共享授权已被其他管理员修改")
	errApplyNotVisible  = errors.New("设备不在可操作范围内")
	errApplyTarget      = errors.New("共享目标不存在、停用或不在可授权范围内")
	errApplyWriteFailed = errors.New("处理共享授权失败")
)

const (
	applyStatusChanged = "changed"
	applyStatusSkipped = "skipped"
	applyStatusFailed  = "failed"
)

type ApplyDevice struct {
	DeviceID         uint   `json:"deviceId"`
	ExpectedRevision string `json:"expectedRevision"`
}

type ApplyTarget struct {
	Type string `json:"type"`
	ID   uint   `json:"id"`
}

type ApplyRequest struct {
	Items     []ApplyDevice `json:"items"`
	Mode      ApplyMode     `json:"mode"`
	Targets   []ApplyTarget `json:"targets"`
	CreatedBy uint          `json:"-"`
}

type ApplySummary struct {
	Requested        int `json:"requested"`
	Changed          int `json:"changed"`
	Skipped          int `json:"skipped"`
	Failed           int `json:"failed"`
	Added            int `json:"added"`
	Removed          int `json:"removed"`
	RelationsSkipped int `json:"relationsSkipped"`
}

type ApplyResultItem struct {
	DeviceID   uint   `json:"deviceId"`
	DeviceCode string `json:"deviceCode"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	Added      int    `json:"added"`
	Removed    int    `json:"removed"`
	Skipped    int    `json:"skipped"`
	Revision   string `json:"revision"`
}

type ApplyResult struct {
	Summary ApplySummary      `json:"summary"`
	Results []ApplyResultItem `json:"results"`
}

func (s *Service) Apply(ctx context.Context, request ApplyRequest, access datascope.OwnerDeptAccess) (ApplyResult, error) {
	if request.Mode != ApplyModeAdd && request.Mode != ApplyModeRemove {
		return ApplyResult{}, ErrApplyRequestInvalid
	}
	items := uniqueApplyDevices(request.Items)
	targets, err := uniqueApplyTargets(request.Targets)
	if err != nil || len(items) == 0 || len(targets) == 0 {
		return ApplyResult{}, ErrApplyRequestInvalid
	}
	result := ApplyResult{
		Summary: ApplySummary{Requested: len(items)},
		Results: make([]ApplyResultItem, 0, len(items)),
	}
	for _, input := range items {
		item := s.applyOne(ctx, input, request.Mode, targets, request.CreatedBy, access)
		result.Results = append(result.Results, item)
		result.Summary.Added += item.Added
		result.Summary.Removed += item.Removed
		result.Summary.RelationsSkipped += item.Skipped
		switch item.Status {
		case applyStatusChanged:
			result.Summary.Changed++
		case applyStatusSkipped:
			result.Summary.Skipped++
		default:
			result.Summary.Failed++
		}
	}
	return result, nil
}

func (s *Service) applyOne(ctx context.Context, input ApplyDevice, mode ApplyMode, targets []ApplyTarget, createdBy uint, access datascope.OwnerDeptAccess) ApplyResultItem {
	item := ApplyResultItem{DeviceID: input.DeviceID, Status: applyStatusFailed}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		deviceQuery := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&gbmodels.GbDevice{})
		if s.scope != nil {
			deviceQuery = deviceQuery.Scopes(s.scope)
		}
		var device gbmodels.GbDevice
		deviceResult := deviceQuery.Where("id = ?", input.DeviceID).First(&device)
		if deviceResult.Error != nil {
			if errors.Is(deviceResult.Error, gorm.ErrRecordNotFound) {
				return errApplyNotVisible
			}
			return deviceResult.Error
		}
		if deviceResult.RowsAffected == 0 {
			// The configured query hook may mask ErrRecordNotFound; the row count
			// remains the portable way to distinguish an invisible device.
			return errApplyNotVisible
		}
		item.DeviceCode = device.DeviceID
		item.Name = device.Name

		current, err := grantsForUpdate(tx, input.DeviceID)
		if err != nil {
			return err
		}
		item.Revision = revisionFor(current)
		if input.ExpectedRevision == "" || input.ExpectedRevision != item.Revision {
			return errApplyStale
		}
		if mode == ApplyModeAdd {
			if err := validateAddTargets(tx, targets, access); err != nil {
				return err
			}
			for _, target := range targets {
				added, err := addGrant(tx, input.DeviceID, target, createdBy)
				if err != nil {
					return err
				}
				if added {
					item.Added++
				} else {
					item.Skipped++
				}
			}
		} else {
			for _, target := range targets {
				removed, err := removeGrant(tx, input.DeviceID, target)
				if err != nil {
					return err
				}
				if removed {
					item.Removed++
				} else {
					item.Skipped++
				}
			}
		}
		after, err := grantsForUpdate(tx, input.DeviceID)
		if err != nil {
			return err
		}
		item.Revision = revisionFor(after)
		return nil
	})
	if err != nil {
		item.Added = 0
		item.Removed = 0
		item.Skipped = 0
		switch {
		case errors.Is(err, errApplyStale), errors.Is(err, errApplyNotVisible), errors.Is(err, errApplyTarget):
			item.Message = err.Error()
		default:
			item.Message = errApplyWriteFailed.Error()
		}
		return item
	}
	if item.Added > 0 || item.Removed > 0 {
		item.Status = applyStatusChanged
		if item.Added > 0 {
			item.Message = fmt.Sprintf("新增 %d 条共享授权", item.Added)
		} else {
			item.Message = fmt.Sprintf("收回 %d 条共享授权", item.Removed)
		}
		return item
	}
	item.Status = applyStatusSkipped
	item.Message = "共享授权无需变更"
	return item
}

func grantsForUpdate(tx *gorm.DB, deviceID uint) ([]gbmodels.GbDeviceGrant, error) {
	var grants []gbmodels.GbDeviceGrant
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("device_id = ?", deviceID).
		Order("target_type ASC, target_id ASC").Find(&grants).Error
	return grants, err
}

func validateAddTargets(tx *gorm.DB, targets []ApplyTarget, access datascope.OwnerDeptAccess) error {
	departmentIDs := make([]uint, 0)
	userIDs := make([]uint, 0)
	for _, target := range targets {
		if target.Type == gbmodels.GrantTargetTypeDept {
			departmentIDs = append(departmentIDs, target.ID)
		} else {
			userIDs = append(userIDs, target.ID)
		}
	}
	if !access.FullAccess && len(access.DeptIDs) == 0 {
		return errApplyTarget
	}
	if len(departmentIDs) > 0 {
		var count int64
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&basemodels.SysDepartment{}).
			Where("id IN ? AND (status = ? OR status IS NULL)", departmentIDs, 1)
		if !access.FullAccess {
			query = query.Where("id IN ?", access.DeptIDs)
		}
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count != int64(len(departmentIDs)) {
			return errApplyTarget
		}
	}
	if len(userIDs) > 0 {
		var count int64
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&basemodels.User{}).
			Where("id IN ? AND status = ?", userIDs, 1)
		if !access.FullAccess {
			query = query.Where("dept_id IN ?", access.DeptIDs)
		}
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count != int64(len(userIDs)) {
			return errApplyTarget
		}
	}
	return nil
}

func addGrant(tx *gorm.DB, deviceID uint, target ApplyTarget, createdBy uint) (bool, error) {
	var existing gbmodels.GbDeviceGrant
	result := tx.Unscoped().Where("device_id = ? AND target_type = ? AND target_id = ?", deviceID, target.Type, target.ID).First(&existing)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		current := gbmodels.GbDeviceGrant{DeviceID: deviceID, TargetType: target.Type, TargetID: target.ID, CreatedBy: createdBy}
		if !isSQLiteDialect(tx) {
			return true, tx.Create(&current).Error
		}
		created := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "device_id"}, {Name: "target_type"}, {Name: "target_id"}},
			DoNothing: true,
		}).Create(&current)
		if created.Error != nil {
			return false, created.Error
		}
		if created.RowsAffected > 0 {
			return true, nil
		}
		result = tx.Unscoped().Where("device_id = ? AND target_type = ? AND target_id = ?", deviceID, target.Type, target.ID).First(&existing)
		if result.Error != nil {
			return false, result.Error
		}
		if result.RowsAffected == 0 {
			return false, errors.New("授权冲突后未找到目标记录")
		}
	}
	if !existing.DeletedAt.Valid {
		return false, nil
	}
	err := tx.Unscoped().Model(&gbmodels.GbDeviceGrant{}).Where("id = ?", existing.ID).
		Updates(map[string]any{"deleted_at": nil, "created_by": createdBy}).Error
	return true, err
}

func removeGrant(tx *gorm.DB, deviceID uint, target ApplyTarget) (bool, error) {
	result := tx.Where("device_id = ? AND target_type = ? AND target_id = ?", deviceID, target.Type, target.ID).
		Delete(&gbmodels.GbDeviceGrant{})
	return result.RowsAffected > 0, result.Error
}

func uniqueApplyDevices(values []ApplyDevice) []ApplyDevice {
	result := make([]ApplyDevice, 0, len(values))
	seen := make(map[uint]struct{}, len(values))
	for _, value := range values {
		if value.DeviceID == 0 {
			continue
		}
		if _, ok := seen[value.DeviceID]; ok {
			continue
		}
		seen[value.DeviceID] = struct{}{}
		result = append(result, value)
	}
	return result
}

func uniqueApplyTargets(values []ApplyTarget) ([]ApplyTarget, error) {
	result := make([]ApplyTarget, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value.ID == 0 || (value.Type != gbmodels.GrantTargetTypeDept && value.Type != gbmodels.GrantTargetTypeUser) {
			return nil, ErrApplyRequestInvalid
		}
		key := fmt.Sprintf("%s:%d", value.Type, value.ID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}
