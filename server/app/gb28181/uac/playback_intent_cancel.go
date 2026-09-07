package uac

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/emiago/sipgo/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func prepareStoredPlaybackCancel(ctx context.Context, store *playauth.DeviceOperationIntentStore, id playauth.DeviceOperationIntentIdentity, version int64, invite playauth.DeviceSIPInviteIdentity, request *sip.Request) (playauth.DeviceSIPInviteSteps, error) {
	if ctx == nil || store == nil || id.Kind != "playback" || id.TargetScope != "channel" {
		return playauth.DeviceSIPInviteSteps{}, errPlaybackIntentSnapshot
	}
	snapshot, err := snapshotPlaybackRequest(request, sip.CANCEL)
	if err != nil {
		return playauth.DeviceSIPInviteSteps{}, err
	}
	// The supported fixed CANCEL has no arbitrary headers, routes or payload.
	for _, header := range request.Headers() {
		switch strings.ToLower(header.Name()) {
		case "via", "from", "to", "call-id", "cseq", "contact", "max-forwards", "content-length":
		default:
			return playauth.DeviceSIPInviteSteps{}, errPlaybackIntentSnapshot
		}
	}
	expected := invite
	empty := sha256.Sum256(nil)
	expected.ContentType, expected.BodyLength, expected.BodySHA256 = "", 0, hex.EncodeToString(empty[:])
	actual := playbackIntentStorageIdentity(invite.StepID, snapshot)
	if actual != expected {
		return playauth.DeviceSIPInviteSteps{}, errPlaybackIntentSnapshot
	}
	return store.PrepareSIPCancel(ctx, id, version, playauth.DeviceSIPCancelIdentity(actual))
}
