package talk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// 水星抓包中的 Subject 首段是平台 ID，From 是设备 ID，均不是 Notify 的通道 ID。
func TestPrepareBroadcastInviteMatchesDeviceSubject(t *testing.T) {
	const deviceID = "37010301021180000010"
	const channelID = "37292800001320003005"
	const platformID = "34020000002000000002"
	const offer = "v=0\r\no=37010301021180000010 0 0 IN IP4 192.168.10.205\r\ns=Play\r\nc=IN IP4 192.168.10.205\r\nt=0 0\r\nm=audio 54657 TCP/RTP/AVP 8\r\na=recvonly\r\na=rtpmap:8 PCMA/8000\r\na=setup:active\r\na=connection:new\r\ny=0000000003\r\nf=v/////a/1/8/1\r\n"

	for _, tt := range []struct {
		name          string
		peerID        string
		targetID      string
		otherDeviceID string
		wantStatus    int
	}{
		{name: "platform Subject and device From", peerID: deviceID, targetID: platformID},
		{name: "channel Subject and device From", peerID: deviceID, targetID: channelID},
		{name: "channel From compatibility", peerID: channelID, targetID: platformID},
		{name: "missing Subject", peerID: deviceID},
		{name: "trim device identity", peerID: " " + deviceID + " ", targetID: " " + platformID + " "},
		{name: "other device does not cause ambiguity", peerID: deviceID, targetID: platformID, otherDeviceID: "other-device"},
		{name: "same device without channel is ambiguous", peerID: deviceID, targetID: platformID, otherDeviceID: deviceID, wantStatus: 486},
		{name: "explicit channel resolves same device ambiguity", peerID: deviceID, targetID: channelID, otherDeviceID: deviceID},
		{name: "unknown device is rejected", peerID: "unknown-device", targetID: platformID, wantStatus: 403},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			media := &fakeActivationMedia{info: pcmaTalkInfo(), localPort: 31000}
			service, repo, session, _, _ := newBroadcastActivationService(t, media)
			service.activation.deps.Platform.ServerID = platformID
			require.NoError(t, repo.db.Model(&models.GbTalkSession{}).Where("session_id = ?", session.SessionID).
				Updates(map[string]any{"device_id": deviceID, "target_id": channelID, "state": models.TalkSessionInviting}).Error)
			if tt.otherDeviceID != "" {
				require.NoError(t, repo.Create(ctx, &models.GbTalkSession{
					SessionID: "broadcast-other", ChannelID: 10, DeviceID: tt.otherDeviceID, TargetID: "other-channel",
					Mode: models.TalkSessionModeBroadcast, State: models.TalkSessionInviting,
					NodeID: 1, App: "talk", SourceStream: "source-other", RecvStream: "recv-other", SSRC: "0200000002",
					ExpiresAt: time.Now().Add(time.Minute),
				}, "token-other"))
			}
			invite := BroadcastInvite{PeerID: tt.peerID, TargetID: tt.targetID,
				CallID: "gH10srLE23BH4onNs3Z35z41G.2X75qv", CSeq: 4661, SDP: offer}
			prepared, err := service.PrepareBroadcastInvite(ctx, invite)
			if tt.wantStatus != 0 {
				var sipErr *BroadcastSIPError
				require.ErrorAs(t, err, &sipErr)
				require.Equal(t, tt.wantStatus, sipErr.Status)
				require.Zero(t, media.starts)
				stored, findErr := repo.FindBySession(ctx, session.SessionID)
				require.NoError(t, findErr)
				require.Empty(t, stored.CallID)
				return
			}
			require.NoError(t, err)
			require.Equal(t, session.SessionID, prepared.SessionID)
			require.Contains(t, prepared.AnswerSDP, "m=audio 31000 TCP/RTP/AVP 8")
			require.Contains(t, prepared.AnswerSDP, "a=setup:passive")
			require.Contains(t, prepared.AnswerSDP, "a=sendonly")
			// 同一个 INVITE 重传应复用已认领的 dialog，不能重复启动发送。
			repeated, err := service.PrepareBroadcastInvite(ctx, invite)
			require.NoError(t, err)
			require.Equal(t, prepared, repeated)
			require.Equal(t, 1, media.starts)
			require.NoError(t, service.OnBroadcastAck(ctx, invite.CallID))
			stored, err := repo.FindBySession(ctx, session.SessionID)
			require.NoError(t, err)
			require.Equal(t, models.TalkSessionActive, stored.State)
			require.Equal(t, "tcp_passive", stored.SenderMode)
			require.Equal(t, "192.168.10.205", stored.RemoteMediaIP)
			require.Equal(t, 54657, stored.RemoteMediaPort)
		})
	}
}
