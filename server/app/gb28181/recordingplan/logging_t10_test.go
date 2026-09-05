package recordingplan

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordingplan/schedule"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

func TestLoggingRecordingActionStartStopHaveExecutionCorrelation(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	oldLogger := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = oldLogger })
	ctx := schedulerhelper.WithExecutionContext(context.Background(), "recording-1", 1, "recording-plan-dispatch-executor")

	logRecordingAction(ctx, ActionStart, "success", nil, zap.String("channel_id", "C1"))
	logRecordingAction(ctx, ActionStop, "success", nil, zap.String("channel_id", "C1"))

	entries := logs.All()
	require.Len(t, entries, 2)
	for i, action := range []string{ActionStart, ActionStop} {
		require.Equal(t, zap.InfoLevel, entries[i].Level)
		fields := entries[i].ContextMap()
		require.Equal(t, "recording_plan.action", fields["event"])
		require.Equal(t, action, fields["action"])
		require.Equal(t, "success", fields["result"])
		require.Equal(t, "recording-1", fields["execution_id"])
		require.Equal(t, "dispatch", fields["trigger"])
	}
}

func TestLoggingRecordingActionHealKeepsExecutionCorrelation(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	oldLogger := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = oldLogger })
	ctx := schedulerhelper.WithExecutionContext(context.Background(), "heal-1", 2, "recording-plan-heal-executor")

	logRecordingAction(ctx, ActionStart, "success", nil)

	require.Len(t, logs.All(), 1)
	fields := logs.All()[0].ContextMap()
	require.Equal(t, "heal", fields["trigger"])
	require.Equal(t, "heal-1", fields["execution_id"])
	require.EqualValues(t, 2, fields["attempt"])
}

func TestLoggingRecordingActionFailureDoesNotExposeRawError(t *testing.T) {
	var output bytes.Buffer
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.MessageKey = "message"
	core := zapcore.NewCore(zapcore.NewJSONEncoder(encoderConfig), zapcore.AddSync(&output), zap.ErrorLevel)
	oldLogger := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = oldLogger })
	ctx := schedulerhelper.WithExecutionContext(context.Background(), "failed-1", 1, "recording-plan-dispatch-executor")

	logRecordingAction(ctx, ActionStop, "failed", errors.New("raw recording secret"))

	encoded := output.String()
	require.NotContains(t, encoded, "raw recording secret")
	require.Contains(t, encoded, "recording_plan.action")
	require.Contains(t, encoded, "failed-1")
}

func TestLoggingRecordingActionEngineLogsOnlyCompletedActions(t *testing.T) {
	db := newRepositoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}))
	now := time.Date(2026, 9, 5, 9, 0, 0, 0, schedule.BeijingLocation())
	channel := models.GbChannel{DeviceID: "D-t10", ChannelID: "C-t10", OwnerDeptID: 1, Status: models.ChannelStatusOnline, RecordingMode: models.RecordingModeContinuous}
	require.NoError(t, db.Create(&channel).Error)
	state := models.GbRecordingPlanChannelState{ChannelID: channel.ID, DesiredState: models.RecordingDesiredRecording, ActualState: models.RecordingStateIdle, ReconcileAt: now}
	require.NoError(t, db.Create(&state).Error)

	core, logs := observer.New(zap.DebugLevel)
	oldLogger := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = oldLogger })
	operator := &fakeChannelOperator{}
	engine := NewEngine(db, operator, EngineOptions{Now: func() time.Time { return now }})
	ctx := schedulerhelper.WithExecutionContext(context.Background(), "recording-engine-1", 1, "recording-plan-dispatch-executor")

	require.NoError(t, engine.reconcile(ctx, &state, now))
	require.Len(t, logs.All(), 1)
	require.Equal(t, "start", logs.All()[0].ContextMap()["action"])
	require.Equal(t, zap.InfoLevel, logs.All()[0].Level)

	var stored models.GbRecordingPlanChannelState
	require.NoError(t, db.First(&stored, "channel_id = ?", channel.ID).Error)
	stored.DesiredState = models.RecordingDesiredRecording
	stored.ActualState = models.RecordingStateRecording
	stored.ReconcileAt = now
	beforeNoop := len(logs.All())
	require.NoError(t, engine.reconcile(ctx, &stored, now))
	require.Len(t, logs.All(), beforeNoop, "a state reconciliation without an action must not emit INFO")

	var executions []models.GbRecordingPlanExecution
	require.NoError(t, db.Find(&executions).Error)
	require.Len(t, executions, 1, "action persistence remains the business record")
}
