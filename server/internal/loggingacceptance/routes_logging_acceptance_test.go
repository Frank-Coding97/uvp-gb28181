//go:build logging_acceptance

package loggingacceptance

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

func TestAcceptanceExecutorCompletesWithStaticEvents(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })

	err := (&acceptanceExecutor{}).Execute(context.Background(), &schedulerhelper.Job{
		ID: "component-job", Parameters: map[string]interface{}{"duration_ms": 1},
	})
	require.NoError(t, err)
	require.Len(t, logs.All(), 2)
	require.Equal(t, "scheduler.acceptance", logs.All()[0].LoggerName)
	require.Equal(t, "scheduler.acceptance.started", logs.All()[0].ContextMap()["event"])
	require.Equal(t, "scheduler.acceptance.completed", logs.All()[1].ContextMap()["event"])
}

func TestAcceptanceExecutorReturnsCancellationWithoutCompletion(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (&acceptanceExecutor{}).Execute(ctx, &schedulerhelper.Job{
		ID: "component-job", Parameters: map[string]interface{}{"duration_ms": 10000},
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, logs.All(), 2)
	require.Equal(t, "scheduler.acceptance.started", logs.All()[0].ContextMap()["event"])
	require.Equal(t, "scheduler.acceptance.canceled", logs.All()[1].ContextMap()["event"])
	require.NotEqual(t, "scheduler.acceptance.completed", logs.All()[1].ContextMap()["event"])
}

func TestAcceptanceFixtureBoundsIdentifiersAndDurations(t *testing.T) {
	require.True(t, validAcceptanceJobID("acceptance-job-1"))
	require.False(t, validAcceptanceJobID("acceptance/job"))
	require.False(t, validAcceptanceJobID(""))
	require.Equal(t, 10, mustAcceptanceDuration(t, map[string]interface{}{"duration_ms": 10}))
	_, err := acceptanceDuration(map[string]interface{}{"duration_ms": 10001})
	require.Error(t, err)
	_, err = acceptanceDuration(map[string]interface{}{"duration_ms": 1.5})
	require.Error(t, err)
}

func mustAcceptanceDuration(t *testing.T, parameters map[string]interface{}) int {
	t.Helper()
	duration, err := acceptanceDuration(parameters)
	require.NoError(t, err)
	return duration
}
