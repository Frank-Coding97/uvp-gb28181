package recordingplan

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordingplan/schedule"
)

type ChannelOperator interface {
	Start(context.Context, ChannelTarget) (*play.Result, error)
	Stop(context.Context, uint) error
}

type EngineOptions struct {
	InstanceID string
	BatchSize  int
	LeaseTTL   time.Duration
	Enabled    *bool
	Now        func() time.Time
}

type Engine struct {
	db         *gorm.DB
	repo       *Repository
	operator   ChannelOperator
	instanceID string
	batchSize  int
	leaseTTL   time.Duration
	enabled    bool
	now        func() time.Time
	runMu      sync.Mutex
}

func NewEngine(db *gorm.DB, operator ChannelOperator, options EngineOptions) *Engine {
	if options.InstanceID == "" {
		options.InstanceID = "recording-plan"
	}
	if options.BatchSize <= 0 {
		options.BatchSize = 100
	}
	if options.LeaseTTL <= 0 {
		options.LeaseTTL = 15 * time.Second
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	enabled := true
	if options.Enabled != nil {
		enabled = *options.Enabled
	}
	return &Engine{db: db, repo: NewRepository(db), operator: operator, instanceID: options.InstanceID, batchSize: options.BatchSize, leaseTTL: options.LeaseTTL, enabled: enabled, now: options.Now}
}

func (e *Engine) Dispatch(ctx context.Context) error {
	if !e.enabled || e.operator == nil {
		return nil
	}
	e.runMu.Lock()
	defer e.runMu.Unlock()
	now := e.now()
	states, err := e.repo.ClaimDueStates(ctx, e.instanceID, now, e.leaseTTL, e.batchSize)
	if err != nil {
		return err
	}
	var combined error
	for i := range states {
		if err := e.reconcile(ctx, &states[i], now); err != nil {
			combined = errors.Join(combined, err)
		}
	}
	return combined
}

func (e *Engine) Heal(ctx context.Context) error {
	if !e.enabled || e.operator == nil {
		return nil
	}
	now := e.now()
	if err := e.ensureStateRows(ctx, now); err != nil {
		return err
	}
	if err := e.requestAllStateReconcile(ctx, now); err != nil {
		return err
	}
	return e.Dispatch(ctx)
}

func (e *Engine) reconcile(ctx context.Context, state *models.GbRecordingPlanChannelState, now time.Time) error {
	var channel models.GbChannel
	result := e.db.WithContext(ctx).First(&channel, state.ChannelID)
	if result.Error != nil {
		return result.Error
	}
	input := ReconcileInput{Mode: channel.RecordingMode, DeviceOnline: channel.Status == models.ChannelStatusOnline, ActualState: state.ActualState, Attempt: state.AttemptCount, Now: now}
	var planVersion uint64
	var nextTransition *time.Time
	if channel.RecordingMode == models.RecordingModeScheduled {
		var binding models.GbRecordingPlanBinding
		found := e.db.WithContext(ctx).Where("channel_id = ?", channel.ID).Limit(1).Find(&binding)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 {
			var plan models.GbRecordingPlan
			planFound := e.db.WithContext(ctx).Where("id = ?", binding.PlanID).Limit(1).Find(&plan)
			if planFound.Error != nil {
				return planFound.Error
			}
			if planFound.RowsAffected > 0 {
				periods, err := loadPeriods(e.db.WithContext(ctx), plan.ID)
				if err != nil {
					return err
				}
				evaluation := schedule.Evaluate(periods, now)
				input.PlanEnabled, input.ScheduleMatched = plan.Status == 1, evaluation.Matched
				input.PlanChanged = state.PlanVersion != 0 && state.PlanVersion != plan.Version
				planVersion, nextTransition = plan.Version, evaluation.NextTransition
			}
		}
	}
	decision := DecideReconcile(input)
	execution := models.GbRecordingPlanExecution{PlanID: state.PlanID, ChannelID: channel.ID, DeviceID: channel.DeviceID, TriggerSource: "scheduler", Attempt: state.AttemptCount + 1, StartedAt: now, CreatedAt: now}
	if decision.Action == ActionStart {
		execution.Action, execution.Stage = ActionStart, FailureStreamStart
		live, err := e.operator.Start(ctx, ChannelTarget{ID: channel.ID, DeviceCode: channel.DeviceID, ChannelCode: channel.ChannelID})
		if err != nil {
			failureStage, message := FailureStreamStart, err.Error()
			var orchestration *OrchestrationError
			if errors.As(err, &orchestration) {
				failureStage = orchestration.Stage
			}
			decision = DecideReconcile(ReconcileInput{Mode: channel.RecordingMode, PlanEnabled: input.PlanEnabled, ScheduleMatched: input.ScheduleMatched, DeviceOnline: input.DeviceOnline, FailureStage: failureStage, FailureMessage: message, Attempt: state.AttemptCount, Now: now})
			execution.Result, execution.ReasonCode, execution.ReasonMessage = "failed", decision.ReasonCode, message
		} else {
			decision.ActualState, decision.Attempt, decision.CloseGap = models.RecordingStateRecording, 0, true
			decision.NextRetryAt = nil
			execution.Result, execution.Stage = "success", FailureRecordStart
			if live != nil {
				execution.StreamID, execution.Generation = live.StreamID, live.Generation
				if live.Node != nil {
					execution.NodeID = fmt.Sprint(live.Node.ID)
				}
			}
		}
	} else if decision.Action == ActionStop {
		execution.Action, execution.Stage = ActionStop, "record_stop"
		if err := e.operator.Stop(ctx, channel.ID); err != nil {
			decision.ActualState, decision.ReasonCode, decision.ReasonMessage = models.RecordingStateStopping, "RECORD_STOP_FAILED", err.Error()
			retry := now.Add(time.Minute)
			decision.NextRetryAt = &retry
			decision.Attempt = state.AttemptCount + 1
			execution.Result, execution.ReasonCode, execution.ReasonMessage = "failed", decision.ReasonCode, err.Error()
		} else {
			decision.ActualState, decision.Attempt = models.RecordingStateIdle, 0
			execution.Result = "success"
		}
	}
	if decision.OpenGap {
		_, _ = e.repo.OpenGap(ctx, planIDValue(state.PlanID), channel.ID, decision.ReasonCode, decision.ReasonMessage, now)
	}
	if decision.CloseGap {
		_, _ = e.repo.CloseOpenGap(ctx, channel.ID, now, nil)
	}
	if execution.Action != "" {
		ended := e.now()
		execution.EndedAt = &ended
		execution.DurationMs = ended.Sub(execution.StartedAt).Milliseconds()
		if err := e.db.WithContext(ctx).Create(&execution).Error; err != nil {
			return err
		}
	}
	reconcileAt := now.Add(time.Minute)
	if decision.NextRetryAt != nil {
		reconcileAt = *decision.NextRetryAt
	} else if nextTransition != nil && nextTransition.Before(reconcileAt) {
		reconcileAt = *nextTransition
	}
	updates := map[string]any{
		"plan_version": planVersion, "desired_state": decision.DesiredState, "actual_state": decision.ActualState,
		"reason_code": decision.ReasonCode, "reason_message": decision.ReasonMessage,
		"next_transition_at": nextTransition, "next_retry_at": decision.NextRetryAt, "reconcile_at": reconcileAt,
		"attempt_count": decision.Attempt, "lease_owner": "", "lease_until": nil,
	}
	_, err := e.repo.UpdateStateCAS(ctx, channel.ID, state.PlanVersion, state.StateVersion, updates)
	return err
}

func (e *Engine) ensureStateRows(ctx context.Context, now time.Time) error {
	lastID := uint(0)
	for {
		var channels []models.GbChannel
		if err := e.db.WithContext(ctx).Where("id > ? AND recording_mode <> ?", lastID, models.RecordingModeOff).Order("id").Limit(500).Find(&channels).Error; err != nil {
			return err
		}
		if len(channels) == 0 {
			return nil
		}
		for _, channel := range channels {
			state := models.GbRecordingPlanChannelState{ChannelID: channel.ID, DesiredState: models.RecordingDesiredIdle, ActualState: models.RecordingStateIdle, ReconcileAt: now}
			if err := e.db.WithContext(ctx).Where("channel_id = ?", channel.ID).FirstOrCreate(&state).Error; err != nil {
				return err
			}
			lastID = channel.ID
		}
	}
}

func (e *Engine) requestAllStateReconcile(ctx context.Context, now time.Time) error {
	lastID := uint(0)
	for {
		var ids []uint
		if err := e.db.WithContext(ctx).Model(&models.GbRecordingPlanChannelState{}).Where("channel_id > ?", lastID).Order("channel_id").Limit(500).Pluck("channel_id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := e.db.WithContext(ctx).Model(&models.GbRecordingPlanChannelState{}).Where("channel_id IN ?", ids).Update("reconcile_at", now).Error; err != nil {
			return err
		}
		lastID = ids[len(ids)-1]
	}
}

func planIDValue(planID *uint64) uint64 {
	if planID == nil {
		return 0
	}
	return *planID
}
