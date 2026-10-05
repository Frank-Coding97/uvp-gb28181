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
	return activateSessionForCleanupWithSender(t, repo, sessionID, channelID, callID, expiresAt, 0)
}

// activateSessionForCleanupWithSender 同上，另外可选地标记"发送会话已建立"（local_port>0）。
// 清理链路据此区分「有物可停」与「设备从未应答、本来就没有发送会话」—— 见
// TestCleanupStopSendRtpWithoutSenderStillFinishesLease。
func activateSessionForCleanupWithSender(t *testing.T, repo *GormRepo, sessionID string, channelID uint, callID string, expiresAt time.Time, localPort int) *models.GbTalkSession {
	t.Helper()
	session := &models.GbTalkSession{
		SessionID: sessionID, ChannelID: channelID, DeviceID: "device-1", NodeID: 1,
		App: "talk", SourceStream: "source-" + sessionID, RecvStream: "recv-" + sessionID,
		SSRC: fmt.Sprintf("%010d", channelID), State: models.TalkSessionReserved, ExpiresAt: expiresAt,
		LocalPort: localPort,
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

// ⛔⛔ 回归锚点：2026-10-03 现场 —— 点「停止对讲/广播」时 DELETE 返回
// 500「语音对讲操作失败」，而会话最终是被后台 sweep 按"租约过期"收掉的。
//
// 设备从未应答（local_port=0，见 broadcast/talk 的 answering 阶段才写 local_port）⇒
// ZLM 侧没有该 ssrc 的发送会话 ⇒ stopSendRtp 回 `code=-300 "stopSendRtp failed"`
// （ZLM `MediaSource::stopSendRtp` 找不到 ssrc 即 false），客户端只容忍 -500。
// 旧实现把它当可重试 ⇒ 租约吊在 stopping、会话不落终态 ⇒ 控制器映射成 500。
// 要求：错误仍要记进 error 列（可追溯），但会话必须收尾。
func TestCleanupStopSendRtpWithoutSenderStillFinishesLease(t *testing.T) {
	media := &fakeActivationMedia{stopErr: errors.New(`stopSendRtp code=-300 msg=stopSendRtp failed`)}
	service, repo, _ := newActivationService(t, media, &fakeTalkInviter{})
	session := activateSessionForCleanup(t, repo, "cleanup-no-sender", 81, "", time.Now().Add(time.Minute))

	err := service.Cleanup(context.Background(), session.SessionID, models.TalkSessionEnded, "user stopped")
	require.ErrorContains(t, err, "stopSendRtp")
	require.Equal(t, 1, media.stops)

	stored, findErr := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, findErr)
	require.Equal(t, models.TalkSessionEnded, stored.State, "无物可停不能把会话吊在 stopping")
	require.Nil(t, stored.LeaseKey)
	require.Contains(t, stored.Error, "stopSendRtp", "失败仍要落库可追溯")
}

// 反面锚点：真的建过发送会话（local_port>0）时，stopSendRtp 失败**必须**继续保留
// stopping 租约等重试 —— 否则媒体侧会漏放。防止上一条把口子开太大。
func TestCleanupStopSendRtpFailureRetainsLeaseWhenSenderExisted(t *testing.T) {
	media := &fakeActivationMedia{stopErr: errors.New(`stopSendRtp code=-300 msg=stopSendRtp failed`)}
	service, repo, _ := newActivationService(t, media, &fakeTalkInviter{})
	session := activateSessionForCleanupWithSender(t, repo, "cleanup-has-sender", 82, "", time.Now().Add(time.Minute), 40022)

	err := service.Cleanup(context.Background(), session.SessionID, models.TalkSessionEnded, "user stopped")
	require.ErrorContains(t, err, "stopSendRtp")

	stored, findErr := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, findErr)
	require.Equal(t, models.TalkSessionStopping, stored.State)
	require.NotNil(t, stored.LeaseKey)
	require.Nil(t, stored.EndedAt)
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
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(media.release) }) }
	defer release()
	go func() {
		ownerDone <- service.Cleanup(context.Background(), session.SessionID, models.TalkSessionEnded, "owner")
	}()
	select {
	case <-media.started:
	case <-time.After(time.Second):
		t.Fatal("cleanup owner did not reach media close")
	}

	waiterCtx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, service.Cleanup(waiterCtx, session.SessionID, models.TalkSessionEnded, "waiter"), context.Canceled)
	stored, err := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionStopping, stored.State)
	require.NotNil(t, stored.LeaseKey)

	release()
	ownerTimer := time.NewTimer(time.Second)
	defer ownerTimer.Stop()
	select {
	case ownerErr := <-ownerDone:
		require.NoError(t, ownerErr)
	case <-ownerTimer.C:
		t.Fatal("cleanup owner did not finish after media release")
	}
	stored, err = repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionEnded, stored.State)
	require.Nil(t, stored.LeaseKey)
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
