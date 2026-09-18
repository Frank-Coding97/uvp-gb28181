package directory

import (
	"context"
	"sort"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type CustomMemberService struct{ db *gorm.DB }

type MemberMutationResult struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Skipped int `json:"skipped"`
}

func NewCustomMemberService(db *gorm.DB) *CustomMemberService { return &CustomMemberService{db: db} }

func (s *CustomMemberService) Add(ctx context.Context, ownerDeptID, actorID, groupID uint, rawDeviceIDs []uint) (*MemberMutationResult, error) {
	deviceIDs, err := normalizeDeviceIDs(rawDeviceIDs)
	if err != nil {
		return nil, err
	}
	result := &MemberMutationResult{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := findGroup(tx, ownerDeptID, groupID); err != nil {
			return err
		}
		if err := validateDevices(tx, ownerDeptID, deviceIDs); err != nil {
			return err
		}
		var existing []uint
		if err := tx.Model(&gbmodels.GbCustomGroupDevice{}).Where("group_id = ? AND device_id IN ?", groupID, deviceIDs).Pluck("device_id", &existing).Error; err != nil {
			return err
		}
		exists := make(map[uint]struct{}, len(existing))
		for _, id := range existing {
			exists[id] = struct{}{}
		}
		for _, id := range deviceIDs {
			if _, ok := exists[id]; ok {
				result.Skipped++
				continue
			}
			if err := tx.Create(&gbmodels.GbCustomGroupDevice{GroupID: groupID, DeviceID: id, CreatedBy: actorID}).Error; err != nil {
				return err
			}
			result.Added++
		}
		return nil
	})
	return result, err
}

func (s *CustomMemberService) Remove(ctx context.Context, ownerDeptID, groupID uint, rawDeviceIDs []uint) (*MemberMutationResult, error) {
	deviceIDs, err := normalizeDeviceIDs(rawDeviceIDs)
	if err != nil {
		return nil, err
	}
	result := &MemberMutationResult{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := findGroup(tx, ownerDeptID, groupID); err != nil {
			return err
		}
		if err := validateDevices(tx, ownerDeptID, deviceIDs); err != nil {
			return err
		}
		deleted := tx.Where("group_id = ? AND device_id IN ?", groupID, deviceIDs).Delete(&gbmodels.GbCustomGroupDevice{})
		if deleted.Error != nil {
			return deleted.Error
		}
		result.Removed = int(deleted.RowsAffected)
		result.Skipped = len(deviceIDs) - result.Removed
		return nil
	})
	return result, err
}

func (s *CustomMemberService) DeviceIDsForGroup(ctx context.Context, ownerDeptID, groupID uint) ([]uint, error) {
	group, err := findGroup(s.db.WithContext(ctx), ownerDeptID, groupID)
	if err != nil {
		return nil, err
	}
	var groupIDs []uint
	if err := s.db.WithContext(ctx).Model(&gbmodels.GbCustomGroup{}).Where("owner_dept_id = ? AND path LIKE ?", ownerDeptID, group.Path+"%").Pluck("id", &groupIDs).Error; err != nil {
		return nil, err
	}
	var deviceIDs []uint
	if err := s.db.WithContext(ctx).Model(&gbmodels.GbCustomGroupDevice{}).Distinct("device_id").Where("group_id IN ?", groupIDs).Order("device_id").Pluck("device_id", &deviceIDs).Error; err != nil {
		return nil, err
	}
	return deviceIDs, nil
}

func (s *CustomMemberService) UngroupedDeviceIDs(ctx context.Context, ownerDeptID uint) ([]uint, error) {
	var deviceIDs []uint
	err := s.db.WithContext(ctx).Model(&gbmodels.GbDevice{}).
		Where("owner_dept_id = ?", ownerDeptID).
		Where("NOT EXISTS (?)", s.db.Model(&gbmodels.GbCustomGroupDevice{}).Select("1").Where("gb_custom_group_device.device_id = gb_device.id")).
		Order("id").Pluck("id", &deviceIDs).Error
	return deviceIDs, err
}

func normalizeDeviceIDs(raw []uint) ([]uint, error) {
	if len(raw) == 0 || len(raw) > 500 {
		return nil, ErrDeviceBatchInvalid
	}
	seen := make(map[uint]struct{}, len(raw))
	ids := make([]uint, 0, len(raw))
	for _, id := range raw {
		if id == 0 {
			return nil, ErrDeviceNotFound
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func validateDevices(db *gorm.DB, ownerDeptID uint, deviceIDs []uint) error {
	var count int64
	if err := db.Model(&gbmodels.GbDevice{}).Where("owner_dept_id = ? AND id IN ?", ownerDeptID, deviceIDs).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(deviceIDs)) {
		return ErrDeviceNotFound
	}
	return nil
}
