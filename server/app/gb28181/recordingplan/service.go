package recordingplan

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordingplan/schedule"
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
	ErrPlanNotFound      = &DomainError{Code: "RECORDING_PLAN_NOT_FOUND", Message: "录像计划不存在"}
	ErrPlanNameInvalid   = &DomainError{Code: "RECORDING_PLAN_NAME_INVALID", Message: "计划名称不能为空且不能超过 128 个字符"}
	ErrPlanPeriodInvalid = &DomainError{Code: "RECORDING_PLAN_PERIOD_INVALID", Message: "录像时段配置不合法"}
	ErrPlanHasBindings   = &DomainError{Code: "RECORDING_PLAN_HAS_BINDINGS", Message: "录像计划仍有关联通道，不能删除"}
)

type PlanInput struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Enabled     bool              `json:"enabled"`
	Periods     []schedule.Period `json:"periods"`
}

type PlanDetail struct {
	ID          uint64            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Enabled     bool              `json:"enabled"`
	Version     uint64            `json:"version"`
	OwnerDeptID uint              `json:"ownerDeptId"`
	Periods     []schedule.Period `json:"periods"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}

type Service struct {
	db  *gorm.DB
	now func() time.Time
}

func NewService(db *gorm.DB) *Service { return &Service{db: db, now: time.Now} }

func (s *Service) Create(ctx context.Context, ownerDeptID, actorID uint, input PlanInput) (*PlanDetail, error) {
	name, periods, err := validatePlanInput(input)
	if err != nil {
		return nil, err
	}
	plan := &models.GbRecordingPlan{
		Name: name, Description: strings.TrimSpace(input.Description), Status: boolStatus(input.Enabled),
		Version: 1, OwnerDeptID: ownerDeptID, CreatedBy: actorID, UpdatedBy: actorID,
	}
	periodRows := makePeriodRows(periods)
	if err := NewRepository(s.db).CreatePlan(ctx, plan, periodRows); err != nil {
		return nil, err
	}
	return planDetail(plan, periods), nil
}

func (s *Service) Get(ctx context.Context, ownerDeptID uint, planID uint64) (*PlanDetail, error) {
	plan, err := findPlan(s.db.WithContext(ctx), ownerDeptID, planID)
	if err != nil {
		return nil, err
	}
	periods, err := loadPeriods(s.db.WithContext(ctx), planID)
	if err != nil {
		return nil, err
	}
	return planDetail(plan, periods), nil
}

func (s *Service) Page(ctx context.Context, ownerDeptID uint, keyword string, page, pageSize int) ([]models.GbRecordingPlan, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	query := s.db.WithContext(ctx).Model(&models.GbRecordingPlan{}).Where("owner_dept_id = ?", ownerDeptID)
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		query = query.Where("name LIKE ?", "%"+keyword+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.GbRecordingPlan
	err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	return rows, total, err
}

func (s *Service) Update(ctx context.Context, ownerDeptID, actorID uint, planID uint64, input PlanInput) (*PlanDetail, error) {
	name, periods, err := validatePlanInput(input)
	if err != nil {
		return nil, err
	}
	var updated models.GbRecordingPlan
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		plan, findErr := findPlan(tx.Clauses(clause.Locking{Strength: "UPDATE"}), ownerDeptID, planID)
		if findErr != nil {
			return findErr
		}
		newVersion := plan.Version + 1
		result := tx.Model(&models.GbRecordingPlan{}).
			Where("id = ? AND owner_dept_id = ? AND version = ?", planID, ownerDeptID, plan.Version).
			Updates(map[string]any{"name": name, "description": strings.TrimSpace(input.Description), "status": boolStatus(input.Enabled), "version": newVersion, "updated_by": actorID})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("录像计划已被其他请求修改")
		}
		if err := tx.Where("plan_id = ?", planID).Delete(&models.GbRecordingPlanPeriod{}).Error; err != nil {
			return err
		}
		periodRows := makePeriodRows(periods)
		for i := range periodRows {
			periodRows[i].PlanID = planID
		}
		if len(periodRows) > 0 {
			if err := tx.Create(&periodRows).Error; err != nil {
				return err
			}
		}
		channelIDs := tx.Model(&models.GbRecordingPlanBinding{}).Select("channel_id").Where("plan_id = ?", planID)
		if err := tx.Model(&models.GbRecordingPlanChannelState{}).Where("channel_id IN (?)", channelIDs).
			Updates(map[string]any{"plan_version": newVersion, "reconcile_at": s.now(), "lease_owner": "", "lease_until": nil}).Error; err != nil {
			return err
		}
		updated = *plan
		updated.Name = name
		updated.Description = strings.TrimSpace(input.Description)
		updated.Status = boolStatus(input.Enabled)
		updated.Version = newVersion
		updated.UpdatedBy = actorID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return planDetail(&updated, periods), nil
}

func (s *Service) SetEnabled(ctx context.Context, ownerDeptID, actorID uint, planID uint64, enabled bool) (*PlanDetail, error) {
	current, err := s.Get(ctx, ownerDeptID, planID)
	if err != nil {
		return nil, err
	}
	return s.Update(ctx, ownerDeptID, actorID, planID, PlanInput{
		Name: current.Name, Description: current.Description, Enabled: enabled, Periods: current.Periods,
	})
}

func (s *Service) Delete(ctx context.Context, ownerDeptID uint, planID uint64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := findPlan(tx.Clauses(clause.Locking{Strength: "UPDATE"}), ownerDeptID, planID); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&models.GbRecordingPlanBinding{}).Where("plan_id = ?", planID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return &DomainError{Code: ErrPlanHasBindings.Code, Message: ErrPlanHasBindings.Message, Details: map[string]int{"channelCount": int(count)}}
		}
		if err := tx.Where("plan_id = ?", planID).Delete(&models.GbRecordingPlanPeriod{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND owner_dept_id = ?", planID, ownerDeptID).Delete(&models.GbRecordingPlan{}).Error
	})
}

func validatePlanInput(input PlanInput) (string, []schedule.Period, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > 128 {
		return "", nil, ErrPlanNameInvalid
	}
	periods, err := schedule.Normalize(input.Periods, input.Enabled)
	if err != nil {
		return "", nil, &DomainError{Code: ErrPlanPeriodInvalid.Code, Message: err.Error()}
	}
	return name, periods, nil
}

func findPlan(db *gorm.DB, ownerDeptID uint, planID uint64) (*models.GbRecordingPlan, error) {
	var plan models.GbRecordingPlan
	result := db.Where("id = ? AND owner_dept_id = ?", planID, ownerDeptID).Limit(1).Find(&plan)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrPlanNotFound
	}
	return &plan, nil
}

func loadPeriods(db *gorm.DB, planID uint64) ([]schedule.Period, error) {
	var rows []models.GbRecordingPlanPeriod
	if err := db.Where("plan_id = ?", planID).Order("weekday, start_slot, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	periods := make([]schedule.Period, 0, len(rows))
	for _, row := range rows {
		periods = append(periods, schedule.Period{Weekday: int(row.Weekday), StartSlot: int(row.StartSlot), EndSlot: int(row.EndSlot)})
	}
	return periods, nil
}

func makePeriodRows(periods []schedule.Period) []models.GbRecordingPlanPeriod {
	rows := make([]models.GbRecordingPlanPeriod, 0, len(periods))
	for _, period := range periods {
		rows = append(rows, models.GbRecordingPlanPeriod{Weekday: int8(period.Weekday), StartSlot: int16(period.StartSlot), EndSlot: int16(period.EndSlot)})
	}
	return rows
}

func planDetail(plan *models.GbRecordingPlan, periods []schedule.Period) *PlanDetail {
	return &PlanDetail{ID: plan.ID, Name: plan.Name, Description: plan.Description, Enabled: plan.Status == 1, Version: plan.Version, OwnerDeptID: plan.OwnerDeptID, Periods: periods, CreatedAt: plan.CreatedAt, UpdatedAt: plan.UpdatedAt}
}

func boolStatus(value bool) int8 {
	if value {
		return 1
	}
	return 0
}
