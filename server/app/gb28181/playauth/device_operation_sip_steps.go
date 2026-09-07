package playauth

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"time"

	"gorm.io/gorm"
)

const (
	SIPStepPrepared          = "prepared"
	SIPStepMayHaveDispatched = "may_have_dispatched"
	maxIntentSIPSteps        = 16
	maxIntentSIPBytes        = 32768
)

type DeviceSIPInviteStep struct {
	Identity          DeviceSIPInviteIdentity `json:"-"`
	State             string                  `json:"-"`
	RowVersion        int64                   `json:"-"`
	PreparedAt        time.Time               `json:"-"`
	DispatchStartedAt *time.Time              `json:"-"`
}

// No remote branches, terminal state or coverage are represented here. In
// particular, empty/NULL steps do not prove that no INVITE was ever sent.
type DeviceSIPInviteSteps struct {
	Intent DeviceOperationIntent `json:"-"`
	Steps  []DeviceSIPInviteStep `json:"-"`
}

type sipInviteStepWire struct {
	Version           int                   `json:"version"`
	Action            string                `json:"action"`
	Identity          sipInviteIdentityWire `json:"identity"`
	State             string                `json:"state"`
	RowVersion        int64                 `json:"rowVersion"`
	PreparedAt        time.Time             `json:"preparedAt"`
	DispatchStartedAt *time.Time            `json:"dispatchStartedAt"`
}

type sipInviteStepsWire struct {
	Version int                 `json:"version"`
	Steps   []sipInviteStepWire `json:"steps"`
}

type sipIntentRow struct {
	DeviceOperationIntent `gorm:"embedded" json:"-"`
	SIPStepsJSON          *string `gorm:"column:sip_steps_json" json:"-"`
}

func sipStepToWire(s DeviceSIPInviteStep) sipInviteStepWire {
	return sipInviteStepWire{1, "invite", s.Identity.wire(), s.State, s.RowVersion, s.PreparedAt, s.DispatchStartedAt}
}

func validSIPStepTime(value time.Time) bool {
	_, offset := value.Zone()
	return !value.IsZero() && offset == 0 && value.Nanosecond()%1000 == 0
}

func readSIPInviteSteps(tx *gorm.DB, id DeviceOperationIntentIdentity) (DeviceSIPInviteSteps, error) {
	var rows []sipIntentRow
	// An unmigrated database must fail, not appear to have empty evidence.
	err := tx.Table("gb_device_operation_intent").Select("gb_device_operation_intent.*, sip_steps_json").
		Where("operation_id = ?", id.OperationID).Limit(1).Find(&rows).Error
	if err != nil {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
	}
	if len(rows) != 1 {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentConflict
	}
	row := rows[0]
	if !validIntentRow(row.DeviceOperationIntent) {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
	}
	if row.DeviceOperationIntentIdentity != id || row.State != IntentDispatched {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentConflict
	}
	out := DeviceSIPInviteSteps{Intent: row.DeviceOperationIntent}
	if row.SIPStepsJSON == nil {
		return out, nil
	}
	raw := []byte(*row.SIPStepsJSON)
	if len(raw) == 0 || len(raw) > maxIntentSIPBytes {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
	}
	var wire sipInviteStepsWire
	if json.Unmarshal(raw, &wire) != nil || wire.Version != 1 || wire.Steps == nil || len(wire.Steps) > maxIntentSIPSteps {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
	}
	canonical, err := json.Marshal(wire)
	if err != nil || !bytes.Equal(raw, canonical) {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
	}
	ids := map[string]bool{}
	type inviteKey struct{ callID, localTag string }
	invites := map[inviteKey]bool{}
	for _, w := range wire.Steps {
		i := DeviceSIPInviteIdentity(w.Identity)
		key := inviteKey{i.CallID, i.LocalTag}
		if w.Version != 1 || w.Action != "invite" || !validSIPInviteIdentity(i) || ids[i.StepID] || invites[key] ||
			!validSIPStepTime(w.PreparedAt) || w.PreparedAt.Before(*row.DispatchStartedAt) || w.PreparedAt.After(row.UpdatedAt) {
			return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
		}
		switch w.State {
		case SIPStepPrepared:
			if w.RowVersion != 1 || w.DispatchStartedAt != nil {
				return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
			}
		case SIPStepMayHaveDispatched:
			if w.RowVersion != 2 || w.DispatchStartedAt == nil || !validSIPStepTime(*w.DispatchStartedAt) || w.DispatchStartedAt.Before(w.PreparedAt) || w.DispatchStartedAt.After(row.UpdatedAt) {
				return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
			}
		default:
			return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
		}
		ids[i.StepID], invites[key] = true, true
		out.Steps = append(out.Steps, DeviceSIPInviteStep{i, w.State, w.RowVersion, w.PreparedAt, w.DispatchStartedAt})
	}
	return out, nil
}

// LoadSIPInviteSteps is recovery observation, including after device transfer.
// Neither persisted state authorizes sending or retrying an INVITE.
func (s *DeviceOperationIntentStore) LoadSIPInviteSteps(ctx context.Context, id DeviceOperationIntentIdentity) (DeviceSIPInviteSteps, error) {
	if !s.available(ctx) {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
	}
	if !validIntentIdentity(id) {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentInvalid
	}
	return readSIPInviteSteps(s.db.WithContext(ctx), id)
}

func (s *DeviceOperationIntentStore) AddSIPInviteStep(ctx context.Context, id DeviceOperationIntentIdentity, version int64, identity DeviceSIPInviteIdentity) (DeviceSIPInviteSteps, error) {
	if !validSIPInviteIdentity(identity) {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentInvalid
	}
	return s.mutateSIPInviteStep(ctx, id, version, func(out *DeviceSIPInviteSteps, now time.Time) (bool, error) {
		for _, old := range out.Steps {
			if old.Identity.StepID == identity.StepID {
				if old.Identity == identity {
					return false, nil
				}
				return false, ErrDeviceIntentConflict
			}
			if old.Identity.CallID == identity.CallID && old.Identity.LocalTag == identity.LocalTag {
				return false, ErrDeviceIntentConflict
			}
		}
		if len(out.Steps) >= maxIntentSIPSteps {
			return false, ErrDeviceIntentConflict
		}
		out.Steps = append(out.Steps, DeviceSIPInviteStep{Identity: identity, State: SIPStepPrepared, RowVersion: 1, PreparedAt: now})
		return true, nil
	})
}

// Only this call's confirmed CAS winner can gain permission for future network
// dispatch. Commit failure/unknown returns an empty result. Do not infer the
// permission by reading back, and retain the shared device operation lease
// through actual dispatch. There is no production executor in this store.
func (s *DeviceOperationIntentStore) DispatchSIPInviteStep(ctx context.Context, id DeviceOperationIntentIdentity, version int64, stepID string) (DeviceSIPInviteSteps, error) {
	if !validIntentID(stepID) {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentInvalid
	}
	return s.mutateSIPInviteStep(ctx, id, version, func(out *DeviceSIPInviteSteps, now time.Time) (bool, error) {
		for index := range out.Steps {
			step := &out.Steps[index]
			if step.Identity.StepID != stepID {
				continue
			}
			if step.State != SIPStepPrepared {
				return false, ErrDeviceIntentConflict
			}
			step.State, step.RowVersion, step.DispatchStartedAt = SIPStepMayHaveDispatched, 2, &now
			return true, nil
		}
		return false, ErrDeviceIntentConflict
	})
}

func (s *DeviceOperationIntentStore) mutateSIPInviteStep(ctx context.Context, id DeviceOperationIntentIdentity, version int64, mutate func(*DeviceSIPInviteSteps, time.Time) (bool, error)) (DeviceSIPInviteSteps, error) {
	if !s.available(ctx) {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
	}
	if !validIntentIdentity(id) || version < 2 || version == math.MaxInt64 {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentInvalid
	}
	var out DeviceSIPInviteSteps
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeIntentDevice(tx, ctx, id); err != nil {
			return err
		}
		var err error
		out, err = readSIPInviteSteps(tx, id)
		if err != nil {
			return err
		}
		if out.Intent.RowVersion != version {
			return ErrDeviceIntentConflict
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if now.Before(out.Intent.UpdatedAt) {
			return ErrDeviceIntentUnavailable
		}
		changed, err := mutate(&out, now)
		if err != nil || !changed {
			return err
		}
		wire := sipInviteStepsWire{Version: 1, Steps: make([]sipInviteStepWire, 0, len(out.Steps))}
		for _, step := range out.Steps {
			wire.Steps = append(wire.Steps, sipStepToWire(step))
		}
		body, err := json.Marshal(wire)
		if err != nil || len(body) > maxIntentSIPBytes {
			return ErrDeviceIntentUnavailable
		}
		result := tx.Model(&DeviceOperationIntent{}).
			Where("operation_id = ? AND contract_version = 1 AND state = ? AND row_version = ?", id.OperationID, IntentDispatched, version).
			Updates(map[string]any{"sip_steps_json": string(body), "row_version": version + 1, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDeviceIntentConflict
		}
		out.Intent.RowVersion, out.Intent.UpdatedAt = version+1, now
		return nil
	})
	if err != nil {
		return DeviceSIPInviteSteps{}, normalizeIntentError(err)
	}
	return out, nil
}
