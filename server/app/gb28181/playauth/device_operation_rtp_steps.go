package playauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	RTPStepPrepared          = "prepared"
	RTPStepMayHaveDispatched = "may_have_dispatched"
	maxIntentRTPSteps        = 16
	maxIntentRTPBytes        = 32768
)

// NewDeviceRTPResourceID binds one creation resource to its original operation
// and independently CSPRNG-generated step ID. Generate once BEFORE persistence;
// a lost commit response is never permission to refresh now/deadline or the ID.
// v1 suffix = first 128 bits of SHA-256(domain NUL operationID NUL stepID).
func NewDeviceRTPResourceID(operationID, stepID string, now time.Time) (string, error) {
	ms := now.UnixMilli()
	if !validIntentID(operationID) || !validIntentID(stepID) || ms < 1000000000000 || ms > 9999999999999-25000 {
		return "", ErrDeviceIntentInvalid
	}
	return "d" + strconv.FormatInt(ms+25000, 10) + "-" + rtpResourceIdentitySuffix(operationID, stepID), nil
}

func rtpResourceIdentitySuffix(operationID, stepID string) string {
	sum := sha256.Sum256([]byte("UVP-RTP-RESOURCE/1\x00" + operationID + "\x00" + stepID))
	return hex.EncodeToString(sum[:16])
}

// DeviceRTPResourceIdentity is internal creation evidence, not a client DTO.
// The operation owner generates the resource/step IDs once, and retains the
// original node/config/boot snapshot. No secrets or executable payload belong here.
type DeviceRTPResourceIdentity struct {
	StepID       string `json:"-"`
	NodePK       int64  `json:"-"`
	NodeUUID     string `json:"-"`
	NodeRevision int64  `json:"-"`
	BootNonce    string `json:"-"`
	ResourceID   string `json:"-"`
	VHost        string `json:"-"`
	App          string `json:"-"`
	Stream       string `json:"-"`
	Port         int    `json:"-"`
	LocalIP      string `json:"-"`
	TCPMode      int    `json:"-"`
	SSRC         uint32 `json:"-"`
	OnlyTrack    int    `json:"-"`
}

type DeviceRTPResourceStep struct {
	Identity                DeviceRTPResourceIdentity `json:"-"`
	State                   string                    `json:"-"`
	RowVersion              int64                     `json:"-"`
	PreparedAt              time.Time                 `json:"-"`
	DispatchStartedAt       *time.Time                `json:"-"`
	OwnerRunID              string                    `json:"-"`
	OpenResult              *DeviceRTPOpenResult      `json:"-"`
	OpenObservedAt          *time.Time                `json:"-"`
	ResourceCloseResult     string                    `json:"-"`
	ResourceCloseObservedAt *time.Time                `json:"-"`
	IngressCloseResult      string                    `json:"-"`
	IngressCloseObservedAt  *time.Time                `json:"-"`
	LocalQuiescedAt         *time.Time                `json:"-"`
	Recovery                *DeviceRTPRecovery        `json:"-"`
}

type DeviceRTPOpenResult struct {
	Result string `json:"-"`
	Port   int    `json:"-"`
}

type rtpOpenResultWire struct {
	Result string `json:"result"`
	Port   int    `json:"port"`
}

// This snapshot has no completion/coverage field and grants no dispatch rights.
type DeviceRTPResourceSteps struct {
	Intent DeviceOperationIntent   `json:"-"`
	Steps  []DeviceRTPResourceStep `json:"-"`
}

// Persistence-only DTO: every field is fixed. In particular there is no raw
// payload, endpoint, credentials, terminal state or generic action dispatcher.
type rtpStepWire struct {
	Version                 int                `json:"version"`
	Action                  string             `json:"action"`
	StepID                  string             `json:"stepID"`
	NodePK                  int64              `json:"nodePK"`
	NodeUUID                string             `json:"nodeUUID"`
	NodeRevision            int64              `json:"nodeRevision"`
	BootNonce               string             `json:"bootNonce"`
	ResourceID              string             `json:"resourceID"`
	VHost                   string             `json:"vhost"`
	App                     string             `json:"app"`
	Stream                  string             `json:"stream"`
	Port                    int                `json:"port"`
	LocalIP                 string             `json:"localIP"`
	TCPMode                 int                `json:"tcpMode"`
	SSRC                    uint32             `json:"ssrc"`
	OnlyTrack               int                `json:"onlyTrack"`
	State                   string             `json:"state"`
	RowVersion              int64              `json:"rowVersion"`
	PreparedAt              time.Time          `json:"preparedAt"`
	DispatchStartedAt       *time.Time         `json:"dispatchStartedAt"`
	OwnerRunID              string             `json:"ownerRunID,omitempty"`
	OpenResult              *rtpOpenResultWire `json:"openResult,omitempty"`
	OpenObservedAt          *time.Time         `json:"openObservedAt,omitempty"`
	ResourceCloseResult     string             `json:"resourceCloseResult,omitempty"`
	ResourceCloseObservedAt *time.Time         `json:"resourceCloseObservedAt,omitempty"`
	IngressCloseResult      string             `json:"ingressCloseResult,omitempty"`
	IngressCloseObservedAt  *time.Time         `json:"ingressCloseObservedAt,omitempty"`
	LocalQuiescedAt         *time.Time         `json:"localQuiescedAt,omitempty"`
	Recovery                *rtpRecoveryWire   `json:"cleanup,omitempty"`
}

type rtpStepsWire struct {
	Version int           `json:"version"`
	Steps   []rtpStepWire `json:"steps"`
}

type rtpIntentRow struct {
	DeviceOperationIntent `gorm:"embedded" json:"-"`
	RTPStepsJSON          *string `gorm:"column:rtp_steps_json" json:"-"`
}

func stepToWire(step DeviceRTPResourceStep) rtpStepWire {
	i := step.Identity
	w := rtpStepWire{Version: 1, Action: "open_rtp", StepID: i.StepID, NodePK: i.NodePK, NodeUUID: i.NodeUUID, NodeRevision: i.NodeRevision, BootNonce: i.BootNonce,
		ResourceID: i.ResourceID, VHost: i.VHost, App: i.App, Stream: i.Stream, Port: i.Port, LocalIP: i.LocalIP, TCPMode: i.TCPMode, SSRC: i.SSRC, OnlyTrack: i.OnlyTrack,
		State: step.State, RowVersion: step.RowVersion, PreparedAt: step.PreparedAt, DispatchStartedAt: step.DispatchStartedAt,
		OwnerRunID: step.OwnerRunID, OpenObservedAt: step.OpenObservedAt, ResourceCloseResult: step.ResourceCloseResult, ResourceCloseObservedAt: step.ResourceCloseObservedAt,
		IngressCloseResult: step.IngressCloseResult, IngressCloseObservedAt: step.IngressCloseObservedAt, LocalQuiescedAt: step.LocalQuiescedAt, Recovery: recoveryToWire(step.Recovery)}
	if step.OpenResult != nil {
		w.OpenResult = &rtpOpenResultWire{Result: step.OpenResult.Result, Port: step.OpenResult.Port}
	}
	return w
}

func (w rtpStepWire) step() DeviceRTPResourceStep {
	step := DeviceRTPResourceStep{Identity: DeviceRTPResourceIdentity{
		StepID: w.StepID, NodePK: w.NodePK, NodeUUID: w.NodeUUID, NodeRevision: w.NodeRevision,
		BootNonce: w.BootNonce, ResourceID: w.ResourceID, VHost: w.VHost, App: w.App, Stream: w.Stream,
		Port: w.Port, LocalIP: w.LocalIP, TCPMode: w.TCPMode, SSRC: w.SSRC, OnlyTrack: w.OnlyTrack},
		State: w.State, RowVersion: w.RowVersion, PreparedAt: w.PreparedAt, DispatchStartedAt: w.DispatchStartedAt,
		OwnerRunID: w.OwnerRunID, OpenObservedAt: w.OpenObservedAt, ResourceCloseResult: w.ResourceCloseResult, ResourceCloseObservedAt: w.ResourceCloseObservedAt,
		IngressCloseResult: w.IngressCloseResult, IngressCloseObservedAt: w.IngressCloseObservedAt, LocalQuiescedAt: w.LocalQuiescedAt, Recovery: w.Recovery.recovery()}
	if w.OpenResult != nil {
		step.OpenResult = &DeviceRTPOpenResult{Result: w.OpenResult.Result, Port: w.OpenResult.Port}
	}
	return step
}

func validRTPStepIdentity(i DeviceRTPResourceIdentity) bool {
	resource := i.ResourceID
	part := func(s string, limit int) bool {
		return s != "" && len(s) <= limit && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-") == ""
	}
	return validIntentID(i.StepID) && i.NodePK > 0 && i.NodeRevision > 0 && validOpenAPINodeUUID(i.NodeUUID) &&
		validOpenAPIBootNonce(i.BootNonce) && len(resource) == 47 && resource[0] == 'd' && resource[14] == '-' &&
		strings.Trim(resource[1:14], "0123456789") == "" && validIntentID(resource[15:]) &&
		i.VHost == "__defaultVhost__" && part(i.App, 64) && part(i.Stream, 256) &&
		i.Port >= 0 && i.Port <= 65534 && i.TCPMode >= 0 && i.TCPMode <= 1 && i.OnlyTrack >= 0 && i.OnlyTrack <= 2 &&
		len(i.LocalIP) <= 64 && net.ParseIP(i.LocalIP) != nil
}

func readRTPSteps(tx *gorm.DB, id DeviceOperationIntentIdentity) (DeviceRTPResourceSteps, error) {
	var rows []rtpIntentRow
	// Explicitly selecting the new column makes an unmigrated schema fail closed.
	err := tx.Table("gb_device_operation_intent").Select("gb_device_operation_intent.*, rtp_steps_json").
		Where("operation_id = ?", id.OperationID).Limit(1).Find(&rows).Error
	if err != nil {
		return DeviceRTPResourceSteps{}, ErrDeviceIntentUnavailable
	}
	if len(rows) != 1 {
		return DeviceRTPResourceSteps{}, ErrDeviceIntentConflict
	}
	row := rows[0]
	if !validIntentRow(row.DeviceOperationIntent) {
		return DeviceRTPResourceSteps{}, ErrDeviceIntentUnavailable
	}
	if row.DeviceOperationIntentIdentity != id || row.State != IntentDispatched {
		return DeviceRTPResourceSteps{}, ErrDeviceIntentConflict
	}
	out := DeviceRTPResourceSteps{Intent: row.DeviceOperationIntent}
	if row.RTPStepsJSON == nil {
		return out, nil
	} // Historical unknown, NOT empty coverage.
	raw := []byte(*row.RTPStepsJSON)
	if len(raw) == 0 || len(raw) > maxIntentRTPBytes {
		return DeviceRTPResourceSteps{}, ErrDeviceIntentUnavailable
	}
	var wire rtpStepsWire
	if json.Unmarshal(raw, &wire) != nil || wire.Version != 1 || wire.Steps == nil || len(wire.Steps) > maxIntentRTPSteps {
		return DeviceRTPResourceSteps{}, ErrDeviceIntentUnavailable
	}
	// This is stored TEXT, not a native JSON column. Only our canonical encoder
	// writes it; byte comparison rejects duplicates, case shadows, extra/null
	// fields and type coercion. No database JSON normalizer is involved.
	canonical, err := json.Marshal(wire)
	if err != nil || !bytes.Equal(raw, canonical) {
		return DeviceRTPResourceSteps{}, ErrDeviceIntentUnavailable
	}
	ids, resources := map[string]bool{}, map[string]bool{}
	for _, w := range wire.Steps {
		step := w.step()
		if w.Version != 1 || w.Action != "open_rtp" || !validRTPStepIdentity(step.Identity) || w.ResourceID[15:] != rtpResourceIdentitySuffix(id.OperationID, w.StepID) || ids[w.StepID] || resources[w.ResourceID] ||
			w.PreparedAt.IsZero() || w.PreparedAt.Before(*row.DispatchStartedAt) || w.PreparedAt.After(row.UpdatedAt) {
			return DeviceRTPResourceSteps{}, ErrDeviceIntentUnavailable
		}
		switch w.State {
		case RTPStepPrepared:
			if w.RowVersion != 1 || w.DispatchStartedAt != nil {
				return DeviceRTPResourceSteps{}, ErrDeviceIntentUnavailable
			}
		case RTPStepMayHaveDispatched:
			if w.RowVersion < 2 || (w.OwnerRunID == "" && w.RowVersion != 2) || w.DispatchStartedAt == nil || w.DispatchStartedAt.Before(w.PreparedAt) || w.DispatchStartedAt.After(row.UpdatedAt) {
				return DeviceRTPResourceSteps{}, ErrDeviceIntentUnavailable
			}
		default:
			return DeviceRTPResourceSteps{}, ErrDeviceIntentUnavailable
		}
		if !validRTPExecution(step, row.UpdatedAt) || !validRTPRecovery(step, row.UpdatedAt) || step.RowVersion > row.RowVersion-2 {
			return DeviceRTPResourceSteps{}, ErrDeviceIntentUnavailable
		}
		ids[w.StepID], resources[w.ResourceID] = true, true
		out.Steps = append(out.Steps, step)
	}
	return out, nil
}

// LoadRTPResourceSteps reads the original intent even after device transfer.
// Neither a prepared nor a may-have-dispatched snapshot authorizes recovery open.
func (s *DeviceOperationIntentStore) LoadRTPResourceSteps(ctx context.Context, id DeviceOperationIntentIdentity) (DeviceRTPResourceSteps, error) {
	if !s.available(ctx) {
		return DeviceRTPResourceSteps{}, ErrDeviceIntentUnavailable
	}
	if !validIntentIdentity(id) {
		return DeviceRTPResourceSteps{}, ErrDeviceIntentInvalid
	}
	return readRTPSteps(s.db.WithContext(ctx), id)
}

// AddRTPResourceStep is preparation only; duplicate exact identity is observation,
// never network permission. It cannot replace an old resource or refresh its boot.
func (s *DeviceOperationIntentStore) AddRTPResourceStep(ctx context.Context, id DeviceOperationIntentIdentity, version int64, identity DeviceRTPResourceIdentity) (DeviceRTPResourceSteps, error) {
	if !validRTPStepIdentity(identity) || identity.ResourceID[15:] != rtpResourceIdentitySuffix(id.OperationID, identity.StepID) {
		return DeviceRTPResourceSteps{}, ErrDeviceIntentInvalid
	}
	return s.mutateRTPStep(ctx, id, version, func(out *DeviceRTPResourceSteps, now time.Time) (bool, error) {
		for _, old := range out.Steps {
			if old.Identity.StepID == identity.StepID {
				if old.Identity == identity {
					return false, nil
				}
				return false, ErrDeviceIntentConflict
			}
			if old.Identity.ResourceID == identity.ResourceID {
				return false, ErrDeviceIntentConflict
			}
		}
		if len(out.Steps) >= maxIntentRTPSteps {
			return false, ErrDeviceIntentConflict
		}
		out.Steps = append(out.Steps, DeviceRTPResourceStep{Identity: identity, State: RTPStepPrepared, RowVersion: 1, PreparedAt: now})
		return true, nil
	})
}

// DispatchRTPResourceStep grants permission only to this call's confirmed CAS
// winner. Never reconstruct that permission from Load, retry a dispatched step,
// or use this store without the operation owner's shared device lease. This
// foundation has no production callers or network executor.
func (s *DeviceOperationIntentStore) DispatchRTPResourceStep(ctx context.Context, id DeviceOperationIntentIdentity, version int64, stepID string) (DeviceRTPResourceSteps, error) {
	if !validIntentID(stepID) {
		return DeviceRTPResourceSteps{}, ErrDeviceIntentInvalid
	}
	return s.mutateRTPStep(ctx, id, version, func(out *DeviceRTPResourceSteps, now time.Time) (bool, error) {
		for index := range out.Steps {
			step := &out.Steps[index]
			if step.Identity.StepID != stepID {
				continue
			}
			if step.State != RTPStepPrepared {
				return false, ErrDeviceIntentConflict
			}
			step.State, step.RowVersion, step.DispatchStartedAt = RTPStepMayHaveDispatched, 2, &now
			return true, nil
		}
		return false, ErrDeviceIntentConflict
	})
}

func (s *DeviceOperationIntentStore) mutateRTPStep(ctx context.Context, id DeviceOperationIntentIdentity, version int64, mutate func(*DeviceRTPResourceSteps, time.Time) (bool, error)) (DeviceRTPResourceSteps, error) {
	return s.mutateRTPFacts(ctx, id, version, authorizeIntentDevice, mutate)
}

func (s *DeviceOperationIntentStore) mutateRTPFacts(ctx context.Context, id DeviceOperationIntentIdentity, version int64, authorize func(*gorm.DB, context.Context, DeviceOperationIntentIdentity) error, mutate func(*DeviceRTPResourceSteps, time.Time) (bool, error)) (DeviceRTPResourceSteps, error) {
	if !s.available(ctx) {
		return DeviceRTPResourceSteps{}, ErrDeviceIntentUnavailable
	}
	if !validIntentIdentity(id) || version < 2 || version == math.MaxInt64 {
		return DeviceRTPResourceSteps{}, ErrDeviceIntentInvalid
	}
	var out DeviceRTPResourceSteps
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorize(tx, ctx, id); err != nil {
			return err
		}
		var err error
		out, err = readRTPSteps(tx, id)
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
		wire := rtpStepsWire{Version: 1, Steps: make([]rtpStepWire, 0, len(out.Steps))}
		for _, step := range out.Steps {
			wire.Steps = append(wire.Steps, stepToWire(step))
		}
		body, err := json.Marshal(wire)
		if err != nil || len(body) > maxIntentRTPBytes {
			return ErrDeviceIntentUnavailable
		}
		result := tx.Model(&DeviceOperationIntent{}).
			Where("operation_id = ? AND contract_version = 1 AND state = ? AND row_version = ?", id.OperationID, IntentDispatched, version).
			Updates(map[string]any{"rtp_steps_json": string(body), "row_version": version + 1, "updated_at": now})
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
		return DeviceRTPResourceSteps{}, normalizeIntentError(err)
	}
	return out, nil
}
