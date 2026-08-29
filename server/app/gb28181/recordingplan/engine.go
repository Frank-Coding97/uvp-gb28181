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
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
)

type ChannelOperator interface {
	Start(context.Context, ChannelTarget) (*play.Result, error)
	Stop(context.Context, uint) error
}

type LiveCurrentProvider interface {
	CurrentLiveRef(string) (stream.LiveRef, bool)
}

type EngineOptions struct {
	InstanceID        string
	BatchSize         int
	Workers           int
	DeviceConcurrency int
	BoundaryJitter    time.Duration
	LeaseTTL          time.Duration
	Enabled           *bool
	Now               func() time.Time
}

type deviceLimiter struct {
	semaphore chan struct{}
	refs      int
}

type Engine struct {
	db         *gorm.DB
	repo       *Repository
	operator   ChannelOperator
	current    LiveCurrentProvider
	instanceID string
	batchSize  int
	workers    int
	deviceMax  int
	jitterMax  time.Duration
	leaseTTL   time.Duration
	enabled    bool
	now        func() time.Time
	runMu      sync.Mutex
	deviceMu   sync.Mutex
	devices    map[string]*deviceLimiter
}

func (e *Engine) SetLiveCurrentProvider(provider LiveCurrentProvider) { e.current = provider }

func (e *Engine) ObserveStream(ctx context.Context, streamID string, registered bool) error {
	if registered || streamID == "" || !e.enabled {
		return nil
	}
	reconcileAt := e.now().Add(2 * time.Second)
	return e.db.WithContext(ctx).Model(&models.GbRecordingPlanChannelState{}).
		Where("stream_id = ? AND actual_state = ?", streamID, models.RecordingStateRecording).
		Updates(map[string]any{"reconcile_at": reconcileAt, "reason_code": "MEDIA_STREAM_LOST_PENDING"}).Error
}

func NewEngine(db *gorm.DB, operator ChannelOperator, options EngineOptions) *Engine {
	if options.InstanceID == "" {
		options.InstanceID = "recording-plan"
	}
	if options.BatchSize <= 0 {
		options.BatchSize = 200
	}
	if options.Workers <= 0 {
		options.Workers = 8
	}
	if options.DeviceConcurrency <= 0 {
		options.DeviceConcurrency = 2
	}
	if options.BoundaryJitter <= 0 {
		options.BoundaryJitter = 5 * time.Second
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
	return &Engine{
		db: db, repo: NewRepository(db), operator: operator, instanceID: options.InstanceID,
		batchSize: options.BatchSize, workers: options.Workers, deviceMax: options.DeviceConcurrency,
		jitterMax: options.BoundaryJitter, leaseTTL: options.LeaseTTL, enabled: enabled,
		now: options.Now, devices: make(map[string]*deviceLimiter),
	}
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
	if len(states) == 0 {
		return nil
	}
	workerCount := min(e.workers, len(states))
	jobs := make(chan *models.GbRecordingPlanChannelState)
	errs := make(chan error, len(states))
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for state := range jobs {
				if err := e.reconcile(ctx, state, now); err != nil {
					errs <- err
				}
			}
		}()
	}
	for i := range states {
		jobs <- &states[i]
	}
	close(jobs)
	workers.Wait()
	close(errs)
	var combined error
	for err := range errs {
		combined = errors.Join(combined, err)
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

func (e *Engine) DeviceStatusChanged(ctx context.Context, deviceID string, online bool, reason string) {
	now := e.now()
	lastID := uint(0)
	for {
		var channels []models.GbChannel
		if err := e.db.WithContext(ctx).Where("device_id = ? AND id > ?", deviceID, lastID).Order("id").Limit(500).Find(&channels).Error; err != nil || len(channels) == 0 {
			return
		}
		for _, channel := range channels {
			updates := map[string]any{"reconcile_at": now}
			var state models.GbRecordingPlanChannelState
			found := e.db.WithContext(ctx).Where("channel_id = ?", channel.ID).Limit(1).Find(&state)
			if found.Error != nil {
				return
			}
			if found.RowsAffected > 0 && !online && state.DesiredState == models.RecordingDesiredRecording {
				updates["actual_state"] = models.RecordingStateWaitingDevice
				updates["reason_code"] = ReasonDeviceOffline
				updates["reason_message"] = reason
				_, _ = e.repo.OpenGap(ctx, state.PlanID, channel.ID, ReasonDeviceOffline, reason, now)
			}
			if found.RowsAffected > 0 {
				_ = e.db.WithContext(ctx).Model(&models.GbRecordingPlanChannelState{}).Where("channel_id = ?", channel.ID).Updates(updates).Error
			}
			lastID = channel.ID
		}
	}
}

func (e *Engine) reconcile(ctx context.Context, state *models.GbRecordingPlanChannelState, now time.Time) error {
	var channel models.GbChannel
	result := e.db.WithContext(ctx).First(&channel, state.ChannelID)
	if result.Error != nil {
		return result.Error
	}
	input := ReconcileInput{Mode: channel.RecordingMode, DeviceOnline: channel.Status == models.ChannelStatusOnline, ActualState: state.ActualState, Attempt: state.AttemptCount, Now: now}
	mediaLost := false
	if state.ActualState == models.RecordingStateRecording && state.StreamID != "" && e.current != nil {
		current, exists := e.current.CurrentLiveRef(state.StreamID)
		if !exists || (state.Generation > 0 && current.Generation != state.Generation) {
			mediaLost = true
			input.ActualState = models.RecordingStateIdle
		}
	}
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
	if mediaLost {
		_, _ = e.repo.OpenGap(ctx, state.PlanID, channel.ID, ReasonMediaStreamLost, "媒体流已注销", now)
	}
	streamID, generation, nodeID := state.StreamID, state.Generation, state.NodeID
	execution := models.GbRecordingPlanExecution{PlanID: state.PlanID, ChannelID: channel.ID, DeviceID: channel.DeviceID, TriggerSource: "scheduler", Attempt: state.AttemptCount + 1, StartedAt: now, CreatedAt: now}
	if decision.Action == ActionStart {
		execution.Action, execution.Stage = ActionStart, FailureStreamStart
		releaseDevice, acquireErr := e.acquireDevice(ctx, channel.DeviceID)
		if acquireErr != nil {
			return acquireErr
		}
		live, err := e.operator.Start(ctx, ChannelTarget{ID: channel.ID, DeviceCode: channel.DeviceID, ChannelCode: channel.ChannelID})
		releaseDevice()
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
				streamID, generation = live.StreamID, live.Generation
				if live.Node != nil {
					execution.NodeID = fmt.Sprint(live.Node.ID)
					nodeID = execution.NodeID
				}
			}
		}
	} else if decision.Action == ActionStop {
		execution.Action, execution.Stage = ActionStop, "record_stop"
		releaseDevice, acquireErr := e.acquireDevice(ctx, channel.DeviceID)
		if acquireErr != nil {
			return acquireErr
		}
		err := e.operator.Stop(ctx, channel.ID)
		releaseDevice()
		if err != nil {
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
		_, _ = e.repo.OpenGap(ctx, state.PlanID, channel.ID, decision.ReasonCode, decision.ReasonMessage, now)
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
	} else if nextTransition != nil {
		jitteredTransition := nextTransition.Add(deterministicJitter(channel.ID, e.jitterMax))
		if jitteredTransition.Before(reconcileAt) {
			reconcileAt = jitteredTransition
		}
	}
	updates := map[string]any{
		"plan_version": planVersion, "desired_state": decision.DesiredState, "actual_state": decision.ActualState,
		"reason_code": decision.ReasonCode, "reason_message": decision.ReasonMessage,
		"next_transition_at": nextTransition, "next_retry_at": decision.NextRetryAt, "reconcile_at": reconcileAt,
		"attempt_count": decision.Attempt, "lease_owner": "", "lease_until": nil,
		"stream_id": streamID, "generation": generation, "node_id": nodeID,
	}
	_, err := e.repo.UpdateStateCAS(ctx, channel.ID, state.PlanVersion, state.StateVersion, updates)
	return err
}

func (e *Engine) acquireDevice(ctx context.Context, deviceID string) (func(), error) {
	e.deviceMu.Lock()
	limiter := e.devices[deviceID]
	if limiter == nil {
		limiter = &deviceLimiter{semaphore: make(chan struct{}, e.deviceMax)}
		e.devices[deviceID] = limiter
	}
	limiter.refs++
	e.deviceMu.Unlock()
	select {
	case limiter.semaphore <- struct{}{}:
		return func() {
			<-limiter.semaphore
			e.releaseDeviceRef(deviceID, limiter)
		}, nil
	case <-ctx.Done():
		e.releaseDeviceRef(deviceID, limiter)
		return nil, ctx.Err()
	}
}

func (e *Engine) releaseDeviceRef(deviceID string, limiter *deviceLimiter) {
	e.deviceMu.Lock()
	defer e.deviceMu.Unlock()
	limiter.refs--
	if limiter.refs == 0 && e.devices[deviceID] == limiter {
		delete(e.devices, deviceID)
	}
}

func deterministicJitter(channelID uint, maximum time.Duration) time.Duration {
	if maximum <= 0 {
		return 0
	}
	steps := uint64(maximum / time.Millisecond)
	if steps == 0 {
		return 0
	}
	return time.Duration(uint64(channelID)%steps) * time.Millisecond
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
