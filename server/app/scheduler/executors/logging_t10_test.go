package executors

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/utils/logging"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

type loggingT10Runtime struct {
	dispatch context.Context
	heal     context.Context
}

func (r *loggingT10Runtime) Dispatch(ctx context.Context) error {
	r.dispatch = ctx
	logging.FromContext(ctx, nil).Named("db").Info("recording executor downstream", zap.String("event", "scheduler.downstream"))
	return nil
}

func (r *loggingT10Runtime) Heal(ctx context.Context) error {
	r.heal = ctx
	return nil
}

func TestLoggingJobRecordingPlanExecutorsPreserveExecutionContext(t *testing.T) {
	runtime := &loggingT10Runtime{}
	SetRecordingPlanRuntime(runtime)
	t.Cleanup(func() { SetRecordingPlanRuntime(nil) })
	ctx := schedulerhelper.WithExecutionContext(context.Background(), "job-1-2", 2, RecordingPlanHealExecutorName, "job-1")

	require.NoError(t, (&RecordingPlanDispatchExecutor{}).Execute(ctx, &schedulerhelper.Job{}))
	require.NoError(t, (&RecordingPlanHealExecutor{}).Execute(ctx, &schedulerhelper.Job{}))

	dispatch, ok := schedulerhelper.ExecutionContextFromContext(runtime.dispatch)
	require.True(t, ok)
	require.Equal(t, "job-1-2", dispatch.ExecutionID)
	require.Equal(t, "job-1", dispatch.JobID)
	require.Equal(t, 2, dispatch.Attempt)
	heal, ok := schedulerhelper.ExecutionContextFromContext(runtime.heal)
	require.True(t, ok)
	require.Equal(t, RecordingPlanHealExecutorName, heal.ExecutorName)
}

func TestLoggingJobExecutionScopeReachesRecordingExecutor(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	runtime := &loggingT10Runtime{}
	SetRecordingPlanRuntime(runtime)
	t.Cleanup(func() { SetRecordingPlanRuntime(nil) })
	scheduler := schedulerhelper.NewJobScheduler(
		schedulerhelper.WithLogger(schedulerhelper.NewZapJobLogger(zap.New(core))),
		schedulerhelper.WithJobResultsBufferSize(4),
	)
	scheduler.RegisterExecutor(&RecordingPlanDispatchExecutor{})
	job := &schedulerhelper.Job{
		ID: "job-context", Group: "logging", Name: "context",
		ExecutorName: RecordingPlanDispatchExecutorName, ExecutionPolicy: schedulerhelper.PolicyRepeat,
		Status: schedulerhelper.StatusDisabled, CronExpression: "*/5 * * * * *", Timeout: time.Second,
	}
	_, err := scheduler.AddOrUpdateJob(job)
	require.NoError(t, err)
	require.NoError(t, scheduler.ExecuteNow(job.ID))
	result := <-scheduler.GetResults()
	require.Equal(t, "SUCCESS", result.Status)

	var downstream observer.LoggedEntry
	for _, entry := range logs.All() {
		if entry.ContextMap()["event"] == "scheduler.downstream" {
			downstream = entry
		}
	}
	require.Equal(t, "scheduler.downstream", downstream.ContextMap()["event"])
	require.Equal(t, "db", downstream.LoggerName)
	fields := downstream.ContextMap()
	require.Equal(t, result.ExecutionID, fields["execution_id"])
	require.Equal(t, result.JobID, fields["job_id"])
	require.EqualValues(t, result.Attempt, fields["attempt"])
	require.Equal(t, job.ExecutorName, fields["executor"])
}
