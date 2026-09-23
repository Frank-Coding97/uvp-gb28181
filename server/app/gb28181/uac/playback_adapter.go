package uac

import (
	"context"
	"errors"
	"time"

	gbplayback "uvplatform.cn/uvp-gb28181/app/gb28181/playback"
)

// PlaybackAdapter bridges the SIP UAC dialog to the playback service's narrow
// invite/control contracts. It keeps protocol details in the uac package.
type PlaybackAdapter struct {
	UAC *UAC
}

func NewPlaybackAdapter(u *UAC) *PlaybackAdapter { return &PlaybackAdapter{UAC: u} }

func (a *PlaybackAdapter) Invite(ctx context.Context, in gbplayback.UACInvite) (gbplayback.DialogInfo, error) {
	metadata, err := a.UAC.InvitePlayback(ctx, PlaybackInviteRequest{
		DeviceID: in.DeviceID, ChannelID: in.ChannelID, Destination: in.Destination,
		Transport: in.Transport, SSRC: in.SSRC, SDP: in.SDP,
	})
	if err != nil {
		// Only return a cleanup handle that the UAC actually retained. A
		// generated Call-ID alone (e.g. a rejected INVITE) is not such a handle.
		if errors.Is(err, ErrPlaybackACKPending) && a.UAC != nil && a.UAC.playbackDialogs.get(metadata.CallID) != nil {
			return gbplayback.DialogInfo{CallID: metadata.CallID, CleanupRequired: true}, err
		}
		return gbplayback.DialogInfo{}, err
	}
	return gbplayback.DialogInfo{CallID: metadata.CallID}, nil
}

func (a *PlaybackAdapter) Teardown(ctx context.Context, callID string) error {
	return a.UAC.TeardownPlayback(ctx, callID)
}

func (a *PlaybackAdapter) Action(ctx context.Context, callID, action string, positionSeconds, scale float64, segmentDuration time.Duration) (float64, float64, error) {
	return a.UAC.Action(ctx, callID, action, positionSeconds, scale, segmentDuration)
}
