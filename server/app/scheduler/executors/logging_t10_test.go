package executors

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

type loggingT10Runtime struct {
	dispatch context.Context
	heal     context.Context
}

func (r *loggingT10Runtime) Dispatch(ctx context.Context) error {
	r.dispatch = ctx
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
	ctx := schedulerhelper.WithExecutionContext(context.Background(), "job-1-2", 2, RecordingPlanHealExecutorName)

	require.NoError(t, (&RecordingPlanDispatchExecutor{}).Execute(ctx, &schedulerhelper.Job{}))
	require.NoError(t, (&RecordingPlanHealExecutor{}).Execute(ctx, &schedulerhelper.Job{}))

	dispatch, ok := schedulerhelper.ExecutionContextFromContext(runtime.dispatch)
	require.True(t, ok)
	require.Equal(t, "job-1-2", dispatch.ExecutionID)
	require.Equal(t, 2, dispatch.Attempt)
	heal, ok := schedulerhelper.ExecutionContextFromContext(runtime.heal)
	require.True(t, ok)
	require.Equal(t, RecordingPlanHealExecutorName, heal.ExecutorName)
}
