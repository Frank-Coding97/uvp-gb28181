package uac

import (
	"context"

	"github.com/emiago/sipgo/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// observeStoredPlaybackBranch only records the first known response. The
// eventual transaction owner must separately obtain ACK permission and retain
// ownership on any error. This adapter never calls ACK/Close or publishes a
// dialog; missing or unsupported material remains unresolved.
func observeStoredPlaybackBranch(ctx context.Context, store *playauth.DeviceOperationIntentStore, id playauth.DeviceOperationIntentIdentity, version int64, invite playauth.DeviceSIPInviteIdentity, request *sip.Request, response *sip.Response) (playauth.DeviceSIPInviteSteps, error) {
	return observeStoredPlaybackBranchWith(ctx, store, id, version, invite, request, response, false)
}

func observeStoredPlaybackBranchWith(ctx context.Context, store *playauth.DeviceOperationIntentStore, id playauth.DeviceOperationIntentIdentity, version int64, invite playauth.DeviceSIPInviteIdentity, request *sip.Request, response *sip.Response, additional bool) (playauth.DeviceSIPInviteSteps, error) {
	if store == nil || ctx == nil || !playbackIntentKind(id.Kind) || id.TargetScope != "channel" || response == nil || response.StatusCode < 200 || response.StatusCode > 299 {
		return playauth.DeviceSIPInviteSteps{}, errPlaybackIntentSnapshot
	}
	if err := ctx.Err(); err != nil {
		return playauth.DeviceSIPInviteSteps{}, err
	}
	snapshot, err := snapshotPlaybackIntentRequest(request)
	if err != nil || playbackIntentStorageIdentity(invite.StepID, snapshot) != invite {
		return playauth.DeviceSIPInviteSteps{}, errPlaybackIntentSnapshot
	}
	identity, err := snapshotPlaybackBranch(invite, response)
	if err != nil {
		return playauth.DeviceSIPInviteSteps{}, err
	}
	if additional {
		return store.ObserveSIPAdditionalBranch(ctx, id, version, identity)
	}
	return store.ObserveSIPKnownBranch(ctx, id, version, identity)
}

// Extracts an actual response against an already validated immutable identity.
// The live caller additionally proves its original request; recovery must use
// strict Load plus detached transport/source observation, never a fake request.
func snapshotPlaybackBranch(invite playauth.DeviceSIPInviteIdentity, response *sip.Response) (playauth.DeviceSIPKnownBranchIdentity, error) {
	if response == nil || !response.IsSuccess() {
		return playauth.DeviceSIPKnownBranchIdentity{}, errPlaybackIntentSnapshot
	}
	for _, header := range []string{"Call-ID", "CSeq", "From", "To", "Contact"} {
		if len(response.GetHeaders(header)) != 1 {
			return playauth.DeviceSIPKnownBranchIdentity{}, errPlaybackIntentSnapshot
		}
	}
	callID, cseq, from, to, contact := response.CallID(), response.CSeq(), response.From(), response.To(), response.Contact()
	if callID == nil || cseq == nil || from == nil || to == nil || contact == nil || string(*callID) != invite.CallID ||
		cseq.MethodName != sip.INVITE || cseq.SeqNo != invite.CSeq || from.Address.String() != invite.FromURI || to.Address.String() != invite.ToURI {
		return playauth.DeviceSIPKnownBranchIdentity{}, errPlaybackIntentSnapshot
	}
	localTag, remoteTag := singlePlaybackBranchTag(from.Params), singlePlaybackBranchTag(to.Params)
	if localTag != invite.LocalTag || remoteTag == "" {
		return playauth.DeviceSIPKnownBranchIdentity{}, errPlaybackIntentSnapshot
	}
	routes := make([]string, 0)
	for _, header := range response.GetHeaders("Record-Route") {
		route, ok := header.(*sip.RecordRouteHeader)
		if !ok || route == nil {
			return playauth.DeviceSIPKnownBranchIdentity{}, errPlaybackIntentSnapshot
		}
		routes = append(routes, route.Address.String())
	}
	identity := playauth.DeviceSIPKnownBranchIdentity{
		InviteStepID: invite.StepID, CallID: invite.CallID, LocalTag: localTag, RemoteTag: remoteTag,
		CSeq: invite.CSeq, StatusCode: response.StatusCode, RemoteTarget: contact.Address.String(), RouteSet: routes,
	}
	if err := playauth.ValidateSIPBranchObservation(identity); err != nil {
		return playauth.DeviceSIPKnownBranchIdentity{}, errPlaybackIntentSnapshot
	}
	return identity, nil
}

func snapshotRecoveredPlaybackBranch(invite playauth.DeviceSIPInviteIdentity, response *sip.Response) (playauth.DeviceSIPKnownBranchIdentity, error) {
	if response == nil || len(response.GetHeaders("Via")) != 1 {
		return playauth.DeviceSIPKnownBranchIdentity{}, errPlaybackIntentSnapshot
	}
	via := response.Via()
	if via == nil || via.ProtocolName != "SIP" || via.ProtocolVersion != "2.0" || via.Transport != invite.ViaTransport || via.Host != invite.ViaHost || via.Port != invite.ViaPort {
		return playauth.DeviceSIPKnownBranchIdentity{}, errPlaybackIntentSnapshot
	}
	count := 0
	for _, p := range via.Params {
		if p.K == "branch" {
			count++
			if p.V != invite.Branch {
				return playauth.DeviceSIPKnownBranchIdentity{}, errPlaybackIntentSnapshot
			}
		}
	}
	if count != 1 {
		return playauth.DeviceSIPKnownBranchIdentity{}, errPlaybackIntentSnapshot
	}
	return snapshotPlaybackBranch(invite, response)
}

func singlePlaybackBranchTag(params sip.HeaderParams) string {
	var tag string
	count := 0
	for _, param := range params {
		if param.K == "tag" {
			tag = param.V
			count++
		}
	}
	if count != 1 {
		return ""
	}
	return tag
}
