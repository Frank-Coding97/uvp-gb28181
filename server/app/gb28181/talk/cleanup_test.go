package talk

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func activateSessionForCleanup(t *testing.T, repo *GormRepo, sessionID string, channelID uint, callID string, expiresAt time.Time) *models.GbTalkSession {
	t.Helper()
	session := &models.GbTalkSession{
		SessionID: sessionID, ChannelID: channelID, DeviceID: "device-1", NodeID: 1,
		App: "talk", SourceStream: "source-" + sessionID, RecvStream: "recv-" + sessionID,
		SSRC: fmt.Sprintf("%010d", channelID), State: models.TalkSessionReserved, ExpiresAt: expiresAt,
	}
	require.NoError(t, repo.Create(context.Background(), session, "token"))
	changed, err := repo.Transition(context.Background(), sessionID, models.TalkSessionReserved, models.TalkSessionPublishing, TransitionPatch{})
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = repo.Transition(context.Background(), sessionID, models.TalkSessionPublishing, models.TalkSessionInviting, TransitionPatch{})
	require.NoError(t, err)
	require.True(t, changed)
	startedAt := time.Now()
	changed, err = repo.Transition(context.Background(), sessionID, models.TalkSessionInviting, models.TalkSessionActive,
		TransitionPatch{CallID: &callID, StartedAt: &startedAt})
	require.NoError(t, err)
	require.True(t, changed)
	return session
}

func TestCleanupContinuesAfterEachStepFailureAndRetainsLease(t *testing.T) {
	media := &fakeActivationMedia{stopErr: errors.New("stop failed"), closeErr: errors.New("close failed")}
	inviter := &fakeTalkInviter{byeErr: errors.New("bye failed")}
	service, repo, _ := newActivationService(t, media, inviter)
	session := activateSessionForCleanup(t, repo, "cleanup-1", 77, "call-1", time.Now().Add(time.Minute))

	err := service.Cleanup(context.Background(), session.SessionID, models.TalkSessionEnded, "user stopped")
	require.ErrorContains(t, err, "bye failed")
	require.Equal(t, 1, inviter.byes)
	require.Equal(t, 1, media.stops)
	require.Equal(t, 1, media.closes)
	stored, findErr := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, findErr)
	require.Equal(t, models.TalkSessionStopping, stored.State)
	require.NotNil(t, stored.LeaseKey)
	require.NotNil(t, stored.SourceKey)
	require.Nil(t, stored.EndedAt)
}

func TestCleanupMediaFailureRetainsStoppingLeaseForRetry(t *testing.T) {
	media := &fakeActivationMedia{closeErr: errors.New("close failed")}
	service, repo, _ := newActivationService(t, media, &fakeTalkInviter{})
	session := activateSessionForCleanup(t, repo, "cleanup-retry", 79, "call-retry", time.Now().Add(time.Minute))

	err := service.Cleanup(context.Background(), session.SessionID, models.TalkSessionEnded, "user stopped")
	require.ErrorContains(t, err, "close source")
	stored, findErr := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, findErr)
	require.Equal(t, models.TalkSessionStopping, stored.State)
	require.NotNil(t, stored.LeaseKey)
	require.NotNil(t, stored.SourceKey)
	require.Nil(t, stored.EndedAt)

	media.mu.Lock()
	media.closeErr = nil
	media.mu.Unlock()
	other := activateSessionForCleanup(t, repo, "cleanup-other", 80, "call-other", time.Now().Add(time.Minute))
	require.NoError(t, service.Cleanup(context.Background(), other.SessionID, models.TalkSessionEnded, "other session"))
	otherStored, findErr := repo.FindBySession(context.Background(), other.SessionID)
	require.NoError(t, findErr)
	require.Equal(t, models.TalkSessionEnded, otherStored.State)
	firstStored, findErr := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, findErr)
	require.Equal(t, models.TalkSessionStopping, firstStored.State)
	require.NotNil(t, firstStored.LeaseKey)

	require.NoError(t, service.Cleanup(context.Background(), session.SessionID, models.TalkSessionEnded, "retry"))
	stored, findErr = repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, findErr)
	require.Equal(t, models.TalkSessionEnded, stored.State)
	require.Nil(t, stored.LeaseKey)

	closeCalls := media.closes
	require.NoError(t, service.Cleanup(context.Background(), session.SessionID, models.TalkSessionEnded, "idempotent retry"))
	require.Equal(t, closeCalls, media.closes)
}

type blockingCleanupMedia struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (m *blockingCleanupMedia) GetMediaInfo(context.Context, string, string, string, string) (*zlm.MediaInfo, error) {
	return nil, nil
}

func (m *blockingCleanupMedia) StartSendRtpPassive(context.Context, zlm.TalkSendRtpRequest) (*zlm.StartSendRtpPassiveResult, error) {
	return nil, nil
}

func (m *blockingCleanupMedia) StopSendRtp(context.Context, string, string, string, string) error {
	return nil
}

func (m *blockingCleanupMedia) CloseTalkSource(ctx context.Context, _, _, _ string) error {
	m.once.Do(func() { close(m.started) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.release:
		return nil
	}
}

func TestCleanupCancelledWaiterCannotReportFalseSuccess(t *testing.T) {
	service, repo, _ := newActivationService(t, &fakeActivationMedia{}, &fakeTalkInviter{})
	media := &blockingCleanupMedia{started: make(chan struct{}), release: make(chan struct{})}
	service.activation.deps.ClientFor = func(*node.Node) TalkMediaClient { return media }
	session := activateSessionForCleanup(t, repo, "cleanup-waiter", 84, "call-waiter", time.Now().Add(time.Minute))

	ownerDone := make(chan error, 1)
	go func() {
		ownerDone <- service.Cleanup(context.Background(), session.SessionID, models.TalkSessionEnded, "owner")
	}()
	<-media.started

	waiterCtx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, service.Cleanup(waiterCtx, session.SessionID, models.TalkSessionEnded, "waiter"), context.Canceled)
	stored, err := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionStopping, stored.State)
	require.NotNil(t, stored.LeaseKey)

	close(media.release)
	require.NoError(t, <-ownerDone)
	stored, err = repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionEnded, stored.State)
	require.Nil(t, stored.LeaseKey)
}

func TestCleanupIsSingleFlightAndIdempotent(t *testing.T) {
	media := &fakeActivationMedia{}
	inviter := &fakeTalkInviter{}
	service, repo, _ := newActivationService(t, media, inviter)
	session := activateSessionForCleanup(t, repo, "cleanup-2", 78, "call-2", time.Now().Add(time.Minute))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, service.Cleanup(context.Background(), session.SessionID, models.TalkSessionEnded, "user stopped"))
		}()
	}
	wg.Wait()
	require.Equal(t, 1, inviter.byes)
	require.Equal(t, 1, media.stops)
	require.Equal(t, 1, media.closes)
	require.NoError(t, service.Cleanup(context.Background(), session.SessionID, models.TalkSessionEnded, "again"))
	require.Equal(t, 1, inviter.byes)
}

func TestRecoverCleansEveryNonterminalSession(t *testing.T) {
	media := &fakeActivationMedia{}
	inviter := &fakeTalkInviter{}
	service, repo, _ := newActivationService(t, media, inviter)
	active := activateSessionForCleanup(t, repo, "recover-active", 81, "call-active", time.Now().Add(time.Minute))
	reserved := &models.GbTalkSession{
		SessionID: "recover-reserved", ChannelID: 82, DeviceID: "device-1", NodeID: 1,
		App: "talk", SourceStream: "source-reserved", RecvStream: "recv-reserved",
		SSRC: "0200000082", State: models.TalkSessionReserved, ExpiresAt: time.Now().Add(time.Minute),
	}
	require.NoError(t, repo.Create(context.Background(), reserved, "token"))

	require.NoError(t, service.Recover(context.Background()))
	for _, sessionID := range []string{active.SessionID, reserved.SessionID} {
		stored, err := repo.FindBySession(context.Background(), sessionID)
		require.NoError(t, err)
		require.Equal(t, models.TalkSessionExpired, stored.State)
		require.Nil(t, stored.LeaseKey)
	}
}

func TestTalkStreamUnpublishAndRemoteByeReuseCleanup(t *testing.T) {
	media := &fakeActivationMedia{}
	service, repo, _ := newActivationService(t, media, &fakeTalkInviter{})
	unpublished := activateSessionForCleanup(t, repo, "unpublished", 91, "call-unpublished", time.Now().Add(time.Minute))
	require.NoError(t, service.OnUnpublished(context.Background(), unpublished.NodeID, unpublished.App, unpublished.SourceStream))
	stored, err := repo.FindBySession(context.Background(), unpublished.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionEnded, stored.State)

	remoteBye := activateSessionForCleanup(t, repo, "remote-bye", 92, "call-remote", time.Now().Add(time.Minute))
	require.NoError(t, service.OnRemoteBye(context.Background(), "call-remote"))
	stored, err = repo.FindBySession(context.Background(), remoteBye.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionEnded, stored.State)
}

func TestCleanupWorkerExpiresLeaseAndStopsSynchronously(t *testing.T) {
	media := &fakeActivationMedia{}
	service, repo, _ := newActivationService(t, media, &fakeTalkInviter{})
	session := activateSessionForCleanup(t, repo, "expired-1", 83, "call-expired", time.Unix(1699999999, 0).UTC())
	expired, err := repo.ListExpired(context.Background(), service.now().UTC())
	require.NoError(t, err)
	require.Len(t, expired, 1)
	worker := service.StartCleanupWorker(5 * time.Millisecond)
	defer worker.Stop()
	require.NoError(t, worker.Err())
	stored, err := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionExpired, stored.State, "stop=%d close=%d", media.stops, media.closes)
	worker.Stop()
	worker.Stop()
}

func TestTalkServiceShutdownEndsEveryLiveSession(t *testing.T) {
	service, repo, _ := newActivationService(t, &fakeActivationMedia{}, &fakeTalkInviter{})
	session := activateSessionForCleanup(t, repo, "shutdown-active", 93, "call-shutdown", time.Now().Add(time.Minute))

	require.NoError(t, service.Shutdown(context.Background()))
	stored, err := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionEnded, stored.State)
	require.Nil(t, stored.LeaseKey)
}
