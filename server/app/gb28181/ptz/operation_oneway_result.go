package ptz

import (
	"gorm.io/gorm"
	"time"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

// One-way controls never acquire an application-response deadline or retry
// slot. A transport-unknown outcome remains unknown, not a safe resend.
func persistOneWayAttemptResult(tx *gorm.DB, operation gbmodels.GbPTZOperation, attempt gbmodels.GbPTZOperationAttempt, result uac.TrackedMessageResult, sendErr error, now time.Time) error {
	status, attemptStatus := gbmodels.PTZOperationRejected, gbmodels.PTZOperationAttemptFailed
	code := schedulerErrorHomePositionUnavailable
	if result.Attempted && (result.StatusCode == 0 || !attempt.LeaseUntil.After(now)) {
		status, attemptStatus, code = gbmodels.PTZOperationUnknown, gbmodels.PTZOperationAttemptUnknown, schedulerErrorTransportUnknown
	} else if sendErr == nil && result.StatusCode >= 200 && result.StatusCode < 300 {
		status, attemptStatus, code = gbmodels.PTZOperationSent, gbmodels.PTZOperationAttemptSent, ""
	}
	updates := schedulerAttemptResultUpdates(attemptStatus, result, sendErr, now)
	updates["error_code"] = code
	if status == gbmodels.PTZOperationSent {
		updates["sent_at"] = now
	}
	changed, err := updateAttemptFromStatus(tx, attempt.ID, attempt.Status, updates)
	if err != nil || !changed {
		return err
	}
	if err := persistScheduledOutboundMetadata(tx, operation.ID, result, now, status == gbmodels.PTZOperationSent); err != nil {
		return err
	}
	values := map[string]any{"status": status, "error_code": code, "error_message": schedulerSendError(sendErr), "next_attempt_at": nil, "deadline_at": nil}
	if status != gbmodels.PTZOperationSent {
		values["completed_at"] = now
	}
	return tx.Model(&gbmodels.GbPTZOperation{}).Where("id = ? AND response_required = ? AND status = ?", operation.ID, false, gbmodels.PTZOperationQueued).Updates(values).Error
}
