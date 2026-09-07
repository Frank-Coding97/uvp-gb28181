package playauth

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"gorm.io/gorm"
)

// DeviceSIPKnownBranchIdentity describes ONE observed 2xx branch, not every
// branch of a Call-ID. It is internal evidence, not a replayable SIP response.
type DeviceSIPKnownBranchIdentity struct {
	InviteStepID string   `json:"-"`
	CallID       string   `json:"-"`
	LocalTag     string   `json:"-"`
	RemoteTag    string   `json:"-"`
	CSeq         uint32   `json:"-"`
	StatusCode   int      `json:"-"`
	RemoteTarget string   `json:"-"`
	RouteSet     []string `json:"-"`
}

type DeviceSIPKnownBranch struct {
	Identity             DeviceSIPKnownBranchIdentity `json:"-"`
	ObservedAt           time.Time                    `json:"-"`
	ACKState             string                       `json:"-"`
	ACKRowVersion        int64                        `json:"-"`
	ACKDispatchStartedAt *time.Time                   `json:"-"`
}

type sipKnownBranchIdentityWire struct {
	InviteStepID string   `json:"inviteStepID"`
	CallID       string   `json:"callID"`
	LocalTag     string   `json:"localTag"`
	RemoteTag    string   `json:"remoteTag"`
	CSeq         uint32   `json:"cseq"`
	StatusCode   int      `json:"statusCode"`
	RemoteTarget string   `json:"remoteTarget"`
	RouteSet     []string `json:"routeSet"`
}

type sipKnownBranchWire struct {
	Version              int                        `json:"version"`
	Identity             sipKnownBranchIdentityWire `json:"identity"`
	ObservedAt           time.Time                  `json:"observedAt"`
	ACKState             string                     `json:"ackState"`
	ACKRowVersion        int64                      `json:"ackRowVersion"`
	ACKDispatchStartedAt *time.Time                 `json:"ackDispatchStartedAt"`
}

func sipKnownBranchToWire(b *DeviceSIPKnownBranch) *sipKnownBranchWire {
	if b == nil {
		return nil
	}
	return &sipKnownBranchWire{1, sipKnownBranchIdentityWire(b.Identity), b.ObservedAt, b.ACKState, b.ACKRowVersion, b.ACKDispatchStartedAt}
}

// Preserve parsed URI order and spelling. The accepted bounded profile is not
// a generic RFC URI sanitizer; unsupported fields fail, never get stripped.
func validSIPDialogURI(raw string, route bool) bool {
	if len(raw) == 0 || len(raw) > 1024 || strings.ContainsAny(raw, "\r\n\x00\t ") {
		return false
	}
	var uri sip.Uri
	if sip.ParseUri(raw, &uri) != nil || uri.Scheme != "sip" || uri.Wildcard || uri.HierarhicalSlashes || uri.Password != "" ||
		len(uri.Headers) != 0 || !sipIdentityHost(uri.Host) || uri.Port < 0 || uri.Port > 65535 || uri.String() != raw ||
		(uri.User == "" && !route) || (uri.User != "" && !sipIdentityPart(uri.User, 256)) {
		return false
	}
	seen := map[string]bool{}
	for _, param := range uri.UriParams {
		key, value := param.K, param.V
		if seen[key] {
			return false
		}
		seen[key] = true
		switch key {
		case "lr":
			if value != "" {
				return false
			}
		case "transport":
			if value != "udp" && value != "tcp" && value != "UDP" && value != "TCP" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func validSIPKnownBranchIdentity(i DeviceSIPKnownBranchIdentity) bool {
	if !validIntentID(i.InviteStepID) || !sipIdentityPart(i.CallID, 256) || !sipIdentityPart(i.LocalTag, 128) ||
		!sipIdentityPart(i.RemoteTag, 128) || i.CSeq == 0 || i.StatusCode < 200 || i.StatusCode > 299 ||
		!validSIPDialogURI(i.RemoteTarget, false) || i.RouteSet == nil || len(i.RouteSet) > 8 {
		return false
	}
	for _, route := range i.RouteSet {
		if !validSIPDialogURI(route, true) {
			return false
		}
	}
	return true
}

func sipBranchMatchesInvite(i DeviceSIPKnownBranchIdentity, step DeviceSIPInviteStep) bool {
	return step.State == SIPStepMayHaveDispatched && i.InviteStepID == step.Identity.StepID &&
		i.CallID == step.Identity.CallID && i.LocalTag == step.Identity.LocalTag && i.CSeq == step.Identity.CSeq
}

func equalSIPKnownBranch(a, b DeviceSIPKnownBranchIdentity) bool {
	return a.InviteStepID == b.InviteStepID && a.CallID == b.CallID && a.LocalTag == b.LocalTag &&
		a.RemoteTag == b.RemoteTag && a.CSeq == b.CSeq && a.StatusCode == b.StatusCode && a.RemoteTarget == b.RemoteTarget && slices.Equal(a.RouteSet, b.RouteSet)
}

func readSIPKnownBranch(w *sipKnownBranchWire, step DeviceSIPInviteStep, updatedAt time.Time) (*DeviceSIPKnownBranch, error) {
	i := DeviceSIPKnownBranchIdentity(w.Identity)
	if w.Version != 1 || !validSIPKnownBranchIdentity(i) || !sipBranchMatchesInvite(i, step) ||
		step.DispatchStartedAt == nil || !validSIPStepTime(w.ObservedAt) || w.ObservedAt.Before(*step.DispatchStartedAt) || w.ObservedAt.After(updatedAt) {
		return nil, ErrDeviceIntentUnavailable
	}
	switch w.ACKState {
	case SIPStepPrepared:
		if w.ACKRowVersion != 1 || w.ACKDispatchStartedAt != nil {
			return nil, ErrDeviceIntentUnavailable
		}
	case SIPStepMayHaveDispatched:
		if w.ACKRowVersion != 2 || w.ACKDispatchStartedAt == nil || !validSIPStepTime(*w.ACKDispatchStartedAt) || w.ACKDispatchStartedAt.Before(w.ObservedAt) || w.ACKDispatchStartedAt.After(updatedAt) {
			return nil, ErrDeviceIntentUnavailable
		}
	default:
		return nil, ErrDeviceIntentUnavailable
	}
	return &DeviceSIPKnownBranch{i, w.ObservedAt, w.ACKState, w.ACKRowVersion, w.ACKDispatchStartedAt}, nil
}

func observeSIPBranchDevice(tx *gorm.DB, ctx context.Context, id DeviceOperationIntentIdentity) error {
	rows, err := queryDeviceCleanupRows(tx, ctx, id.DeviceCode, true)
	if err != nil || len(rows) != 1 || rows[0].ID != id.DevicePK {
		return ErrDeviceIntentUnavailable
	}
	state, err := validateDeviceCleanupRow(rows[0])
	if err != nil || state.AccessEpoch < id.DeviceEpoch {
		return ErrDeviceIntentUnavailable
	}
	return nil
}

// ObserveSIPKnownBranch can retain a late response after transfer. It grants no
// ACK/network permission and never replaces an earlier branch with a late fork.
func (s *DeviceOperationIntentStore) ObserveSIPKnownBranch(ctx context.Context, id DeviceOperationIntentIdentity, version int64, identity DeviceSIPKnownBranchIdentity) (DeviceSIPInviteSteps, error) {
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
			if !sipBranchMatchesInvite(identity, *step) {
				return false, ErrDeviceIntentConflict
			}
			if step.KnownBranch != nil {
				if equalSIPKnownBranch(step.KnownBranch.Identity, identity) {
					return false, nil
				}
				return false, ErrDeviceIntentConflict
			}
			step.KnownBranch = &DeviceSIPKnownBranch{Identity: identity, ObservedAt: now, ACKState: SIPStepPrepared, ACKRowVersion: 1}
			return true, nil
		}
		return false, ErrDeviceIntentConflict
	})
}

// Only this call's confirmed CAS winner may explicitly ACK the exact known
// branch, under its still-held device-bound owner. Load/commit-unknown never
// restores that permission. No network or protocol retransmission occurs here.
func (s *DeviceOperationIntentStore) DispatchSIPKnownBranchACK(ctx context.Context, id DeviceOperationIntentIdentity, version int64, identity DeviceSIPKnownBranchIdentity) (DeviceSIPInviteSteps, error) {
	if !validSIPKnownBranchIdentity(identity) {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentInvalid
	}
	return s.mutateSIPInviteStep(ctx, id, version, func(out *DeviceSIPInviteSteps, now time.Time) (bool, error) {
		for index := range out.Steps {
			step := &out.Steps[index]
			if step.Identity.StepID != identity.InviteStepID {
				continue
			}
			b := step.KnownBranch
			if b == nil || !equalSIPKnownBranch(b.Identity, identity) || b.ACKState != SIPStepPrepared {
				return false, ErrDeviceIntentConflict
			}
			b.ACKState, b.ACKRowVersion, b.ACKDispatchStartedAt = SIPStepMayHaveDispatched, 2, &now
			return true, nil
		}
		return false, ErrDeviceIntentConflict
	})
}
