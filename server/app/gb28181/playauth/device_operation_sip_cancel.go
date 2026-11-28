package playauth

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const sipEmptyBodySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// DeviceSIPCancelIdentity uses the same fixed transport/header fields as its
// parent INVITE, with StepID referring to that INVITE. CANCEL has no body or
// Content-Type. This is evidence from an actual prepared request, never an
// executable payload or permission obtained merely by reading storage.
type DeviceSIPCancelIdentity DeviceSIPInviteIdentity

type DeviceSIPCancel struct {
	Identity          DeviceSIPCancelIdentity `json:"-"`
	State             string                  `json:"-"`
	RowVersion        int64                   `json:"-"`
	PreparedAt        time.Time               `json:"-"`
	DispatchStartedAt *time.Time              `json:"-"`
}

type sipCancelWire struct {
	Version           int                   `json:"version"`
	Action            string                `json:"action"`
	Identity          sipInviteIdentityWire `json:"identity"`
	State             string                `json:"state"`
	RowVersion        int64                 `json:"rowVersion"`
	PreparedAt        time.Time             `json:"preparedAt"`
	DispatchStartedAt *time.Time            `json:"dispatchStartedAt"`
}

func sipCancelToWire(c *DeviceSIPCancel) *sipCancelWire {
	if c == nil {
		return nil
	}
	return &sipCancelWire{1, "cancel", sipInviteIdentityWire(c.Identity), c.State, c.RowVersion, c.PreparedAt, c.DispatchStartedAt}
}

func sipCancelMatchesInvite(cancel DeviceSIPCancelIdentity, step DeviceSIPInviteStep) bool {
	if step.State != SIPStepMayHaveDispatched {
		return false
	}
	expected := DeviceSIPCancelIdentity(step.Identity)
	expected.ContentType, expected.BodyLength, expected.BodySHA256 = "", 0, sipEmptyBodySHA256
	// The parent was fully validated before this check. Exact equality prevents
	// changing a URI, source, route, branch or any other target-selection field.
	return cancel == expected
}

func readSIPCancel(w *sipCancelWire, step DeviceSIPInviteStep, updatedAt time.Time) (*DeviceSIPCancel, error) {
	i := DeviceSIPCancelIdentity(w.Identity)
	if w.Version != 1 || w.Action != "cancel" || !sipCancelMatchesInvite(i, step) || step.DispatchStartedAt == nil ||
		!validSIPStepTime(w.PreparedAt) || w.PreparedAt.Before(*step.DispatchStartedAt) || w.PreparedAt.After(updatedAt) {
		return nil, ErrDeviceIntentUnavailable
	}
	switch w.State {
	case SIPStepPrepared:
		if w.RowVersion != 1 || w.DispatchStartedAt != nil {
			return nil, ErrDeviceIntentUnavailable
		}
	case SIPStepMayHaveDispatched:
		if w.RowVersion != 2 || w.DispatchStartedAt == nil || !validSIPStepTime(*w.DispatchStartedAt) || w.DispatchStartedAt.Before(w.PreparedAt) || w.DispatchStartedAt.After(updatedAt) {
			return nil, ErrDeviceIntentUnavailable
		}
	default:
		return nil, ErrDeviceIntentUnavailable
	}
	if b := step.KnownBranch; b != nil && (w.PreparedAt.After(b.ObservedAt) || (w.DispatchStartedAt != nil && w.DispatchStartedAt.After(b.ObservedAt))) {
		return nil, ErrDeviceIntentUnavailable
	}
	return &DeviceSIPCancel{i, w.State, w.RowVersion, w.PreparedAt, w.DispatchStartedAt}, nil
}

func (s *DeviceOperationIntentStore) PrepareSIPCancel(ctx context.Context, id DeviceOperationIntentIdentity, version int64, identity DeviceSIPCancelIdentity) (DeviceSIPInviteSteps, error) {
	processID, err := sipCleanupProcessID()
	if err != nil {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
	}
	return s.mutateSIPCancelStep(ctx, id, version, func(out *DeviceSIPInviteSteps, now time.Time) (bool, error) {
		for index := range out.Steps {
			step := &out.Steps[index]
			if step.Identity.StepID != identity.StepID {
				continue
			}
			if step.OwnerProcessID != processID || !sipCancelMatchesInvite(identity, *step) {
				return false, ErrDeviceIntentConflict
			}
			if step.Cancel != nil {
				if step.Cancel.Identity == identity {
					return false, nil
				}
				return false, ErrDeviceIntentConflict
			}
			if step.KnownBranch != nil {
				return false, ErrDeviceIntentConflict
			}
			step.Cancel = &DeviceSIPCancel{Identity: identity, State: SIPStepPrepared, RowVersion: 1, PreparedAt: now}
			return true, nil
		}
		return false, ErrDeviceIntentConflict
	})
}

// DispatchSIPCancel grants only this confirmed CAS winner one explicit CANCEL.
// No network is performed here; commit-unknown, repeated calls and recovery
// Load do not grant a second attempt. A later 2xx is still a live branch fact,
// never evidence that this cancellation succeeded.
func (s *DeviceOperationIntentStore) DispatchSIPCancel(ctx context.Context, id DeviceOperationIntentIdentity, version int64, identity DeviceSIPCancelIdentity) (DeviceSIPInviteSteps, error) {
	processID, err := sipCleanupProcessID()
	if err != nil {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
	}
	return s.mutateSIPStepChecked(ctx, id, version, s.effectDeviceCheck(authorizeSIPCancelCleanupDevice), func(out *DeviceSIPInviteSteps, now time.Time) (bool, error) {
		for index := range out.Steps {
			step := &out.Steps[index]
			if step.Identity.StepID != identity.StepID {
				continue
			}
			c := step.Cancel
			if step.OwnerProcessID != processID || c == nil || !sipCancelMatchesInvite(identity, *step) || c.Identity != identity || c.State != SIPStepPrepared || step.KnownBranch != nil {
				return false, ErrDeviceIntentConflict
			}
			c.State, c.RowVersion, c.DispatchStartedAt = SIPStepMayHaveDispatched, 2, &now
			return true, nil
		}
		return false, ErrDeviceIntentConflict
	})
}

func (s *DeviceOperationIntentStore) mutateSIPCancelStep(ctx context.Context, id DeviceOperationIntentIdentity, version int64, mutate func(*DeviceSIPInviteSteps, time.Time) (bool, error)) (DeviceSIPInviteSteps, error) {
	return s.mutateSIPStepChecked(ctx, id, version, authorizeSIPCancelCleanupDevice, mutate)
}

// Old-epoch cancellation is exact-target compensation, not business authority.
// Hold the original device lock through the intent/step CAS, then release it
// before any network work. This path never advances the cleanup watermark.
func authorizeSIPCancelCleanupDevice(tx *gorm.DB, ctx context.Context, id DeviceOperationIntentIdentity) error {
	rows, err := queryDeviceCleanupRows(tx, ctx, id.DeviceCode, true)
	if err != nil || len(rows) != 1 || rows[0].ID != id.DevicePK {
		return ErrDeviceIntentUnavailable
	}
	state, err := validateDeviceCleanupRow(rows[0])
	if err != nil {
		return ErrDeviceIntentUnavailable
	}
	if state.AccessEpoch == id.DeviceEpoch {
		return authorizeIntentDevice(tx, ctx, id)
	}
	if state.AccessEpoch < id.DeviceEpoch || state.CleanupCompletedEpoch >= state.AccessEpoch || state.CleanupCompletedEpoch > id.DeviceEpoch {
		return ErrDeviceIntentRevoked
	}
	// Do not resolve a new channel address after transfer: the immutable intent
	// and CANCEL identity must match the original dispatched INVITE below.
	return nil
}
