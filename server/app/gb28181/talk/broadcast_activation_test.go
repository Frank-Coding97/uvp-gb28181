package talk

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type fakeBroadcastMessageSender struct {
	body  []byte
	calls int
	err   error
}

func (f *fakeBroadcastMessageSender) SendMessageTracked(_ context.Context, _, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
	f.calls++
	f.body = append([]byte(nil), body...)
	return uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, f.err
}

type fakeBroadcastDialogs struct{ byes int }

func (f *fakeBroadcastDialogs) ByeBroadcast(context.Context, string) error {
	f.byes++
	return nil
}

func (f *fakeActivationMedia) StartBroadcastSendRtp(_ context.Context, in zlm.BroadcastSendRtpRequest) (*zlm.StartBroadcastSendRtpResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &zlm.StartBroadcastSendRtpResult{LocalPort: f.localPort}, nil
}

func newBroadcastActivationService(t *testing.T, media *fakeActivationMedia) (*Service, *GormRepo, *models.GbTalkSession, *fakeBroadcastMessageSender, *fakeBroadcastDialogs) {
	t.Helper()
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	mediaNode := &node.Node{ID: 1, Host: "192.0.2.10", State: node.StateActive}
	sender := &fakeBroadcastMessageSender{}
	dialogs := &fakeBroadcastDialogs{}
	service := NewService(repo, fakeTalkNodes{items: map[int64]*node.Node{1: mediaNode}}, nil, nil, nil, func() time.Time { return time.Unix(1700000000, 0) })
	service.ConfigureActivation(ActivationDependencies{
		ClientFor: func(*node.Node) TalkMediaClient { return media },
		Inviter:   &fakeTalkInviter{},
		Targets: fakeTalkTargetLoader{
			channel: &models.GbChannel{ID: 9, DeviceID: "device-1", ChannelID: "channel-1", Status: models.ChannelStatusOnline},
			device:  &models.GbDevice{DeviceID: "device-1", IP: "192.0.2.20", Port: 5060, Transport: "UDP", Status: models.DeviceStatusOnline},
		},
		Platform:         ActivationPlatform{ServerID: "34020000002000000001"},
		BroadcastSender:  sender,
		BroadcastDialogs: dialogs,
	})
	session := &models.GbTalkSession{
		SessionID: "broadcast-1", ChannelID: 9, DeviceID: "device-1", TargetID: "channel-1", Mode: models.TalkSessionModeBroadcast,
		NodeID: 1, App: "talk", SourceStream: "source-1", RecvStream: "recv-1", SSRC: "0200000001",
		State: models.TalkSessionReserved, ExpiresAt: time.Now().Add(time.Minute),
	}
	require.NoError(t, repo.Create(context.Background(), session, "token"))
	changed, err := repo.Transition(context.Background(), session.SessionID, models.TalkSessionReserved, models.TalkSessionPublishing, TransitionPatch{})
	require.NoError(t, err)
	require.True(t, changed)
	return service, repo, session, sender, dialogs
}

func TestBroadcastPublishInviteAckActivatesSession(t *testing.T) {
	media := &fakeActivationMedia{info: pcmaTalkInfo(), localPort: 31000}
	service, repo, session, sender, _ := newBroadcastActivationService(t, media)

	require.NoError(t, service.OnPublished(context.Background(), session.NodeID, session.App, session.SourceStream))
	stored, err := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionInviting, stored.State)
	require.Equal(t, models.TalkSignalPhaseWaitingInvite, stored.SignalPhase)
	require.Positive(t, stored.BroadcastSN)
	require.Contains(t, string(sender.body), "<CmdType>Broadcast</CmdType>")

	prepared, err := service.PrepareBroadcastInvite(context.Background(), BroadcastInvite{
		PeerID: "device-1", TargetID: "channel-1", CallID: "broadcast-call", CSeq: 9,
		SDP: "v=0\r\no=device 0 0 IN IP4 192.0.2.20\r\ns=Play\r\nc=IN IP4 192.0.2.20\r\nt=0 0\r\nm=audio 30000 RTP/AVP 8\r\na=recvonly\r\na=rtpmap:8 PCMA/8000\r\ny=0200000001\r\n",
	})
	require.NoError(t, err)
	require.Equal(t, session.SessionID, prepared.SessionID)
	require.Contains(t, prepared.AnswerSDP, "a=sendonly")

	require.NoError(t, service.OnBroadcastAck(context.Background(), "broadcast-call"))
	stored, err = repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionActive, stored.State)
	require.Equal(t, models.TalkSignalPhaseActive, stored.SignalPhase)
	require.Equal(t, 1, media.starts)
}

func TestBroadcastResponseCorrelatesWithoutActivating(t *testing.T) {
	media := &fakeActivationMedia{info: pcmaTalkInfo(), localPort: 31000}
	service, repo, session, _, _ := newBroadcastActivationService(t, media)
	require.NoError(t, service.OnPublished(context.Background(), session.NodeID, session.App, session.SourceStream))
	stored, _ := repo.FindBySession(context.Background(), session.SessionID)
	body := []byte(`<?xml version="1.0"?><Response><CmdType>Broadcast</CmdType><SN>` + strconv.FormatUint(uint64(stored.BroadcastSN), 10) + `</SN><DeviceID>device-1</DeviceID><TargetID>channel-1</TargetID><Result>OK</Result></Response>`)
	require.NoError(t, service.OnBroadcastMessage(context.Background(), "device-1", body))
	stored, _ = repo.FindBySession(context.Background(), session.SessionID)
	require.Equal(t, "success", stored.BroadcastReplyStatus)
	require.Equal(t, models.TalkSessionInviting, stored.State)
}

func TestBroadcastCleanupUsesUASByeAndReleasesMedia(t *testing.T) {
	media := &fakeActivationMedia{info: pcmaTalkInfo(), localPort: 31000}
	service, repo, session, _, dialogs := newBroadcastActivationService(t, media)
	require.NoError(t, service.OnPublished(context.Background(), session.NodeID, session.App, session.SourceStream))
	_, err := service.PrepareBroadcastInvite(context.Background(), BroadcastInvite{
		PeerID: "device-1", TargetID: "channel-1", CallID: "broadcast-cleanup", CSeq: 9,
		SDP: "v=0\r\nc=IN IP4 192.0.2.20\r\nt=0 0\r\nm=audio 30000 RTP/AVP 8\r\na=rtpmap:8 PCMA/8000\r\n",
	})
	require.NoError(t, err)
	require.NoError(t, service.OnBroadcastAck(context.Background(), "broadcast-cleanup"))
	require.NoError(t, service.Cleanup(context.Background(), session.SessionID, models.TalkSessionEnded, "user stopped"))

	stored, err := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionEnded, stored.State)
	require.Nil(t, stored.LeaseKey)
	require.Equal(t, 1, dialogs.byes)
	require.Equal(t, 1, media.stops)
	require.Equal(t, 1, media.closes)
}
