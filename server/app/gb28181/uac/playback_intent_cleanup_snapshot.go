package uac

import (
	"strings"

	"github.com/emiago/sipgo/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// Extract exact cleanup request evidence; never build, normalize, dispatch or
// infer permission. PrepareSIPBranchCleanup still binds it to the stored branch.
func snapshotPlaybackCleanupRequest(r *sip.Request, method sip.RequestMethod, stepID string) (playauth.DeviceSIPCleanupRequestIdentity, error) {
	if method != sip.ACK && method != sip.BYE {
		return playauth.DeviceSIPCleanupRequestIdentity{}, errPlaybackIntentSnapshot
	}
	return snapshotPlaybackDialogRequest(r, method, stepID)
}

func snapshotPlaybackINFORequest(r *sip.Request, stepID string) (playauth.DeviceSIPCleanupRequestIdentity, error) {
	return snapshotPlaybackDialogRequest(r, sip.INFO, stepID)
}

func snapshotPlaybackDialogRequest(r *sip.Request, method sip.RequestMethod, stepID string) (playauth.DeviceSIPCleanupRequestIdentity, error) {
	snapshot, err := snapshotPlaybackRequest(r, method)
	if err != nil {
		return playauth.DeviceSIPCleanupRequestIdentity{}, err
	}
	for _, h := range r.Headers() {
		switch strings.ToLower(h.Name()) {
		case "via", "from", "to", "call-id", "cseq", "contact", "max-forwards", "content-length", "route":
		case "content-type":
			if method != sip.INFO {
				return playauth.DeviceSIPCleanupRequestIdentity{}, errPlaybackIntentSnapshot
			}
		default:
			return playauth.DeviceSIPCleanupRequestIdentity{}, errPlaybackIntentSnapshot
		}
	}
	routes := make([]string, 0)
	for _, h := range r.GetHeaders("Route") {
		var uri sip.Uri
		params := sip.NewParams()
		name, err := sip.ParseAddressValue(h.Value(), &uri, &params)
		if err != nil || name != "" || len(params) != 0 || uri.Password != "" || len(uri.Headers) != 0 {
			return playauth.DeviceSIPCleanupRequestIdentity{}, errPlaybackIntentSnapshot
		}
		routes = append(routes, uri.String())
	}
	if len(routes) > 8 {
		return playauth.DeviceSIPCleanupRequestIdentity{}, errPlaybackIntentSnapshot
	}
	return playauth.DeviceSIPCleanupRequestIdentity{Request: playbackIntentStorageIdentity(stepID, snapshot), RemoteTag: singlePlaybackBranchTag(r.To().Params), Routes: routes}, nil
}
