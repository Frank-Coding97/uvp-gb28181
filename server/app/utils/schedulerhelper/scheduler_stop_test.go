package schedulerhelper

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/require"
)

type loggingStopLogger struct {
	closeCount atomic.Int32
}

func (l *loggingStopLogger) Debug(string, string, ...interface{}) {}
func (l *loggingStopLogger) Info(string, string, ...interface{})  {}
func (l *loggingStopLogger) Warn(string, string, ...interface{})  {}
func (l *loggingStopLogger) Error(string, string, ...interface{}) {}
func (l *loggingStopLogger) Fatal(string, string, ...interface{}) {}
func (l *loggingStopLogger) LogJobExecution(*JobResult)           {}
func (l *loggingStopLogger) LogJobLifecycle(*Job, string)         {}
func (l *loggingStopLogger) Close() error                         { l.closeCount.Add(1); return nil }

type loggingStopExecutor struct {
	name         string
	started      chan struct{}
	release      <-chan struct{}
	executionCnt atomic.Int32
}

type loggingStopScheduleParser struct{}

func (loggingStopScheduleParser) Parse(string) (cron.Schedule, error) {
	return loggingStopEveryMillisecondSchedule{}, nil
}

type loggingStopEveryMillisecondSchedule struct{}

func (loggingStopEveryMillisecondSchedule) Next(now time.Time) time.Time {
	return now.Add(time.Millisecond)
}

func (e *loggingStopExecutor) Name() string { return e.name }

func (e *loggingStopExecutor) Execute(ctx context.Context, _ *Job) error {
	e.executionCnt.Add(1)
	select {
	case e.started <- struct{}{}:
	default:
	}
	if e.release == nil {
		return nil
	}
	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newLoggingStopScheduler(t *testing.T, executor Executor, logger *loggingStopLogger, buffer int, options ...Option) (*JobScheduler, *Job) {
	t.Helper()
	baseOptions := []Option{WithLogger(logger), WithJobResultsBufferSize(buffer)}
	scheduler := NewJobScheduler(append(baseOptions, options...)...)
	scheduler.RegisterExecutor(executor)
	job := &Job{
		ID:              "logging-stop-job",
		Group:           "logging-stop",
		Name:            "logging stop job",
		ExecutorName:    executor.Name(),
		ExecutionPolicy: PolicyRepeat,
		Status:          StatusDisabled,
		CronExpression:  "@every 1ms",
		BlockingPolicy:  BlockDiscard,
		Timeout:         time.Second,
	}
	_, err := scheduler.AddOrUpdateJob(job)
	require.NoError(t, err)
	return scheduler, job
}

func waitLoggingStopExecution(t *testing.T, executor *loggingStopExecutor) {
	t.Helper()
	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fake executor")
	}
}

func waitLoggingStopState(t *testing.T, scheduler *JobScheduler) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		scheduler.mu.RLock()
		stopping := scheduler.stopping
		scheduler.mu.RUnlock()
		if stopping {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("scheduler did not enter stopping state")
}

func collectLoggingStopResults(t *testing.T, results <-chan *JobResult) []*JobResult {
	t.Helper()
	collected := make([]*JobResult, 0)
	for result := range results {
		collected = append(collected, result)
	}
	return collected
}

func TestLoggingStopWaitsForAcceptedTask(t *testing.T) {
	release := make(chan struct{})
	executor := &loggingStopExecutor{name: "blocking", started: make(chan struct{}, 1), release: release}
	logger := &loggingStopLogger{}
	scheduler, job := newLoggingStopScheduler(t, executor, logger, 4)

	require.NoError(t, scheduler.ExecuteNow(job.ID))
	waitLoggingStopExecution(t, executor)

	stopDone := make(chan error, 1)
	go func() { stopDone <- scheduler.StopContext(context.Background()) }()
	select {
	case err := <-stopDone:
		t.Fatalf("StopContext returned before task release: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	select {
	case _, ok := <-scheduler.GetResults():
		if !ok {
			t.Fatal("results channel closed while task was still running")
		}
	default:
	}

	close(release)
	require.NoError(t, <-stopDone)
	results := collectLoggingStopResults(t, scheduler.GetResults())
	require.Len(t, results, 1)
	require.NoError(t, scheduler.StopContext(context.Background()))
	require.Equal(t, int32(1), logger.closeCount.Load())
}

func TestLoggingStopCompatibilityWrapperWaits(t *testing.T) {
	release := make(chan struct{})
	executor := &loggingStopExecutor{name: "compat", started: make(chan struct{}, 1), release: release}
	logger := &loggingStopLogger{}
	scheduler, job := newLoggingStopScheduler(t, executor, logger, 4)

	require.NoError(t, scheduler.ExecuteNow(job.ID))
	waitLoggingStopExecution(t, executor)
	stopDone := make(chan struct{})
	go func() {
		scheduler.Stop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
		t.Fatal("Stop returned before task release")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish after task release")
	}
	require.Len(t, collectLoggingStopResults(t, scheduler.GetResults()), 1)
	require.Equal(t, int32(1), logger.closeCount.Load())
}

func TestLoggingStopContextHonorsTimeout(t *testing.T) {
	release := make(chan struct{})
	executor := &loggingStopExecutor{name: "timeout", started: make(chan struct{}, 1), release: release}
	logger := &loggingStopLogger{}
	scheduler, job := newLoggingStopScheduler(t, executor, logger, 4)

	require.NoError(t, scheduler.ExecuteNow(job.ID))
	waitLoggingStopExecution(t, executor)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	stopResult := make(chan error, 1)
	go func() { stopResult <- scheduler.StopContext(ctx) }()
	select {
	case err := <-stopResult:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("StopContext did not honor context timeout")
	}

	select {
	case _, ok := <-scheduler.GetResults():
		if !ok {
			t.Fatal("results channel closed after timeout with producer in flight")
		}
	default:
	}
	close(release)
	require.NoError(t, <-waitLoggingStopCompletion(scheduler))
	require.Len(t, collectLoggingStopResults(t, scheduler.GetResults()), 1)
}

func waitLoggingStopCompletion(scheduler *JobScheduler) <-chan error {
	result := make(chan error, 1)
	go func() { result <- scheduler.StopContext(context.Background()) }()
	return result
}

func TestLoggingStopRejectsAdmissionsAfterStop(t *testing.T) {
	executor := &loggingStopExecutor{name: "reject", started: make(chan struct{}, 1)}
	logger := &loggingStopLogger{}
	scheduler, job := newLoggingStopScheduler(t, executor, logger, 8)
	require.NoError(t, scheduler.StopContext(context.Background()))

	require.Error(t, scheduler.ExecuteNow(job.ID))
	newJob := *job
	newJob.ID = "logging-stop-new-job"
	_, err := scheduler.AddOrUpdateJob(&newJob)
	require.Error(t, err)
	require.Error(t, scheduler.EnableJob(job.ID))
	require.NoError(t, scheduler.StopContext(context.Background()))
}

func TestLoggingStopBackpressuresResultsUntilConsumer(t *testing.T) {
	release := make(chan struct{})
	executor := &loggingStopExecutor{name: "backpressure", started: make(chan struct{}, 3), release: release}
	logger := &loggingStopLogger{}
	scheduler, job := newLoggingStopScheduler(t, executor, logger, 1)
	job.BlockingPolicy = BlockParallel
	job.ParallelNum = 3

	for range 3 {
		require.NoError(t, scheduler.ExecuteNow(job.ID))
	}
	for range 3 {
		waitLoggingStopExecution(t, executor)
	}
	close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	stopResult := make(chan error, 1)
	go func() { stopResult <- scheduler.StopContext(ctx) }()
	select {
	case err := <-stopResult:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("StopContext did not return after context timeout")
	}

	drained := make(chan int, 1)
	go func() {
		count := 0
		for range scheduler.GetResults() {
			count++
		}
		drained <- count
	}()
	require.NoError(t, <-waitLoggingStopCompletion(scheduler))
	require.Equal(t, 3, <-drained)
}

func TestLoggingStopConcurrentCronExecuteAndStop(t *testing.T) {
	for round := 0; round < 100; round++ {
		release := make(chan struct{})
		executor := &loggingStopExecutor{name: "race", started: make(chan struct{}, 1), release: release}
		logger := &loggingStopLogger{}
		scheduler, job := newLoggingStopScheduler(t, executor, logger, 8, WithCronOptions(cron.WithParser(loggingStopScheduleParser{})))
		scheduler.Start()
		_, err := scheduler.AddOrUpdateJob(&Job{
			ID:              job.ID,
			Group:           job.Group,
			Name:            job.Name,
			ExecutorName:    job.ExecutorName,
			ExecutionPolicy: PolicyRepeat,
			Status:          StatusEnabled,
			CronExpression:  job.CronExpression,
			BlockingPolicy:  BlockDiscard,
			Timeout:         time.Second,
		})
		require.NoError(t, err)
		waitLoggingStopExecution(t, executor)

		stopDone := make(chan error, 1)
		go func() { stopDone <- scheduler.StopContext(context.Background()) }()
		waitLoggingStopState(t, scheduler)
		scheduler.Start()
		_, err = scheduler.AddOrUpdateJob(&Job{
			ID:              "logging-stop-rejected-job",
			Group:           job.Group,
			Name:            job.Name,
			ExecutorName:    job.ExecutorName,
			ExecutionPolicy: PolicyRepeat,
			Status:          StatusEnabled,
			CronExpression:  job.CronExpression,
			BlockingPolicy:  BlockDiscard,
			Timeout:         time.Second,
		})
		require.Error(t, err)
		require.Error(t, scheduler.EnableJob(job.ID))
		require.Error(t, scheduler.ExecuteNow(job.ID))
		close(release)
		require.NoError(t, <-stopDone)
		require.Len(t, collectLoggingStopResults(t, scheduler.GetResults()), 1)
		require.NoError(t, scheduler.StopContext(context.Background()))
	}
}

func TestLoggingStopConcurrentCallsCloseOnce(t *testing.T) {
	executor := &loggingStopExecutor{name: "idempotent", started: make(chan struct{}, 1)}
	logger := &loggingStopLogger{}
	scheduler, job := newLoggingStopScheduler(t, executor, logger, 8)
	require.NoError(t, scheduler.ExecuteNow(job.ID))
	waitLoggingStopExecution(t, executor)

	const callers = 8
	stops := make(chan error, callers)
	for range callers {
		go func() { stops <- scheduler.StopContext(context.Background()) }()
	}
	for range callers {
		require.NoError(t, <-stops)
	}
	require.Len(t, collectLoggingStopResults(t, scheduler.GetResults()), 1)
	require.Equal(t, int32(1), logger.closeCount.Load())
}

func TestLoggingStopCronChainWaitsForCallback(t *testing.T) {
	release := make(chan struct{})
	executor := &loggingStopExecutor{name: "cron-chain", started: make(chan struct{}, 1), release: release}
	logger := &loggingStopLogger{}
	scheduler, job := newLoggingStopScheduler(t, executor, logger, 4, WithCronOptions(
		cron.WithParser(loggingStopScheduleParser{}),
		cron.WithChain(cron.SkipIfStillRunning(cron.DiscardLogger)),
	))
	scheduler.Start()
	_, err := scheduler.AddOrUpdateJob(&Job{
		ID:              job.ID,
		Group:           job.Group,
		Name:            job.Name,
		ExecutorName:    job.ExecutorName,
		ExecutionPolicy: PolicyRepeat,
		Status:          StatusEnabled,
		CronExpression:  job.CronExpression,
		BlockingPolicy:  BlockDiscard,
		Timeout:         time.Second,
	})
	require.NoError(t, err)
	waitLoggingStopExecution(t, executor)

	cronDone := scheduler.cron.Stop()
	select {
	case <-cronDone.Done():
		t.Fatal("cron chain released before callback finished")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case <-cronDone.Done():
	case <-time.After(time.Second):
		t.Fatal("cron did not wait for callback completion")
	}

	require.NoError(t, scheduler.StopContext(context.Background()))
	require.Len(t, collectLoggingStopResults(t, scheduler.GetResults()), 1)
}

func TestLoggingStopExecuteNowSnapshotUnderBackpressure(t *testing.T) {
	executor := &loggingStopExecutor{name: "snapshot", started: make(chan struct{}, 20)}
	logger := &loggingStopLogger{}
	scheduler, job := newLoggingStopScheduler(t, executor, logger, 1)
	job.BlockingPolicy = BlockParallel
	job.ParallelNum = 32

	const executions = 20
	consumerEntered := make(chan struct{})
	consumerRelease := make(chan struct{})
	consumerDone := make(chan int, 1)
	go func() {
		count := 0
		first := true
		for range scheduler.GetResults() {
			if first {
				close(consumerEntered)
				<-consumerRelease
				first = false
			}
			count++
		}
		consumerDone <- count
	}()

	start := make(chan struct{})
	errs := make(chan error, executions)
	var launch sync.WaitGroup
	launch.Add(executions)
	for range executions {
		go func() {
			defer launch.Done()
			<-start
			errs <- scheduler.ExecuteNow(job.ID)
		}()
	}
	close(start)
	launch.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	deadline := time.Now().Add(time.Second)
	for executor.executionCnt.Load() != executions && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	require.Equal(t, int32(executions), executor.executionCnt.Load())
	select {
	case <-consumerEntered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for slow result consumer")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, scheduler.StopContext(ctx), context.DeadlineExceeded)

	close(consumerRelease)
	require.NoError(t, <-waitLoggingStopCompletion(scheduler))
	require.Equal(t, executions, <-consumerDone)
}
