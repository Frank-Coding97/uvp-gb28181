package recordingplan

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestStateMachineStopsOrIdlesWhenRecordingIsNotDesired(t *testing.T) {
	tests := []struct {
		name   string
		input  ReconcileInput
		state  string
		action string
		reason string
	}{
		{"off idle", ReconcileInput{Mode: models.RecordingModeOff, ActualState: models.RecordingStateIdle}, models.RecordingStateIdle, ActionNone, ReasonModeOff},
		{"disabled recording", ReconcileInput{Mode: models.RecordingModeScheduled, PlanEnabled: false, ActualState: models.RecordingStateRecording}, models.RecordingStateStopping, ActionStop, ReasonPlanDisabled},
		{"outside schedule", ReconcileInput{Mode: models.RecordingModeScheduled, PlanEnabled: true, ScheduleMatched: false, ActualState: models.RecordingStateIdle}, models.RecordingStateOutsideSchedule, ActionNone, ReasonOutsideSchedule},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideReconcile(tt.input)
			require.Equal(t, tt.state, got.ActualState)
			require.Equal(t, tt.action, got.Action)
			require.Equal(t, tt.reason, got.ReasonCode)
		})
	}
}

func TestStateMachineWaitsForOfflineDeviceAndOpensGap(t *testing.T) {
	now := time.Now()
	got := DecideReconcile(ReconcileInput{Mode: models.RecordingModeContinuous, DeviceOnline: false, Now: now})
	require.Equal(t, models.RecordingStateWaitingDevice, got.ActualState)
	require.Equal(t, ReasonDeviceOffline, got.ReasonCode)
	require.True(t, got.OpenGap)
	require.Equal(t, now.Add(time.Minute), *got.NextRetryAt)
}

func TestStateMachineClassifiesStartFailuresAndUsesBoundedBackoff(t *testing.T) {
	now := time.Now()
	stages := map[string]string{FailureStreamStart: ReasonStreamStartFailed, FailureMediaWait: ReasonMediaTimeout, FailureRecordStart: ReasonRecordStartFailed}
	for stage, reason := range stages {
		got := DecideReconcile(ReconcileInput{Mode: models.RecordingModeContinuous, DeviceOnline: true, FailureStage: stage, FailureMessage: "boom", Attempt: 2, Now: now})
		require.Equal(t, models.RecordingStateRecovering, got.ActualState)
		require.Equal(t, reason, got.ReasonCode)
		require.Equal(t, "boom", got.ReasonMessage)
		require.True(t, got.OpenGap)
		require.Equal(t, now.Add(10*time.Second), *got.NextRetryAt)
	}
	for attempt, delay := range []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second, time.Minute, time.Minute} {
		got := DecideReconcile(ReconcileInput{Mode: models.RecordingModeContinuous, DeviceOnline: true, FailureStage: FailureMediaWait, Attempt: attempt, Now: now})
		require.Equal(t, now.Add(delay), *got.NextRetryAt)
	}
}

func TestStateMachineCancelsStaleGenerationWhenPlanChanges(t *testing.T) {
	got := DecideReconcile(ReconcileInput{Mode: models.RecordingModeScheduled, PlanEnabled: true, ScheduleMatched: false, DeviceOnline: true, ActualState: models.RecordingStateStartingStream, PlanChanged: true})
	require.Equal(t, ActionStop, got.Action)
	require.Equal(t, models.RecordingStateStopping, got.ActualState)
	require.Equal(t, ReasonPlanChanged, got.ReasonCode)
}

func TestStateMachineStartsAndClosesGapAfterRecovery(t *testing.T) {
	start := DecideReconcile(ReconcileInput{Mode: models.RecordingModeContinuous, DeviceOnline: true, ActualState: models.RecordingStateIdle})
	require.Equal(t, ActionStart, start.Action)
	require.Equal(t, models.RecordingStateStartingStream, start.ActualState)

	recovered := DecideReconcile(ReconcileInput{Mode: models.RecordingModeContinuous, DeviceOnline: true, ActualState: models.RecordingStateRecording, Attempt: 5})
	require.Equal(t, ActionNone, recovered.Action)
	require.Equal(t, models.RecordingStateRecording, recovered.ActualState)
	require.Zero(t, recovered.Attempt)
	require.True(t, recovered.CloseGap)
}
