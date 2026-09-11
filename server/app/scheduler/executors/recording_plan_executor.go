package executors

import (
	"context"
	"sync"

	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

const (
	RecordingPlanDispatchExecutorName = "recording-plan-dispatch-executor"
	RecordingPlanHealExecutorName     = "recording-plan-heal-executor"
)

type RecordingPlanRuntime interface {
	Dispatch(context.Context) error
	Heal(context.Context) error
}

var recordingPlanRuntime struct {
	sync.RWMutex
	value RecordingPlanRuntime
}

func SetRecordingPlanRuntime(runtime RecordingPlanRuntime) {
	recordingPlanRuntime.Lock()
	recordingPlanRuntime.value = runtime
	recordingPlanRuntime.Unlock()
}

func currentRecordingPlanRuntime() RecordingPlanRuntime {
	recordingPlanRuntime.RLock()
	runtime := recordingPlanRuntime.value
	recordingPlanRuntime.RUnlock()
	return runtime
}

type RecordingPlanDispatchExecutor struct{}

func (e *RecordingPlanDispatchExecutor) Name() string { return RecordingPlanDispatchExecutorName }
func (e *RecordingPlanDispatchExecutor) Execute(ctx context.Context, _ *schedulerhelper.Job) error {
	if runtime := currentRecordingPlanRuntime(); runtime != nil {
		return runtime.Dispatch(ctx)
	}
	return nil
}

type RecordingPlanHealExecutor struct{}

func (e *RecordingPlanHealExecutor) Name() string { return RecordingPlanHealExecutorName }
func (e *RecordingPlanHealExecutor) Execute(ctx context.Context, _ *schedulerhelper.Job) error {
	if runtime := currentRecordingPlanRuntime(); runtime != nil {
		return runtime.Heal(ctx)
	}
	return nil
}
