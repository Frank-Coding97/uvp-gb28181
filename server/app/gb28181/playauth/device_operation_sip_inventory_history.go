package playauth

import "time"

// Validate relationships outside a single dialog's local CSeq/state chain.
// Selected INFO history survives inventory growth, but no later preparation
// or dispatch may cross an additional-branch/fault/cleanup observation fence.
func validSIPInventoryHistory(out DeviceSIPInviteSteps) bool {
	type branchUse struct{ step, remoteTag, method string }
	used := map[string]branchUse{}
	claim := func(via string, use branchUse) bool {
		if old, exists := used[via]; exists {
			// Historical v1 allowed retransmission ACK branch reuse within the
			// SAME dialog. Retain those bytes on upgrade; new prepares forbid it.
			return use.method == "ACK" && old == use
		}
		used[via] = use
		return true
	}
	for _, step := range out.Steps {
		if !claim(step.Identity.Branch, branchUse{step.Identity.StepID, "", "INVITE"}) {
			return false
		}
	}
	for si := range out.Steps {
		step := &out.Steps[si]
		var fences []time.Time
		if at := step.BranchInventoryFaultObservedAt; at != nil {
			fences = append(fences, *at)
		}
		for _, b := range step.AdditionalBranches {
			fences = append(fences, b.ObservedAt)
		}
		var cleanups []DeviceSIPCleanupAttempt
		for _, b := range sipObservedBranches(step) {
			use := branchUse{step.Identity.StepID, b.Identity.RemoteTag, "INFO"}
			for _, info := range b.InfoSteps {
				if !claim(info.Identity.Request.Request.Branch, use) {
					return false
				}
			}
			for _, a := range b.CleanupAttempts {
				use.method = "ACK"
				if !claim(a.Identity.ACK.Request.Branch, use) {
					return false
				}
				use.method = "BYE"
				if !claim(a.Identity.BYE.Request.Branch, use) {
					return false
				}
				fences = append(fences, a.PreparedAt)
				cleanups = append(cleanups, a)
			}
		}
		if selected := step.KnownBranch; selected != nil {
			for _, fence := range fences {
				if selected.ACKDispatchStartedAt != nil && selected.ACKDispatchStartedAt.After(fence) {
					return false
				}
				for _, info := range selected.InfoSteps {
					if info.PreparedAt.After(fence) || (info.DispatchStartedAt != nil && info.DispatchStartedAt.After(fence)) {
						return false
					}
				}
			}
			for _, a := range cleanups {
				for _, info := range selected.InfoSteps {
					if a.PreparedAt.Before(info.PreparedAt) || (a.OwnerRunID == info.OwnerRunID &&
						(info.LocalQuiescedAt == nil || info.LocalQuiescedAt.After(a.PreparedAt))) {
						return false
					}
				}
			}
		}
		for i, a := range cleanups {
			for _, b := range cleanups[i+1:] {
				if a.OwnerRunID != b.OwnerRunID {
					continue
				}
				aBeforeB := a.LocalQuiescedAt != nil && !a.LocalQuiescedAt.After(b.PreparedAt)
				bBeforeA := b.LocalQuiescedAt != nil && !b.LocalQuiescedAt.After(a.PreparedAt)
				if !aBeforeB && !bBeforeA {
					return false
				}
			}
		}
	}
	return true
}
