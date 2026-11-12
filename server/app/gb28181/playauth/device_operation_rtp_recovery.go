package playauth

import "time"

const (
	rtpCleanupResource = "close_resource"
	rtpCleanupIngress  = "close_ingress"
	rtpCallNotInvoked  = "not_invoked"
	rtpCallObserved    = "response_observed"
	rtpCallUnknown     = "response_unknown"
)

// A successful conditional fence is sub-evidence, not remote completion.
// Transport/runtime mismatches cannot erase it; a UDP drain is stronger only
// for ingress. There is deliberately no rank that means "device complete".
func rtpCleanupEvidenceRank(result string) int {
	switch result {
	case "rtp_ingress_drained":
		return 2
	case "shutdown_scheduled", "close_pending", "not_active_fenced":
		return 1
	default:
		return 0
	}
}

// DeviceRTPRecovery is observation only. Neither this snapshot nor a process ID
// reconstructed from storage can grant ownership or permission to send.
type DeviceRTPRecovery struct {
	Version                    int                       `json:"-"`
	Generation                 int64                     `json:"-"`
	OwnerProcessID             string                    `json:"-"`
	OwnerRunID                 string                    `json:"-"`
	ReservedAt                 time.Time                 `json:"-"`
	LocalQuiescedAt            *time.Time                `json:"-"`
	CallSequence               int64                     `json:"-"`
	CurrentCall                *DeviceRTPCleanupCall     `json:"-"`
	ResourceEvidence           *DeviceRTPCleanupEvidence `json:"-"`
	IngressEvidence            *DeviceRTPCleanupEvidence `json:"-"`
	PriorObservationIncomplete bool                      `json:"-"`
}

type DeviceRTPCleanupCall struct {
	Sequence          int64      `json:"-"`
	Action            string     `json:"-"`
	DispatchStartedAt time.Time  `json:"-"`
	Outcome           string     `json:"-"`
	Result            string     `json:"-"`
	ResultObservedAt  *time.Time `json:"-"`
	LocalQuiescedAt   *time.Time `json:"-"`
}

// Evidence is a sub-result, never proof that the whole device is clean.
type DeviceRTPCleanupEvidence struct {
	Sequence   int64     `json:"-"`
	Result     string    `json:"-"`
	ObservedAt time.Time `json:"-"`
}

type rtpRecoveryWire struct {
	Version                    int                     `json:"version"`
	Generation                 int64                   `json:"generation"`
	OwnerProcessID             string                  `json:"ownerProcessID"`
	OwnerRunID                 string                  `json:"ownerRunID"`
	ReservedAt                 time.Time               `json:"reservedAt"`
	LocalQuiescedAt            *time.Time              `json:"localQuiescedAt,omitempty"`
	CallSequence               int64                   `json:"callSequence"`
	CurrentCall                *rtpCleanupCallWire     `json:"currentCall,omitempty"`
	ResourceEvidence           *rtpCleanupEvidenceWire `json:"resourceEvidence,omitempty"`
	IngressEvidence            *rtpCleanupEvidenceWire `json:"ingressEvidence,omitempty"`
	PriorObservationIncomplete bool                    `json:"priorObservationIncomplete,omitempty"`
}

type rtpCleanupCallWire struct {
	Sequence          int64      `json:"sequence"`
	Action            string     `json:"action"`
	DispatchStartedAt time.Time  `json:"dispatchStartedAt"`
	Outcome           string     `json:"outcome,omitempty"`
	Result            string     `json:"result,omitempty"`
	ResultObservedAt  *time.Time `json:"resultObservedAt,omitempty"`
	LocalQuiescedAt   *time.Time `json:"localQuiescedAt,omitempty"`
}

type rtpCleanupEvidenceWire struct {
	Sequence   int64     `json:"sequence"`
	Result     string    `json:"result"`
	ObservedAt time.Time `json:"observedAt"`
}

func recoveryToWire(r *DeviceRTPRecovery) *rtpRecoveryWire {
	if r == nil {
		return nil
	}
	w := &rtpRecoveryWire{Version: r.Version, Generation: r.Generation, OwnerProcessID: r.OwnerProcessID, OwnerRunID: r.OwnerRunID,
		ReservedAt: r.ReservedAt, LocalQuiescedAt: r.LocalQuiescedAt, CallSequence: r.CallSequence, PriorObservationIncomplete: r.PriorObservationIncomplete}
	if r.CurrentCall != nil {
		v := rtpCleanupCallWire(*r.CurrentCall)
		w.CurrentCall = &v
	}
	if r.ResourceEvidence != nil {
		v := rtpCleanupEvidenceWire(*r.ResourceEvidence)
		w.ResourceEvidence = &v
	}
	if r.IngressEvidence != nil {
		v := rtpCleanupEvidenceWire(*r.IngressEvidence)
		w.IngressEvidence = &v
	}
	return w
}

func (w *rtpRecoveryWire) recovery() *DeviceRTPRecovery {
	if w == nil {
		return nil
	}
	r := &DeviceRTPRecovery{Version: w.Version, Generation: w.Generation, OwnerProcessID: w.OwnerProcessID, OwnerRunID: w.OwnerRunID,
		ReservedAt: w.ReservedAt, LocalQuiescedAt: w.LocalQuiescedAt, CallSequence: w.CallSequence, PriorObservationIncomplete: w.PriorObservationIncomplete}
	if w.CurrentCall != nil {
		v := DeviceRTPCleanupCall(*w.CurrentCall)
		r.CurrentCall = &v
	}
	if w.ResourceEvidence != nil {
		v := DeviceRTPCleanupEvidence(*w.ResourceEvidence)
		r.ResourceEvidence = &v
	}
	if w.IngressEvidence != nil {
		v := DeviceRTPCleanupEvidence(*w.IngressEvidence)
		r.IngressEvidence = &v
	}
	return r
}

func validRTPRecovery(step DeviceRTPResourceStep, updated time.Time) bool {
	r := step.Recovery
	if r == nil {
		return true
	}
	if step.State != RTPStepMayHaveDispatched || step.DispatchStartedAt == nil || r.Version != 1 || r.Generation <= 0 ||
		!validIntentID(r.OwnerProcessID) || !validIntentID(r.OwnerRunID) || r.CallSequence < 0 {
		return false
	}
	between := func(value time.Time, start time.Time) bool {
		return validSIPStepTime(value) && !value.Before(start) && !value.After(updated)
	}
	if !between(r.ReservedAt, *step.DispatchStartedAt) || (step.LocalQuiescedAt != nil && r.ReservedAt.Before(*step.LocalQuiescedAt)) {
		return false
	}
	if r.LocalQuiescedAt != nil && !between(*r.LocalQuiescedAt, r.ReservedAt) {
		return false
	}
	c := r.CurrentCall
	if c != nil {
		if c.Sequence <= 0 || c.Sequence != r.CallSequence || (c.Action != rtpCleanupResource && c.Action != rtpCleanupIngress) || !between(c.DispatchStartedAt, r.ReservedAt) {
			return false
		}
		if c.Outcome == "" {
			if c.Result != "" || c.ResultObservedAt != nil || c.LocalQuiescedAt != nil || r.LocalQuiescedAt != nil {
				return false
			}
		} else {
			if c.LocalQuiescedAt == nil || !between(*c.LocalQuiescedAt, c.DispatchStartedAt) {
				return false
			}
			switch c.Outcome {
			case rtpCallObserved:
				if !validRTPCloseResult(c.Result, c.Action == rtpCleanupIngress) || c.ResultObservedAt == nil ||
					!between(*c.ResultObservedAt, c.DispatchStartedAt) || c.ResultObservedAt.After(*c.LocalQuiescedAt) ||
					(c.Result == "rtp_ingress_drained" && step.Identity.TCPMode != 0) {
					return false
				}
			case rtpCallNotInvoked, rtpCallUnknown:
				if c.Result != "" || c.ResultObservedAt != nil {
					return false
				}
			default:
				return false
			}
			if r.LocalQuiescedAt != nil && r.LocalQuiescedAt.Before(*c.LocalQuiescedAt) {
				return false
			}
		}
	}
	if r.Generation == 1 && c == nil && r.CallSequence != 0 {
		return false
	}
	for index, e := range []*DeviceRTPCleanupEvidence{r.ResourceEvidence, r.IngressEvidence} {
		if e == nil {
			continue
		}
		if e.Sequence <= 0 || e.Sequence > r.CallSequence || !between(e.ObservedAt, *step.DispatchStartedAt) ||
			!validRTPCloseResult(e.Result, index == 1) || (e.Result == "rtp_ingress_drained" && step.Identity.TCPMode != 0) {
			return false
		}
		if c != nil && e.Sequence == c.Sequence {
			action := rtpCleanupResource
			if index == 1 {
				action = rtpCleanupIngress
			}
			if c.Action != action || c.Outcome != rtpCallObserved || c.Result != e.Result || c.ResultObservedAt == nil || !c.ResultObservedAt.Equal(e.ObservedAt) {
				return false
			}
		} else {
			limit := r.ReservedAt
			if c != nil {
				limit = c.DispatchStartedAt
			}
			if e.ObservedAt.After(limit) {
				return false
			}
		}
	}
	if c != nil && c.Outcome == rtpCallObserved {
		e := r.ResourceEvidence
		if c.Action == rtpCleanupIngress {
			e = r.IngressEvidence
		}
		// A later weaker response cannot erase earlier fence/drain sub-evidence.
		if e == nil || (e.Sequence != c.Sequence && rtpCleanupEvidenceRank(e.Result) <= rtpCleanupEvidenceRank(c.Result)) {
			return false
		}
	}
	return true
}
