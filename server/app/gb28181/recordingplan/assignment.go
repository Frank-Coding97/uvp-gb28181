package recordingplan

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordingplan/schedule"
)

const (
	SelectionByDevice  = "device"
	SelectionByChannel = "channel"

	AssignmentAssigned  = "assigned"
	AssignmentConflict  = "conflict"
	AssignmentForbidden = "forbidden"
	AssignmentNotFound  = "not_found"
)

var (
	ErrPlanDisabled          = &DomainError{Code: "RECORDING_PLAN_DISABLED", Message: "停用的录像计划不能新增分配"}
	ErrSelectionInvalid      = &DomainError{Code: "RECORDING_PLAN_SELECTION_INVALID", Message: "分配类型或选择项不合法"}
	ErrChannelModeInvalid    = &DomainError{Code: "RECORDING_MODE_INVALID", Message: "录像模式不合法"}
	ErrScheduledPlanRequired = &DomainError{Code: "RECORDING_SCHEDULE_REQUIRED", Message: "切换为计划录像前必须先分配启用的录像计划"}
)

type AssignmentSelection struct {
	Type string `json:"type"`
	IDs  []uint `json:"ids"`
}

type AssignmentItem struct {
	ID     uint   `json:"id"`
	Status string `json:"status"`
}

type AssignmentResult struct {
	Items         []AssignmentItem `json:"items"`
	AssignedCount int              `json:"assignedCount"`
}

type AssignmentOption struct {
	ID         uint   `json:"id"`
	Name       string `json:"name"`
	Code       string `json:"code"`
	DeviceCode string `json:"deviceCode,omitempty"`
	Online     bool   `json:"online"`
	Bound      bool   `json:"bound"`
}

type AssignmentOptionPage struct {
	List     []AssignmentOption `json:"list"`
	Total    int64              `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"pageSize"`
}

type AssignmentService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewAssignmentService(db *gorm.DB) *AssignmentService {
	return &AssignmentService{db: db, now: time.Now}
}

func (s *AssignmentService) SearchDevices(ctx context.Context, ownerDeptID uint, keyword string, page, pageSize int) (*AssignmentOptionPage, error) {
	return s.SearchDevicesFiltered(ctx, ownerDeptID, keyword, nil, page, pageSize)
}

func (s *AssignmentService) SearchDevicesFiltered(ctx context.Context, ownerDeptID uint, keyword string, online *bool, page, pageSize int) (*AssignmentOptionPage, error) {
	page, pageSize = normalizePage(page, pageSize)
	query := s.db.WithContext(ctx).Model(&models.GbDevice{}).Where("owner_dept_id = ?", ownerDeptID)
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR alias LIKE ? OR device_id LIKE ?", like, like, like)
	}
	if online != nil {
		query = query.Where("status = ?", *online)
	}
	result := &AssignmentOptionPage{List: []AssignmentOption{}, Page: page, PageSize: pageSize}
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	var rows []models.GbDevice
	if err := query.Order("id").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result.List = append(result.List, AssignmentOption{ID: row.ID, Name: row.Name, Code: row.DeviceID, Online: row.Status == models.DeviceStatusOnline})
	}
	return result, nil
}

func (s *AssignmentService) SearchChannels(ctx context.Context, ownerDeptID uint, keyword string, online *bool, page, pageSize int) (*AssignmentOptionPage, error) {
	page, pageSize = normalizePage(page, pageSize)
	query := s.db.WithContext(ctx).Model(&models.GbChannel{}).Where("owner_dept_id = ?", ownerDeptID)
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR alias LIKE ? OR channel_id LIKE ? OR device_id LIKE ?", like, like, like, like)
	}
	if online != nil {
		query = query.Where("status = ?", *online)
	}
	result := &AssignmentOptionPage{List: []AssignmentOption{}, Page: page, PageSize: pageSize}
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	var rows []models.GbChannel
	if err := query.Order("id").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	var bindings []models.GbRecordingPlanBinding
	if len(ids) > 0 {
		if err := s.db.WithContext(ctx).Where("channel_id IN ?", ids).Find(&bindings).Error; err != nil {
			return nil, err
		}
	}
	bound := make(map[uint]bool, len(bindings))
	for _, binding := range bindings {
		bound[binding.ChannelID] = true
	}
	for _, row := range rows {
		result.List = append(result.List, AssignmentOption{ID: row.ID, Name: row.Name, Code: row.ChannelID, DeviceCode: row.DeviceID, Online: row.Status == models.ChannelStatusOnline, Bound: bound[row.ID]})
	}
	return result, nil
}

func (s *AssignmentService) Assign(ctx context.Context, ownerDeptID, actorID uint, planID uint64, selection AssignmentSelection) (*AssignmentResult, error) {
	plan, err := findPlan(s.db.WithContext(ctx), ownerDeptID, planID)
	if err != nil {
		return nil, err
	}
	if plan.Status != 1 {
		return nil, ErrPlanDisabled
	}
	if len(selection.IDs) == 0 || (selection.Type != SelectionByChannel && selection.Type != SelectionByDevice) {
		return nil, ErrSelectionInvalid
	}
	channelIDs, result, err := s.resolveSelection(ctx, ownerDeptID, selection)
	if err != nil {
		return nil, err
	}
	if len(channelIDs) == 0 {
		return result, nil
	}
	periods, err := loadPeriods(s.db.WithContext(ctx), planID)
	if err != nil {
		return nil, err
	}
	evaluation := schedule.Evaluate(periods, s.now())
	desired := models.RecordingDesiredIdle
	if evaluation.Matched {
		desired = models.RecordingDesiredRecording
	}
	now := s.now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, channelID := range channelIDs {
			binding := models.GbRecordingPlanBinding{PlanID: planID, ChannelID: channelID, OwnerDeptID: ownerDeptID, AssignedBy: actorID, AssignedAt: now}
			if err := tx.Create(&binding).Error; err != nil {
				if isDuplicateError(err) {
					return ErrChannelAlreadyBound
				}
				return err
			}
			if err := tx.Model(&models.GbChannel{}).Where("id = ? AND owner_dept_id = ?", channelID, ownerDeptID).
				Updates(map[string]any{"recording_mode": models.RecordingModeScheduled, "cloud_recording_enabled": evaluation.Matched}).Error; err != nil {
				return err
			}
			state := models.GbRecordingPlanChannelState{
				ChannelID: channelID, PlanID: &planID, PlanVersion: plan.Version, DesiredState: desired,
				ActualState: models.RecordingStateIdle, NextTransitionAt: evaluation.NextTransition, ReconcileAt: now,
			}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "channel_id"}}, DoUpdates: clause.Assignments(map[string]any{
				"plan_id": planID, "plan_version": plan.Version, "desired_state": desired,
				"next_transition_at": evaluation.NextTransition, "reconcile_at": now, "lease_owner": "", "lease_until": nil,
			})}).Create(&state).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	result.AssignedCount = len(channelIDs)
	return result, nil
}

func (s *AssignmentService) SetMode(ctx context.Context, ownerDeptID, channelID uint, mode string) error {
	if mode != models.RecordingModeOff && mode != models.RecordingModeContinuous && mode != models.RecordingModeScheduled {
		return ErrChannelModeInvalid
	}
	now := s.now()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var channel models.GbChannel
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND owner_dept_id = ?", channelID, ownerDeptID).Limit(1).Find(&channel)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrPlanNotFound
		}
		desired := mode == models.RecordingModeContinuous
		var planID *uint64
		var planVersion uint64
		var nextTransition *time.Time
		if mode == models.RecordingModeScheduled {
			var binding models.GbRecordingPlanBinding
			found := tx.Where("channel_id = ?", channelID).Limit(1).Find(&binding)
			if found.Error != nil {
				return found.Error
			}
			if found.RowsAffected == 0 {
				return ErrScheduledPlanRequired
			}
			plan, err := findPlan(tx, ownerDeptID, binding.PlanID)
			if err != nil || plan.Status != 1 {
				return ErrScheduledPlanRequired
			}
			periods, err := loadPeriods(tx, binding.PlanID)
			if err != nil {
				return err
			}
			evaluation := schedule.Evaluate(periods, now)
			desired = evaluation.Matched
			planID, planVersion, nextTransition = &binding.PlanID, plan.Version, evaluation.NextTransition
		} else if err := tx.Where("channel_id = ?", channelID).Delete(&models.GbRecordingPlanBinding{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.GbChannel{}).Where("id = ?", channelID).Updates(map[string]any{
			"recording_mode": mode, "cloud_recording_enabled": desired,
		}).Error; err != nil {
			return err
		}
		desiredState := models.RecordingDesiredIdle
		if desired {
			desiredState = models.RecordingDesiredRecording
		}
		state := models.GbRecordingPlanChannelState{ChannelID: channelID, PlanID: planID, PlanVersion: planVersion, DesiredState: desiredState, ActualState: models.RecordingStateIdle, NextTransitionAt: nextTransition, ReconcileAt: now}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "channel_id"}}, DoUpdates: clause.Assignments(map[string]any{
			"plan_id": planID, "plan_version": planVersion, "desired_state": desiredState,
			"next_transition_at": nextTransition, "reconcile_at": now, "lease_owner": "", "lease_until": nil,
		})}).Create(&state).Error
	})
}

func (s *AssignmentService) resolveSelection(ctx context.Context, ownerDeptID uint, selection AssignmentSelection) ([]uint, *AssignmentResult, error) {
	if selection.Type == SelectionByDevice {
		var devices []models.GbDevice
		if err := s.db.WithContext(ctx).Where("id IN ? AND owner_dept_id = ?", selection.IDs, ownerDeptID).Find(&devices).Error; err != nil {
			return nil, nil, err
		}
		codes := make([]string, 0, len(devices))
		for _, device := range devices {
			codes = append(codes, device.DeviceID)
		}
		var channels []models.GbChannel
		if len(codes) > 0 {
			if err := s.db.WithContext(ctx).Where("device_id IN ? AND owner_dept_id = ?", codes, ownerDeptID).Order("id").Find(&channels).Error; err != nil {
				return nil, nil, err
			}
		}
		ids := make([]uint, 0, len(channels))
		for _, channel := range channels {
			ids = append(ids, channel.ID)
		}
		return s.classifyChannels(ctx, ownerDeptID, ids)
	}
	return s.classifyChannels(ctx, ownerDeptID, selection.IDs)
}

func (s *AssignmentService) classifyChannels(ctx context.Context, ownerDeptID uint, requested []uint) ([]uint, *AssignmentResult, error) {
	requested = uniqueIDs(requested)
	var channels []models.GbChannel
	if err := s.db.WithContext(ctx).Where("id IN ?", requested).Find(&channels).Error; err != nil {
		return nil, nil, err
	}
	channelByID := make(map[uint]models.GbChannel, len(channels))
	for _, channel := range channels {
		channelByID[channel.ID] = channel
	}
	var bindings []models.GbRecordingPlanBinding
	if err := s.db.WithContext(ctx).Where("channel_id IN ?", requested).Find(&bindings).Error; err != nil {
		return nil, nil, err
	}
	bound := make(map[uint]struct{}, len(bindings))
	for _, binding := range bindings {
		bound[binding.ChannelID] = struct{}{}
	}
	result := &AssignmentResult{Items: make([]AssignmentItem, 0, len(requested))}
	valid := make([]uint, 0, len(requested))
	for _, id := range requested {
		status := AssignmentAssigned
		channel, exists := channelByID[id]
		if !exists {
			status = AssignmentNotFound
		} else if channel.OwnerDeptID != ownerDeptID {
			status = AssignmentForbidden
		} else if _, exists := bound[id]; exists {
			status = AssignmentConflict
		} else {
			valid = append(valid, id)
		}
		result.Items = append(result.Items, AssignmentItem{ID: id, Status: status})
	}
	return valid, result, nil
}

func uniqueIDs(values []uint) []uint {
	seen := make(map[uint]struct{}, len(values))
	result := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}
