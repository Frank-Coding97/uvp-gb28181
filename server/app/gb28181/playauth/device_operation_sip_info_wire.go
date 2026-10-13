package playauth

import (
	"math"
	"time"
)

type sipINFOCommandWire struct {
	Action               string  `json:"action"`
	PositionNanos        int64   `json:"positionNanos"`
	SegmentDurationNanos int64   `json:"segmentDurationNanos"`
	Scale                float64 `json:"scale"`
}

type sipINFOIdentityWire struct {
	InfoID  string                `json:"infoID"`
	Request sipCleanupRequestWire `json:"request"`
	Command sipINFOCommandWire    `json:"command"`
}

type sipINFOResponseWire struct {
	InfoID         string `json:"infoID"`
	CallID         string `json:"callID"`
	CSeq           uint32 `json:"cseq"`
	LocalTag       string `json:"localTag"`
	RemoteTag      string `json:"remoteTag"`
	SIPStatus      int    `json:"sipStatus"`
	BodyClass      string `json:"bodyClass"`
	MANSRTSPStatus int    `json:"mansrtspStatus"`
	MANSRTSPCSeq   uint32 `json:"mansrtspCseq"`
}

type sipINFOStepWire struct {
	Version            int                  `json:"version"`
	Identity           sipINFOIdentityWire  `json:"identity"`
	OwnerRunID         string               `json:"ownerRunID"`
	State              string               `json:"state"`
	RowVersion         int64                `json:"rowVersion"`
	PreparedAt         time.Time            `json:"preparedAt"`
	DispatchStartedAt  *time.Time           `json:"dispatchStartedAt"`
	Response           *sipINFOResponseWire `json:"response"`
	ResponseObservedAt *time.Time           `json:"responseObservedAt"`
	LocalQuiescedAt    *time.Time           `json:"localQuiescedAt"`
}

func sipINFOStepsToWire(steps []DeviceSIPINFOStep) []sipINFOStepWire {
	var out []sipINFOStepWire
	for _, a := range steps {
		var response *sipINFOResponseWire
		if a.Response != nil {
			r := sipINFOResponseWire(*a.Response)
			response = &r
		}
		i := a.Identity
		identity := sipINFOIdentityWire{i.InfoID, sipCleanupRequestWire{i.Request.Request.wire(), i.Request.RemoteTag, i.Request.Routes}, sipINFOCommandWire(i.Command)}
		out = append(out, sipINFOStepWire{1, identity, a.OwnerRunID, a.State, a.RowVersion, a.PreparedAt,
			a.DispatchStartedAt, response, a.ResponseObservedAt, a.LocalQuiescedAt})
	}
	return out
}

func readSIPINFOSteps(wires []sipINFOStepWire, step DeviceSIPInviteStep, updatedAt time.Time) ([]DeviceSIPINFOStep, error) {
	if len(wires) > maxSIPINFOSteps || (len(wires) != 0 && step.KnownBranch.ACKState != SIPStepMayHaveDispatched) {
		return nil, ErrDeviceIntentUnavailable
	}
	var out []DeviceSIPINFOStep
	lastCSeq := step.Identity.CSeq
	ids, branches := map[string]bool{}, map[string]bool{}
	for _, w := range wires {
		i := DeviceSIPINFOIdentity{w.Identity.InfoID, DeviceSIPCleanupRequestIdentity{DeviceSIPInviteIdentity(w.Identity.Request.Request), w.Identity.Request.RemoteTag, w.Identity.Request.Routes}, DeviceSIPINFOCommand(w.Identity.Command)}
		if w.Version != 1 || !validIntentID(w.OwnerRunID) || ids[i.InfoID] || branches[i.Request.Request.Branch] || lastCSeq == math.MaxUint32 ||
			!sipINFOIdentityMatches(i, step, lastCSeq+1) || !validSIPStepTime(w.PreparedAt) ||
			step.KnownBranch.ACKDispatchStartedAt == nil || w.PreparedAt.Before(*step.KnownBranch.ACKDispatchStartedAt) || w.PreparedAt.After(updatedAt) {
			return nil, ErrDeviceIntentUnavailable
		}
		if len(out) != 0 {
			last := out[len(out)-1]
			if w.OwnerRunID != last.OwnerRunID || last.LocalQuiescedAt == nil || w.PreparedAt.Before(*last.LocalQuiescedAt) ||
				(last.State == SIPStepMayHaveDispatched && (last.ResponseObservedAt == nil || w.PreparedAt.Before(*last.ResponseObservedAt))) {
				return nil, ErrDeviceIntentUnavailable
			}
		}
		a := DeviceSIPINFOStep{Identity: i, OwnerRunID: w.OwnerRunID, State: w.State, RowVersion: w.RowVersion, PreparedAt: w.PreparedAt,
			DispatchStartedAt: w.DispatchStartedAt, ResponseObservedAt: w.ResponseObservedAt, LocalQuiescedAt: w.LocalQuiescedAt}
		version, lastDispatch := int64(1), w.PreparedAt
		switch w.State {
		case SIPStepPrepared:
			if w.DispatchStartedAt != nil {
				return nil, ErrDeviceIntentUnavailable
			}
		case SIPStepMayHaveDispatched:
			if w.DispatchStartedAt == nil || !validSIPStepTime(*w.DispatchStartedAt) || w.DispatchStartedAt.Before(w.PreparedAt) || w.DispatchStartedAt.After(updatedAt) {
				return nil, ErrDeviceIntentUnavailable
			}
			version, lastDispatch = 2, *w.DispatchStartedAt
		default:
			return nil, ErrDeviceIntentUnavailable
		}
		if w.Response != nil {
			r := DeviceSIPINFOResponse(*w.Response)
			if w.State != SIPStepMayHaveDispatched || !sipINFOResponseMatches(r, a) || w.ResponseObservedAt == nil ||
				!validSIPStepTime(*w.ResponseObservedAt) || w.ResponseObservedAt.Before(lastDispatch) || w.ResponseObservedAt.After(updatedAt) {
				return nil, ErrDeviceIntentUnavailable
			}
			a.Response = &r
			version++
		} else if w.ResponseObservedAt != nil {
			return nil, ErrDeviceIntentUnavailable
		}
		if w.LocalQuiescedAt != nil {
			if !validSIPStepTime(*w.LocalQuiescedAt) || w.LocalQuiescedAt.Before(lastDispatch) || w.LocalQuiescedAt.After(updatedAt) {
				return nil, ErrDeviceIntentUnavailable
			}
			version++
		}
		if w.RowVersion != version {
			return nil, ErrDeviceIntentUnavailable
		}
		out = append(out, a)
		ids[i.InfoID], branches[i.Request.Request.Branch], lastCSeq = true, true, i.Request.Request.CSeq
	}
	return out, nil
}
