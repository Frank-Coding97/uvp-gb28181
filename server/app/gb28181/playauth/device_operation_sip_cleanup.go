package playauth

import (
	"context"
	"math"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"uvplatform.cn/uvp-gb28181/app/openapi/processauthority"
)

const (
	SIPCleanupPrepared      = "prepared"
	SIPCleanupACKDispatched = "ack_may_have_dispatched"
	SIPCleanupBYEDispatched = "bye_may_have_dispatched"
	SIPCleanupBYEObserved   = "bye_2xx_observed"
	maxSIPCleanupAttempts   = 8
)

// One random identity for this actual process, not a caller supplied restart
// assertion. Reconstructing a store in the same process cannot change it.
var sipCleanupProcessID = processauthority.ProcessID

type DeviceSIPCleanupRequestIdentity struct {
	Request   DeviceSIPInviteIdentity `json:"-"`
	RemoteTag string                  `json:"-"`
	Routes    []string                `json:"-"`
}

type DeviceSIPCleanupAttemptIdentity struct {
	AttemptID string                          `json:"-"`
	ACK       DeviceSIPCleanupRequestIdentity `json:"-"`
	BYE       DeviceSIPCleanupRequestIdentity `json:"-"`
}

type DeviceSIPCleanupBYEResponse struct {
	AttemptID  string `json:"-"`
	CallID     string `json:"-"`
	CSeq       uint32 `json:"-"`
	LocalTag   string `json:"-"`
	RemoteTag  string `json:"-"`
	StatusCode int    `json:"-"`
}

type DeviceSIPCleanupAttempt struct {
	Identity             DeviceSIPCleanupAttemptIdentity `json:"-"`
	OwnerRunID           string                          `json:"-"`
	State                string                          `json:"-"`
	RowVersion           int64                           `json:"-"`
	PreparedAt           time.Time                       `json:"-"`
	ACKDispatchStartedAt *time.Time                      `json:"-"`
	BYEDispatchStartedAt *time.Time                      `json:"-"`
	Response             *DeviceSIPCleanupBYEResponse    `json:"-"`
	ResponseObservedAt   *time.Time                      `json:"-"`
	LocalQuiescedAt      *time.Time                      `json:"-"`
}

func cloneSIPCleanupIdentity(i DeviceSIPCleanupAttemptIdentity) DeviceSIPCleanupAttemptIdentity {
	i.ACK.Routes, i.BYE.Routes = slices.Clone(i.ACK.Routes), slices.Clone(i.BYE.Routes)
	return i
}

func equalSIPCleanupIdentity(a, b DeviceSIPCleanupAttemptIdentity) bool {
	return a.AttemptID == b.AttemptID && a.ACK.Request == b.ACK.Request && a.BYE.Request == b.BYE.Request &&
		a.ACK.RemoteTag == b.ACK.RemoteTag && a.BYE.RemoteTag == b.BYE.RemoteTag &&
		slices.Equal(a.ACK.Routes, b.ACK.Routes) && slices.Equal(a.BYE.Routes, b.BYE.Routes)
}

// Validate the current non-RewriteContact UAC builder's exact route shape.
// No arbitrary recovered destination or repaired/stripped header is accepted.
func sipCleanupRequestMatches(i DeviceSIPCleanupRequestIdentity, step DeviceSIPInviteStep, cseq uint32) bool {
	return sipDialogRequestMatches(i, step, cseq, "", 0, sipEmptyBodySHA256)
}

func sipDialogRequestMatches(i DeviceSIPCleanupRequestIdentity, step DeviceSIPInviteStep, cseq uint32, contentType string, bodyLength int, bodySHA256 string) bool {
	b := sipFindBranch(&step, i.RemoteTag)
	if b == nil || i.RemoteTag != b.Identity.RemoteTag || i.Routes == nil ||
		!sipIdentityPart(i.Request.Branch, 128) || !strings.HasPrefix(i.Request.Branch, "z9hG4bK") || i.Request.Branch == step.Identity.Branch {
		return false
	}
	routes := slices.Clone(b.Identity.RouteSet)
	slices.Reverse(routes)
	if !slices.Equal(routes, i.Routes) {
		return false
	}
	requestURI, target := b.Identity.RemoteTarget, b.Identity.RemoteTarget
	if len(routes) != 0 {
		target = routes[0]
		var top sip.Uri
		if sip.ParseUri(target, &top) != nil {
			return false
		}
		if !top.UriParams.Has("lr") {
			requestURI = target
		}
	}
	var uri sip.Uri
	if sip.ParseUri(target, &uri) != nil {
		return false
	}
	port := uri.Port
	if port == 0 {
		port = 5060
	}
	expected := step.Identity
	expected.RequestURI, expected.CSeq, expected.Branch = requestURI, cseq, i.Request.Branch
	expected.Destination = net.JoinHostPort(strings.Trim(uri.Host, "[]"), strconv.Itoa(port))
	expected.MaxForwards = 70
	expected.ContentType, expected.BodyLength, expected.BodySHA256 = contentType, bodyLength, bodySHA256
	return i.Request == expected
}

func sipCleanupIdentityMatches(i DeviceSIPCleanupAttemptIdentity, step DeviceSIPInviteStep, cseq uint32) bool {
	return validIntentID(i.AttemptID) && step.KnownBranch != nil && i.ACK.RemoteTag == step.KnownBranch.Identity.RemoteTag &&
		i.BYE.RemoteTag == i.ACK.RemoteTag && cseq > step.Identity.CSeq &&
		sipCleanupRequestMatches(i.ACK, step, step.Identity.CSeq) && sipCleanupRequestMatches(i.BYE, step, cseq) &&
		i.ACK.Request.Branch != i.BYE.Request.Branch
}

func sipCleanupResponseMatches(r DeviceSIPCleanupBYEResponse, a DeviceSIPCleanupAttempt) bool {
	b := a.Identity.BYE
	return r.AttemptID == a.Identity.AttemptID && r.CallID == b.Request.CallID && r.CSeq == b.Request.CSeq &&
		r.LocalTag == b.Request.LocalTag && r.RemoteTag == b.RemoteTag && r.StatusCode >= 200 && r.StatusCode <= 299
}

// Prepare persists both immutable requests at once; it performs no network.
// A new attempt consumes the next dialog CSeq after every prepared INFO and
// permanently closes business INFO admission, even if this BYE is never sent.
func (s *DeviceOperationIntentStore) PrepareSIPBranchCleanup(ctx context.Context, id DeviceOperationIntentIdentity, version int64, identity DeviceSIPCleanupAttemptIdentity) (DeviceSIPInviteSteps, error) {
	runID, err := sipCleanupProcessID()
	if err != nil {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
	}
	identity = cloneSIPCleanupIdentity(identity)
	return s.mutateSIPStepChecked(ctx, id, version, authorizeSIPCancelCleanupDevice, func(out *DeviceSIPInviteSteps, now time.Time) (bool, error) {
		for index := range out.Steps {
			step := &out.Steps[index]
			for _, b := range sipObservedBranches(step) {
				for _, old := range b.CleanupAttempts {
					if old.Identity.AttemptID == identity.AttemptID {
						if equalSIPCleanupIdentity(old.Identity, identity) {
							return false, nil
						}
						return false, ErrDeviceIntentConflict
					}
				}
			}
		}
		if sipCleanupLocalWorkActive(out, runID) || sipCleanupBranchesUsed(out, identity) {
			return false, ErrDeviceIntentConflict
		}
		for index := range out.Steps {
			step := &out.Steps[index]
			b := sipFindBranch(step, identity.ACK.RemoteTag)
			if step.Identity.StepID != identity.ACK.Request.StepID || b == nil {
				continue
			}
			attempts := b.CleanupAttempts
			if len(attempts) >= maxSIPCleanupAttempts {
				return false, ErrDeviceIntentConflict
			}
			branchStep := *step
			branchStep.KnownBranch = b
			lastCSeq := lastSIPINFOCSeq(branchStep)
			for _, info := range b.InfoSteps {
				if (info.OwnerRunID == runID && info.LocalQuiescedAt == nil) ||
					info.Identity.Request.Request.Branch == identity.ACK.Request.Branch || info.Identity.Request.Request.Branch == identity.BYE.Request.Branch {
					return false, ErrDeviceIntentConflict
				}
			}
			if len(attempts) != 0 {
				last := attempts[len(attempts)-1]
				if last.Response != nil || (last.OwnerRunID == runID && last.LocalQuiescedAt == nil) {
					return false, ErrDeviceIntentConflict
				}
				lastCSeq = last.Identity.BYE.Request.CSeq
			}
			if lastCSeq == math.MaxUint32 || identity.ACK.RemoteTag != identity.BYE.RemoteTag || !sipCleanupIdentityMatches(identity, branchStep, lastCSeq+1) {
				return false, ErrDeviceIntentConflict
			}
			for _, old := range attempts {
				if old.Response != nil {
					return false, ErrDeviceIntentConflict
				}
				if old.Identity.BYE.Request.Branch == identity.BYE.Request.Branch || old.Identity.ACK.Request.Branch == identity.BYE.Request.Branch ||
					old.Identity.BYE.Request.Branch == identity.ACK.Request.Branch {
					return false, ErrDeviceIntentConflict
				}
			}
			b.CleanupAttempts = append(attempts, DeviceSIPCleanupAttempt{Identity: identity, OwnerRunID: runID, State: SIPCleanupPrepared, RowVersion: 1, PreparedAt: now})
			return true, nil
		}
		return false, ErrDeviceIntentConflict
	})
}

func (s *DeviceOperationIntentStore) mutateSIPCleanupAttempt(ctx context.Context, id DeviceOperationIntentIdentity, version int64, attemptID string, observation bool, mutate func(*DeviceSIPCleanupAttempt, time.Time, string) (bool, error)) (DeviceSIPInviteSteps, error) {
	if !validIntentID(attemptID) {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentInvalid
	}
	runID, err := sipCleanupProcessID()
	if err != nil {
		return DeviceSIPInviteSteps{}, ErrDeviceIntentUnavailable
	}
	check := authorizeSIPCancelCleanupDevice
	if observation {
		check = observeSIPBranchDevice
	}
	return s.mutateSIPStepChecked(ctx, id, version, check, func(out *DeviceSIPInviteSteps, now time.Time) (bool, error) {
		for si := range out.Steps {
			for _, b := range sipObservedBranches(&out.Steps[si]) {
				for ai := range b.CleanupAttempts {
					a := &b.CleanupAttempts[ai]
					if a.Identity.AttemptID == attemptID {
						if !observation {
							for _, old := range b.CleanupAttempts {
								if old.Response != nil {
									return false, ErrDeviceIntentConflict
								}
							}
						}
						if !observation && (a.OwnerRunID != runID || a.LocalQuiescedAt != nil || ai != len(b.CleanupAttempts)-1) {
							return false, ErrDeviceIntentConflict
						}
						return mutate(a, now, runID)
					}
				}
			}
		}
		return false, ErrDeviceIntentConflict
	})
}

func (s *DeviceOperationIntentStore) DispatchSIPCleanupACK(ctx context.Context, id DeviceOperationIntentIdentity, version int64, attemptID string) (DeviceSIPInviteSteps, error) {
	return s.mutateSIPCleanupAttempt(ctx, id, version, attemptID, false, func(a *DeviceSIPCleanupAttempt, now time.Time, _ string) (bool, error) {
		if a.State != SIPCleanupPrepared {
			return false, ErrDeviceIntentConflict
		}
		a.State, a.ACKDispatchStartedAt, a.RowVersion = SIPCleanupACKDispatched, &now, a.RowVersion+1
		return true, nil
	})
}

// The concrete owner must additionally hold the successful ACK Write result
// from THIS attempt in THIS process. Persisted ack_may_have is not that proof;
// neither Load nor a restarted owner may invoke this as a recovery executor.
func (s *DeviceOperationIntentStore) DispatchSIPCleanupBYE(ctx context.Context, id DeviceOperationIntentIdentity, version int64, attemptID string) (DeviceSIPInviteSteps, error) {
	return s.mutateSIPCleanupAttempt(ctx, id, version, attemptID, false, func(a *DeviceSIPCleanupAttempt, now time.Time, _ string) (bool, error) {
		if a.State != SIPCleanupACKDispatched {
			return false, ErrDeviceIntentConflict
		}
		a.State, a.BYEDispatchStartedAt, a.RowVersion = SIPCleanupBYEDispatched, &now, a.RowVersion+1
		return true, nil
	})
}

func (s *DeviceOperationIntentStore) ObserveSIPCleanupBYE(ctx context.Context, id DeviceOperationIntentIdentity, version int64, response DeviceSIPCleanupBYEResponse) (DeviceSIPInviteSteps, error) {
	return s.mutateSIPCleanupAttempt(ctx, id, version, response.AttemptID, true, func(a *DeviceSIPCleanupAttempt, now time.Time, _ string) (bool, error) {
		if !sipCleanupResponseMatches(response, *a) {
			return false, ErrDeviceIntentConflict
		}
		if a.Response != nil {
			if *a.Response == response {
				return false, nil
			}
			return false, ErrDeviceIntentConflict
		}
		if a.State != SIPCleanupBYEDispatched {
			return false, ErrDeviceIntentConflict
		}
		a.State, a.Response, a.ResponseObservedAt, a.RowVersion = SIPCleanupBYEObserved, &response, &now, a.RowVersion+1
		return true, nil
	})
}

// Only the current concrete owner may attest its local work has actually
// quiesced. This is not remote completion and must not be called on timeout.
func (s *DeviceOperationIntentStore) ObserveSIPCleanupQuiesced(ctx context.Context, id DeviceOperationIntentIdentity, version int64, attemptID string) (DeviceSIPInviteSteps, error) {
	return s.mutateSIPCleanupAttempt(ctx, id, version, attemptID, true, func(a *DeviceSIPCleanupAttempt, now time.Time, runID string) (bool, error) {
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
