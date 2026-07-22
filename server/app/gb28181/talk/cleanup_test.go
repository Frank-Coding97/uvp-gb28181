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

func TestCleanupContinuesAfterEachStepFailureAndReleasesLease(t *testing.T) {
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
	require.Equal(t, models.TalkSessionEnded, stored.State)
	require.Nil(t, stored.LeaseKey)
	require.Contains(t, stored.Error, "bye failed")
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
