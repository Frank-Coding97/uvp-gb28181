package assign

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// ErrTargetDeptInvalid 目标部门不存在或停用
var ErrTargetDeptInvalid = errors.New("目标部门不存在或已停用")

// ErrDeviceNotVisible 源设备不在操作者可见范围内
var ErrDeviceNotVisible = errors.New("设备不在可操作范围内")

var ErrAssignmentStale = errors.New("设备归属已被其他管理员修改")

type AssignmentStatus string

const (
	AssignmentChanged AssignmentStatus = "changed"
	AssignmentSkipped AssignmentStatus = "skipped"
	AssignmentFailed  AssignmentStatus = "failed"
)

type AssignmentInput struct {
	DeviceID            uint `json:"deviceId"`
	ExpectedOwnerDeptID uint `json:"expectedOwnerDeptId"`
}

type BatchSummary struct {
	Requested int `json:"requested"`
	Changed   int `json:"changed"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

type AssignmentResultItemV2 struct {
	DeviceID   uint             `json:"deviceId"`
	DeviceCode string           `json:"deviceCode"`
	Name       string           `json:"name"`
	Status     AssignmentStatus `json:"status"`
	Message    string           `json:"message"`
}

type AssignmentResultV2 struct {
	Summary BatchSummary             `json:"summary"`
	Results []AssignmentResultItemV2 `json:"results"`
}

// DeptValidator 校验目标部门存在/启用,返回(可见部门集合, 是否需要过滤, error)。
// 由 controller 注入 datascope.GetOwnerDeptIDsWithDB,service 不依赖 gin。
type DeptValidator func(ctx context.Context, db *gorm.DB) (visibleDeptIDs []uint, needFilter bool, err error)

// AssignResultItem 单台设备的分配结果。
type AssignResultItem struct {
	DeviceID uint   `json:"deviceId"`
	Success  bool   `json:"success"`
	Message  string `json:"message"`
}

// AssignResult 批量分配结果。
type AssignResult struct {
	Results []AssignResultItem `json:"results"`
}

// Service 设备归属分配(单事务,逐台)。
type Service struct {
	db       *gorm.DB
	validate DeptValidator
}

func NewService(db *gorm.DB, validate DeptValidator) *Service {
	return &Service{db: db, validate: validate}
}

func (s *Service) AssignBatchV2(ctx context.Context, inputs []AssignmentInput, targetDeptID uint) (AssignmentResultV2, error) {
	visibleDeptIDs, needFilter, err := s.validateTarget(ctx, targetDeptID)
	if err != nil {
		return AssignmentResultV2{}, err
	}
	unique := make([]AssignmentInput, 0, len(inputs))
	seen := make(map[uint]struct{}, len(inputs))
	for _, input := range inputs {
		if input.DeviceID == 0 {
			continue
		}
		if _, exists := seen[input.DeviceID]; exists {
			continue
		}
		seen[input.DeviceID] = struct{}{}
		unique = append(unique, input)
	}
	result := AssignmentResultV2{
		Summary: BatchSummary{Requested: len(unique)},
		Results: make([]AssignmentResultItemV2, 0, len(unique)),
	}
	for _, input := range unique {
		item := s.assignOneV2(ctx, input, targetDeptID, visibleDeptIDs, needFilter)
		result.Results = append(result.Results, item)
		switch item.Status {
		case AssignmentChanged:
			result.Summary.Changed++
		case AssignmentSkipped:
			result.Summary.Skipped++
		default:
			result.Summary.Failed++
		}
	}
	return result, nil
}

// AssignBatch 批量分配设备归属(逐台事务,一台失败不影响其他)。
func (s *Service) AssignBatch(ctx context.Context, deviceIDs []uint, targetDeptID uint) (AssignResult, error) {
	visibleDeptIDs, needFilter, err := s.validateTarget(ctx, targetDeptID)
	if err != nil {
		return AssignResult{}, err
	}

	result := AssignResult{Results: make([]AssignResultItem, 0, len(deviceIDs))}
	for _, deviceID := range deviceIDs {
		if itemErr := s.AssignOne(ctx, deviceID, targetDeptID, visibleDeptIDs, needFilter); itemErr != nil {
			result.Results = append(result.Results, AssignResultItem{DeviceID: deviceID, Success: false, Message: itemErr.Error()})
			continue
		}
		result.Results = append(result.Results, AssignResultItem{DeviceID: deviceID, Success: true, Message: "ok"})
	}
	return result, nil
}

// AssignOne 单台设备归属流转(单事务,含八类关联对象)。
func (s *Service) AssignOne(ctx context.Context, deviceID uint, targetDeptID uint, visibleDeptIDs []uint, needFilter bool) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var device gbmodels.GbDevice
		q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", deviceID)
		if needFilter {
			q = q.Where("owner_dept_id IN ?", visibleDeptIDs)
		}
		if err := q.First(&device).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeviceNotVisible
			}
			return err
		}

		return cascadeAssignment(tx, &device, targetDeptID)
	})
}

func (s *Service) assignOneV2(ctx context.Context, input AssignmentInput, targetDeptID uint, visibleDeptIDs []uint, needFilter bool) AssignmentResultItemV2 {
	item := AssignmentResultItemV2{DeviceID: input.DeviceID, Status: AssignmentFailed}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var device gbmodels.GbDevice
		q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", input.DeviceID)
		if needFilter {
			q = q.Where("owner_dept_id IN ?", visibleDeptIDs)
		}
		if err := q.First(&device).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeviceNotVisible
			}
			return err
		}
		item.DeviceCode = device.DeviceID
		item.Name = device.Name
		if device.OwnerDeptID != input.ExpectedOwnerDeptID {
			return ErrAssignmentStale
		}
		if device.OwnerDeptID == targetDeptID {
			item.Status = AssignmentSkipped
			item.Message = "设备已属于目标部门"
			return nil
		}
		if err := cascadeAssignment(tx, &device, targetDeptID); err != nil {
			return err
		}
		item.Status = AssignmentChanged
		item.Message = "归属调整成功"
		return nil
	})
	if err != nil {
		item.Status = AssignmentFailed
		item.Message = err.Error()
	}
	return item
}

func cascadeAssignment(tx *gorm.DB, device *gbmodels.GbDevice, targetDeptID uint) error {
	if err := tx.Model(&gbmodels.GbDevice{}).Where("id = ?", device.ID).
		UpdateColumn("owner_dept_id", targetDeptID).Error; err != nil {
		return err
	}
	if err := tx.Model(&gbmodels.GbChannel{}).Where("device_id = ?", device.DeviceID).
		UpdateColumn("owner_dept_id", targetDeptID).Error; err != nil {
		return err
	}
	// 3. 录像文件(按编码)
	if err := tx.Model(&gbmodels.GbRecordingFile{}).Where("device_id = ?", device.DeviceID).
		UpdateColumn("owner_dept_id", targetDeptID).Error; err != nil {
		return err
	}
	// 4. 告警资源(按主键)
	if err := tx.Model(&gbmodels.GbAlarmResource{}).Where("device_id = ?", device.ID).
		UpdateColumn("owner_dept_id", targetDeptID).Error; err != nil {
		return err
	}
	// 5. 异常记录(按 source_device_id)
	if err := tx.Model(&gbmodels.GbAnomalyRecord{}).Where("source_device_id = ?", device.ID).
		UpdateColumn("owner_dept_id", targetDeptID).Error; err != nil {
		return err
	}
	// 6. 目录投影(设备节点)与通道挂载
	if err := tx.Where("device_id = ?", device.ID).Delete(&gbmodels.GbCatalogNode{}).Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM gb_channel_mount WHERE channel_id IN (SELECT id FROM gb_channel WHERE device_id = ?)",
		device.DeviceID).Error; err != nil {
		return err
	}
	// 7. 自定义分组关系
	if err := tx.Where("device_id = ?", device.ID).Delete(&gbmodels.GbCustomGroupDevice{}).Error; err != nil {
		return err
	}
	// 8. 级联投影
	if err := tx.Exec("DELETE FROM gb_cascade_device_projection WHERE source_device_id = ?", device.ID).Error; err != nil {
		return err
	}
	return nil
}

func (s *Service) validateTarget(ctx context.Context, targetDeptID uint) ([]uint, bool, error) {
	if s.validate == nil {
		return nil, false, fmt.Errorf("缺少部门校验器")
	}
	visibleDeptIDs, needFilter, err := s.validate(ctx, s.db)
	if err != nil {
		return nil, false, err
	}
	if needFilter && !contains(visibleDeptIDs, targetDeptID) {
		return nil, false, ErrTargetDeptInvalid
	}
	if err := s.ensureDeptActive(ctx, targetDeptID); err != nil {
		return nil, false, err
	}
	return visibleDeptIDs, needFilter, nil
}

func (s *Service) ensureDeptActive(ctx context.Context, targetDeptID uint) error {
	var count int64
	if err := s.db.WithContext(ctx).Table("sys_department").
		Where("id = ? AND (status = 1 OR status IS NULL) AND deleted_at IS NULL", targetDeptID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrTargetDeptInvalid
	}
	return nil
}

func contains(list []uint, v uint) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
