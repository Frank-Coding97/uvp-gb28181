package talk

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type recordingBroadcastMedia struct {
	*fakeActivationMedia
	request zlm.BroadcastSendRtpRequest
}

func (f *recordingBroadcastMedia) StartBroadcastSendRtp(ctx context.Context, in zlm.BroadcastSendRtpRequest) (*zlm.StartBroadcastSendRtpResult, error) {
	f.request = in
	return f.fakeActivationMedia.StartBroadcastSendRtp(ctx, in)
}

func TestPrepareBroadcastInviteDahuaCapture(t *testing.T) {
	ctx := context.Background()
	base := &fakeActivationMedia{info: pcmaTalkInfo(), localPort: 31000}
	service, repo, session, _, _ := newBroadcastActivationService(t, base)
	media := &recordingBroadcastMedia{fakeActivationMedia: base}
	service.activation.deps.ClientFor = func(*node.Node) TalkMediaClient { return media }
	service.activation.deps.Platform.ServerID = "34020000002000000002"
	require.NoError(t, repo.db.Model(&models.GbTalkSession{}).Where("session_id = ?", session.SessionID).
		Updates(map[string]any{
			"device_id": "37010301021320000018", "target_id": "37010301021320000006", "state": models.TalkSessionInviting,
		}).Error)
	offer, err := os.ReadFile("../sdp/testdata/dahua-broadcast-offer.sdp")
	require.NoError(t, err)
	invite := BroadcastInvite{
		PeerID: "37010301021320000018", TargetID: "34020000002000000002",
		CallID: "e9f37cbed37edf78bbd3e531c47fbcf4", CSeq: 1, SDP: string(offer),
	}
	prepared, err := service.PrepareBroadcastInvite(ctx, invite)
	require.NoError(t, err)
	require.Equal(t, session.SessionID, prepared.SessionID)
	require.Contains(t, prepared.AnswerSDP, "m=audio 31000 RTP/AVP 8\r\n")
	require.Contains(t, prepared.AnswerSDP, "a=sendonly")
	require.NotContains(t, prepared.AnswerSDP, "PS/90000")
	require.NotContains(t, prepared.AnswerSDP, "a=setup:")
	require.Equal(t, zlm.BroadcastSendRtpRequest{
		VHost: defaultTalkVHost, App: session.App, SourceStream: session.SourceStream,
		SSRC: session.SSRC, RemoteIP: "192.168.10.204", RemotePort: 9732, IsUDP: true,
	}, media.request)
	repeated, err := service.PrepareBroadcastInvite(ctx, invite)
	require.NoError(t, err)
	require.Equal(t, prepared, repeated)
	require.Equal(t, 1, base.starts)
	require.NoError(t, service.OnBroadcastAck(ctx, invite.CallID))
	stored, err := repo.FindBySession(ctx, session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionActive, stored.State)
	require.Equal(t, "udp", stored.SenderMode)
	require.Equal(t, "192.168.10.204", stored.RemoteMediaIP)
	require.Equal(t, 9732, stored.RemoteMediaPort)
}
