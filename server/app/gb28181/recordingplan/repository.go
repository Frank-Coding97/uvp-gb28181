package recordingplan

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

var ErrChannelAlreadyBound = errors.New("通道已分配到其他录像计划")

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) CreatePlan(ctx context.Context, plan *models.GbRecordingPlan, periods []models.GbRecordingPlanPeriod) error {
	desiredStatus := plan.Status
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(plan).Error; err != nil {
			return err
		}
		if plan.Status != desiredStatus {
			if err := tx.Model(plan).Update("status", desiredStatus).Error; err != nil {
				return err
			}
			plan.Status = desiredStatus
		}
		if len(periods) == 0 {
			return nil
		}
		for i := range periods {
			periods[i].ID = 0
			periods[i].PlanID = plan.ID
		}
		return tx.Create(&periods).Error
	})
}

func (r *Repository) BindChannels(ctx context.Context, planID uint64, ownerDeptID, actorID uint, channelIDs []uint, assignedAt time.Time) error {
	if len(channelIDs) == 0 {
		return nil
	}
	rows := make([]models.GbRecordingPlanBinding, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		rows = append(rows, models.GbRecordingPlanBinding{
			PlanID: planID, ChannelID: channelID, OwnerDeptID: ownerDeptID,
			AssignedBy: actorID, AssignedAt: assignedAt,
		})
	}
	err := r.db.WithContext(ctx).Create(&rows).Error
	if isDuplicateError(err) {
		return ErrChannelAlreadyBound
	}
	return err
}

func (r *Repository) ClaimDueStates(ctx context.Context, owner string, now time.Time, ttl time.Duration, batch int) ([]models.GbRecordingPlanChannelState, error) {
	if batch <= 0 {
		return nil, nil
	}
	var candidates []uint
	err := r.db.WithContext(ctx).Model(&models.GbRecordingPlanChannelState{}).
		Where("reconcile_at <= ? AND (lease_until IS NULL OR lease_until <= ?)", now, now).
		Order("reconcile_at, channel_id").Limit(batch).Pluck("channel_id", &candidates).Error
	if err != nil {
		return nil, err
	}
	leaseUntil := now.Add(ttl)
	claimed := make([]models.GbRecordingPlanChannelState, 0, len(candidates))
	for _, channelID := range candidates {
		result := r.db.WithContext(ctx).Model(&models.GbRecordingPlanChannelState{}).
			Where("channel_id = ? AND reconcile_at <= ? AND (lease_until IS NULL OR lease_until <= ?)", channelID, now, now).
			Updates(map[string]any{"lease_owner": owner, "lease_until": leaseUntil})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			continue
		}
		var row models.GbRecordingPlanChannelState
		if err := r.db.WithContext(ctx).First(&row, "channel_id = ?", channelID).Error; err != nil {
			return nil, err
		}
		claimed = append(claimed, row)
	}
	return claimed, nil
}

func (r *Repository) UpdateStateCAS(ctx context.Context, channelID uint, planVersion, stateVersion uint64, updates map[string]any) (bool, error) {
	values := make(map[string]any, len(updates)+1)
	for key, value := range updates {
		values[key] = value
	}
	values["state_version"] = gorm.Expr("state_version + 1")
	result := r.db.WithContext(ctx).Model(&models.GbRecordingPlanChannelState{}).
		Where("channel_id = ? AND plan_version = ? AND state_version = ?", channelID, planVersion, stateVersion).
		Updates(values)
	return result.RowsAffected == 1, result.Error
}

func (r *Repository) OpenGap(ctx context.Context, planID *uint64, channelID uint, reasonCode, reasonMessage string, startedAt time.Time) (*models.GbRecordingPlanGap, error) {
	var gap models.GbRecordingPlanGap
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("channel_id = ? AND ended_at IS NULL", channelID).Order("id DESC").Limit(1).Find(&gap)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			return nil
		}
		gap = models.GbRecordingPlanGap{
			PlanID: planID, ChannelID: channelID, StartedAt: startedAt,
			ReasonCode: reasonCode, ReasonMessage: reasonMessage,
		}
		return tx.Create(&gap).Error
	})
	return &gap, err
}

func (r *Repository) CloseOpenGap(ctx context.Context, channelID uint, endedAt time.Time, executionID *uint64) (bool, error) {
	var gap models.GbRecordingPlanGap
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("channel_id = ? AND ended_at IS NULL", channelID).Order("id DESC").Limit(1).Find(&gap)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		duration := endedAt.Sub(gap.StartedAt).Milliseconds()
		if duration < 0 {
			duration = 0
		}
		return tx.Model(&gap).Updates(map[string]any{
			"ended_at": endedAt, "duration_ms": duration, "recovered": true, "execution_id": executionID,
		}).Error
	})
	return gap.ID != 0, err
}

func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate") || strings.Contains(message, "unique constraint")
}
