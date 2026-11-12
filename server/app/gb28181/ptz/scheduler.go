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

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/utils/logging"
)

const (
	schedulerErrorApplicationTimeout      = "APPLICATION_TIMEOUT"
	schedulerErrorTransportUnknown        = "TRANSPORT_UNKNOWN"
	schedulerErrorHomePositionUnavailable = "HOME_POSITION_UNAVAILABLE"

	defaultSchedulerInterval = 250 * time.Millisecond

	// defaultSchedulerCapacity 是全局在途指令的并发槽位数。
	//
	// ⛔ 它直接决定后台配置刷新的消化速度：一个槽位从发出到收到设备 200 OK 实测约 2 秒，
	// 所以吞吐 ≈ capacity/2 条/秒。原值 16 ⇒ **约 8 条/秒**。2026-11-09 万级压测里
	// 10002 台设备首次注册会一次性排 20006 条 ConfigDownload（每台 BasicParam +
	// 每通道 VideoParamOpt），8 条/秒根本排不完 ⇒ 17291 条（86%）在排队窗口到点被判
	// `PTZ_QUEUE_TIMEOUT`，现场看到的是"配置一个都没刷出来"。
	// 一条指令就是一个 SIP MESSAGE（UDP 单包、等 200 OK），128 在途对设备侧依然很轻，
	// 且注册链路本来就能同时处理 150+ 条/秒的信令，所以放到 128（吞吐 ≈ 64 条/秒）。
	// 需要更保守的现场可用 WithSchedulerCapacity 覆盖。
	defaultSchedulerCapacity     = 128
	schedulerTransactionAttempts = 8

	// ⛔⛔ 结果回写"一直失败"必须能收敛，否则一条坏结果会锁死整条流水线。
	//
	// 2026-11-09 第三轮万级压测实测：一条 attempt 的结果写回撞上注册高峰，
	// 5 秒预算（defaultSchedulerPersistTimeout）超时；而 flushPendingResults 原写法是
	// "队首失败即 return"，于是每个 tick 都在队首花 5 秒再退出，**claimDue 永不执行**
	// ⇒ 42 条指令卡在 queued 四十分钟零动作，且 Execute 每次也要先 flush（同一把
	// pendingMu 全局锁）⇒ 99.6% 的能力刷新请求静默卡死，连一条 WARN 都没有。
	//
	// ⇒ 两条硬规则：
	//  1. 单项失败**不能中断其它项**（本轮其它成功项照常落库）；
	//  2. 单项连续失败超阈值要**放弃并记 ERROR**。放弃是安全的：operation 仍在
	//     queued，随后 `expireTransport` 会按 transport_deadline_at 收敛成
	//     TRANSPORT_UNKNOWN，终态不会悬空。
	// 停机路径走的是 FlushResults(strict)，仍保持"不丢 owner"的强语义。
	schedulerWritebackMaxAttempts = 8
	schedulerWritebackRetryFor    = 90 * time.Second

	// schedulerStepLogInterval 是"同一步骤同一类失败"的日志节流窗口。
	// tick 间隔 250ms，故障时不去重就是每秒 4 条 ERROR。
	schedulerStepLogInterval = 60 * time.Second

	// schedulerExpireChunk / schedulerExpireRounds 控制单轮 tick 里"排队超时判死"的批量。
	//
	// ⛔ 这两个值配合 expireQueued 的分批改写使用，不要当成纯性能参数：
	// 一次性范围 UPDATE 会先锁二级索引再回表锁主键，与单条回写的加锁顺序相反，
	// 万级并发下会持续撞 1213（实测 6311 次）。分批 + 按主键升序更新是**正确性**要求。
	schedulerExpireChunk       = 256
	schedulerExpireRounds      = 8
	schedulerTransportWindow   = 15 * time.Second
	schedulerApplicationWindow = 15 * time.Second

	// 结果回写预算。一次回写要跑「查 attempt → 查 operation → 更新 attempt →
	// 更新 operation」四趟往返，跨机房/隧道链路上单趟就有 80~150ms 抖动，丢包
	// 重传更是 200ms 起步。原先硬编码 2s 会让正常的抖动被记成 persist_failed:
	// 结果没丢(会进 pendingResults 等下一 tick 补写),但 operation 状态会滞后,
	// 且补写失败会让整轮 runDue 提前返回,连带停掉超时收敛与新指令派发。
	// 故放宽到 5s,并允许通过 WithSchedulerPersistTimeout 覆盖。
	defaultSchedulerPersistTimeout = 5 * time.Second
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
	interval       time.Duration
	dispatcher     schedulerDispatcher
	capacity       int
	persistTimeout time.Duration
}

type SchedulerOption func(*schedulerConfig)

func WithSchedulerInterval(interval time.Duration) SchedulerOption {
	return func(config *schedulerConfig) {
		if interval > 0 {
			config.interval = interval
		}
	}
}

// WithSchedulerPersistTimeout 覆盖结果回写的落库预算。
func WithSchedulerPersistTimeout(timeout time.Duration) SchedulerOption {
	return func(config *schedulerConfig) {
		if timeout > 0 {
			config.persistTimeout = timeout
		}
	}
}

func WithSchedulerCapacity(capacity int) SchedulerOption {
	return func(config *schedulerConfig) {
		if capacity > 0 {
			config.capacity = capacity
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
	// capacity 是在途槽位数，只用于给候选查询定量（见 claimDue），不再二次限流。
	capacity int
	// persistTimeout 是结果回写的落库预算,见 defaultSchedulerPersistTimeout。
	persistTimeout time.Duration

	lifecycleMu sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}

	activeMu            sync.Mutex
	active              map[uint]schedulerActiveAttempt
	pendingMu           sync.Mutex
	pendingResults      map[uint]schedulerPendingResult
	pendingClaims       []*playauth.PTZUnissuedAttempt
	pendingReservations []*playauth.PTZReservationOutcome

	// stepLogMu 保护 stepLogAt：给 runDue 的逐步失败日志做节流。
	stepLogMu sync.Mutex
	stepLogAt map[string]time.Time
}

type schedulerPendingResult struct {
	attempt    gbmodels.GbPTZOperationAttempt
	result     uac.TrackedMessageResult
	sendErr    error
	observedAt time.Time
	lease      playauth.DeviceOperationLease
	timing     *schedulerSendTiming

	// writebackAttempts / writebackFailedAt 只用于"写回一直失败"的收敛判定，
	// 见 schedulerWritebackMaxAttempts。首次失败时间决定是否已超时放弃。
	writebackAttempts int
	writebackFailedAt time.Time
}

// Ownership is committed before Send; these are facts observed by that owner,
// retained with the result so a failed writeback never restarts a timer or Send.
type schedulerSendTiming struct {
	startedAt, deadline, transportDeadline time.Time
	nextAttempt                            *time.Time
}

type schedulerActiveAttempt struct {
	cancel   context.CancelFunc
	done     chan struct{}
	deadline time.Time
}

func NewScheduler(service *Service, options ...SchedulerOption) *Scheduler {
	config := schedulerConfig{interval: defaultSchedulerInterval, capacity: defaultSchedulerCapacity, persistTimeout: defaultSchedulerPersistTimeout}
	for _, option := range options {
		option(&config)
	}
	if config.dispatcher == nil {
		config.dispatcher = newBoundedSchedulerDispatcher(defaultSchedulerCapacity)
	}
	if config.capacity <= 0 {
		config.capacity = defaultSchedulerCapacity
	}
	scheduler := &Scheduler{
		service: service, dispatcher: config.dispatcher, interval: config.interval,
		capacity: config.capacity, persistTimeout: config.persistTimeout,
		active:         make(map[uint]schedulerActiveAttempt),
		pendingResults: make(map[uint]schedulerPendingResult),
		stepLogAt:      make(map[string]time.Time),
	}
	if service != nil {
		scheduler.db = service.db
	}
	return scheduler
}

// writebackContext 从派发 ctx 派生一棵"只保留值、不继承取消"的树。
// 派发 ctx 会因为指令租约/截止时间被取消,而结果回写是已发生的事实,
// 必须独立跑完 —— 否则取消信号会让早已发出的指令丢失结果证据。
func (s *Scheduler) writebackContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := s.persistTimeout
	if timeout <= 0 {
		timeout = defaultSchedulerPersistTimeout
	}
	return context.WithTimeout(context.WithoutCancel(parent), timeout)
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
	// ⛔ 停机必须走严格模式：这里要的恰恰是"还有没有写不进去的"，走运行期的
	// 尽力模式会恒返回 nil，这条告警就永远不会触发，租约也会被误判成已排空。
	if err := s.FlushResults(ctx); err != nil && app.ZapLog != nil {
		app.ZapLog.Error("PTZ 停止后仍有未持久化结果，保留设备租约", zap.String("event", "ptz.scheduler.stop_flush_failed"), zap.Error(err))
	}
}

// Retained facts are retried without calling the sender. A failed flush leaves
// the original lease owned here, including after Stop, rather than falsely
// declaring the device drained.
// flushPendingResults 推进待落库事实，**失败要上报**（调用方据此决定能不能领新任务）。
//
// ⛔⛔ 为什么不能吞错：写回失败意味着"上一批指令的外部副作用还没有落成事实"，
// 此时再领新指令会让审计链断掉。既有契约（scheduler_epoch_test 的
// claim-commit-unknown / queue-expiry-write-fails）就是"失败不派发"，必须守住。
//
// 所以停摆的解药不是"忽略失败"，而是**让坏项能过期淘汰**（abandon=true）：
// 同一条结果连续失败到阈值后放弃并记 ERROR，收敛交给 expireTransport；
// 下一轮 flush 就干净了，claim 自然恢复。
func (s *Scheduler) flushPendingResults(ctx context.Context) error {
	return s.flushPending(ctx, true)
}

// FlushResults retries local facts only. Root teardown must retain this owner
// when it fails, and must not construct a replacement runtime over it.
//
// 停机路径不允许放弃（abandon=false）：宁可留下待重试，也不能丢事实。
func (s *Scheduler) FlushResults(ctx context.Context) error { return s.flushPending(ctx, false) }

// flushPending 逐项推进待落库事实。
//
// 三个关键设计（都是 2026-11-09 停摆事故的直接产物）：
//  1. **先取快照再解锁**：原来全程持 pendingMu 做 DB IO，而 pendingMu 是全局锁，
//     一次 5 秒超时就把所有 Execute 一起挡在门外。
//  2. **单项失败不阻断同批其它项**：本轮把能推进的都推进掉，失败的统一放回队首重试。
//  3. **允许放弃**（abandon=true）：连续 schedulerWritebackMaxAttempts 次或超过
//     schedulerWritebackRetryFor 就丢弃该项并记 ERROR —— 这是"永久停摆"的真正解药。
func (s *Scheduler) flushPending(ctx context.Context, abandon bool) error {
	if s == nil {
		return nil
	}
	now := time.Now()
	if s.service != nil {
		now = s.service.now()
	}
	reservations, claims, results := s.takePending()

	var firstErr error
	failedReservations := make([]*playauth.PTZReservationOutcome, 0, len(reservations))
	for _, reservation := range reservations {
		if reservation == nil {
			continue
		}
		if err := reservation.Reconcile(ctx); err != nil {
			failedReservations = append(failedReservations, reservation)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	failedClaims := make([]*playauth.PTZUnissuedAttempt, 0, len(claims))
	for _, claim := range claims {
		if claim == nil {
			continue
		}
		if err := claim.Reconcile(ctx); err != nil {
			failedClaims = append(failedClaims, claim)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	survivors := make(map[uint]schedulerPendingResult, len(results))
	for id, pending := range results {
		err := s.persistAttemptOutcome(ctx, pending.attempt, pending.result, pending.sendErr, pending.observedAt, pending.timing)
		if err == nil {
			if pending.lease != nil {
				pending.lease.Release()
			}
			continue
		}
		if firstErr == nil {
			firstErr = err
		}
		// 停机路径（abandon=false）只报告、不计数、不判放弃：停机重试不能自己把事实丢掉。
		if !abandon {
			survivors[id] = pending
			continue
		}
		pending.writebackAttempts++
		if pending.writebackFailedAt.IsZero() {
			pending.writebackFailedAt = now
		}
		abandoned := pending.writebackAttempts >= schedulerWritebackMaxAttempts ||
			now.Sub(pending.writebackFailedAt) >= schedulerWritebackRetryFor
		if abandoned {
			// 放弃写回：operation 仍是 queued，expireTransport 会按
			// transport_deadline_at 收敛成 TRANSPORT_UNKNOWN；不放弃的话
			// 整条流水线会被这一条结果永久锁死。
			app.Log(ctx).Named("gb28181.ptz.scheduler").Error("PTZ 调度结果写回连续失败，已放弃并交由超时收敛",
				zap.String("event", "ptz.scheduler.writeback_abandoned"),
				zap.Uint("operation_id", pending.attempt.OperationID),
				zap.Uint("attempt_id", pending.attempt.ID),
				zap.Int("writeback_attempts", pending.writebackAttempts),
				zap.Duration("writeback_failing_for", now.Sub(pending.writebackFailedAt)),
				zap.String("reason_code", "writeback_abandoned"),
				zap.String("likely_cause", "结果写回预算内落库失败（数据库抖动/锁等待），交由 expireTransport 收敛"),
				logging.Error(err))
			if pending.lease != nil {
				pending.lease.Release()
			}
			continue
		}
		survivors[id] = pending
	}

	s.restorePending(failedReservations, failedClaims, survivors)
	return firstErr
}

// takePending 在一把锁内取出并清空待落库事实，随后释放锁再做 IO。
func (s *Scheduler) takePending() ([]*playauth.PTZReservationOutcome, []*playauth.PTZUnissuedAttempt, map[uint]schedulerPendingResult) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	reservations := s.pendingReservations
	s.pendingReservations = nil
	claims := s.pendingClaims
	s.pendingClaims = nil
	results := s.pendingResults
	s.pendingResults = make(map[uint]schedulerPendingResult, len(results))
	return reservations, claims, results
}

// restorePending 把失败项放回队首（保持原有"先来先重试"的顺序）。
// ⛔ 期间新到的同 id 结果**优先**：那是更新的观测，不能被旧快照覆盖。
func (s *Scheduler) restorePending(reservations []*playauth.PTZReservationOutcome, claims []*playauth.PTZUnissuedAttempt, results map[uint]schedulerPendingResult) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if len(reservations) > 0 {
		s.pendingReservations = append(reservations, s.pendingReservations...)
	}
	if len(claims) > 0 {
		s.pendingClaims = append(claims, s.pendingClaims...)
	}
	for id, pending := range results {
		if _, exists := s.pendingResults[id]; exists {
			if pending.lease != nil {
				pending.lease.Release()
			}
			continue
		}
		s.pendingResults[id] = pending
	}
}

func (s *Scheduler) retainUnissued(err error) {
	var ticket *playauth.PTZUnissuedAttempt
	if errors.As(err, &ticket) {
		s.pendingMu.Lock()
		s.pendingClaims = append(s.pendingClaims, ticket)
		s.pendingMu.Unlock()
	}
}

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

// runDue 做一轮收敛，全部收敛成功后才领取新任务。
//
// ⛔⛔ 两条约束同时成立，缺一不可：
//
//	(a) **任何收敛步骤失败 ⇒ 本轮不领新任务**。写回/判死没落库就发新指令，
//	    会让"已发出但没记账"的指令和重试叠在一起（既有契约见 scheduler_epoch_test
//	    的 claim-commit-unknown / queue-expiry-write-fails，不许翻）。
//	(b) **收敛步骤之间互相隔离**。原实现是纯串联 + 调用方 `_ = runDue(...)` 吞错，
//	    于是第一步持续失败就让后面所有步骤永远轮不到、且零日志。
//	    2026-11-09 实测：一条结果写回卡住 ⇒ 42 条指令在队列里躺了四十分钟。
//
// 停摆的真正解药在别处：坏结果会被 [schedulerWritebackMaxAttempts] 淘汰，
// 所以最坏也只是"挡若干轮"，不会再永久卡住。
func (s *Scheduler) runDue(ctx context.Context, now time.Time) error {
	if s == nil || s.db == nil || s.service == nil || s.service.sender == nil {
		return errors.New("PTZ scheduler 未就绪")
	}
	steps := []schedulerStep{{name: "flush_results", run: func(ctx context.Context, _ time.Time) error {
		return s.flushPendingResults(ctx)
	}}}
	if syncOwner := s.service.synchronous; syncOwner != nil && syncOwner != s {
		steps = append(steps, schedulerStep{name: "flush_synchronous", run: func(ctx context.Context, _ time.Time) error {
			return syncOwner.flushPendingResults(ctx)
		}})
	}
	steps = append(steps,
		schedulerStep{name: "expire_queued", run: s.expireQueued},
		schedulerStep{name: "expire_transport", run: s.expireTransport},
		schedulerStep{name: "expire_application", run: s.expireApplication},
		schedulerStep{name: "recover_expired_leases", run: s.recoverExpiredLeases},
		schedulerStep{name: "cleanup_query_stages", run: s.service.cleanupQueryStages},
		// 配置读取的静止收敛：设备少回几条时不能干等到 transport deadline,
		// 否则"设备不支持这些类型"（正确的 type_absent 结论）会被误报成超时。
		schedulerStep{name: "settle_config_read_stages", run: s.service.settleConfigReadStages},
	)

	var firstErr error
	for _, step := range steps {
		if err := step.run(ctx, now); err != nil {
			s.reportStepFailure(ctx, step.name, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr != nil {
		// 约束 (a)：状态没收敛干净就不许领新任务。
		return firstErr
	}
	if err := s.claimDue(ctx, now); err != nil {
		s.reportStepFailure(ctx, "claim_due", err)
		return err
	}
	return nil
}

type schedulerStep struct {
	name string
	run  func(context.Context, time.Time) error
}

// reportStepFailure 记录单步失败，按 schedulerStepLogInterval 对同一步骤节流。
//
// 节流的意义：tick 间隔 250ms，故障时不去重就是每秒 4 条 ERROR，
// 几分钟就把日志刷成无用的噪声（现场排障时反而更难看见真正的新错误）。
func (s *Scheduler) reportStepFailure(ctx context.Context, step string, err error) {
	s.stepLogMu.Lock()
	if s.stepLogAt == nil {
		s.stepLogAt = make(map[string]time.Time)
	}
	now := time.Now()
	last, seen := s.stepLogAt[step]
	throttled := seen && now.Sub(last) < schedulerStepLogInterval
	if !throttled {
		s.stepLogAt[step] = now
	}
	s.stepLogMu.Unlock()

	if throttled {
		return
	}
	app.Log(ctx).Named("gb28181.ptz.scheduler").Error("PTZ 调度步骤失败（后续步骤仍会执行）",
		zap.String("event", "ptz.scheduler.step_failed"),
		zap.String("step", step),
		zap.String("reason_code", "scheduler_step_failed"),
		zap.String("likely_cause", "该步骤自身失败；其余步骤已独立执行，不再造成整轮停摆"),
		logging.Error(err))
}

func (s *Scheduler) expireQueued(ctx context.Context, now time.Time) error {
	// ⛔⛔ 本函数**不能**用一条范围 UPDATE 收尾。那条语句的加锁顺序是
	// "先锁 (response_required,status,queue_deadline_at) 二级索引 → 再回表锁主键"，
	// 而单条结果回写（persistAttemptOutcome）是"先主键 → 再改索引里的 status"，
	// 两者在万级并发下必然互撞，实测 6311 次 1213（2026-11-09）。
	// 改成"先按主键取出 ID → 再按 ID 更新"，与回写路径统一成先锁主键；
	// 顺带把"一次扫全表"压成固定批大小，慢盘上也不会把 tick 拖成秒级。
	var bound []gbmodels.GbPTZOperation
	if err := ptzWriter(s.db).WithContext(ctx).
		Where("response_required = ? AND status = ? AND attempt = 0 AND queue_deadline_at <= ?", true, gbmodels.PTZOperationQueued, gbmodels.PTZTimeComparison(s.db, now)).
		Where("device_intent_id IS NOT NULL OR device_epoch IS NOT NULL").
		Order("id").Limit(schedulerExpireChunk * schedulerExpireRounds).Find(&bound).Error; err != nil {
		return err
	}
	// ⛔ 逐条对账时**不能**"第一条失败就整体退出"：一条状态不对的指令
	// （intent 已推进/行版本变化）会把同批其它几百条的判死一起拖住。
	// 失败逐条记录、继续处理其余，最后返回第一个错误（已由 runDue 记录并隔离）。
	var firstErr error
	for _, op := range bound {
		id, err := ptzIntentIdentity(op)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if s.service.intents == nil {
			if firstErr == nil {
				firstErr = playauth.ErrDeviceIntentUnavailable
			}
			continue
		}
		if err := s.service.intents.ExpirePTZReservation(ctx, id, op.ID, now); err != nil {
			// ErrDeviceIntentConflict 是"这条指令已被别处推进"的正常竞态，
			// 不是故障，不该当错误上报，也不该影响其余指令。
			if !errors.Is(err, playauth.ErrDeviceIntentConflict) && firstErr == nil {
				firstErr = err
			}
			continue
		}
	}
	for round := 0; round < schedulerExpireRounds; round++ {
		var ids []uint
		if err := ptzWriter(s.db).WithContext(ctx).Model(&gbmodels.GbPTZOperation{}).
			Where("device_intent_id IS NULL AND device_epoch IS NULL").
			Where("response_required = ? AND status = ? AND attempt = 0 AND queue_deadline_at IS NOT NULL AND queue_deadline_at <= ?", true, gbmodels.PTZOperationQueued, gbmodels.PTZTimeComparison(s.db, now)).
			Order("id").Limit(schedulerExpireChunk).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			// ⛔ 这里必须是 break 而不是 return nil：否则上面逐条对账的
			// firstErr 会被吞掉，调用方会误以为"收敛干净了"从而去领新任务。
			break
		}
		if err := s.db.WithContext(ctx).Model(&gbmodels.GbPTZOperation{}).
			Where("id IN ?", ids).
			Updates(map[string]interface{}{
				"status": gbmodels.PTZOperationRejected, "error_code": playauth.PTZQueueTimeout,
				"error_message": "排队超时：指令在排队窗口内未被调度发出", "completed_at": now, "next_attempt_at": nil,
			}).Error; err != nil {
			return err
		}
		if len(ids) < schedulerExpireChunk {
			break
		}
	}
	return firstErr
}

func (s *Scheduler) expireTransport(ctx context.Context, now time.Time) error {
	return s.expireByPrimaryKey(ctx, now, schedulerExpiry{
		from: gbmodels.PTZOperationQueued, to: gbmodels.PTZOperationUnknown,
		deadlineColumn: "transport_deadline_at",
		errorCode:      schedulerErrorTransportUnknown, errorMessage: "PTZ MESSAGE 传输结果不确定",
	})
}

func (s *Scheduler) expireApplication(ctx context.Context, now time.Time) error {
	return s.expireByPrimaryKey(ctx, now, schedulerExpiry{
		from: gbmodels.PTZOperationSent, to: gbmodels.PTZOperationTimeout,
		deadlineColumn: "deadline_at",
		errorCode:      schedulerErrorApplicationTimeout, errorMessage: "等待设备应用层响应超时",
	})
}

type schedulerExpiry struct {
	from, to       gbmodels.PTZOperationStatus
	deadlineColumn string
	errorCode      string
	errorMessage   string
}

func (s *Scheduler) expireByPrimaryKey(ctx context.Context, now time.Time, expiry schedulerExpiry) error {
	// 候选查询不加锁；逐条按主键条件更新，避免与应答回写形成二级索引/主键锁反序。
	for round := 0; round < schedulerExpireRounds; round++ {
		var ids []uint
		if err := ptzWriter(s.db).WithContext(ctx).Model(&gbmodels.GbPTZOperation{}).
			Where("response_required = ? AND status = ? AND "+expiry.deadlineColumn+" IS NOT NULL AND "+expiry.deadlineColumn+" <= ?",
				true, expiry.from, gbmodels.PTZTimeComparison(s.db, now)).
			Order("id").Limit(schedulerExpireChunk).Pluck("id", &ids).Error; err != nil {
			return err
		}
		for _, id := range ids {
			if err := ptzWriter(s.db).WithContext(ctx).Model(&gbmodels.GbPTZOperation{}).
				Where("id = ? AND response_required = ? AND status = ? AND "+expiry.deadlineColumn+" IS NOT NULL AND "+expiry.deadlineColumn+" <= ?",
					id, true, expiry.from, gbmodels.PTZTimeComparison(s.db, now)).
				Updates(map[string]interface{}{
					"status": expiry.to, "error_code": expiry.errorCode,
					"error_message": expiry.errorMessage, "completed_at": now, "next_attempt_at": nil,
				}).Error; err != nil {
				return err
			}
		}
		if len(ids) < schedulerExpireChunk {
			break
		}
	}
	return nil
}

func (s *Scheduler) recoverExpiredLeases(ctx context.Context, now time.Time) error {
	// Local phase deadlines can precede the conservative durable owner lease.
	// Cancel and join the owner before allowing its persisted exit to be used.
	s.activeMu.Lock()
	var expired []uint
	for id, active := range s.active {
		if !active.deadline.IsZero() && !active.deadline.After(now) {
			expired = append(expired, id)
		}
	}
	s.activeMu.Unlock()
	for _, id := range expired {
		if err := s.cancelAttempt(ctx, id); err != nil {
			return err
		}
	}
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

// claimCandidateLimit 是单轮 tick 最多取几条候选。
//
// ⛔ 这里必须限量。原先的候选查询没有 LIMIT，积压 2 万条时每 250ms 就全量捞一遍
// （≈8 万行/秒的扫描），既是纯浪费，也让这条查询长期占着共享锁、放大与回写路径的冲突。
// 循环本身每拿到一个槽位就先 Reserve()，槽位用完立刻返回，所以"取 capacity×4 条"
// 与"取全部"在正常路径上等价，多取的是给"候选已被别的实例领走"留的余量。
func (s *Scheduler) claimCandidateLimit() int {
	capacity := s.capacity
	if capacity <= 0 {
		capacity = defaultSchedulerCapacity
	}
	limit := capacity * 4
	if limit < 64 {
		limit = 64
	}
	if limit > 1024 {
		limit = 1024
	}
	return limit
}

func (s *Scheduler) claimDue(ctx context.Context, now time.Time) error {
	var candidates []gbmodels.GbPTZOperation
	if err := ptzWriter(s.db).WithContext(ctx).
		Where("response_required = ? AND status IN ? AND attempt < max_attempts", true, []gbmodels.PTZOperationStatus{gbmodels.PTZOperationQueued, gbmodels.PTZOperationSent}).
		Where(`(attempt = 0 AND dispatch_started_at IS NULL AND queue_deadline_at > ?)
			OR (attempt > 0 AND next_attempt_at IS NOT NULL AND next_attempt_at <= ? AND transport_deadline_at > ?)`, gbmodels.PTZTimeComparison(s.db, now), gbmodels.PTZTimeComparison(s.db, now), gbmodels.PTZTimeComparison(s.db, now)).
		Order("id").Limit(s.claimCandidateLimit()).Find(&candidates).Error; err != nil {
		return err
	}
	for _, candidate := range candidates {
		reservation, ok := s.dispatcher.Reserve()
		if !ok {
			return nil
		}
		// Earlier candidates and convergence queries may have spent seconds in DB.
		claimNow := s.service.now()
		claimCtx, cancel := context.WithDeadline(ctx, claimNow.Add(playauth.PTZPreparationWindow))
		attempt, claimed, err := s.claimAttempt(claimCtx, candidate.ID, claimNow)
		cancel()
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
	if s.service.intents != nil || s.service.barrier != nil {
		// A runtime with authorization enabled cannot recover authority for a
		// historical queued row whose durable intent binding is missing.
		return gbmodels.GbPTZOperationAttempt{}, false, playauth.ErrDeviceIntentRevoked
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

		transportDeadline := now.Add(playauth.PTZPreparationWindow + schedulerTransportWindow)
		if operation.TransportDeadlineAt != nil {
			transportDeadline = *operation.TransportDeadlineAt
		}
		attemptNo := operation.Attempt + 1
		leaseUntil := transportDeadline

		updates := map[string]interface{}{
			"attempt": attemptNo, "next_attempt_at": nil,
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
	ownerCtx, cancel := context.WithCancel(dispatchCtx)
	prepareDeadline := attempt.StartedAt.Add(playauth.PTZPreparationWindow)
	if attempt.LeaseUntil.Before(prepareDeadline) {
		prepareDeadline = attempt.LeaseUntil
	}
	ctx, prepareCancel := context.WithDeadline(ownerCtx, prepareDeadline)
	defer prepareCancel()
	stopPreparation := context.AfterFunc(ctx, cancel)
	defer stopPreparation()
	done := make(chan struct{})
	s.activeMu.Lock()
	s.active[attempt.ID] = schedulerActiveAttempt{cancel: cancel, done: done, deadline: prepareDeadline}
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
	var timing *schedulerSendTiming
	var err error
	if !prepareDeadline.After(s.service.now()) {
		err = context.DeadlineExceeded
	} else {
		err = ptzWriter(s.db).WithContext(ctx).First(&operation, attempt.OperationID).Error
	}
	var lease playauth.DeviceOperationLease
	if err == nil && requiresPTZIntent(operation) {
		var id playauth.DeviceOperationIntentIdentity
		id, err = ptzIntentIdentity(operation)
		if err == nil && s.service.barrier == nil {
			err = playauth.ErrDeviceIntentUnavailable
		}
		if err == nil {
			lease, err = s.service.barrier.BeginEpoch(ownerCtx, id.DeviceCode, id.DeviceEpoch)
			if err == nil {
				var cancelQueries context.CancelFunc
				ctx, cancelQueries = context.WithDeadline(lease.Context(), prepareDeadline)
				defer cancelQueries()
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
				if operation.Action == actionCascadeConfigWrite {
					err = s.service.validateCascadeConfigWriteAuthorization(ctx, operation)
				} else if isCascadeAdvancedControlOperation(operation) {
					err = s.service.validateCascadeAdvancedControlAuthorization(ctx, operation)
				} else if isCascadeMaintenanceOperation(operation) {
					err = s.service.validateCascadeMaintenanceAuthorization(ctx, operation)
				} else if isCascadeTargetTrackOperation(operation) {
					err = s.service.validateCascadeTargetTrackAuthorization(ctx, operation)
				}
				if err == nil {
					sendStarted := s.service.now()
					if !prepareDeadline.After(sendStarted) || ctx.Err() != nil {
						err = context.DeadlineExceeded
					} else {
						start := sendStarted
						transportDeadline := start.Add(schedulerTransportWindow)
						if attempt.AttemptNo > 1 && operation.DispatchStartedAt != nil && operation.TransportDeadlineAt != nil &&
							!operation.TransportDeadlineAt.After(operation.DispatchStartedAt.Add(schedulerTransportWindow)) {
							start = *operation.DispatchStartedAt
							transportDeadline = *operation.TransportDeadlineAt
						}
						deadline, next := schedulerBoundaries(start, sendStarted, operation.MaxAttempts)
						timing = &schedulerSendTiming{startedAt: start, deadline: deadline, transportDeadline: transportDeadline, nextAttempt: next}
						// Stop the preparation timer, retaining owner/epoch cancellation.
						stopPreparation()
						sendBase := ownerCtx
						if lease != nil {
							sendBase = lease.Context()
						}
						sendCtx, sendCancel := context.WithDeadline(sendBase, deadline)
						s.activeMu.Lock()
						s.active[attempt.ID] = schedulerActiveAttempt{cancel: cancel, done: done, deadline: deadline}
						s.activeMu.Unlock()
						result, err = s.service.sender.SendMessageTracked(sendCtx, operation.DeviceCode, destination, transport, body)
						sendCancel()
					}
				}
			}
		}
	}
	observedAt := s.service.now()
	persistCtx, persistCancel := s.writebackContext(dispatchCtx)
	defer persistCancel()
	persistStarted := time.Now()
	if persistErr := s.persistAttemptOutcome(persistCtx, attempt, result, err, observedAt, timing); persistErr != nil {
		s.pendingMu.Lock()
		if s.pendingResults == nil {
			s.pendingResults = make(map[uint]schedulerPendingResult)
		}
		s.pendingResults[attempt.ID] = schedulerPendingResult{attempt: attempt, result: result, sendErr: err, observedAt: observedAt, lease: lease, timing: timing}
		s.pendingMu.Unlock()
		// 写回失败不得无声:否则 attempt 停留 dispatching,直到租约恢复才可能被发现
		if app.ZapLog != nil {
			app.Log(persistCtx).Named("ptz.scheduler").Error("PTZ 调度结果持久化失败",
				zap.String("event", "ptz.scheduler.persist_failed"),
				zap.Uint("attempt_id", attempt.ID), zap.Uint("operation_id", attempt.OperationID),
				// 预算与实际耗时分开放:预算吃满(耗时≈预算)是链路慢,
				// 远小于预算就失败则是事务冲突/约束错误,两者排查方向完全不同。
				zap.Duration("writeback_budget", s.persistTimeout),
				zap.Duration("writeback_elapsed", time.Since(persistStarted)),
				zap.Error(persistErr))
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
	if operation.TargetScope == gbmodels.ControlTargetScopeDevice {
		return net.JoinHostPort(device.IP, strconv.Itoa(device.Port)), device.Transport, nil
	}
	if isCascadeAdvancedControlOperation(operation) && (operation.Action == "guard_set" || operation.Action == "guard_reset" || operation.Action == "alarm_reset") && operation.TargetCode == device.DeviceID {
		return net.JoinHostPort(device.IP, strconv.Itoa(device.Port)), device.Transport, nil
	}
	// 通道级重试路径必须复核通道:设备在线而通道已离线或目录已变更时,
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
		if operation.Action == actionCascadeConfigWrite {
			return buildCascadeConfigWriteOperation(operation)
		}
		// ⛔ `VideoParamAttribute`（A-5）与配置族共用 `CmdType=DeviceConfig`，所以这里
		// **必须分流**，只看 CmdType 会把两者混成一件事。
		//
		// 只看 CmdType 的历史后果（2026-09-19 真机验证发现，已提交的回归）：配置族的
		// 下发全部被重建成 A-5 的报文 `<VideoParamAttribute Num="0">`（payload 里没有
		// `items`，Items 为 nil），**一块配置都发不出去**；设备照回 `Result=OK`，平台回读
		// 只表现为 `mismatch` —— 症状酷似"设备没照做"，归因成本极高。
		//
		// ⛔ 分流判据走 [deviceConfigOperationForm]（按 payload 的 `blocks` 判），
		// **不要退回 "action 白名单"**：白名单漏过一个 action 的代价就是上面这段历史
		// 重演一遍 —— 2026-09-20 抓拍会话（action=`snapshot_config`）正是这样整条静默失效。
		blocks, blockFamily, err := deviceConfigOperationForm(operation.Action, operation.PayloadJSON)
		if err != nil {
			return nil, err
		}
		if blockFamily {
			return manscdp.BuildDeviceConfigBlocksWithProfile(profile, targetCode, operation.SN, blocks)
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
		if operation.Action == actionTargetTrack {
			return buildCascadeTargetTrackOperation(operation)
		}
		if operation.Action == "precise" {
			return buildPrecise()
		}
		if operation.Action == "record_start" || operation.Action == "record_stop" || operation.Action == "guard_set" || operation.Action == "guard_reset" || operation.Action == "alarm_reset" || operation.Action == "teleboot" || operation.Action == "iframe" || operation.Action == "drag_zoom_in" || operation.Action == "drag_zoom_out" || operation.Action == actionFormatStorageCard {
			var payload struct {
				Action       string                 `json:"action"`
				AlarmMethod  string                 `json:"alarmMethod"`
				AlarmType    string                 `json:"alarmType"`
				Region       manscdp.DragZoomRegion `json:"region"`
				StreamNumber *int                   `json:"streamNumber"`
				// CardIndex 是存储卡格式化（A.2.3.1.13）的卡编号；0 表示"全部卡"，
				// 所以这里同样**不能用零值判断"没给"**，得由 Action 先分流。
				CardIndex int `json:"cardIndex"`
			}
			if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil {
				return nil, err
			}
			request := manscdp.AlarmResetOptions{AlarmMethod: payload.AlarmMethod, AlarmType: payload.AlarmType}
			switch operation.Action {
			case "iframe":
				return manscdp.BuildIFrameControlWithProfile(profile, targetCode, operation.SN)
			case "record_start":
				return manscdp.BuildRecordControlForStreamWithProfile(profile, targetCode, operation.SN, manscdp.RecordStart, payload.StreamNumber)
			case "record_stop":
				return manscdp.BuildRecordControlForStreamWithProfile(profile, targetCode, operation.SN, manscdp.RecordStop, payload.StreamNumber)
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
			case actionFormatStorageCard:
				// 重发时按落库的 cardIndex 原样重建：这是"同一张卡"，不是"当时那张卡的序号"。
				// 卡列表在此期间变化时，也许编号已指向另一张卡 —— 但那属于"重发本身就不该发生"
				// 的范畴（本 action 的 MaxAttempts=1，正常路径下这里根本走不到）。
				return manscdp.BuildFormatSDCardControlWithProfile(profile, targetCode, operation.SN, payload.CardIndex)
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

func (s *Scheduler) persistAttemptOutcome(ctx context.Context, attempt gbmodels.GbPTZOperationAttempt, result uac.TrackedMessageResult, sendErr error, observedAt time.Time, timing *schedulerSendTiming) error {
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
		if timing != nil {
			// Only narrow an existing ownership lease; never revive an expired
			// owner or overwrite another attempt's scheduling state.
			if timing.deadline.After(currentAttempt.LeaseUntil) || operation.Attempt != attempt.AttemptNo {
				return playauth.ErrDeviceIntentConflict
			}
			if err := tx.Model(&gbmodels.GbPTZOperationAttempt{}).Where("id = ?", currentAttempt.ID).
				UpdateColumn("lease_until", timing.deadline).Error; err != nil {
				return err
			}
			if err := tx.Model(&gbmodels.GbPTZOperation{}).Where("id = ? AND attempt = ?", operation.ID, attempt.AttemptNo).
				Updates(map[string]interface{}{"dispatch_started_at": timing.startedAt, "transport_deadline_at": timing.transportDeadline, "next_attempt_at": timing.nextAttempt}).Error; err != nil {
				return err
			}
			currentAttempt.LeaseUntil = timing.deadline
			operation.TransportDeadlineAt = &timing.transportDeadline
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
