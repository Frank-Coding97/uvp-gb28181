package playauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"slices"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/mansrtsp"
)

const maxSIPINFOSteps = 16

// Only the existing playback commands are durable; no arbitrary INFO body,
// request, credentials or response reason is retained in the intent row.
type DeviceSIPINFOCommand struct {
	Action               string  `json:"-"`
	PositionNanos        int64   `json:"-"`
	SegmentDurationNanos int64   `json:"-"`
	Scale                float64 `json:"-"`
}

func (c DeviceSIPINFOCommand) Body(cseq uint32) ([]byte, error) {
	if (c.Action != "seek" && (c.PositionNanos != 0 || c.SegmentDurationNanos != 0)) || (c.Action != "scale" && c.Scale != 0) {
		return nil, ErrDeviceIntentInvalid
	}
	switch c.Action {
	case "play":
		return mansrtsp.BuildPlay(cseq)
	case "pause":
		return mansrtsp.BuildPause(cseq)
	case "resume":
		return mansrtsp.BuildResume(cseq)
	case "seek":
		return mansrtsp.BuildSeek(cseq, time.Duration(c.PositionNanos), time.Duration(c.SegmentDurationNanos))
	case "scale":
		return mansrtsp.BuildScale(cseq, c.Scale)
	case "teardown":
		return mansrtsp.BuildTeardown(cseq)
	default:
		return nil, ErrDeviceIntentInvalid
	}
}

type DeviceSIPINFOIdentity struct {
	InfoID  string                          `json:"-"`
	Request DeviceSIPCleanupRequestIdentity `json:"-"`
	Command DeviceSIPINFOCommand            `json:"-"`
}

type DeviceSIPINFOResponse struct {
	InfoID         string `json:"-"`
	CallID         string `json:"-"`
	CSeq           uint32 `json:"-"`
	LocalTag       string `json:"-"`
	RemoteTag      string `json:"-"`
	SIPStatus      int    `json:"-"`
	BodyClass      string `json:"-"`
	MANSRTSPStatus int    `json:"-"`
	MANSRTSPCSeq   uint32 `json:"-"`
}

type DeviceSIPINFOStep struct {
	Identity           DeviceSIPINFOIdentity  `json:"-"`
	OwnerRunID         string                 `json:"-"`
	State              string                 `json:"-"`
	RowVersion         int64                  `json:"-"`
	PreparedAt         time.Time              `json:"-"`
	DispatchStartedAt  *time.Time             `json:"-"`
	Response           *DeviceSIPINFOResponse `json:"-"`
	ResponseObservedAt *time.Time             `json:"-"`
	LocalQuiescedAt    *time.Time             `json:"-"`
}

func equalSIPINFOIdentity(a, b DeviceSIPINFOIdentity) bool {
	return a.InfoID == b.InfoID && a.Command == b.Command && a.Request.Request == b.Request.Request &&
		a.Request.RemoteTag == b.Request.RemoteTag && slices.Equal(a.Request.Routes, b.Request.Routes)
}

func sipINFOIdentityMatches(i DeviceSIPINFOIdentity, step DeviceSIPInviteStep, cseq uint32) bool {
	body, err := i.Command.Body(cseq)
	if !validIntentID(i.InfoID) || cseq <= step.Identity.CSeq || err != nil || len(body) == 0 || len(body) > 4096 {
		return false
	}
	digest := sha256.Sum256(body)
	return sipDialogRequestMatches(i.Request, step, cseq, mansrtsp.ContentType, len(body), hex.EncodeToString(digest[:]))
}

func sipINFOResponseMatches(r DeviceSIPINFOResponse, a DeviceSIPINFOStep) bool {
	i := a.Identity
	if r.InfoID != i.InfoID || r.CallID != i.Request.Request.CallID || r.CSeq != i.Request.Request.CSeq ||
		r.LocalTag != i.Request.Request.LocalTag || r.RemoteTag != i.Request.RemoteTag {
		return false
	}
	switch r.BodyClass {
	case "sip_rejected":
		return r.SIPStatus >= 300 && r.SIPStatus <= 699 && r.MANSRTSPStatus == 0 && r.MANSRTSPCSeq == 0
	case "empty":
		return r.SIPStatus >= 200 && r.SIPStatus <= 299 && r.MANSRTSPStatus == 0 && r.MANSRTSPCSeq == 0
	case "mansrtsp":
		return r.SIPStatus >= 200 && r.SIPStatus <= 299 && r.MANSRTSPStatus >= 100 && r.MANSRTSPStatus <= 599 && r.MANSRTSPCSeq == r.CSeq
	default:
		return false
	}
}

func lastSIPINFOCSeq(step DeviceSIPInviteStep) uint32 {
	if b := step.KnownBranch; b != nil && len(b.InfoSteps) != 0 {
		return b.InfoSteps[len(b.InfoSteps)-1].Identity.Request.Request.CSeq
	}
	return step.Identity.CSeq
}

// Prepare burns a sequence even if dispatch never happens. Only a confirmed
// new CAS and a still-owned, actually ACKed dialog permit the caller to proceed.
// Neither this store nor Load proves the original ACK write succeeded.
func (s *DeviceOperationIntentStore) PrepareSIPINFO(ctx context.Context, id DeviceOperationIntentIdentity, version int64, identity DeviceSIPINFOIdentity) (DeviceSIPInviteSteps, error) {
	runID, err := sipCleanupProcessID()
	if err != nil {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
	}
	identity.Request.Routes = slices.Clone(identity.Request.Routes)
	return s.mutateSIPStepChecked(ctx, id, version, authorizeIntentDevice, func(out *DeviceSIPInviteSteps, now time.Time) (bool, error) {
		for _, step := range out.Steps {
			if step.KnownBranch == nil {
				continue
			}
			for _, old := range step.KnownBranch.InfoSteps {
				if old.Identity.InfoID == identity.InfoID {
					if equalSIPINFOIdentity(old.Identity, identity) {
						return false, nil
					}
					return false, ErrDeviceIntentConflict
				}
			}
		}
		for si := range out.Steps {
			step := &out.Steps[si]
			b := step.KnownBranch
			if step.Identity.StepID != identity.Request.Request.StepID || b == nil {
				continue
			}
			if b.ACKState != SIPStepMayHaveDispatched || len(b.CleanupAttempts) != 0 || len(b.InfoSteps) >= maxSIPINFOSteps {
				return false, ErrDeviceIntentConflict
			}
			if len(b.InfoSteps) != 0 {
				last := b.InfoSteps[len(b.InfoSteps)-1]
				if last.OwnerRunID != runID || last.LocalQuiescedAt == nil || (last.State == SIPStepMayHaveDispatched && last.Response == nil) {
					return false, ErrDeviceIntentConflict
				}
			}
			last := lastSIPINFOCSeq(*step)
			if last == math.MaxUint32 || !sipINFOIdentityMatches(identity, *step, last+1) {
				return false, ErrDeviceIntentConflict
			}
			for _, old := range b.InfoSteps {
				if old.Identity.Request.Request.Branch == identity.Request.Request.Branch {
					return false, ErrDeviceIntentConflict
				}
			}
			b.InfoSteps = append(b.InfoSteps, DeviceSIPINFOStep{Identity: identity, OwnerRunID: runID, State: SIPStepPrepared, RowVersion: 1, PreparedAt: now})
			return true, nil
		}
		return false, ErrDeviceIntentConflict
	})
}

func (s *DeviceOperationIntentStore) mutateSIPINFO(ctx context.Context, id DeviceOperationIntentIdentity, version int64, infoID string, observation bool, mutate func(*DeviceSIPINFOStep, time.Time, string) (bool, error)) (DeviceSIPInviteSteps, error) {
	if !validIntentID(infoID) {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentInvalid
	}
	runID, err := sipCleanupProcessID()
	if err != nil {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
	}
	check := authorizeIntentDevice
	if observation {
		check = observeSIPBranchDevice
	}
	return s.mutateSIPStepChecked(ctx, id, version, check, func(out *DeviceSIPInviteSteps, now time.Time) (bool, error) {
		for si := range out.Steps {
			b := out.Steps[si].KnownBranch
			if b == nil {
				continue
			}
			for ai := range b.InfoSteps {
				a := &b.InfoSteps[ai]
				if a.Identity.InfoID != infoID {
					continue
				}
				if !observation && (len(b.CleanupAttempts) != 0 || a.OwnerRunID != runID || a.LocalQuiescedAt != nil || ai != len(b.InfoSteps)-1) {
					return false, ErrDeviceIntentConflict
				}
				return mutate(a, now, runID)
			}
		}
		return false, ErrDeviceIntentConflict
	})
}

// The confirmed CAS winner alone may create/connect/init its concrete INFO
// transaction. Commit-unknown and later readback grant no network permission.
func (s *DeviceOperationIntentStore) DispatchSIPINFO(ctx context.Context, id DeviceOperationIntentIdentity, version int64, infoID string) (DeviceSIPInviteSteps, error) {
	return s.mutateSIPINFO(ctx, id, version, infoID, false, func(a *DeviceSIPINFOStep, now time.Time, _ string) (bool, error) {
		if a.State != SIPStepPrepared {
			return false, ErrDeviceIntentConflict
		}
		a.State, a.DispatchStartedAt, a.RowVersion = SIPStepMayHaveDispatched, &now, a.RowVersion+1
		return true, nil
	})
}

func (s *DeviceOperationIntentStore) ObserveSIPINFOResponse(ctx context.Context, id DeviceOperationIntentIdentity, version int64, response DeviceSIPINFOResponse) (DeviceSIPInviteSteps, error) {
	return s.mutateSIPINFO(ctx, id, version, response.InfoID, true, func(a *DeviceSIPINFOStep, now time.Time, _ string) (bool, error) {
		if a.State != SIPStepMayHaveDispatched || !sipINFOResponseMatches(response, *a) {
			return false, ErrDeviceIntentConflict
		}
		if a.Response != nil {
			if *a.Response == response {
				return false, nil
			}
			return false, ErrDeviceIntentConflict
		}
		a.Response, a.ResponseObservedAt, a.RowVersion = &response, &now, a.RowVersion+1
		return true, nil
	})
}

// The concrete owner calls this only after its actual transaction AND workflow
// have exited, never from a timeout, persisted flag or another process.
func (s *DeviceOperationIntentStore) ObserveSIPINFOQuiesced(ctx context.Context, id DeviceOperationIntentIdentity, version int64, infoID string) (DeviceSIPInviteSteps, error) {
	return s.mutateSIPINFO(ctx, id, version, infoID, true, func(a *DeviceSIPINFOStep, now time.Time, runID string) (bool, error) {
		if a.OwnerRunID != runID {
			return false, ErrDeviceIntentConflict
		}
		if a.LocalQuiescedAt != nil {
			return false, nil
		}
		a.LocalQuiescedAt, a.RowVersion = &now, a.RowVersion+1
		return true, nil
	})
}
