package playauth

import (
	"math"
	"time"
)

type sipCleanupRequestWire struct {
	Request   sipInviteIdentityWire `json:"request"`
	RemoteTag string                `json:"remoteTag"`
	Routes    []string              `json:"routes"`
}

type sipCleanupIdentityWire struct {
	AttemptID string                `json:"attemptID"`
	ACK       sipCleanupRequestWire `json:"ack"`
	BYE       sipCleanupRequestWire `json:"bye"`
}

type sipCleanupResponseWire struct {
	AttemptID  string `json:"attemptID"`
	CallID     string `json:"callID"`
	CSeq       uint32 `json:"cseq"`
	LocalTag   string `json:"localTag"`
	RemoteTag  string `json:"remoteTag"`
	StatusCode int    `json:"statusCode"`
}

type sipCleanupAttemptWire struct {
	Version              int                     `json:"version"`
	Identity             sipCleanupIdentityWire  `json:"identity"`
	OwnerRunID           string                  `json:"ownerRunID"`
	State                string                  `json:"state"`
	RowVersion           int64                   `json:"rowVersion"`
	PreparedAt           time.Time               `json:"preparedAt"`
	ACKDispatchStartedAt *time.Time              `json:"ackDispatchStartedAt"`
	BYEDispatchStartedAt *time.Time              `json:"byeDispatchStartedAt"`
	Response             *sipCleanupResponseWire `json:"response"`
	ResponseObservedAt   *time.Time              `json:"responseObservedAt"`
	LocalQuiescedAt      *time.Time              `json:"localQuiescedAt"`
}

func sipCleanupAttemptsToWire(attempts []DeviceSIPCleanupAttempt) []sipCleanupAttemptWire {
	var out []sipCleanupAttemptWire
	for _, a := range attempts {
		var response *sipCleanupResponseWire
		if a.Response != nil {
			r := sipCleanupResponseWire(*a.Response)
			response = &r
		}
		identity := sipCleanupIdentityWire{a.Identity.AttemptID,
			sipCleanupRequestWire{a.Identity.ACK.Request.wire(), a.Identity.ACK.RemoteTag, a.Identity.ACK.Routes},
			sipCleanupRequestWire{a.Identity.BYE.Request.wire(), a.Identity.BYE.RemoteTag, a.Identity.BYE.Routes}}
		out = append(out, sipCleanupAttemptWire{1, identity, a.OwnerRunID, a.State, a.RowVersion, a.PreparedAt,
			a.ACKDispatchStartedAt, a.BYEDispatchStartedAt, response, a.ResponseObservedAt, a.LocalQuiescedAt})
	}
	return out
}

func readSIPCleanupAttempts(wires []sipCleanupAttemptWire, step DeviceSIPInviteStep, updatedAt time.Time) ([]DeviceSIPCleanupAttempt, error) {
	if len(wires) > maxSIPCleanupAttempts {
		return nil, ErrDeviceIntentUnavailable
	}
	var out []DeviceSIPCleanupAttempt
	lastCSeq := step.Identity.CSeq
	ids, byeBranches := map[string]bool{}, map[string]bool{}
	for _, w := range wires {
		i := DeviceSIPCleanupAttemptIdentity{w.Identity.AttemptID,
			DeviceSIPCleanupRequestIdentity{DeviceSIPInviteIdentity(w.Identity.ACK.Request), w.Identity.ACK.RemoteTag, w.Identity.ACK.Routes},
			DeviceSIPCleanupRequestIdentity{DeviceSIPInviteIdentity(w.Identity.BYE.Request), w.Identity.BYE.RemoteTag, w.Identity.BYE.Routes}}
		if w.Version != 1 || !validIntentID(w.OwnerRunID) || ids[i.AttemptID] || lastCSeq == math.MaxUint32 ||
			!sipCleanupIdentityMatches(i, step, lastCSeq+1) || byeBranches[i.BYE.Request.Branch] || byeBranches[i.ACK.Request.Branch] ||
			!validSIPStepTime(w.PreparedAt) || w.PreparedAt.Before(step.KnownBranch.ObservedAt) || w.PreparedAt.After(updatedAt) ||
			(step.KnownBranch.ACKDispatchStartedAt != nil && w.PreparedAt.Before(*step.KnownBranch.ACKDispatchStartedAt)) {
			return nil, ErrDeviceIntentUnavailable
		}
		for _, old := range out {
			if old.Identity.ACK.Request.Branch == i.BYE.Request.Branch || (old.ResponseObservedAt != nil && old.ResponseObservedAt.Before(w.PreparedAt)) {
				return nil, ErrDeviceIntentUnavailable
			}
		}
		if len(out) != 0 {
			previous := out[len(out)-1]
			if w.PreparedAt.Before(previous.PreparedAt) || (w.OwnerRunID == previous.OwnerRunID &&
				(previous.LocalQuiescedAt == nil || w.PreparedAt.Before(*previous.LocalQuiescedAt))) {
				return nil, ErrDeviceIntentUnavailable
			}
		}
		a := DeviceSIPCleanupAttempt{Identity: i, OwnerRunID: w.OwnerRunID, State: w.State, RowVersion: w.RowVersion, PreparedAt: w.PreparedAt,
			ACKDispatchStartedAt: w.ACKDispatchStartedAt, BYEDispatchStartedAt: w.BYEDispatchStartedAt, ResponseObservedAt: w.ResponseObservedAt, LocalQuiescedAt: w.LocalQuiescedAt}
		version := int64(1)
		lastDispatch := w.PreparedAt
		if w.State != SIPCleanupPrepared {
			if w.ACKDispatchStartedAt == nil || !validSIPStepTime(*w.ACKDispatchStartedAt) || w.ACKDispatchStartedAt.Before(lastDispatch) || w.ACKDispatchStartedAt.After(updatedAt) {
				return nil, ErrDeviceIntentUnavailable
			}
			lastDispatch, version = *w.ACKDispatchStartedAt, 2
		} else if w.ACKDispatchStartedAt != nil {
			return nil, ErrDeviceIntentUnavailable
		}
		if w.State == SIPCleanupBYEDispatched || w.State == SIPCleanupBYEObserved {
			if w.BYEDispatchStartedAt == nil || !validSIPStepTime(*w.BYEDispatchStartedAt) || w.BYEDispatchStartedAt.Before(lastDispatch) || w.BYEDispatchStartedAt.After(updatedAt) {
				return nil, ErrDeviceIntentUnavailable
			}
			lastDispatch, version = *w.BYEDispatchStartedAt, 3
		} else if w.BYEDispatchStartedAt != nil {
			return nil, ErrDeviceIntentUnavailable
		}
		if w.State == SIPCleanupBYEObserved {
			if w.Response == nil || w.ResponseObservedAt == nil || !validSIPStepTime(*w.ResponseObservedAt) || w.ResponseObservedAt.Before(lastDispatch) || w.ResponseObservedAt.After(updatedAt) {
				return nil, ErrDeviceIntentUnavailable
			}
			r := DeviceSIPCleanupBYEResponse(*w.Response)
			if !sipCleanupResponseMatches(r, a) {
				return nil, ErrDeviceIntentUnavailable
			}
			a.Response, version = &r, 4
		} else if w.Response != nil || w.ResponseObservedAt != nil {
			return nil, ErrDeviceIntentUnavailable
		}
		if w.State != SIPCleanupPrepared && w.State != SIPCleanupACKDispatched && w.State != SIPCleanupBYEDispatched && w.State != SIPCleanupBYEObserved {
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
		ids[i.AttemptID], byeBranches[i.BYE.Request.Branch], lastCSeq = true, true, i.BYE.Request.CSeq
	}
	return out, nil
}
