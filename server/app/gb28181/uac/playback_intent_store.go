package uac

import (
	"context"
	"encoding/hex"

	"github.com/emiago/sipgo/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// prepareStoredPlaybackInvite persists the exact prepared request, without
// starting a transaction. Its result is not dispatch permission: the eventual
// device-bound owner must retain this request and obtain its one-shot CAS before
// sending. In particular, failed or unknown persistence never returns a request.
func (u *UAC) prepareStoredPlaybackInvite(ctx context.Context, store *playauth.DeviceOperationIntentStore, id playauth.DeviceOperationIntentIdentity, version int64, stepID string, in PlaybackInviteRequest) (*sip.Request, playauth.DeviceSIPInviteSteps, error) {
	if ctx == nil || store == nil || u == nil || u.client == nil || !playbackIntentKind(id.Kind) || id.TargetScope != "channel" || in.DeviceID != id.DeviceCode || in.ChannelID != id.TargetCode {
		return nil, playauth.DeviceSIPInviteSteps{}, playauth.ErrDeviceIntentInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, playauth.DeviceSIPInviteSteps{}, err
	}
	request, _, err := u.buildPlaybackInviteRequest(in)
	if err != nil {
		return nil, playauth.DeviceSIPInviteSteps{}, err
	}
	prepared, snapshot, err := u.preparePlaybackIntentSnapshot(request)
	if err != nil {
		return nil, playauth.DeviceSIPInviteSteps{}, err
	}
	stored, err := store.AddSIPInviteStep(ctx, id, version, playbackIntentStorageIdentity(stepID, snapshot))
	if err != nil {
		return nil, playauth.DeviceSIPInviteSteps{}, err
	}
	return prepared, stored, nil
}

func playbackIntentStorageIdentity(stepID string, snapshot playbackIntentSnapshot) playauth.DeviceSIPInviteIdentity {
	return playauth.DeviceSIPInviteIdentity{
		StepID: stepID, CallID: snapshot.callID, CSeq: snapshot.cseq,
		RequestURI: snapshot.requestURI, FromURI: snapshot.fromURI, LocalTag: snapshot.localTag,
		ToURI: snapshot.toURI, ContactURI: snapshot.contactURI,
		Transport: snapshot.transport, Destination: snapshot.destination,
		ViaHost: snapshot.viaHost, ViaPort: snapshot.viaPort, ViaTransport: snapshot.viaTransport,
		Branch: snapshot.branch, RPortPresent: snapshot.rportPresent, RPortValue: snapshot.rportValue,
		MaxForwards: snapshot.maxForwards, ContentType: snapshot.contentType,
		BodyLength: snapshot.bodyLength, BodySHA256: hex.EncodeToString(snapshot.bodySHA256[:]),
	}
}
