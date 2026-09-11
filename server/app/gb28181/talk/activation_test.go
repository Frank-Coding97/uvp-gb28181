package talk

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type fakeActivationMedia struct {
	mu        sync.Mutex
	info      *zlm.MediaInfo
	startErr  error
	stopErr   error
	closeErr  error
	localPort int
	starts    int
	stops     int
	closes    int
}

func (f *fakeActivationMedia) GetMediaInfo(context.Context, string, string, string, string) (*zlm.MediaInfo, error) {
	return f.info, nil
}

func (f *fakeActivationMedia) StartSendRtpPassive(context.Context, zlm.TalkSendRtpRequest) (*zlm.StartSendRtpPassiveResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &zlm.StartSendRtpPassiveResult{LocalPort: f.localPort}, nil
}

func (f *fakeActivationMedia) StopSendRtp(context.Context, string, string, string, string) error {
	f.mu.Lock()
	f.stops++
	f.mu.Unlock()
	return f.stopErr
}

func (f *fakeActivationMedia) CloseTalkSource(context.Context, string, string, string) error {
	f.mu.Lock()
	f.closes++
	f.mu.Unlock()
	return f.closeErr
}

type fakeTalkInviter struct {
	started chan struct{}
	release chan struct{}
	err     error
	byeErr  error
	invites int
	byes    int
}

func (f *fakeTalkInviter) InviteTalk(context.Context, uac.TalkInviteRequest) (uac.TalkDialogMetadata, error) {
	f.invites++
	if f.started != nil {
		close(f.started)
		<-f.release
	}
	return uac.TalkDialogMetadata{CallID: "talk-call", CSeq: 7, StatusCode: 200}, f.err
}

func (f *fakeTalkInviter) ByeTalk(context.Context, string) error {
	f.byes++
	return f.byeErr
}

type fakeTalkTargetLoader struct {
	channel *models.GbChannel
	device  *models.GbDevice
}

func (f fakeTalkTargetLoader) LoadTalkTarget(context.Context, uint, string) (*models.GbChannel, *models.GbDevice, error) {
	return f.channel, f.device, nil
}

func newActivationService(t *testing.T, media *fakeActivationMedia, inviter *fakeTalkInviter) (*Service, *GormRepo, *models.GbTalkSession) {
	t.Helper()
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	mediaNode := &node.Node{ID: 1, Host: "192.0.2.10", State: node.StateActive}
	service := NewService(repo, fakeTalkNodes{items: map[int64]*node.Node{1: mediaNode}}, nil, nil, nil, func() time.Time {
		return time.Unix(1700000000, 0)
	})
	service.ConfigureActivation(ActivationDependencies{
		ClientFor: func(*node.Node) TalkMediaClient { return media },
		Inviter:   inviter,
		Targets: fakeTalkTargetLoader{
			channel: &models.GbChannel{ID: 9, DeviceID: "device-1", ChannelID: "channel-1", Status: models.ChannelStatusOnline},
			device:  &models.GbDevice{DeviceID: "device-1", IP: "192.0.2.20", Port: 5060, Transport: "TCP", Status: models.DeviceStatusOnline},
		},
		Platform: ActivationPlatform{ServerID: "34020000002000000001"},
	})
	session := &models.GbTalkSession{
		SessionID: "session-1", ChannelID: 9, DeviceID: "device-1", NodeID: 1,
		App: "talk", SourceStream: "source-1", RecvStream: "recv-1", SSRC: "0200000001",
		State: models.TalkSessionReserved, ExpiresAt: time.Now().Add(time.Minute),
	}
	require.NoError(t, repo.Create(context.Background(), session, "token"))
	changed, err := repo.Transition(context.Background(), session.SessionID, models.TalkSessionReserved, models.TalkSessionPublishing, TransitionPatch{})
	require.NoError(t, err)
	require.True(t, changed)
	return service, repo, session
}

func pcmaTalkInfo() *zlm.MediaInfo {
	return &zlm.MediaInfo{Online: true, Tracks: []zlm.MediaTrack{{CodecType: 1, CodecIDName: "PCMA", Ready: true, SampleRate: 8000}}}
}

func TestOnPublishedActivatesTalkAndPersistsTransport(t *testing.T) {
	media := &fakeActivationMedia{info: pcmaTalkInfo(), localPort: 32100}
	inviter := &fakeTalkInviter{}
	service, repo, session := newActivationService(t, media, inviter)

	require.NoError(t, service.OnPublished(context.Background(), session.NodeID, session.App, session.SourceStream))
	stored, err := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionActive, stored.State)
	require.Equal(t, 32100, stored.LocalPort)
	require.Equal(t, "talk-call", stored.CallID)
	require.NotNil(t, stored.StartedAt)
	require.Equal(t, 1, media.starts)
	require.Equal(t, 1, inviter.invites)

	require.NoError(t, service.OnPublished(context.Background(), session.NodeID, session.App, session.SourceStream))
	require.Equal(t, 1, media.starts)
	require.Equal(t, 1, inviter.invites)
}

func TestOnPublishedRejectsNonPCMAWithoutStartingSender(t *testing.T) {
	media := &fakeActivationMedia{info: &zlm.MediaInfo{Online: true, Tracks: []zlm.MediaTrack{{CodecType: 1, CodecIDName: "OPUS", Ready: true}}}, localPort: 32100}
	service, repo, session := newActivationService(t, media, &fakeTalkInviter{})

	err := service.OnPublished(context.Background(), session.NodeID, session.App, session.SourceStream)
	require.ErrorIs(t, err, ErrTalkCodecUnsupported)
	stored, findErr := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, findErr)
	require.Equal(t, models.TalkSessionFailed, stored.State)
	require.Zero(t, media.starts)
	require.Equal(t, 1, media.closes)
}

func TestOnPublishedInviteFailureCompensatesSenderAndSource(t *testing.T) {
	media := &fakeActivationMedia{info: pcmaTalkInfo(), localPort: 32100}
	service, repo, session := newActivationService(t, media, &fakeTalkInviter{err: errors.New("486 Busy Here")})

	require.Error(t, service.OnPublished(context.Background(), session.NodeID, session.App, session.SourceStream))
	stored, err := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionFailed, stored.State)
	require.Equal(t, 1, media.stops)
	require.Equal(t, 1, media.closes)
}

func TestOnPublishedStartFailureStillAttemptsIdempotentCleanup(t *testing.T) {
	media := &fakeActivationMedia{info: pcmaTalkInfo(), startErr: errors.New("source disappeared")}
	service, repo, session := newActivationService(t, media, &fakeTalkInviter{})

	require.Error(t, service.OnPublished(context.Background(), session.NodeID, session.App, session.SourceStream))
	stored, err := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionFailed, stored.State)
	require.Equal(t, 1, media.stops)
	require.Equal(t, 1, media.closes)
}

func TestTalkServiceRenewOnlyExtendsActiveLease(t *testing.T) {
	media := &fakeActivationMedia{info: pcmaTalkInfo(), localPort: 32100}
	service, _, session := newActivationService(t, media, &fakeTalkInviter{})
	require.NoError(t, service.OnPublished(context.Background(), session.NodeID, session.App, session.SourceStream))

	renewed, err := service.Renew(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionActive, renewed.State)
	require.True(t, renewed.ExpiresAt.Equal(time.Unix(1700000000, 0).Add(defaultTalkLease)))
}

func TestOnPublishedStopDuringInviteCannotReturnToActive(t *testing.T) {
	media := &fakeActivationMedia{info: pcmaTalkInfo(), localPort: 32100}
	inviter := &fakeTalkInviter{started: make(chan struct{}), release: make(chan struct{})}
	service, repo, session := newActivationService(t, media, inviter)
	done := make(chan error, 1)
	go func() {
		done <- service.OnPublished(context.Background(), session.NodeID, session.App, session.SourceStream)
	}()
	<-inviter.started
	changed, err := repo.Transition(context.Background(), session.SessionID, models.TalkSessionInviting, models.TalkSessionStopping, TransitionPatch{})
	require.NoError(t, err)
	require.True(t, changed)
	close(inviter.release)
	require.NoError(t, <-done)

	stored, err := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionStopping, stored.State)
	require.Equal(t, 1, inviter.byes)
	require.Equal(t, 1, media.stops)
}
