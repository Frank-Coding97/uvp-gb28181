package ptz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

const (
	schedulerErrorApplicationTimeout      = "APPLICATION_TIMEOUT"
	schedulerErrorTransportUnknown        = "TRANSPORT_UNKNOWN"
	schedulerErrorHomePositionUnavailable = "HOME_POSITION_UNAVAILABLE"

	defaultSchedulerInterval     = 250 * time.Millisecond
	defaultSchedulerCapacity     = 16
	schedulerTransactionAttempts = 8
	schedulerTransportWindow     = 15 * time.Second
	schedulerApplicationWindow   = 15 * time.Second
)

type schedulerDispatchReservation interface {
	Commit(func(context.Context))
	Release()
}

type schedulerDispatcher interface {
	Reserve() (schedulerDispatchReservation, bool)
	Stop()
}

type schedulerDispatcherReservation struct {
	dispatcher *boundedSchedulerDispatcher
	once       sync.Once
}

func (r *schedulerDispatcherReservation) Commit(task func(context.Context)) {
	r.once.Do(func() { r.dispatcher.commit(task) })
}

func (r *schedulerDispatcherReservation) Release() {
	r.once.Do(r.dispatcher.release)
}

type boundedSchedulerDispatcher struct {
	ctx      context.Context
	cancel   context.CancelFunc
	slots    chan struct{}
	mu       sync.Mutex
	stopped  bool
	wg       sync.WaitGroup
	stopOnce sync.Once
}

func newBoundedSchedulerDispatcher(capacity int) *boundedSchedulerDispatcher {
	if capacity <= 0 {
		capacity = defaultSchedulerCapacity
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &boundedSchedulerDispatcher{ctx: ctx, cancel: cancel, slots: make(chan struct{}, capacity)}
}

func (d *boundedSchedulerDispatcher) Reserve() (schedulerDispatchReservation, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return nil, false
	}
	select {
	case d.slots <- struct{}{}:
		return &schedulerDispatcherReservation{dispatcher: d}, true
	default:
		return nil, false
	}
}

func (d *boundedSchedulerDispatcher) commit(task func(context.Context)) {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		d.release()
		return
	}
	d.wg.Add(1)
	d.mu.Unlock()
	go func() {
		defer d.wg.Done()
		defer d.release()
		task(d.ctx)
	}()
}

func (d *boundedSchedulerDispatcher) release() {
	select {
	case <-d.slots:
	default:
	}
}

func (d *boundedSchedulerDispatcher) Stop() {
	d.stopOnce.Do(func() {
		d.mu.Lock()
		d.stopped = true
		d.cancel()
		d.mu.Unlock()
		d.wg.Wait()
	})
}

type schedulerConfig struct {
	interval   time.Duration
	dispatcher schedulerDispatcher
}

type SchedulerOption func(*schedulerConfig)

func WithSchedulerInterval(interval time.Duration) SchedulerOption {
	return func(config *schedulerConfig) {
		if interval > 0 {
			config.interval = interval
		}
	}
}

func WithSchedulerCapacity(capacity int) SchedulerOption {
	return func(config *schedulerConfig) {
		if capacity > 0 {
			config.dispatcher = newBoundedSchedulerDispatcher(capacity)
		}
	}
}

func WithSchedulerDispatcher(dispatcher schedulerDispatcher) SchedulerOption {
	return func(config *schedulerConfig) {
		if dispatcher != nil {
			config.dispatcher = dispatcher
		}
	}
}

// Scheduler owns the durable PTZ attempt coordinator and its bounded sender
// workers. RunDue is deterministic so recovery can be tested with a fake clock.
type Scheduler struct {
	service    *Service
	db         *gorm.DB
	dispatcher schedulerDispatcher
	interval   time.Duration

	lifecycleMu sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}

	activeMu            sync.Mutex
	active              map[uint]schedulerActiveAttempt
	pendingMu           sync.Mutex
	pendingResults      map[uint]schedulerPendingResult
	pendingClaims       []*playauth.PTZUnissuedAttempt
	pendingReservations []*playauth.PTZReservationOutcome
}

type schedulerPendingResult struct {
	attempt    gbmodels.GbPTZOperationAttempt
	result     uac.TrackedMessageResult
	sendErr    error
	observedAt time.Time
	lease      playauth.DeviceOperationLease
}

type schedulerActiveAttempt struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func NewScheduler(service *Service, options ...SchedulerOption) *Scheduler {
	config := schedulerConfig{interval: defaultSchedulerInterval}
	for _, option := range options {
		option(&config)
	}
	if config.dispatcher == nil {
		config.dispatcher = newBoundedSchedulerDispatcher(defaultSchedulerCapacity)
	}
	scheduler := &Scheduler{
		service: service, dispatcher: config.dispatcher, interval: config.interval,
		active: make(map[uint]schedulerActiveAttempt),
	}
	if service != nil {
		scheduler.db = service.db
	}
	return scheduler
}

func (s *Scheduler) Start(parent context.Context) {
	if s == nil {
		return
	}
	if parent == nil {
		parent = context.Background()
	}
	s.lifecycleMu.Lock()
	if s.cancel != nil {
		s.lifecycleMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	s.cancel, s.done = cancel, done
	s.lifecycleMu.Unlock()

	go func() {
		defer close(done)
		_ = s.runDue(ctx, s.service.now())
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.runDue(ctx, s.service.now())
			}
		}
	}()
}

// Stop first prevents new claims, then cancels and waits for every sender.
func (s *Scheduler) Stop() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	s.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	s.cancelAllActive()
	s.dispatcher.Stop()
	ctx, finish := context.WithTimeout(context.Background(), 2*time.Second)
	defer finish()
	if err := s.flushPendingResults(ctx); err != nil && app.ZapLog != nil {
		app.ZapLog.Error("PTZ 停止后仍有未持久化结果，保留设备租约", zap.String("event", "ptz.scheduler.stop_flush_failed"), zap.Error(err))
	}
}

// Retained facts are retried without calling the sender. A failed flush leaves
// the original lease owned here, including after Stop, rather than falsely
// declaring the device drained.
func (s *Scheduler) flushPendingResults(ctx context.Context) error {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	for len(s.pendingReservations) > 0 {
		if err := s.pendingReservations[0].Reconcile(ctx); err != nil {
			return err
		}
		s.pendingReservations = s.pendingReservations[1:]
	}
	for len(s.pendingClaims) > 0 {
		if err := s.pendingClaims[0].Reconcile(ctx); err != nil {
			return err
		}
		s.pendingClaims = s.pendingClaims[1:]
	}
	for id, pending := range s.pendingResults {
		if err := s.persistAttemptResult(ctx, pending.attempt, pending.result, pending.sendErr, pending.observedAt); err != nil {
			return err
		}
		if pending.lease != nil {
			pending.lease.Release()
		}
		delete(s.pendingResults, id)
	}
	return nil
}

func (s *Scheduler) retainUnissued(err error) {
	var ticket *playauth.PTZUnissuedAttempt
	if errors.As(err, &ticket) {
		s.pendingMu.Lock()
		s.pendingClaims = append(s.pendingClaims, ticket)
		s.pendingMu.Unlock()
	}
}

// FlushResults retries local facts only. Root teardown must retain this owner
// when it fails, and must not construct a replacement runtime over it.
func (s *Scheduler) FlushResults(ctx context.Context) error { return s.flushPendingResults(ctx) }

func (s *Scheduler) cancelAllActive() {
	s.activeMu.Lock()
	for _, owner := range s.active {
		owner.cancel()
	}
	s.activeMu.Unlock()
}

func (s *Scheduler) cancelAttempt(ctx context.Context, attemptID uint) error {
	s.activeMu.Lock()
	owner, exists := s.active[attemptID]
	s.activeMu.Unlock()
	if exists {
		owner.cancel()
		// Expiration requests cancellation; it does not prove that Send or
		// its result writeback has returned. Never recover over a live owner.
		select {
		case <-owner.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// RunDue performs all deadline convergence before it claims any work.
func (s *Scheduler) RunDue(now time.Time) error {
	return s.runDue(context.Background(), now)
}

func (s *Scheduler) runDue(ctx context.Context, now time.Time) error {
	if s == nil || s.db == nil || s.service == nil || s.service.sender == nil {
		return errors.New("PTZ scheduler 未就绪")
	}
	if err := s.flushPendingResults(ctx); err != nil {
		return err
	}
	if syncOwner := s.service.synchronous; syncOwner != nil && syncOwner != s {
		if err := syncOwner.flushPendingResults(ctx); err != nil {
			return err
		}
	}
	steps := []func(context.Context, time.Time) error{
		s.expireQueued, s.expireTransport, s.expireApplication, s.recoverExpiredLeases,
	}
	for _, step := range steps {
		if err := step(ctx, now); err != nil {
			return err
		}
	}
	if err := s.service.cleanupQueryStages(ctx, now); err != nil {
		return err
	}
	// 配置读取的静止收敛：设备少回几条时不能干等到 transport deadline,
	// 否则"设备不支持这些类型"（正确的 type_absent 结论）会被误报成超时。
	if err := s.service.settleConfigReadStages(ctx, now); err != nil {
		return err
	}
	return s.claimDue(ctx, now)
}

func (s *Scheduler) expireQueued(ctx context.Context, now time.Time) error {
	var bound []gbmodels.GbPTZOperation
	if err := ptzWriter(s.db).WithContext(ctx).
		Where("response_required = ? AND status = ? AND attempt = 0 AND queue_deadline_at <= ?", true, gbmodels.PTZOperationQueued, gbmodels.PTZTimeComparison(s.db, now)).
		Where("device_intent_id IS NOT NULL OR device_epoch IS NOT NULL").Find(&bound).Error; err != nil {
		return err
	}
	for _, op := range bound {
		id, err := ptzIntentIdentity(op)
		if err != nil {
			return err
		}
		if s.service.intents == nil {
			return playauth.ErrDeviceIntentUnavailable
		}
		if err := s.service.intents.ExpirePTZReservation(ctx, id, op.ID, now); err != nil {
			return err
		}
	}
	return s.db.WithContext(ctx).Model(&gbmodels.GbPTZOperation{}).
		Where("device_intent_id IS NULL AND device_epoch IS NULL").
		Where("response_required = ? AND status = ? AND attempt = 0 AND queue_deadline_at IS NOT NULL AND queue_deadline_at <= ?", true, gbmodels.PTZOperationQueued, gbmodels.PTZTimeComparison(s.db, now)).
		Updates(map[string]interface{}{
			"status": gbmodels.PTZOperationRejected, "error_code": schedulerErrorHomePositionUnavailable,
			"error_message": "PTZ operation 排队超时", "completed_at": now, "next_attempt_at": nil,
		}).Error
}

func (s *Scheduler) expireTransport(ctx context.Context, now time.Time) error {
	return s.db.WithContext(ctx).Model(&gbmodels.GbPTZOperation{}).
		Where("response_required = ? AND status = ? AND transport_deadline_at IS NOT NULL AND transport_deadline_at <= ?", true, gbmodels.PTZOperationQueued, gbmodels.PTZTimeComparison(s.db, now)).
		Updates(map[string]interface{}{
			"status": gbmodels.PTZOperationUnknown, "error_code": schedulerErrorTransportUnknown,
			"error_message": "PTZ MESSAGE 传输结果不确定", "completed_at": now, "next_attempt_at": nil,
		}).Error
}

func (s *Scheduler) expireApplication(ctx context.Context, now time.Time) error {
	return s.db.WithContext(ctx).Model(&gbmodels.GbPTZOperation{}).
		Where("response_required = ? AND status = ? AND deadline_at IS NOT NULL AND deadline_at <= ?", true, gbmodels.PTZOperationSent, gbmodels.PTZTimeComparison(s.db, now)).
		Updates(map[string]interface{}{
			"status": gbmodels.PTZOperationTimeout, "error_code": schedulerErrorApplicationTimeout,
			"error_message": "等待设备应用层响应超时", "completed_at": now, "next_attempt_at": nil,
		}).Error
}

func (s *Scheduler) recoverExpiredLeases(ctx context.Context, now time.Time) error {
	var attempts []gbmodels.GbPTZOperationAttempt
	if err := ptzWriter(s.db).WithContext(ctx).Table("gb_ptz_operation_attempt AS attempt").
		Select("attempt.*").
		Joins("JOIN gb_ptz_operation AS operation ON operation.id = attempt.operation_id").
		Where("operation.response_required = ? AND attempt.status = ? AND attempt.lease_until <= ?", true, gbmodels.PTZOperationAttemptDispatching, gbmodels.PTZTimeComparison(s.db, now)).
		Order("attempt.id").Find(&attempts).Error; err != nil {
		return err
	}
	for _, attempt := range attempts {
		if err := s.cancelAttempt(ctx, attempt.ID); err != nil {
			return err
		}
		if err := schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
			result := tx.Model(&gbmodels.GbPTZOperationAttempt{}).
				Where("id = ? AND status = ? AND lease_until <= ?", attempt.ID, gbmodels.PTZOperationAttemptDispatching, gbmodels.PTZTimeComparison(tx, now)).
				Updates(map[string]interface{}{
					"status": gbmodels.PTZOperationAttemptUnknown, "completed_at": now,
					"error_code": schedulerErrorTransportUnknown, "error_message": "sender lease expired",
				})
			if result.Error != nil || result.RowsAffected == 0 {
				return result.Error
			}
			var operation gbmodels.GbPTZOperation
			if err := tx.First(&operation, attempt.OperationID).Error; err != nil {
				return err
			}
			// 没有剩余重试槽位时必须收敛 operation 终态:否则 attempt==max_attempts
			// 且 next_attempt_at==nil,claimDue 永远跳过,API 轮询永久停在未完成
			if operation.MaxAttempts <= 1 || operation.Attempt >= operation.MaxAttempts {
				return tx.Model(&gbmodels.GbPTZOperation{}).
					Where("id = ? AND response_required = ? AND status = ?", operation.ID, true, gbmodels.PTZOperationQueued).
					Updates(map[string]interface{}{
						"status": gbmodels.PTZOperationUnknown, "error_code": schedulerErrorTransportUnknown,
						"error_message": "PTZ MESSAGE 传输结果不确定", "completed_at": now, "next_attempt_at": nil,
					}).Error
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scheduler) claimDue(ctx context.Context, now time.Time) error {
	var candidates []gbmodels.GbPTZOperation
	if err := ptzWriter(s.db).WithContext(ctx).
		Where("response_required = ? AND status IN ? AND attempt < max_attempts", true, []gbmodels.PTZOperationStatus{gbmodels.PTZOperationQueued, gbmodels.PTZOperationSent}).
		Where(`(attempt = 0 AND dispatch_started_at IS NULL AND queue_deadline_at > ?)
			OR (attempt > 0 AND next_attempt_at IS NOT NULL AND next_attempt_at <= ? AND transport_deadline_at > ?)`, gbmodels.PTZTimeComparison(s.db, now), gbmodels.PTZTimeComparison(s.db, now), gbmodels.PTZTimeComparison(s.db, now)).
		Order("id").Find(&candidates).Error; err != nil {
		return err
	}
	for _, candidate := range candidates {
		reservation, ok := s.dispatcher.Reserve()
		if !ok {
			return nil
		}
		attempt, claimed, err := s.claimAttempt(ctx, candidate.ID, now)
		if err != nil {
			s.retainUnissued(err)
			reservation.Release()
			if errors.Is(err, playauth.ErrDeviceIntentRevoked) {
				if err := s.revokeOperation(ctx, candidate, now); err != nil {
					return err
				}
				continue
			}
			return err
		}
		if !claimed {
			reservation.Release()
			continue
		}
		reservation.Commit(func(dispatchCtx context.Context) {
			s.dispatchAttempt(dispatchCtx, attempt)
		})
	}
	return nil
}

// Revocation stops future work, not history. A possibly sent attempt remains
// unknown; only a command without such evidence can be marked rejected.
func (s *Scheduler) revokeOperation(ctx context.Context, expected gbmodels.GbPTZOperation, now time.Time) error {
	return schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		var uncertain int64
		if err := tx.Model(&gbmodels.GbPTZOperationAttempt{}).
			Where("operation_id = ? AND status IN ?", expected.ID, []gbmodels.PTZOperationAttemptStatus{gbmodels.PTZOperationAttemptSent, gbmodels.PTZOperationAttemptUnknown, gbmodels.PTZOperationAttemptDispatching}).Count(&uncertain).Error; err != nil {
			return err
		}
		status := gbmodels.PTZOperationRejected
		if uncertain != 0 || expected.Status == gbmodels.PTZOperationSent || ((expected.DeviceEpoch == nil || expected.DeviceIntentID == nil) && expected.Attempt > 0) {
			status = gbmodels.PTZOperationUnknown
		}
		return tx.Model(&gbmodels.GbPTZOperation{}).
			Where("id = ? AND attempt = ? AND status = ?", expected.ID, expected.Attempt, expected.Status).
			Updates(map[string]any{"status": status, "error_code": "DEVICE_EPOCH_REVOKED", "error_message": "原设备授权已失效", "next_attempt_at": nil, "completed_at": now}).Error
	})
}

func (s *Scheduler) claimAttempt(ctx context.Context, operationID uint, now time.Time) (gbmodels.GbPTZOperationAttempt, bool, error) {
	// This read selects a protocol; it never refreshes the operation's epoch.
	// The authorized store rechecks the exact binding inside its transaction.
	var original gbmodels.GbPTZOperation
	if err := ptzWriter(s.db).WithContext(ctx).First(&original, operationID).Error; err != nil {
		return gbmodels.GbPTZOperationAttempt{}, false, err
	}
	if requiresPTZIntent(original) {
		id, err := ptzIntentIdentity(original)
		if err != nil {
			// Historical NULL authorization is never refreshed. Stop this
			// command rather than failing every future scheduling batch.
			return gbmodels.GbPTZOperationAttempt{}, false, playauth.ErrDeviceIntentRevoked
		}
		if s.service.intents == nil || s.service.barrier == nil {
			return gbmodels.GbPTZOperationAttempt{}, false, playauth.ErrDeviceIntentUnavailable
		}
		attempt, err := s.service.intents.ClaimPTZAttempt(ctx, id, original.ID, original.Attempt, now)
		return attempt, err == nil, err
	}
	var claimed gbmodels.GbPTZOperationAttempt
	err := schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		claimed = gbmodels.GbPTZOperationAttempt{}
		var operation gbmodels.GbPTZOperation
		if result := tx.First(&operation, operationID); result.Error != nil {
			return result.Error
		}
		if !schedulerOperationDue(operation, now) {
			return nil
		}
		var active int64
		if err := tx.Model(&gbmodels.GbPTZOperationAttempt{}).
			Where("operation_id = ? AND status = ?", operation.ID, gbmodels.PTZOperationAttemptDispatching).
			Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return nil
		}

		startedAt := now
		transportDeadline := now.Add(schedulerTransportWindow)
		if operation.DispatchStartedAt != nil {
			startedAt = *operation.DispatchStartedAt
		}
		if operation.TransportDeadlineAt != nil {
			transportDeadline = *operation.TransportDeadlineAt
		}
		attemptNo := operation.Attempt + 1
		leaseUntil, nextAttempt := schedulerBoundaries(startedAt, now, operation.MaxAttempts)
		if transportDeadline.Before(leaseUntil) {
			leaseUntil = transportDeadline
		}

		updates := map[string]interface{}{
			"attempt": attemptNo, "next_attempt_at": nextAttempt,
		}
		if operation.Attempt == 0 {
			updates["dispatch_started_at"] = now
			updates["transport_deadline_at"] = transportDeadline
		}
		query := tx.Model(&gbmodels.GbPTZOperation{}).
			Where("id = ? AND response_required = ? AND status IN ? AND attempt = ? AND attempt < max_attempts",
				operation.ID, true, []gbmodels.PTZOperationStatus{gbmodels.PTZOperationQueued, gbmodels.PTZOperationSent}, operation.Attempt)
		if operation.Attempt == 0 {
			query = query.Where("dispatch_started_at IS NULL AND queue_deadline_at > ?", gbmodels.PTZTimeComparison(tx, now))
		} else {
			query = query.Where("next_attempt_at IS NOT NULL AND next_attempt_at <= ? AND transport_deadline_at > ?", gbmodels.PTZTimeComparison(tx, now), gbmodels.PTZTimeComparison(tx, now))
		}
		result := query.Updates(updates)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		claimed = gbmodels.GbPTZOperationAttempt{
			OperationID: operation.ID, AttemptNo: attemptNo, SN: operation.SN,
			Status: gbmodels.PTZOperationAttemptDispatching, StartedAt: now, LeaseUntil: leaseUntil,
		}
		return tx.Create(&claimed).Error
	})
	return claimed, claimed.ID != 0, err
}

func schedulerOperationDue(operation gbmodels.GbPTZOperation, now time.Time) bool {
	if !operation.ResponseRequired || operation.Attempt >= operation.MaxAttempts ||
		(operation.Status != gbmodels.PTZOperationQueued && operation.Status != gbmodels.PTZOperationSent) {
		return false
	}
	if operation.Attempt == 0 {
		return operation.DispatchStartedAt == nil && operation.QueueDeadlineAt != nil && operation.QueueDeadlineAt.After(now)
	}
	return operation.NextAttemptAt != nil && !operation.NextAttemptAt.After(now) &&
		operation.TransportDeadlineAt != nil && operation.TransportDeadlineAt.After(now)
}

func schedulerBoundaries(startedAt, now time.Time, maxAttempts int) (time.Time, *time.Time) {
	transportDeadline := startedAt.Add(schedulerTransportWindow)
	if maxAttempts <= 1 {
		return transportDeadline, nil
	}
	retrySlots := []time.Time{startedAt.Add(time.Second), startedAt.Add(6 * time.Second)}
	leaseUntil := transportDeadline
	var nextAttempt *time.Time
	for _, slot := range retrySlots {
		if slot.After(now) {
			leaseUntil = slot
			next := slot
			nextAttempt = &next
			break
		}
	}
	return leaseUntil, nextAttempt
}

func (s *Scheduler) dispatchAttempt(dispatchCtx context.Context, attempt gbmodels.GbPTZOperationAttempt) {
	_ = s.dispatchPreparedAttempt(dispatchCtx, attempt, nil)
}

func (s *Scheduler) dispatchPreparedAttempt(dispatchCtx context.Context, attempt gbmodels.GbPTZOperationAttempt, preparedBody []byte) error {
	ctx, cancel := context.WithDeadline(dispatchCtx, attempt.LeaseUntil)
	done := make(chan struct{})
	s.activeMu.Lock()
	s.active[attempt.ID] = schedulerActiveAttempt{cancel: cancel, done: done}
	s.activeMu.Unlock()
	defer func() {
		cancel()
		s.activeMu.Lock()
		delete(s.active, attempt.ID)
		close(done)
		s.activeMu.Unlock()
	}()

	var operation gbmodels.GbPTZOperation
	var result uac.TrackedMessageResult
	err := ptzWriter(s.db).WithContext(ctx).First(&operation, attempt.OperationID).Error
	var lease playauth.DeviceOperationLease
	if err == nil && requiresPTZIntent(operation) {
		var id playauth.DeviceOperationIntentIdentity
		id, err = ptzIntentIdentity(operation)
		if err == nil && s.service.barrier == nil {
			err = playauth.ErrDeviceIntentUnavailable
		}
		if err == nil {
			lease, err = s.service.barrier.BeginEpoch(ctx, id.DeviceCode, id.DeviceEpoch)
			if err == nil {
				ctx = lease.Context()
			}
		}
	}
	if err == nil {
		var destination, transport string
		destination, transport, err = s.schedulerTarget(ctx, operation)
		if err == nil {
			var body []byte
			if preparedBody != nil {
				body = preparedBody
			} else {
				body, err = buildScheduledPTZBody(operation)
			}
			if err == nil {
				result, err = s.service.sender.SendMessageTracked(ctx, operation.DeviceCode, destination, transport, body)
			}
		}
	}
	observedAt := s.service.now()
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(dispatchCtx), 2*time.Second)
	defer persistCancel()
	if persistErr := s.persistAttemptResult(persistCtx, attempt, result, err, observedAt); persistErr != nil {
		s.pendingMu.Lock()
		if s.pendingResults == nil {
			s.pendingResults = make(map[uint]schedulerPendingResult)
		}
		s.pendingResults[attempt.ID] = schedulerPendingResult{attempt: attempt, result: result, sendErr: err, observedAt: observedAt, lease: lease}
		s.pendingMu.Unlock()
		// 写回失败不得无声:否则 attempt 停留 dispatching,直到租约恢复才可能被发现
		if app.ZapLog != nil {
			app.Log(persistCtx).Named("ptz.scheduler").Error("PTZ 调度结果持久化失败",
				zap.String("event", "ptz.scheduler.persist_failed"),
				zap.Uint("attempt_id", attempt.ID), zap.Uint("operation_id", attempt.OperationID), zap.Error(persistErr))
		}
		return persistErr
	} else if lease != nil {
		lease.Release()
	}
	return err
}

func (s *Scheduler) schedulerTarget(ctx context.Context, operation gbmodels.GbPTZOperation) (string, string, error) {
	var device gbmodels.GbDevice
	result := ptzWriter(s.db).WithContext(ctx).Where("id = ? AND device_id = ?", operation.DeviceID, operation.DeviceCode).Limit(1).Find(&device)
	if result.Error != nil {
		return "", "", result.Error
	}
	if result.RowsAffected == 0 || strings.TrimSpace(device.IP) == "" || device.Port <= 0 || device.Status != gbmodels.DeviceStatusOnline {
		return "", "", errors.New("PTZ 设备地址不可用")
	}
	// 重试路径必须复核通道:设备在线而通道已离线或目录已变更时,
	// 不得对失效目标继续发送 PTZ 指令。用写库句柄防读副本返回过期在线状态
	var channel gbmodels.GbChannel
	channelResult := ptzWriter(s.db).WithContext(ctx).
		Where("id = ? AND device_id = ? AND channel_id = ?", operation.ChannelID, operation.DeviceCode, operation.ChannelCode).
		Limit(1).Find(&channel)
	if channelResult.Error != nil {
		return "", "", channelResult.Error
	}
	if channelResult.RowsAffected == 0 || channel.Status != gbmodels.ChannelStatusOnline {
		return "", "", errors.New("PTZ 通道不在线或已失效")
	}
	return net.JoinHostPort(device.IP, strconv.Itoa(device.Port)), device.Transport, nil
}

func buildScheduledPTZBody(operation gbmodels.GbPTZOperation) ([]byte, error) {
	profile := operationProtocolProfile(operation)
	targetCode := strings.TrimSpace(operation.TargetCode)
	if targetCode == "" {
		targetCode = operation.ChannelCode
	}
	buildPrecise := func() ([]byte, error) {
		var payload struct {
			Pan   *float64
			Tilt  *float64
			Zoom  *float64
			Focus *float64
			Iris  *float64
			Speed int
		}
		if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil {
			return nil, err
		}
		command := manscdp.PTZPreciseControl{
			Pan: payload.Pan, Tilt: payload.Tilt, Zoom: payload.Zoom,
			Focus: payload.Focus, Iris: payload.Iris, Speed: payload.Speed,
		}
		if profile.SupportsPrecisePTZ() {
			return manscdp.BuildPTZPreciseDeviceControlWithProfile(profile, targetCode, operation.SN, command)
		}
		return manscdp.BuildPTZPreciseControlWithProfile(profile, targetCode, operation.SN, command)
	}
	switch operation.CmdType {
	case manscdp.CmdDeviceStatus:
		return manscdp.BuildDeviceStatusQueryWithProfile(profile, targetCode, operation.SN)
	case manscdp.CmdSDCardStatus:
		// 重试路径必须也能重建这一帧,否则超时后的第 2/3 次尝试会直接失败。
		return manscdp.BuildSDCardStatusQueryWithProfile(profile, targetCode, operation.SN)
	case manscdp.CmdConfigDownload:
		var payload struct {
			ConfigTypes []string `json:"configTypes"`
		}
		if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil {
			return nil, err
		}
		if len(payload.ConfigTypes) == 0 {
			// 与 ReadVideoParams 建 operation 时写进去的取值保持一致。
			// 空列表在这里兜底而不是报错:monitoring 里那些只带
			// triggerOperationId 的对账 operation 也该能重建。
			payload.ConfigTypes = []string{manscdp.ConfigTypeVideoParamAttribute}
		}
		return manscdp.BuildConfigDownloadQueryWithProfile(profile, targetCode, operation.SN, payload.ConfigTypes)
	case manscdp.CmdDeviceConfig:
		// ⛔ `VideoParamAttribute`（A-5）与配置族共用 `CmdType=DeviceConfig`，所以这里
		// **必须按 Action 分流**，只看 CmdType 会把两者混成一件事。
		//
		// 只看 CmdType 的历史后果（2026-09-19 真机验证发现，已提交的回归）：配置族的
		// 下发全部被重建成 A-5 的报文 `<VideoParamAttribute Num="0">`（payload 里没有
		// `items`，Items 为 nil），**一块配置都发不出去**；设备照回 `Result=OK`，平台回读
		// 只表现为 `mismatch` —— 症状酷似"设备没照做"，归因成本极高。
		if operation.Action == actionApplyDeviceConfig {
			var payload struct {
				Blocks manscdp.DeviceConfigBlocks `json:"blocks"`
			}
			if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil {
				return nil, err
			}
			// 重建必须能复现"构建"：payload 里没有配置块就说明这条 operation 不是配置族
			// 下发的形态，**报错而不是发一个空块** —— 发空块会让故障变成"设备没照做"。
			if payload.Blocks.IsEmpty() {
				return nil, errors.New("设备配置下发无法重建:operation payload 缺少 blocks")
			}
			return manscdp.BuildDeviceConfigBlocksWithProfile(profile, targetCode, operation.SN, payload.Blocks)
		}
		var payload struct {
			Items []manscdp.VideoParamItem `json:"items"`
		}
		if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil {
			return nil, err
		}
		return manscdp.BuildVideoParamAttributeConfigWithProfile(profile, targetCode, operation.SN, payload.Items)
	case manscdp.CmdHomePositionQuery:
		return manscdp.BuildHomePositionQueryWithProfile(profile, targetCode, operation.SN)
	case manscdp.CmdPresetQuery:
		return manscdp.BuildPresetQueryWithProfile(profile, targetCode, operation.SN)
	case manscdp.CmdCruiseTrackListQuery:
		return manscdp.BuildCruiseTrackListQueryWithProfile(profile, targetCode, operation.SN)
	case manscdp.CmdCruiseTrackQuery:
		var payload struct {
			TrackID int `json:"trackId"`
		}
		if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil {
			return nil, err
		}
		return manscdp.BuildCruiseTrackQueryWithProfile(profile, targetCode, operation.SN, payload.TrackID)
	case manscdp.CmdPTZPreciseStatusQuery, manscdp.CmdPTZPosition:
		return manscdp.BuildPTZPreciseStatusQueryWithProfile(profile, targetCode, operation.SN)
	case manscdp.CmdPTZPreciseCtrl:
		return buildPrecise()
	case manscdp.CmdDeviceControl:
		if operation.Action == "precise" {
			return buildPrecise()
		}
		if operation.Action == "record_start" || operation.Action == "record_stop" || operation.Action == "guard_set" || operation.Action == "guard_reset" || operation.Action == "alarm_reset" || operation.Action == "teleboot" || operation.Action == "iframe" || operation.Action == "drag_zoom_in" || operation.Action == "drag_zoom_out" {
			var payload struct {
				Action      string                 `json:"action"`
				AlarmMethod string                 `json:"alarmMethod"`
				AlarmType   string                 `json:"alarmType"`
				Region      manscdp.DragZoomRegion `json:"region"`
			}
			if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil {
				return nil, err
			}
			request := manscdp.AlarmResetOptions{AlarmMethod: payload.AlarmMethod, AlarmType: payload.AlarmType}
			switch operation.Action {
			case "iframe":
				return manscdp.BuildIFrameControlWithProfile(profile, targetCode, operation.SN)
			case "record_start":
				return manscdp.BuildRecordControlWithProfile(profile, targetCode, operation.SN, manscdp.RecordStart)
			case "record_stop":
				return manscdp.BuildRecordControlWithProfile(profile, targetCode, operation.SN, manscdp.RecordStop)
			case "guard_set":
				return manscdp.BuildGuardControlWithProfile(profile, targetCode, operation.SN, manscdp.GuardSet)
			case "guard_reset":
				return manscdp.BuildGuardControlWithProfile(profile, targetCode, operation.SN, manscdp.GuardReset)
			case "alarm_reset":
				return manscdp.BuildAlarmResetControlWithProfile(profile, targetCode, operation.SN, request)
			case "teleboot":
				return manscdp.BuildTeleBootControlWithProfile(profile, targetCode, operation.SN, true)
			case "drag_zoom_in", "drag_zoom_out":
				direction := manscdp.DragZoomIn
				if operation.Action == "drag_zoom_out" {
					direction = manscdp.DragZoomOut
				}
				return manscdp.BuildDragZoomControlWithProfile(profile, targetCode, operation.SN, manscdp.DragZoomCommand{Direction: direction, Region: payload.Region})
			}
		}
		if operation.Action != "home_position" {
			return nil, fmt.Errorf("不支持持久化调度的 PTZ control: %s", operation.Action)
		}
		var payload struct {
			Enabled   bool `json:"enabled"`
			ResetTime *int `json:"resetTime"`
			PresetID  *int `json:"presetId"`
		}
		if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil {
			return nil, err
		}
		return manscdp.BuildHomePositionControlWithProfile(profile, targetCode, operation.SN, manscdp.HomePositionControl{
			Enabled: payload.Enabled, ResetTime: payload.ResetTime, PresetIndex: payload.PresetID,
		})
	default:
		return nil, fmt.Errorf("不支持持久化调度的 PTZ command: %s", operation.CmdType)
	}
}

func (s *Scheduler) persistAttemptResult(ctx context.Context, attempt gbmodels.GbPTZOperationAttempt, result uac.TrackedMessageResult, sendErr error, observedAt time.Time) error {
	return schedulerTransaction(ctx, s.db, func(tx *gorm.DB) (persistErr error) {
		var currentAttempt gbmodels.GbPTZOperationAttempt
		if err := tx.First(&currentAttempt, attempt.ID).Error; err != nil {
			return err
		}
		bound := currentAttempt.OwnerProcessID != nil || currentAttempt.OwnerRunID != nil || attempt.OwnerProcessID != nil || attempt.OwnerRunID != nil
		if bound {
			if currentAttempt.OwnerProcessID == nil || currentAttempt.OwnerRunID == nil || attempt.OwnerProcessID == nil || attempt.OwnerRunID == nil ||
				*currentAttempt.OwnerProcessID != *attempt.OwnerProcessID || *currentAttempt.OwnerRunID != *attempt.OwnerRunID ||
				currentAttempt.OperationID != attempt.OperationID || currentAttempt.AttemptNo != attempt.AttemptNo {
				return playauth.ErrDeviceIntentConflict
			}
			// This function is called only after Send has returned. A failed
			// result write must roll back its exit marker too; lease expiry is
			// never allowed to manufacture this evidence.
			defer func() {
				if persistErr != nil || currentAttempt.LocalQuiescedAt != nil {
					return
				}
				res := tx.Model(&gbmodels.GbPTZOperationAttempt{}).
					Where("id = ? AND owner_process_id = ? AND owner_run_id = ? AND local_quiesced_at IS NULL", attempt.ID, *attempt.OwnerProcessID, *attempt.OwnerRunID).
					UpdateColumn("local_quiesced_at", observedAt)
				persistErr = res.Error
				if persistErr == nil && res.RowsAffected != 1 {
					persistErr = playauth.ErrDeviceIntentConflict
				}
			}()
		}
		success := sendErr == nil && result.StatusCode >= 200 && result.StatusCode < 300
		recoveredBeforeWriteback := success &&
			currentAttempt.Status == gbmodels.PTZOperationAttemptUnknown &&
			currentAttempt.ErrorCode == schedulerErrorTransportUnknown &&
			currentAttempt.LeaseUntil.After(observedAt)
		if currentAttempt.Status != gbmodels.PTZOperationAttemptDispatching && !recoveredBeforeWriteback {
			return nil
		}
		var operation gbmodels.GbPTZOperation
		if err := tx.First(&operation, currentAttempt.OperationID).Error; err != nil {
			return err
		}
		if !operation.ResponseRequired {
			return persistOneWayAttemptResult(tx, operation, currentAttempt, result, sendErr, observedAt)
		}
		if !currentAttempt.LeaseUntil.After(observedAt) || operation.TransportDeadlineAt == nil || !operation.TransportDeadlineAt.After(observedAt) {
			if currentAttempt.Status == gbmodels.PTZOperationAttemptDispatching {
				return updateLateAttempt(tx, currentAttempt.ID, result, sendErr, observedAt)
			}
			return nil
		}

		uncertain := result.Attempted && result.StatusCode == 0
		switch {
		case success:
			updates := schedulerAttemptResultUpdates(gbmodels.PTZOperationAttemptSent, result, sendErr, observedAt)
			updates["sent_at"] = observedAt
			updates["error_code"] = ""
			if changed, err := updateAttemptFromStatus(tx, currentAttempt.ID, currentAttempt.Status, updates); err != nil || !changed {
				return err
			}
			if err := persistScheduledOutboundMetadata(tx, operation.ID, result, observedAt, true); err != nil {
				return err
			}
			deadline := observedAt.Add(schedulerApplicationWindow)
			return tx.Model(&gbmodels.GbPTZOperation{}).
				Where("id = ? AND response_required = ?", operation.ID, true).
				Where("status = ? OR (status = ? AND error_code = ?)", gbmodels.PTZOperationQueued, gbmodels.PTZOperationUnknown, schedulerErrorTransportUnknown).
				Updates(map[string]interface{}{
					"status": gbmodels.PTZOperationSent, "deadline_at": deadline,
					"error_code": "", "error_message": "", "completed_at": nil,
				}).Error
		case uncertain:
			updates := schedulerAttemptResultUpdates(gbmodels.PTZOperationAttemptUnknown, result, sendErr, observedAt)
			updates["error_code"] = schedulerErrorTransportUnknown
			if changed, err := updateAttemptFromStatus(tx, currentAttempt.ID, currentAttempt.Status, updates); err != nil || !changed {
				return err
			}
			if err := persistScheduledOutboundMetadata(tx, operation.ID, result, observedAt, false); err != nil {
				return err
			}
			if operation.MaxAttempts <= 1 {
				return tx.Model(&gbmodels.GbPTZOperation{}).
					Where("id = ? AND response_required = ? AND status = ?", operation.ID, true, gbmodels.PTZOperationQueued).
					Updates(map[string]interface{}{
						"status": gbmodels.PTZOperationUnknown, "error_code": schedulerErrorTransportUnknown,
						"error_message": schedulerSendError(sendErr), "completed_at": observedAt, "next_attempt_at": nil,
					}).Error
			}
			return nil
		default:
			updates := schedulerAttemptResultUpdates(gbmodels.PTZOperationAttemptFailed, result, sendErr, observedAt)
			updates["error_code"] = schedulerErrorHomePositionUnavailable
			if changed, err := updateAttemptFromStatus(tx, currentAttempt.ID, currentAttempt.Status, updates); err != nil || !changed {
				return err
			}
			if err := persistScheduledOutboundMetadata(tx, operation.ID, result, observedAt, false); err != nil {
				return err
			}
			var uncertainOrSent int64
			if err := tx.Model(&gbmodels.GbPTZOperationAttempt{}).
				Where("operation_id = ? AND status IN ?", operation.ID, []gbmodels.PTZOperationAttemptStatus{gbmodels.PTZOperationAttemptUnknown, gbmodels.PTZOperationAttemptSent}).
				Count(&uncertainOrSent).Error; err != nil {
				return err
			}
			if uncertainOrSent == 0 {
				return tx.Model(&gbmodels.GbPTZOperation{}).
					Where("id = ? AND response_required = ? AND status = ?", operation.ID, true, gbmodels.PTZOperationQueued).
					Updates(map[string]interface{}{
						"status": gbmodels.PTZOperationRejected, "error_code": schedulerErrorHomePositionUnavailable,
						"error_message": schedulerSendError(sendErr), "completed_at": observedAt, "next_attempt_at": nil,
					}).Error
			}
			return nil
		}
	})
}

func schedulerTransaction(ctx context.Context, db *gorm.DB, operation func(*gorm.DB) error) error {
	var err error
	for attempt := 0; attempt < schedulerTransactionAttempts; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		err = ptzWriter(db).WithContext(ctx).Transaction(operation)
		if err == nil || !schedulerRetryableDBError(err) {
			return err
		}
		if attempt+1 == schedulerTransactionAttempts {
			break
		}
		delay := time.Millisecond << attempt
		if delay > 20*time.Millisecond {
			delay = 20 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

func schedulerRetryableDBError(err error) bool {
	var codeErr interface{ Code() int }
	if !errors.As(err, &codeErr) {
		return false
	}
	primaryCode := codeErr.Code() & 0xff
	return primaryCode == 5 || primaryCode == 6 // SQLITE_BUSY / SQLITE_LOCKED
}

func schedulerAttemptResultUpdates(status gbmodels.PTZOperationAttemptStatus, result uac.TrackedMessageResult, sendErr error, observedAt time.Time) map[string]interface{} {
	return map[string]interface{}{
		"status": status, "call_id": result.CallID, "cseq": result.CSeq, "sip_status": result.StatusCode,
		"completed_at": observedAt, "error_message": schedulerSendError(sendErr),
	}
}

func schedulerSendError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func updateAttemptFromStatus(tx *gorm.DB, attemptID uint, expected gbmodels.PTZOperationAttemptStatus, updates map[string]interface{}) (bool, error) {
	result := tx.Model(&gbmodels.GbPTZOperationAttempt{}).
		Where("id = ? AND status = ?", attemptID, expected).
		Updates(updates)
	return result.RowsAffected == 1, result.Error
}

func updateLateAttempt(tx *gorm.DB, attemptID uint, result uac.TrackedMessageResult, sendErr error, observedAt time.Time) error {
	updates := schedulerAttemptResultUpdates(gbmodels.PTZOperationAttemptUnknown, result, sendErr, observedAt)
	updates["error_code"] = schedulerErrorTransportUnknown
	if sendErr == nil {
		updates["error_message"] = "sender result observed after lease/deadline"
	}
	_, err := updateAttemptFromStatus(tx, attemptID, gbmodels.PTZOperationAttemptDispatching, updates)
	return err
}

func persistScheduledOutboundMetadata(tx *gorm.DB, operationID uint, result uac.TrackedMessageResult, observedAt time.Time, sent bool) error {
	if !result.Attempted {
		return nil
	}
	updates := map[string]interface{}{
		"call_id": result.CallID, "cseq": result.CSeq, "sip_status": result.StatusCode,
	}
	if sent {
		updates["sent_at"] = observedAt
	}
	return tx.Model(&gbmodels.GbPTZOperation{}).
		Where("id = ? AND (call_id = '' OR call_id IS NULL)", operationID).
		Updates(updates).Error
}
