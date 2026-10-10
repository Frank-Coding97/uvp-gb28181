package playauth

import (
	"context"
	"encoding/json"
	"slices"
	"time"
)

const (
	maxSIPObservedBranches      = 8 // selected plus additional, never an eviction cache
	SIPBranchInventoryOverflow  = "branch_inventory_overflow"
	SIPBranchIdentityConflict   = "branch_identity_conflict"
	SIPBranchObserverIncomplete = "branch_observer_incomplete"
	sipInventoryFaultReserve    = 160
)

func sipObservedBranches(step *DeviceSIPInviteStep) []*DeviceSIPKnownBranch {
	var branches []*DeviceSIPKnownBranch
	if step.KnownBranch != nil {
		branches = append(branches, step.KnownBranch)
	}
	for index := range step.AdditionalBranches {
		branches = append(branches, &step.AdditionalBranches[index])
	}
	return branches
}

func sipFindBranch(step *DeviceSIPInviteStep, remoteTag string) *DeviceSIPKnownBranch {
	for _, b := range sipObservedBranches(step) {
		if b.Identity.RemoteTag == remoteTag {
			return b
		}
	}
	return nil
}

func sipBranchBusinessClosed(step DeviceSIPInviteStep) bool {
	if len(step.AdditionalBranches) != 0 || step.BranchInventoryFault != "" {
		return true
	}
	return step.KnownBranch != nil && len(step.KnownBranch.CleanupAttempts) != 0
}

func sipCleanupLocalWorkActive(out *DeviceSIPInviteSteps, runID string) bool {
	for si := range out.Steps {
		for _, b := range sipObservedBranches(&out.Steps[si]) {
			for _, info := range b.InfoSteps {
				if info.OwnerRunID == runID && info.LocalQuiescedAt == nil {
					return true
				}
			}
			for _, a := range b.CleanupAttempts {
				if a.OwnerRunID == runID && a.LocalQuiescedAt == nil {
					return true
				}
			}
		}
	}
	return false
}

func sipCleanupBranchesUsed(out *DeviceSIPInviteSteps, identity DeviceSIPCleanupAttemptIdentity) bool {
	ack, bye := identity.ACK.Request.Branch, identity.BYE.Request.Branch
	for si := range out.Steps {
		step := &out.Steps[si]
		if step.Identity.Branch == ack || step.Identity.Branch == bye {
			return true
		}
		for _, b := range sipObservedBranches(step) {
			for _, info := range b.InfoSteps {
				if branch := info.Identity.Request.Request.Branch; branch == ack || branch == bye {
					return true
				}
			}
			for _, a := range b.CleanupAttempts {
				if a.Identity.ACK.Request.Branch == ack || a.Identity.ACK.Request.Branch == bye ||
					a.Identity.BYE.Request.Branch == ack || a.Identity.BYE.Request.Branch == bye {
					return true
				}
			}
		}
	}
	return false
}

func validSIPInventoryFault(fault string) bool {
	return fault == SIPBranchInventoryOverflow || fault == SIPBranchIdentityConflict || fault == SIPBranchObserverIncomplete
}

func setSIPInventoryFault(step *DeviceSIPInviteStep, fault string, now time.Time) bool {
	if step.BranchInventoryFault != "" {
		return false // retain the first failure permanently, never clear on retry
	}
	step.BranchInventoryFault, step.BranchInventoryFaultObservedAt = fault, &now
	return true
}

func encodeSIPInviteSteps(out DeviceSIPInviteSteps) ([]byte, error) {
	wire := sipInviteStepsWire{Version: 1, Steps: make([]sipInviteStepWire, 0, len(out.Steps))}
	for _, step := range out.Steps {
		wire.Steps = append(wire.Steps, sipStepToWire(step))
	}
	return json.Marshal(wire)
}

func sipInventoryFits(out DeviceSIPInviteSteps, size int) bool {
	// v1 legacy rows are unchanged. Once a v2 inventory is introduced, normal
	// writes reserve space for its first fault so saturation cannot erase it.
	for _, step := range out.Steps {
		if len(step.AdditionalBranches) != 0 && step.BranchInventoryFault == "" {
			size += sipInventoryFaultReserve
		}
	}
	return size <= maxIntentSIPBytes
}

func readSIPBranchInventory(w sipInviteStepWire, step *DeviceSIPInviteStep, updatedAt time.Time) error {
	hasV2 := len(w.AdditionalBranches) != 0 || w.BranchInventoryFault != ""
	if (w.Version == 2) != hasV2 || len(w.AdditionalBranches) >= maxSIPObservedBranches ||
		(len(w.AdditionalBranches) != 0 && step.KnownBranch == nil) {
		return ErrDeviceIntentUnavailable
	}
	if w.BranchInventoryFault != "" {
		at := w.BranchInventoryFaultObservedAt
		if !validSIPInventoryFault(w.BranchInventoryFault) || at == nil || !validSIPStepTime(*at) || step.DispatchStartedAt == nil ||
			at.Before(*step.DispatchStartedAt) || at.After(updatedAt) {
			return ErrDeviceIntentUnavailable
		}
		step.BranchInventoryFault, step.BranchInventoryFaultObservedAt = w.BranchInventoryFault, at
	} else if w.BranchInventoryFaultObservedAt != nil {
		return ErrDeviceIntentUnavailable
	}
	for _, wb := range w.AdditionalBranches {
		b, err := readSIPKnownBranch(&wb, *step, updatedAt)
		if err != nil || len(b.InfoSteps) != 0 || b.ACKState != SIPStepPrepared ||
			b.ObservedAt.Before(step.KnownBranch.ObservedAt) || sipFindBranch(step, b.Identity.RemoteTag) != nil {
			return ErrDeviceIntentUnavailable
		}
		step.AdditionalBranches = append(step.AdditionalBranches, *b)
	}
	return nil
}

// ObserveSIPAdditionalBranch never selects/replaces the business dialog. The
// caller must first persist its actual selected response with ObserveKnown.
// Exact duplicates are idempotent; same-tag contradictions and saturation are
// sticky negative evidence, not an error that rolls their marker back.
func (s *DeviceOperationIntentStore) ObserveSIPAdditionalBranch(ctx context.Context, id DeviceOperationIntentIdentity, version int64, identity DeviceSIPKnownBranchIdentity) (DeviceSIPInviteSteps, error) {
	if !validSIPKnownBranchIdentity(identity) {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentInvalid
	}
	identity.RouteSet = slices.Clone(identity.RouteSet)
	return s.mutateSIPStep(ctx, id, version, true, func(out *DeviceSIPInviteSteps, now time.Time) (bool, error) {
		for index := range out.Steps {
			step := &out.Steps[index]
			if step.Identity.StepID != identity.InviteStepID {
				continue
			}
			if !sipBranchMatchesInvite(identity, *step) || step.KnownBranch == nil {
				return false, ErrDeviceIntentConflict
			}
			if old := sipFindBranch(step, identity.RemoteTag); old != nil {
				if equalSIPKnownBranch(old.Identity, identity) {
					return false, nil
				}
				return setSIPInventoryFault(step, SIPBranchIdentityConflict, now), nil
			}
			if len(step.AdditionalBranches) >= maxSIPObservedBranches-1 {
				return setSIPInventoryFault(step, SIPBranchInventoryOverflow, now), nil
			}
			previous := step.AdditionalBranches
			step.AdditionalBranches = append(previous, DeviceSIPKnownBranch{Identity: identity, ObservedAt: now, ACKState: SIPStepPrepared, ACKRowVersion: 1})
			body, err := encodeSIPInviteSteps(*out)
			if err != nil || !sipInventoryFits(*out, len(body)) {
				step.AdditionalBranches = previous
				return setSIPInventoryFault(step, SIPBranchInventoryOverflow, now), nil
			}
			return true, nil
		}
		return false, ErrDeviceIntentConflict
	})
}

// The original concrete observer reports lost/unattributable response evidence.
// This records no caller-supplied SIP payload and confers no network authority.
func (s *DeviceOperationIntentStore) ObserveSIPBranchInventoryFault(ctx context.Context, id DeviceOperationIntentIdentity, version int64, stepID, fault string) (DeviceSIPInviteSteps, error) {
	if !validIntentID(stepID) || !validSIPInventoryFault(fault) {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentInvalid
	}
	return s.mutateSIPStep(ctx, id, version, true, func(out *DeviceSIPInviteSteps, now time.Time) (bool, error) {
		for index := range out.Steps {
			step := &out.Steps[index]
			if step.Identity.StepID == stepID && step.State == SIPStepMayHaveDispatched {
				return setSIPInventoryFault(step, fault, now), nil
			}
		}
		return false, ErrDeviceIntentConflict
	})
}
