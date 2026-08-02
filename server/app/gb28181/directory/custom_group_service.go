package directory

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type DomainError struct {
	Code    string
	Message string
	Details map[string]int
}

func (e *DomainError) Error() string { return e.Message }
func (e *DomainError) Is(target error) bool {
	other, ok := target.(*DomainError)
	return ok && e.Code == other.Code
}

var (
	ErrGroupNameInvalid   = &DomainError{Code: "GROUP_NAME_INVALID", Message: "分组名称不能为空且不能超过 64 个字符"}
	ErrGroupNameConflict  = &DomainError{Code: "GROUP_NAME_CONFLICT", Message: "同级分组名称已存在"}
	ErrGroupNotFound      = &DomainError{Code: "GROUP_NOT_FOUND", Message: "分组不存在"}
	ErrGroupCycle         = &DomainError{Code: "GROUP_CYCLE", Message: "不能把分组移动到自身或后代"}
	ErrGroupHasChildren   = &DomainError{Code: "GROUP_HAS_CHILDREN", Message: "分组包含子分组"}
	ErrDeviceBatchInvalid = &DomainError{Code: "DEVICE_BATCH_INVALID", Message: "设备数量必须在 1 到 500 之间"}
	ErrDeviceNotFound     = &DomainError{Code: "DEVICE_NOT_FOUND", Message: "设备不存在"}
)

type CustomGroupService struct{ db *gorm.DB }

func NewCustomGroupService(db *gorm.DB) *CustomGroupService { return &CustomGroupService{db: db} }

func (s *CustomGroupService) Create(ctx context.Context, ownerDeptID, actorID, parentID uint, rawName string) (*gbmodels.GbCustomGroup, error) {
	name, err := normalizeGroupName(rawName)
	if err != nil {
		return nil, err
	}
	var created gbmodels.GbCustomGroup
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		parentPath := "/"
		depth := uint8(0)
		if parentID != 0 {
			parent, findErr := findGroup(tx, ownerDeptID, parentID)
			if findErr != nil {
				return findErr
			}
			parentPath = parent.Path
			depth = parent.Depth + 1
		}
		created = gbmodels.GbCustomGroup{OwnerDeptID: ownerDeptID, ParentID: parentID, Path: "/", Depth: depth, Name: name, CreatedBy: actorID}
		if createErr := tx.Create(&created).Error; createErr != nil {
			if isUniqueError(createErr) {
				return ErrGroupNameConflict
			}
			return createErr
		}
		created.Path = parentPath + strconv.FormatUint(uint64(created.ID), 10) + "/"
		return tx.Model(&created).Update("path", created.Path).Error
	})
	return &created, err
}

func (s *CustomGroupService) Rename(ctx context.Context, ownerDeptID, groupID uint, rawName string) error {
	name, err := normalizeGroupName(rawName)
	if err != nil {
		return err
	}
	group, err := findGroup(s.db.WithContext(ctx), ownerDeptID, groupID)
	if err != nil {
		return err
	}
	if group.Name == name {
		return nil
	}
	if err := s.db.WithContext(ctx).Model(group).Update("name", name).Error; err != nil {
		if isUniqueError(err) {
			return ErrGroupNameConflict
		}
		return err
	}
	return nil
}

func normalizeGroupName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return "", ErrGroupNameInvalid
	}
	return name, nil
}

func findGroup(db *gorm.DB, ownerDeptID, groupID uint) (*gbmodels.GbCustomGroup, error) {
	var group gbmodels.GbCustomGroup
	result := db.Where("id = ? AND owner_dept_id = ?", groupID, ownerDeptID).Limit(1).Find(&group)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrGroupNotFound
	}
	return &group, nil
}

func isUniqueError(err error) bool {
	if err == nil || errors.Is(err, gorm.ErrDuplicatedKey) {
		return err != nil
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unique constraint") || strings.Contains(text, "duplicate entry") || strings.Contains(text, "duplicate key")
}

func errorWithCount(base *DomainError, key string, count int) error {
	return &DomainError{Code: base.Code, Message: base.Message, Details: map[string]int{key: count}}
}

func domainErrorCode(err error) string {
	var domain *DomainError
	if errors.As(err, &domain) {
		return domain.Code
	}
	return fmt.Sprint(err)
}
