package playauth

import "time"

func validRTPOpenResult(result DeviceRTPOpenResult) bool {
	switch result.Result {
	case "created", "existing":
		return result.Port > 0 && result.Port <= 65534
	case "close_pending", "resource_retired", "resource_conflict", "resource_expired", "capacity_exhausted", "runtime_mismatch":
		return result.Port == 0
	default:
		return false
	}
}

func validRTPCloseResult(result string, ingress bool) bool {
	switch result {
	case "shutdown_scheduled", "close_pending", "not_active_fenced", "resource_mismatch", "runtime_mismatch", "capacity_exhausted":
		return true
	case "rtp_ingress_drained":
		return ingress
	default:
		return false
	}
}

func validRTPExecution(step DeviceRTPResourceStep, updated time.Time) bool {
	if !validRTPOriginalCloseCalls(step, updated) {
		return false
	}
	// Missing process identity is historical unknown, never proof of exit.
	if step.OwnerProcessID != "" && (!validIntentID(step.OwnerProcessID) || step.OwnerRunID == "") {
		return false
	}
	if step.OwnerRunID == "" {
		return step.OpenResult == nil && step.OpenObservedAt == nil && step.ResourceCloseResult == "" && step.ResourceCloseObservedAt == nil &&
			step.IngressCloseResult == "" && step.IngressCloseObservedAt == nil && step.LocalQuiescedAt == nil
	}
	if !validIntentID(step.OwnerRunID) || step.State != RTPStepMayHaveDispatched || step.DispatchStartedAt == nil {
		return false
	}
	if (step.OpenResult == nil) != (step.OpenObservedAt == nil) || (step.ResourceCloseResult == "") != (step.ResourceCloseObservedAt == nil) || (step.IngressCloseResult == "") != (step.IngressCloseObservedAt == nil) {
		return false
	}
	if step.OpenResult != nil && !validRTPOpenResult(*step.OpenResult) {
		return false
	}
	if step.ResourceCloseResult != "" && !validRTPCloseResult(step.ResourceCloseResult, false) {
		return false
	}
	if step.IngressCloseResult != "" && !validRTPCloseResult(step.IngressCloseResult, true) {
		return false
	}
	for _, observed := range []*time.Time{step.OpenObservedAt, step.ResourceCloseObservedAt, step.IngressCloseObservedAt, step.LocalQuiescedAt} {
		if observed == nil {
			continue
		}
		if observed.Before(*step.DispatchStartedAt) || observed.After(updated) || (step.LocalQuiescedAt != nil && observed.After(*step.LocalQuiescedAt)) {
			return false
		}
	}
	return true
}
