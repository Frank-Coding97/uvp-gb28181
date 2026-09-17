package talk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// 现场故障：语音广播"偶发"建立失败，设备 ACK 后 0.36ms 就 BYE。
// 根因是 PrepareBroadcastInvite 拿 FindPendingBroadcast 的旧状态快照当 CAS 的 from，
// 中间隔着两次 ZLM HTTP 调用（实测 100ms+），而 on_stream_changed 触发的
// onBroadcastPublished 也会做 publishing→inviting。CAS 落空后旧代码
// `return PreparedBroadcastInvite{}, transitionErr`（err 为 nil）→ SIP 层回一个
// **没有 SDP 正文的 200 OK** → 设备解析不出平台收流地址 → ACK+BYE。
//
// 这组用例把"另一路并发先推进状态"的时序钉死，防回归。

// stubTalkRepo 把真实 repo 包一层，在指定 CAS 之前先替并发方完成状态推进。
type stubTalkRepo struct {
	TalkRepo
	beforeTransition       func(from, to models.TalkSessionState)
	onUpdateBroadcastFacts func() (bool, error)
}

func (s *stubTalkRepo) Transition(ctx context.Context, sessionID string, from, to models.TalkSessionState, patch TransitionPatch) (bool, error) {
	if s.beforeTransition != nil {
		s.beforeTransition(from, to)
	}
	return s.TalkRepo.Transition(ctx, sessionID, from, to, patch)
}

func (s *stubTalkRepo) UpdateBroadcastFacts(ctx context.Context, sessionID string, patch BroadcastFactsPatch) (bool, error) {
	if s.onUpdateBroadcastFacts != nil {
		return s.onUpdateBroadcastFacts()
	}
	return s.TalkRepo.UpdateBroadcastFacts(ctx, sessionID, patch)
}

const broadcastTestOfferSDP = "v=0\r\no=device 0 0 IN IP4 192.0.2.20\r\ns=Play\r\nc=IN IP4 192.0.2.20\r\nt=0 0\r\nm=audio 30000 RTP/AVP 8\r\na=recvonly\r\na=rtpmap:8 PCMA/8000\r\ny=0200000001\r\n"

// 状态已被并发推进到 inviting 时，必须**用最新状态继续应答**，
// 而不是返回空 SDP；更不能把刚启动的 RTP 发送掐掉。
func TestPrepareBroadcastInviteContinuesWhenPublisherAdvancedFirst(t *testing.T) {
	media := &fakeActivationMedia{info: pcmaTalkInfo(), localPort: 31000}
	service, repo, session, _, _ := newBroadcastActivationService(t, media)
	// 进入本用例时会话是 publishing（与现场一致：FindPendingBroadcast 读到旧快照）

	stub := &stubTalkRepo{TalkRepo: repo}
	alreadyAdvanced := false
	stub.beforeTransition = func(from, to models.TalkSessionState) {
		if alreadyAdvanced || from != models.TalkSessionPublishing || to != models.TalkSessionInviting {
			return
		}
		alreadyAdvanced = true
		// 模拟 onBroadcastPublished：它先把状态推到 inviting，本轮的 CAS 必然落空。
		changed, err := repo.Transition(context.Background(), session.SessionID,
			models.TalkSessionPublishing, models.TalkSessionInviting, TransitionPatch{})
		require.NoError(t, err)
		require.True(t, changed)
	}
	service.repo = stub

	prepared, err := service.PrepareBroadcastInvite(context.Background(), BroadcastInvite{
		PeerID: "device-1", TargetID: "channel-1", CallID: "broadcast-race", CSeq: 9,
		SDP: broadcastTestOfferSDP,
	})

	require.NoError(t, err, "并发推进不是失败，不该把 CAS 落空当错误上报")
	require.Equal(t, session.SessionID, prepared.SessionID)
	require.Contains(t, prepared.AnswerSDP, "a=sendonly", "必须给出真实 SDP，绝不能回没有正文的 200 OK")
	require.Contains(t, prepared.AnswerSDP, "m=audio 31000")
	require.Equal(t, 1, media.starts)
	require.Equal(t, 0, media.stops, "良性并发不得掐掉刚启动的 RTP 发送")

	stored, err := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionInviting, stored.State)
	require.Equal(t, 31000, stored.LocalPort, "媒体事实必须落库")
	require.Equal(t, "192.0.2.20", stored.RemoteMediaIP)
}

// 会话已不可协商（被清理/租约回收）时必须返回**明确错误**：
// 空 answer + nil error 会让 SIP 层发出 Content-Length: 0 的 200 OK。
func TestPrepareBroadcastInviteFailsLoudlyWhenFactsRejected(t *testing.T) {
	media := &fakeActivationMedia{info: pcmaTalkInfo(), localPort: 31000}
	service, repo, session, _, _ := newBroadcastActivationService(t, media)

	stub := &stubTalkRepo{TalkRepo: repo}
	stub.onUpdateBroadcastFacts = func() (bool, error) { return false, nil }
	service.repo = stub

	prepared, err := service.PrepareBroadcastInvite(context.Background(), BroadcastInvite{
		PeerID: "device-1", TargetID: "channel-1", CallID: "broadcast-gone", CSeq: 9,
		SDP: broadcastTestOfferSDP,
	})

	require.Error(t, err, "写不进媒体事实就不能算建立成功")
	require.Empty(t, prepared.AnswerSDP)
	var sipErr *BroadcastSIPError
	require.ErrorAs(t, err, &sipErr)
	require.Equal(t, 480, sipErr.Status)
	require.Equal(t, 1, media.stops, "失败路径要回收已启动的 RTP 发送")

	stored, findErr := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, findErr)
	require.Equal(t, 0, stored.LocalPort, "没协商成功就不该留下半截媒体事实")
}

// 设备 ACK 与 BYE 实测相隔 0.36ms：清理读到的还是 inviting，等真正做 CAS 时
// ACK 已经把行推到 active。这是良性并发，不能再把它记成 cleanup state conflict
// 写进会话 error 列（现场看到的那条 "device sent Broadcast BYE; cleanup state conflict: state=active"）。
func TestCleanupRetriesAfterConcurrentAckInsteadOfRecordingConflict(t *testing.T) {
	media := &fakeActivationMedia{info: pcmaTalkInfo(), localPort: 31000}
	service, repo, session, _, dialogs := newBroadcastActivationService(t, media)
	require.NoError(t, service.OnPublished(context.Background(), session.NodeID, session.App, session.SourceStream))
	_, err := service.PrepareBroadcastInvite(context.Background(), BroadcastInvite{
		PeerID: "device-1", TargetID: "channel-1", CallID: "broadcast-ack-race", CSeq: 9,
		SDP: broadcastTestOfferSDP,
	})
	require.NoError(t, err)

	stub := &stubTalkRepo{TalkRepo: repo}
	armed := false
	stub.beforeTransition = func(from, to models.TalkSessionState) {
		if armed || from != models.TalkSessionInviting || to != models.TalkSessionStopping {
			return
		}
		armed = true
		// 模拟并发的 OnBroadcastAck：它先一步把 inviting 推到 active。
		changed, ackErr := repo.Transition(context.Background(), session.SessionID,
			models.TalkSessionInviting, models.TalkSessionActive, TransitionPatch{})
		require.NoError(t, ackErr)
		require.True(t, changed)
	}
	service.repo = stub

	require.NoError(t, service.Cleanup(context.Background(), session.SessionID, models.TalkSessionEnded, "device sent Broadcast BYE"))

	stored, err := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionEnded, stored.State)
	require.Equal(t, "device sent Broadcast BYE", stored.Error, "并发推进不该被记成故障")
	require.NotContains(t, stored.Error, "cleanup state conflict")
	require.Equal(t, 1, dialogs.byes)
}
