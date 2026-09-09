package schedulerhelper

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"

	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

// 任务调度器
type JobScheduler struct {
	mu            sync.RWMutex
	cron          *cron.Cron
	jobs          map[string]*Job     // 任务存储
	executors     map[string]Executor // 执行器存储
	jobResults    chan *JobResult     // 任务结果通道
	logger        JobLogger           // 日志记录器
	wg            sync.WaitGroup      // 等待已接纳的任务
	lastExecNS    atomic.Int64        // 防止同一调度器内快速执行生成重复ID
	stopping      bool
	stopDone      chan struct{}
	stopErr       error
	resultsMu     sync.RWMutex
	resultsClosed bool
}

var errSchedulerStopping = errors.New("scheduler is stopping")

// NewJobScheduler 创建新的调度器
// 使用函数选项模式进行配置，例如：
//
//	scheduler := NewJobScheduler(
//	    WithLoggerConfig("/var/log/jobs", LevelDebug),
//	    WithJobResultsBufferSize(2000),
//	    WithCronOptions(cron.WithSeconds(), cron.WithLogger(log.New(os.Stdout, "cron: ", log.LstdFlags))),
//	)
func NewJobScheduler(opts ...Option) *JobScheduler {
	// 默认配置
	s := &JobScheduler{
		cron:       cron.New(cron.WithSeconds()),
		jobs:       make(map[string]*Job),
		executors:  make(map[string]Executor),
		jobResults: make(chan *JobResult, 1000),
	}

	// 应用选项函数
	for _, opt := range opts {
		opt(s)
	}

	// 应用启动时由调用者通过 WithLogger 注入共享根 logger。独立工具如需
	// 文件输出，必须显式使用 WithLoggerConfig；默认构造不创建旁路文件。
	if s.logger == nil {
		s.logger = NewZapJobLogger(nil)
	}

	return s
}

// 启动调度器
func (s *JobScheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return
	}
	s.cron.Start()
	s.logger.Debug("system", "调度器已启动")
}

// 停止调度器
func (s *JobScheduler) Stop() {
	if err := s.StopContext(context.Background()); err != nil {
		if s.logger != nil {
			s.logger.Error("system", "调度器停止失败: %v", err)
		}
	}
}

// StopContext stops admitting new work and waits for the cron loop and all
// admitted executions to finish. A timed-out caller does not interrupt the
// shutdown; a later call can continue waiting for the same completion.
func (s *JobScheduler) StopContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if !s.stopping {
		s.stopping = true
		s.stopDone = make(chan struct{})
		cron := s.cron
		done := s.stopDone
		go func() { s.finishStop(cron.Stop(), done) }()
	}
	done := s.stopDone
	s.mu.Unlock()

	select {
	case <-done:
		return s.stopResult()
	default:
	}
	select {
	case <-done:
		return s.stopResult()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *JobScheduler) finishStop(cronDone context.Context, done chan struct{}) {
	<-cronDone.Done()
	s.wg.Wait()

	s.resultsMu.Lock()
	if !s.resultsClosed {
		close(s.jobResults)
		s.resultsClosed = true
	}
	s.resultsMu.Unlock()

	if s.logger != nil {
		s.logger.Info("system", "调度器已停止")
	}
	var stopErr error
	if s.logger != nil {
		stopErr = s.logger.Close()
	}

	s.mu.Lock()
	s.stopErr = stopErr
	close(done)
	s.mu.Unlock()
}

func (s *JobScheduler) stopResult() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stopErr
}

// 注册执行器
func (s *JobScheduler) RegisterExecutor(executor Executor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return
	}
	s.executors[executor.Name()] = executor
	s.logger.Debug("system", "注册执行器: %s", executor.Name())
}

// 添加/更新任务
func (s *JobScheduler) AddOrUpdateJob(job *Job) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return "", errSchedulerStopping
	}
	action := "更新"
	if _, exists := s.jobs[job.ID]; !exists {
		action = "创建"
		if job.ID == "" {
			job.ID = generateJobID()
		}
		job.CreatedAt = time.Now()
	}

	// 验证任务配置
	if err := validateJob(job); err != nil {
		return "", err
	}

	job.UpdatedAt = time.Now()

	// 如果任务已存在且已调度，先移除
	if existingJob, exists := s.jobs[job.ID]; exists && existingJob.cronEntryID != 0 {
		s.cron.Remove(existingJob.cronEntryID)
	}

	// 如果任务启用状态，添加到cron调度
	if job.Status == StatusEnabled {
		entryID, err := s.cron.AddFunc(job.CronExpression, s.createJobFunc(job))
		if err != nil {
			return "", fmt.Errorf("failed to add job to cron: %v", err)
		}
		job.cronEntryID = entryID
	}

	s.jobs[job.ID] = job
	s.logger.LogJobLifecycle(job, action)
	return job.ID, nil
}

// 创建任务执行函数
func (s *JobScheduler) createJobFunc(job *Job) func() {
	return func() {
		jobSnapshot, ok := s.admitExecution(job)
		if !ok {
			return
		}

		defer s.wg.Done()
		defer s.decrementRunningCount(jobSnapshot.ID)
		s.executeJob(jobSnapshot)
	}
}

// 检查任务是否可以执行
func (s *JobScheduler) canExecute(job *Job) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.canExecuteLocked(job)
}

func (s *JobScheduler) canExecuteLocked(job *Job) bool {
	currentJob, exists := s.jobs[job.ID]
	if !exists {
		s.logger.Warn(job.ID, "任务不存在，无法执行")
		return false
	}

	// 获取当前运行中的任务数量
	runningCount := currentJob.RunningCount

	switch job.BlockingPolicy {
	case BlockDiscard:
		// 丢弃：如果任务正在执行中则丢弃当前任务（同一时间只能有一个任务执行）
		if runningCount == 0 {
			return true
		}
		s.logger.Warn(job.ID, "任务被丢弃（BlockDiscard策略）：任务正在执行中，当前运行数(%d)", runningCount)
		return false

	case BlockParallel:

		if job.ParallelNum <= 0 {
			return true
		}
		// 并行：如果超过并行数则丢弃
		if runningCount < job.ParallelNum {
			return true
		}
		s.logger.Warn(job.ID, "任务被丢弃（BlockParallel策略）：当前运行数(%d) >= 并行数(%d)", runningCount, job.ParallelNum)
		return false

	default:
		// 默认策略与 BlockDiscard 相同：如果任务正在执行中则丢弃
		if runningCount == 0 {
			return true
		}
		s.logger.Warn(job.ID, "任务被丢弃（默认策略）：任务正在执行中，当前运行数(%d)", runningCount)
		return false
	}
}

func (s *JobScheduler) admitExecution(job *Job) (*Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return nil, false
	}
	if !s.canExecuteLocked(job) {
		s.logger.LogJobLifecycle(job, "跳过")
		return nil, false
	}
	s.incrementRunningCountLocked(job.ID)
	jobSnapshot := job.Clone()
	s.wg.Add(1)
	return jobSnapshot, true
}

// 立即执行一次任务
func (s *JobScheduler) ExecuteNow(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return errSchedulerStopping
	}
	job, exists := s.jobs[jobID]

	if !exists {
		return fmt.Errorf("job not found: %s", jobID)
	}

	if !s.canExecuteLocked(job) {
		return fmt.Errorf("job cannot execute due to blocking policy")
	}
	s.incrementRunningCountLocked(job.ID)
	jobSnapshot := job.Clone()
	s.wg.Add(1)
	s.logger.LogJobLifecycle(job, "手动触发")

	// 异步执行
	go func() {
		defer s.wg.Done()
		defer s.decrementRunningCount(jobSnapshot.ID)
		s.executeJob(jobSnapshot)
	}()

	return nil
}

// 执行任务（非递归版本）
func (s *JobScheduler) executeJob(job *Job) {
	startTime := time.Now()
	jobExecutionID := s.newExecutionID(job.ID, startTime)

	var err error
	for currentRetry := 0; currentRetry <= job.MaxRetry; currentRetry++ {
		attempt := currentRetry + 1
		if logger, ok := s.logger.(*ZapJobLogger); ok {
			logger.logExecutionStart(job.ID, jobExecutionID, attempt)
		} else {
			s.logger.Debug(job.ID, "开始执行 | 执行ID: %s | 重试次数: %d", jobExecutionID, currentRetry)
		}

		result := &JobResult{
			JobID:           job.ID,
			ExecutionID:     jobExecutionID,
			StartTime:       time.Now(),
			RetryCount:      currentRetry,
			Attempt:         attempt,
			ExecutionPolicy: job.ExecutionPolicy,
		}

		// 获取执行器（加读锁保护）
		s.mu.RLock()
		executor, exists := s.executors[job.ExecutorName]
		s.mu.RUnlock()

		if !exists {
			result.Status = "FAILED"
			result.Error = fmt.Errorf("executor not found: %s", job.ExecutorName)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			s.logExecutionResult(result)
			s.sendResult(result)
			return
		}

		// 创建带超时的上下文
		ctx, cancel := context.WithTimeout(context.Background(), job.Timeout)
		ctx = WithExecutionContext(ctx, jobExecutionID, attempt, job.ExecutorName, job.ID)
		if logger, ok := s.logger.(*ZapJobLogger); ok {
			ctx = logging.WithContext(ctx, logger.executionScope(job.ID, jobExecutionID, attempt, job.ExecutorName))
		}

		// 执行任务（传递 job 的深拷贝，避免并发修改）
		jobCopy := job.Clone()
		err = executor.Execute(ctx, jobCopy)
		cancel()

		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)

		if err != nil {
			result.Status = "FAILED"
			result.Error = err
			if logger, ok := s.logger.(*ZapJobLogger); ok {
				logger.logExecutionFailure(job.ID, jobExecutionID, attempt, err)
			} else {
				s.logger.Warn(job.ID, "执行失败 | 错误: %v", err)
			}

			if currentRetry < job.MaxRetry {
				if logger, ok := s.logger.(*ZapJobLogger); ok {
					logger.logExecutionRetry(job.ID, jobExecutionID, attempt+1, job.RetryInterval)
				} else {
					s.logger.Info(job.ID, "准备重试 | 第%d次重试 | 等待 %v", currentRetry+1, job.RetryInterval)
				}
				time.Sleep(job.RetryInterval)
				continue
			} else {
				s.logExecutionResult(result)
				s.sendResult(result)
				return
			}
		} else {
			result.Status = "SUCCESS"
			s.logExecutionResult(result)
			s.sendResult(result)

			// 单次执行策略：执行成功后自动禁用
			if job.ExecutionPolicy == PolicyOnce {
				if disableErr := s.DisableJob(job.ID); disableErr != nil {
					s.logger.Error(job.ID, "自动禁用任务失败: %v", disableErr)
				} else {
					s.logger.Debug(job.ID, "单次执行任务已完成，已自动禁用")
				}
			}
			return
		}
	}
}

func (s *JobScheduler) newExecutionID(jobID string, now time.Time) string {
	nanoseconds := now.UnixNano()
	for {
		last := s.lastExecNS.Load()
		candidate := nanoseconds
		if candidate <= last {
			candidate = last + 1
		}
		if s.lastExecNS.CompareAndSwap(last, candidate) {
			return fmt.Sprintf("%s-%d", jobID, candidate)
		}
	}
}

func (s *JobScheduler) logExecutionResult(result *JobResult) {
	if logger, ok := s.logger.(*ZapJobLogger); ok {
		logger.logExecutionResult(result)
		return
	}
	if result.Status == "SUCCESS" {
		s.logger.Debug(result.JobID, "执行成功")
		return
	}
	s.logger.Error(result.JobID, "达到最大重试次数")
}

// 新增：安全发送结果
func (s *JobScheduler) sendResult(result *JobResult) {
	// Keep the result lock (rather than the admission lock) across backpressure
	// so a timed StopContext can still mark the scheduler as stopping.
	s.resultsMu.RLock()
	defer s.resultsMu.RUnlock()
	if s.resultsClosed {
		return
	}
	s.jobResults <- result
}

// 启用任务
func (s *JobScheduler) EnableJob(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return errSchedulerStopping
	}

	job, exists := s.jobs[jobID]
	if !exists {
		return fmt.Errorf("job not found: %s", jobID)
	}

	if job.Status == StatusEnabled {
		return nil
	}

	// 添加到cron调度
	entryID, err := s.cron.AddFunc(job.CronExpression, s.createJobFunc(job))
	if err != nil {
		s.logger.Error(job.ID, "启用任务失败: %v", err)
		return fmt.Errorf("failed to enable job: %v", err)
	}

	job.cronEntryID = entryID
	job.Status = StatusEnabled
	job.UpdatedAt = time.Now()
	s.logger.LogJobLifecycle(job, "启用")
	return nil
}

// 禁用任务
func (s *JobScheduler) DisableJob(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, exists := s.jobs[jobID]
	if !exists {
		return fmt.Errorf("job not found: %s", jobID)
	}

	if job.Status == StatusDisabled {
		return nil
	}

	// 从cron调度移除
	if job.cronEntryID != 0 && !s.stopping {
		s.cron.Remove(job.cronEntryID)
	}
	job.cronEntryID = 0

	job.Status = StatusDisabled
	job.UpdatedAt = time.Now()
	s.logger.LogJobLifecycle(job, "禁用")
	return nil
}

// 删除任务
func (s *JobScheduler) DeleteJob(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, exists := s.jobs[jobID]
	if !exists {
		return fmt.Errorf("job not found: %s", jobID)
	}

	// 如果任务已启用，先从cron移除
	if job.Status == StatusEnabled && job.cronEntryID != 0 && !s.stopping {
		s.cron.Remove(job.cronEntryID)
	}

	delete(s.jobs, jobID)
	s.logger.LogJobLifecycle(job, "删除")
	return nil
}

// JobExists 检查任务是否存在
func (s *JobScheduler) JobExists(jobID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.jobs[jobID]
	return exists
}

// 获取任务列表
func (s *JobScheduler) ListJobs() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

// 获取所有执行器
func (s *JobScheduler) ListExecutors() []Executor {
	s.mu.RLock()
	defer s.mu.RUnlock()

	executors := make([]Executor, 0, len(s.executors))
	for _, executor := range s.executors {
		executors = append(executors, executor)
	}
	return executors
}

// 获取任务结果通道
func (s *JobScheduler) GetResults() <-chan *JobResult {
	return s.jobResults
}

// 更新运行计数
func (s *JobScheduler) incrementRunningCount(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.incrementRunningCountLocked(jobID)
}

func (s *JobScheduler) incrementRunningCountLocked(jobID string) {
	if job, exists := s.jobs[jobID]; exists {
		job.RunningCount++
	}
}

func (s *JobScheduler) decrementRunningCount(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, exists := s.jobs[jobID]; exists && job.RunningCount > 0 {
		job.RunningCount--
	}
}
