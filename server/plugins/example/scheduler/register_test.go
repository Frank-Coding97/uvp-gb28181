package scheduler

import (
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
	"uvplatform.cn/uvp-gb28181/internal/standalone/runtimeexecutor"
)

type recordingScheduler struct {
	app.JobSchedulerInterf
	executors []schedulerhelper.Executor
}

func (s *recordingScheduler) RegisterExecutor(executor schedulerhelper.Executor) {
	s.executors = append(s.executors, executor)
}

func TestRegisterExampleExecutorsDoesNotRequireScheduler(t *testing.T) {
	runtimeexecutor.Rollback()
	oldScheduler := app.JobScheduler
	app.JobScheduler = nil
	t.Cleanup(func() { app.JobScheduler = oldScheduler })

	require.NotPanics(t, RegisterExampleExecutors)

	runtimeScheduler := &recordingScheduler{}
	runtimeexecutor.Apply(runtimeScheduler)
	runtimeexecutor.Commit(runtimeScheduler)
	require.Len(t, runtimeScheduler.executors, 1)
	require.Equal(t, "example-executor", runtimeScheduler.executors[0].Name())
}
