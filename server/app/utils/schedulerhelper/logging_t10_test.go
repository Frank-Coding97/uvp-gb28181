package schedulerhelper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type loggingT10Executor struct {
	name   string
	mu     sync.Mutex
	errors []error
	seen   []ExecutionContext
}

func (e *loggingT10Executor) Name() string { return e.name }

func (e *loggingT10Executor) Execute(ctx context.Context, _ *Job) error {
	e.mu.Lock()
	if info, ok := ExecutionContextFromContext(ctx); ok {
		e.seen = append(e.seen, info)
	}
	var err error
	if len(e.errors) > 0 {
		err, e.errors = e.errors[0], e.errors[1:]
	}
	e.mu.Unlock()
	return err
}

func newLoggingT10Scheduler(t *testing.T, logger JobLogger, executor Executor) (*JobScheduler, *Job) {
	t.Helper()
	scheduler := NewJobScheduler(WithLogger(logger), WithJobResultsBufferSize(256))
	scheduler.RegisterExecutor(executor)
	job := &Job{
		ID:              "job-t10",
		Group:           "logging",
		Name:            "logging job",
		ExecutorName:    executor.Name(),
		ExecutionPolicy: PolicyRepeat,
		Status:          StatusDisabled,
		Timeout:         time.Second,
		RetryInterval:   0,
	}
	scheduler.jobs[job.ID] = job
	return scheduler, job
}

func TestLoggingJobNoopProducesDebugResultWithExecutionID(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := NewZapJobLogger(zap.New(core))
	executor := &loggingT10Executor{name: "noop"}
	scheduler, job := newLoggingT10Scheduler(t, logger, executor)

	for range 100 {
		scheduler.executeJob(job)
	}

	seenIDs := make(map[string]struct{}, 100)
	resultCount := 0
	for range 100 {
		result := <-scheduler.GetResults()
		resultCount++
		require.Equal(t, "SUCCESS", result.Status)
		require.NotEmpty(t, result.ExecutionID)
		require.Equal(t, 1, result.Attempt)
		seenIDs[result.ExecutionID] = struct{}{}
	}
	require.Equal(t, 100, resultCount)
	require.Len(t, seenIDs, 100)

	infoSuccessCount := 0
	for _, entry := range logs.All() {
		fields := entry.ContextMap()
		if fields["event"] == "scheduler.execution.completed" && fields["status"] == "SUCCESS" {
			require.Equal(t, zap.DebugLevel, entry.Level)
			require.NotEmpty(t, fields["execution_id"])
			if entry.Level == zap.InfoLevel {
				infoSuccessCount++
			}
		}
	}
	require.Zero(t, infoSuccessCount)
}

func TestLoggingJobRetryKeepsExecutionIDAndIncrementsAttempt(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := NewZapJobLogger(zap.New(core))
	executor := &loggingT10Executor{name: "retry", errors: []error{errors.New("retry secret"), nil}}
	scheduler, job := newLoggingT10Scheduler(t, logger, executor)
	job.MaxRetry = 1
	scheduler.executeJob(job)

	result := <-scheduler.GetResults()
	require.Equal(t, "SUCCESS", result.Status)
	require.Equal(t, 1, result.RetryCount)
	require.Equal(t, 2, result.Attempt)
	require.NotEmpty(t, result.ExecutionID)

	executor.mu.Lock()
	seen := append([]ExecutionContext(nil), executor.seen...)
	executor.mu.Unlock()
	require.Len(t, seen, 2)
	require.Equal(t, seen[0].ExecutionID, seen[1].ExecutionID)
	require.Equal(t, 1, seen[0].Attempt)
	require.Equal(t, 2, seen[1].Attempt)

	var retryFound, completedFound bool
	for _, entry := range logs.All() {
		fields := entry.ContextMap()
		switch fields["event"] {
		case "scheduler.execution.retry":
			retryFound = true
			require.Equal(t, seen[0].ExecutionID, fields["execution_id"])
		case "scheduler.execution.completed":
			completedFound = true
			require.Equal(t, seen[0].ExecutionID, fields["execution_id"])
		}
	}
	require.True(t, retryFound)
	require.True(t, completedFound)
}

func TestLoggingJobMissingExecutorHasCorrelation(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := NewZapJobLogger(zap.New(core))
	scheduler := NewJobScheduler(WithLogger(logger), WithJobResultsBufferSize(2))
	job := &Job{ID: "missing", ExecutorName: "not-registered", Timeout: time.Second}
	scheduler.executeJob(job)

	result := <-scheduler.GetResults()
	require.Equal(t, "FAILED", result.Status)
	require.NotEmpty(t, result.ExecutionID)
	require.Equal(t, 1, result.Attempt)
	entries := logs.All()
	require.NotEmpty(t, entries)
	var completed observer.LoggedEntry
	for _, entry := range entries {
		if entry.ContextMap()["event"] == "scheduler.execution.completed" {
			completed = entry
		}
	}
	require.Equal(t, zap.ErrorLevel, completed.Level)
	require.Equal(t, result.ExecutionID, completed.ContextMap()["execution_id"])
}

func TestLoggingJobConcurrentExecutionsHaveDistinctIDs(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := NewZapJobLogger(zap.New(core))
	executor := &loggingT10Executor{name: "parallel"}
	scheduler, job := newLoggingT10Scheduler(t, logger, executor)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scheduler.executeJob(job)
		}()
	}
	wg.Wait()

	seen := make(map[string]struct{}, 16)
	for range 16 {
		result := <-scheduler.GetResults()
		seen[result.ExecutionID] = struct{}{}
	}
	require.Len(t, seen, 16)
	for _, entry := range logs.All() {
		if event := entry.ContextMap()["event"]; event == "scheduler.execution.started" || event == "scheduler.execution.completed" {
			require.NotEmpty(t, entry.ContextMap()["execution_id"])
		}
	}
}

func TestLoggingJobLifecycleUsesDebugForSetupAndInfoForUserAction(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := NewZapJobLogger(zap.New(core))
	job := &Job{ID: "lifecycle", Group: "g", Name: "n", ExecutorName: "e", CronExpression: "* * * * *"}
	logger.LogJobLifecycle(job, "创建")
	logger.LogJobLifecycle(job, "手动触发")
	logger.LogJobLifecycle(job, "启用")

	entries := logs.All()
	require.Len(t, entries, 3)
	require.Equal(t, zap.DebugLevel, entries[0].Level)
	require.Equal(t, zap.InfoLevel, entries[1].Level)
	require.Equal(t, zap.InfoLevel, entries[2].Level)
}

func TestLoggingJobSchedulerManualTriggerUsesInfo(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := NewZapJobLogger(zap.New(core))
	executor := &loggingT10Executor{name: "manual"}
	scheduler, job := newLoggingT10Scheduler(t, logger, executor)

	require.NoError(t, scheduler.ExecuteNow(job.ID))
	result := <-scheduler.GetResults()
	require.Equal(t, "SUCCESS", result.Status)

	var manual observer.LoggedEntry
	for _, entry := range logs.All() {
		fields := entry.ContextMap()
		if fields["event"] == "scheduler.job.lifecycle" && fields["action"] == "手动触发" {
			manual = entry
		}
	}
	require.Equal(t, zap.InfoLevel, manual.Level)
	require.Equal(t, job.ID, manual.ContextMap()["job_id"])
}

func TestLoggingJobSchedulerEnableDisableUseInfo(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := NewZapJobLogger(zap.New(core))
	scheduler := NewJobScheduler(WithLogger(logger))
	job := &Job{
		ID: "lifecycle-enable-disable", Group: "logging", Name: "enable-disable",
		ExecutorName: "unused", Status: StatusDisabled, CronExpression: "*/5 * * * * *",
		Timeout: time.Second,
	}
	scheduler.jobs[job.ID] = job

	require.NoError(t, scheduler.EnableJob(job.ID))
	require.Equal(t, StatusEnabled, job.Status)
	require.NoError(t, scheduler.DisableJob(job.ID))
	require.Equal(t, StatusDisabled, job.Status)

	var actions []observer.LoggedEntry
	for _, entry := range logs.All() {
		fields := entry.ContextMap()
		if fields["event"] == "scheduler.job.lifecycle" && (fields["action"] == "启用" || fields["action"] == "禁用") {
			actions = append(actions, entry)
		}
	}
	require.Len(t, actions, 2)
	require.Equal(t, zap.InfoLevel, actions[0].Level)
	require.Equal(t, zap.InfoLevel, actions[1].Level)
	require.Equal(t, "启用", actions[0].ContextMap()["action"])
	require.Equal(t, "禁用", actions[1].ContextMap()["action"])
}

func TestLoggingJobErrorSummaryDoesNotExposeRawError(t *testing.T) {
	var output bytes.Buffer
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.MessageKey = "message"
	core := zapcore.NewCore(zapcore.NewJSONEncoder(encoderConfig), zapcore.AddSync(&output), zap.DebugLevel)
	logger := NewZapJobLogger(zap.New(core))
	logger.LogJobExecution(&JobResult{
		JobID:       "failed",
		ExecutionID: "failed-1",
		Attempt:     1,
		Status:      "FAILED",
		Error:       errors.New("raw secret must not be logged"),
	})

	encoded := output.String()
	require.NotContains(t, encoded, "raw secret must not be logged")
	require.Contains(t, encoded, "scheduler.execution.completed")
	require.Contains(t, encoded, "failed-1")
}

func TestLoggingJobSharedLoggerSurvivesAdapterClose(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	root := zap.New(core)
	logger := NewZapJobLogger(root)
	require.NoError(t, logger.Close())
	root.Info("other component", zap.String("event", "other.event"))

	entries := logs.All()
	require.Len(t, entries, 1)
	require.Equal(t, "other.event", entries[0].ContextMap()["event"])
}

func TestLoggingJobExecutionContextCarriesRetryMetadata(t *testing.T) {
	ctx := WithExecutionContext(context.Background(), "job-1-123", 3, "executor")
	got, ok := ExecutionContextFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, ExecutionContext{ExecutionID: "job-1-123", Attempt: 3, ExecutorName: "executor"}, got)
	_, ok = ExecutionContextFromContext(context.Background())
	require.False(t, ok)
	require.False(t, strings.Contains(fmt.Sprint(got), "raw secret"))
}
