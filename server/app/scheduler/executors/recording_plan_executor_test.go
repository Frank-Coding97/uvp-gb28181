package executors

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecordingPlanExecutorsDelegateToConfiguredRuntime(t *testing.T) {
	runtime := &fakeRecordingPlanRuntime{}
	SetRecordingPlanRuntime(runtime)
	t.Cleanup(func() { SetRecordingPlanRuntime(nil) })
	require.NoError(t, (&RecordingPlanDispatchExecutor{}).Execute(context.Background(), nil))
	require.NoError(t, (&RecordingPlanHealExecutor{}).Execute(context.Background(), nil))
	require.Equal(t, 1, runtime.dispatches)
	require.Equal(t, 1, runtime.heals)
}

func TestRecordingPlanExecutorIsNoopWhenRuntimeDisabled(t *testing.T) {
	SetRecordingPlanRuntime(nil)
	require.NoError(t, (&RecordingPlanDispatchExecutor{}).Execute(context.Background(), nil))
}

type fakeRecordingPlanRuntime struct{ dispatches, heals int }

func (f *fakeRecordingPlanRuntime) Dispatch(context.Context) error { f.dispatches++; return nil }
func (f *fakeRecordingPlanRuntime) Heal(context.Context) error     { f.heals++; return nil }
