package uac

import (
	"context"
	"math"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// Only material preparation: no reservation, attempt, lease or network. The
// recovery executor must atomically reserve the shared live registry BEFORE
// calling this, then strongly retain the owner through fresh CAS/lease and
// actual Quiesced. Load cannot revive an old owner or dispatch permission.
func (u *UAC) prepareRecoveredPlaybackCleanup(ctx context.Context, store *playauth.DeviceOperationIntentStore, id playauth.DeviceOperationIntentIdentity, stepID, remoteTag string) (*sipgo.OwnedBranchCleanup, playauth.DeviceSIPInviteSteps, error) {
	if u == nil || u.client == nil || store == nil || !playbackIntentKind(id.Kind) || id.TargetScope != "channel" || remoteTag == "" {
		return nil, playauth.DeviceSIPInviteSteps{}, errPlaybackIntentSnapshot
	}
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	if err != nil {
		return nil, playauth.DeviceSIPInviteSteps{}, err
	}
	for _, step := range loaded.Steps {
		if step.Identity.StepID != stepID || step.State != playauth.SIPStepMayHaveDispatched {
			continue
		}
		var branch *playauth.DeviceSIPKnownBranch
		if step.KnownBranch != nil && step.KnownBranch.Identity.RemoteTag == remoteTag {
			branch = step.KnownBranch
		}
		for index := range step.AdditionalBranches {
			if step.AdditionalBranches[index].Identity.RemoteTag == remoteTag {
				branch = &step.AdditionalBranches[index]
			}
		}
		if branch == nil {
			break
		}
		last := playbackLastINFOCSeq(step.Identity.CSeq, branch)
		for _, old := range branch.CleanupAttempts {
			if old.Response != nil {
				return nil, playauth.DeviceSIPInviteSteps{}, ErrPlaybackCleanupUnknown
			}
			last = old.Identity.BYE.Request.CSeq
		}
		if last == math.MaxUint32 {
			return nil, playauth.DeviceSIPInviteSteps{}, ErrPlaybackCleanupUnknown
		}
		ack, err := recoveredPlaybackCleanupRequest(step.Identity, branch.Identity, sip.ACK, step.Identity.CSeq)
		if err != nil {
			return nil, playauth.DeviceSIPInviteSteps{}, err
		}
		bye, err := recoveredPlaybackCleanupRequest(step.Identity, branch.Identity, sip.BYE, last+1)
		if err != nil {
			return nil, playauth.DeviceSIPInviteSteps{}, err
		}
		owner, err := (&sipgo.DialogUA{Client: u.client}).NewFixedBranchCleanup(ack, bye)
		if err != nil {
			return nil, playauth.DeviceSIPInviteSteps{}, err
		}
		return owner, loaded, nil
	}
	return nil, playauth.DeviceSIPInviteSteps{}, ErrPlaybackCleanupUnknown
}

// Inputs are selected only from strict durable Load above. These are new
// empty-body compensation requests, never a synthetic INVITE or observation.
func recoveredPlaybackCleanupRequest(i playauth.DeviceSIPInviteIdentity, b playauth.DeviceSIPKnownBranchIdentity, method sip.RequestMethod, cseq uint32) (*sip.Request, error) {
	if method != sip.ACK && method != sip.BYE {
		return nil, errPlaybackIntentSnapshot
	}
	var target, from, to, contact sip.Uri
	for _, field := range []struct {
		raw         string
		destination *sip.Uri
	}{{b.RemoteTarget, &target}, {i.FromURI, &from}, {i.ToURI, &to}, {i.ContactURI, &contact}} {
		if err := sip.ParseUri(field.raw, field.destination); err != nil {
			return nil, errPlaybackIntentSnapshot
		}
	}
	routes := slices.Clone(b.RouteSet)
	slices.Reverse(routes)
	routeURIs := make([]sip.Uri, len(routes))
	for index, raw := range routes {
		if err := sip.ParseUri(raw, &routeURIs[index]); err != nil {
			return nil, errPlaybackIntentSnapshot
		}
	}
	destination := target
	if len(routeURIs) != 0 {
		destination = routeURIs[0]
		if !destination.UriParams.Has("lr") {
			target = destination
		}
	}
	r := sip.NewRequest(method, target)
	viaParams := sip.NewParams()
	viaParams.Add("branch", sip.GenerateBranchN(16))
	if i.RPortPresent {
		viaParams.Add("rport", i.RPortValue)
	}
	r.AppendHeader(&sip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: i.ViaTransport, Host: i.ViaHost, Port: i.ViaPort, Params: viaParams})
	fromParams, toParams := sip.NewParams(), sip.NewParams()
	fromParams.Add("tag", i.LocalTag)
	toParams.Add("tag", b.RemoteTag)
	r.AppendHeader(&sip.FromHeader{Address: from, Params: fromParams})
	r.AppendHeader(&sip.ToHeader{Address: to, Params: toParams})
	callID, maxForwards := sip.CallIDHeader(i.CallID), sip.MaxForwardsHeader(70)
	r.AppendHeader(&callID)
	r.AppendHeader(&sip.CSeqHeader{SeqNo: cseq, MethodName: method})
	r.AppendHeader(&maxForwards)
	r.AppendHeader(&sip.ContactHeader{Address: contact})
	for _, route := range routeURIs {
		r.AppendHeader(&sip.RouteHeader{Address: route})
	}
	r.SetBody(nil)
	r.SetTransport(i.Transport)
	port := destination.Port
	if port == 0 {
		port = 5060
	}
	r.SetDestination(net.JoinHostPort(strings.Trim(destination.Host, "[]"), strconv.Itoa(port)))
	return r, nil
}
