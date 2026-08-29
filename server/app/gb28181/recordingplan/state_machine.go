package recordingplan

import (
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const (
	ActionNone  = "none"
	ActionStart = "start"
	ActionStop  = "stop"
)

const (
	FailureStreamStart = "stream_start"
	FailureMediaWait   = "media_wait"
	FailureRecordStart = "record_start"
)

const (
	ReasonModeOff           = "MODE_OFF"
	ReasonPlanDisabled      = "PLAN_DISABLED"
	ReasonOutsideSchedule   = "OUTSIDE_SCHEDULE"
	ReasonDeviceOffline     = "DEVICE_OFFLINE"
	ReasonStreamStartFailed = "STREAM_START_FAILED"
	ReasonMediaTimeout      = "MEDIA_TIMEOUT"
	ReasonRecordStartFailed = "RECORD_START_FAILED"
	ReasonPlanChanged       = "PLAN_CHANGED"
)

type ReconcileInput struct {
	Mode            string
	PlanEnabled     bool
	ScheduleMatched bool
	DeviceOnline    bool
	ActualState     string
	FailureStage    string
	FailureMessage  string
	Attempt         int
	PlanChanged     bool
	Now             time.Time
}

type ReconcileDecision struct {
	DesiredState  string
	ActualState   string
	Action        string
	ReasonCode    string
	ReasonMessage string
	Attempt       int
	NextRetryAt   *time.Time
	OpenGap       bool
	CloseGap      bool
}

func DecideReconcile(input ReconcileInput) ReconcileDecision {
	desired, reason := desiredRecording(input)
	decision := ReconcileDecision{DesiredState: models.RecordingDesiredIdle, ActualState: input.ActualState, Action: ActionNone, ReasonCode: reason}
	if input.PlanChanged && needsStop(input.ActualState) {
		decision.ActualState = models.RecordingStateStopping
		decision.Action = ActionStop
		decision.ReasonCode = ReasonPlanChanged
		return decision
	}
	if !desired {
		decision.CloseGap = true
		if needsStop(input.ActualState) {
			decision.ActualState = models.RecordingStateStopping
			decision.Action = ActionStop
		} else if reason == ReasonOutsideSchedule {
			decision.ActualState = models.RecordingStateOutsideSchedule
		} else {
			decision.ActualState = models.RecordingStateIdle
		}
		return decision
	}

	decision.DesiredState = models.RecordingDesiredRecording
	if !input.DeviceOnline {
		decision.ActualState = models.RecordingStateWaitingDevice
		decision.ReasonCode = ReasonDeviceOffline
		decision.ReasonMessage = "设备离线，等待设备重新上线"
		decision.Attempt = input.Attempt + 1
		retry := decisionTime(input.Now).Add(time.Minute)
		decision.NextRetryAt = &retry
		decision.OpenGap = true
		return decision
	}
	if input.FailureStage != "" {
		decision.ActualState = models.RecordingStateRecovering
		decision.Action = ActionNone
		decision.ReasonCode = failureReason(input.FailureStage)
		decision.ReasonMessage = input.FailureMessage
		decision.Attempt = input.Attempt + 1
		retry := decisionTime(input.Now).Add(retryDelay(input.Attempt))
		decision.NextRetryAt = &retry
		decision.OpenGap = true
		return decision
	}
	if input.ActualState == models.RecordingStateRecording {
		decision.ActualState = models.RecordingStateRecording
		decision.Attempt = 0
		decision.CloseGap = true
		return decision
	}
	decision.ActualState = models.RecordingStateStartingStream
	decision.Action = ActionStart
	decision.Attempt = input.Attempt
	return decision
}

func desiredRecording(input ReconcileInput) (bool, string) {
	switch input.Mode {
	case models.RecordingModeContinuous:
		return true, ""
	case models.RecordingModeScheduled:
		if !input.PlanEnabled {
			return false, ReasonPlanDisabled
		}
		if !input.ScheduleMatched {
			return false, ReasonOutsideSchedule
		}
		return true, ""
	default:
		return false, ReasonModeOff
	}
}

func needsStop(actual string) bool {
	switch actual {
	case models.RecordingStateStartingStream, models.RecordingStateWaitingMedia,
		models.RecordingStateStartingRecord, models.RecordingStateRecording,
		models.RecordingStateRecovering, models.RecordingStateFailed:
		return true
	default:
		return false
	}
}

func failureReason(stage string) string {
	switch stage {
	case FailureStreamStart:
		return ReasonStreamStartFailed
	case FailureRecordStart:
		return ReasonRecordStartFailed
	default:
		return ReasonMediaTimeout
	}
}

func retryDelay(attempt int) time.Duration {
	delays := [...]time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second}
	if attempt < 0 {
		attempt = 0
	}
	if attempt >= len(delays) {
		return time.Minute
	}
	return delays[attempt]
}

func decisionTime(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now()
	}
	return now
}
