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

// 交叉 BYE —— 双方在毫秒内各发一个 BYE，对端不会回 200，我方拆除事务只能按
// T1 退避重传到超时。实测 2026-09-17 21:28 那次，这一步把整个 15s 清理预算吃光：
//   - stopSendRtp / close source 一条都没跑 → ZLM 侧发送会话残留；
//   - DELETE 收到超时错误 → 用户点「停止对讲」看到 504。
//
// 本用例锁住「单步超时不得饿死后续步骤」。
func TestCleanupSIPTerminationTimeoutDoesNotStarveMediaRelease(t *testing.T) {
	media := &fakeActivationMedia{}
	inviter := &fakeTalkInviter{byeBlocks: true}
	service, repo, _ := newActivationService(t, media, inviter)
	session := activateSessionForCleanup(t, repo, "cleanup-bye-hang", 79, "call-bye-hang", time.Now().Add(time.Minute))

	started := time.Now()
	err := service.Cleanup(context.Background(), session.SessionID, models.TalkSessionEnded, "user stopped")
	elapsed := time.Since(started)

	require.ErrorContains(t, err, "TALK BYE")
	// 关键：BYE 卡住之后，媒体释放步骤必须仍然跑在**活的** ctx 上。
	// 只要它们落在父 ctx 到期之后才执行，真实 ZLM 客户端会立刻返回 ctx.Err()，
	// 等于没释放 —— 实测里 stopSendRtp / close source 就是这么丢的。
	require.Equal(t, 1, media.stops, "stopSendRtp 仍须执行")
	require.NoError(t, media.stopCtxErr, "stopSendRtp 必须拿到未过期的 ctx")
	require.Equal(t, 1, media.closes, "close source 仍须执行")
	require.NoError(t, media.closeCtxErr, "close source 必须拿到未过期的 ctx")
	require.GreaterOrEqual(t, elapsed, sipTeardownTimeout, "BYE 必须真的用满自己那份预算")
	require.Less(t, elapsed, defaultCleanupTimeout, "BYE 卡住不得吃掉整个清理预算")
	stored, findErr := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, findErr)
	require.Equal(t, models.TalkSessionEnded, stored.State)
	require.Nil(t, stored.LeaseKey)
	require.Contains(t, stored.Error, "TALK BYE")
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
