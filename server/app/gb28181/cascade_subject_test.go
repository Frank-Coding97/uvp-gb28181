package gb28181

import (
	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestCascadeSubjectRejectsMissingOrInvalidSequence(t *testing.T) {
	for _, subject := range []string{
		"34020000001320000010,34020000002000000001:0",
		"34020000001320000010:,34020000002000000001:0",
		"34020000001320000010:020000000000000000001,34020000002000000001:0",
		"34020000001320000010:2200000001,34020000002000000001:0",
	} {
		t.Run(subject, func(t *testing.T) {
			h, _, _ := newCascadeVideoTestRuntime(t)
			defer cleanupCascadeSubjectRuntime(h)
			req := cascadeTestRequest(15060, "0200000001")
			req.ReplaceHeader(sip.NewHeader("Subject", subject))
			tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
			defer tx.Terminate()
			go h.Handle(req, tx)
			require.GreaterOrEqual(t, cascadeAwaitResponse(t, tx).StatusCode, 400)
		})
	}
}

func cleanupCascadeSubjectRuntime(h *cascadeVideoRuntime) {
	h.mu.Lock()
	for _, s := range h.sessions {
		s.cancel()
	}
	h.mu.Unlock()
	h.wg.Wait()
}

func TestCascadeSubjectReplacesSameSourceAndKeepsRetransmissionIdempotent(t *testing.T) {
	h, _, stops := newCascadeVideoTestRuntime(t)
	h.client.TxRequester = &siptest.ClientTxRequester{OnRequest: func(r *sip.Request) *sip.Response { return sip.NewResponseFromRequest(r, 200, "OK", nil) }}
	defer cleanupCascadeSubjectRuntime(h)
	req := cascadeTestRequest(15060, "0200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	oldDone := make(chan struct{})
	go func() { h.Handle(req, tx); close(oldDone) }()
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 200, res.StatusCode)
	h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx)
	repeatTx := siptest.NewServerTxRecorder(req.Clone())
	defer repeatTx.Terminate()
	require.True(t, h.Handle(req.Clone(), repeatTx))
	require.Empty(t, stops, "transaction retransmission must not stop the old sender")
	other := req.Clone()
	cid := sip.CallIDHeader("replacement-call")
	other.ReplaceHeader(&cid)
	otherTx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(other), make(chan *sip.Response, 10)}
	defer otherTx.Terminate()
	go h.Handle(other, otherTx)
	answer := cascadeAwaitResponse(t, otherTx)
	require.Equal(t, 200, answer.StatusCode)
	h.Handle(cascadeDialogRequest(other, answer, sip.ACK), otherTx)
	select {
	case <-oldDone:
	case <-time.After(time.Second):
		t.Fatal("old source lease not released")
	}
	require.Len(t, stops, 1)
	require.Equal(t, 1, h.sources.LeaseCount("shared-source"))
}

func TestCascadeSubjectStopFailureKeepsOldSession(t *testing.T) {
	h, _, stops := newCascadeVideoTestRuntime(t, 1)
	defer cleanupCascadeSubjectRuntime(h)
	req := cascadeTestRequest(15060, "0200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 200, res.StatusCode)
	h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx)
	other := req.Clone()
	cid := sip.CallIDHeader("replacement-fails")
	other.ReplaceHeader(&cid)
	otherTx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(other), make(chan *sip.Response, 10)}
	defer otherTx.Terminate()
	go h.Handle(other, otherTx)
	require.Equal(t, 503, cascadeAwaitResponse(t, otherTx).StatusCode)
	require.Len(t, stops, 1)
	h.mu.Lock()
	require.Len(t, h.sessions, 1)
	for _, s := range h.sessions {
		require.NoError(t, s.context.Err())
	}
	h.mu.Unlock()
	require.Equal(t, 1, h.sources.LeaseCount("shared-source"))
}

func TestCascadeSubjectConcurrentReplacementIsSerialized(t *testing.T) {
	h, _, stops := newCascadeVideoTestRuntime(t)
	entered, unblock := make(chan struct{}), make(chan struct{})
	var enteredOnce sync.Once
	h.client.TxRequester = &siptest.ClientTxRequester{OnRequest: func(r *sip.Request) *sip.Response {
		if r.Method == sip.BYE {
			enteredOnce.Do(func() { close(entered) })
			<-unblock
		}
		return sip.NewResponseFromRequest(r, 200, "OK", nil)
	}}
	defer cleanupCascadeSubjectRuntime(h)
	req := cascadeTestRequest(15060, "0200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 200, res.StatusCode)
	h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx)
	replacement := req.Clone()
	cid := sip.CallIDHeader("concurrent-1")
	replacement.ReplaceHeader(&cid)
	nextTx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(replacement), make(chan *sip.Response, 10)}
	defer nextTx.Terminate()
	go h.Handle(replacement, nextTx)
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(unblock)
		t.Fatal("replacement did not stop old dialog")
	}
	competing := req.Clone()
	cid = sip.CallIDHeader("concurrent-2")
	competing.ReplaceHeader(&cid)
	competingTx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(competing), make(chan *sip.Response, 10)}
	defer competingTx.Terminate()
	go h.Handle(competing, competingTx)
	rejected := cascadeAwaitResponse(t, competingTx)
	close(unblock)
	require.Equal(t, 503, rejected.StatusCode)
	nextRes := cascadeAwaitResponse(t, nextTx)
	require.Equal(t, 200, nextRes.StatusCode)
	h.Handle(cascadeDialogRequest(replacement, nextRes, sip.ACK), nextTx)
	require.Len(t, stops, 1)
}

func TestCascadeSubjectDifferentSequencesDoNotReplace(t *testing.T) {
	h, _, stops := newCascadeVideoTestRuntime(t)
	defer cleanupCascadeSubjectRuntime(h)
	req := cascadeTestRequest(15060, "0200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 200, res.StatusCode)
	h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx)
	other := req.Clone()
	cid := sip.CallIDHeader("other-sequence")
	other.ReplaceHeader(&cid)
	// Subject sequence is independent of SDP SSRC; collision still must not stop another source.
	other.ReplaceHeader(sip.NewHeader("Subject", "34020000001320000010:0other,34020000002000000001:windowA"))
	otherTx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(other), make(chan *sip.Response, 10)}
	defer otherTx.Terminate()
	go h.Handle(other, otherTx)
	require.Equal(t, 486, cascadeAwaitResponse(t, otherTx).StatusCode)
	require.Empty(t, stops)
}

func TestCascadeSubjectPlaybackReplacementPreservesSourceUntilSenderStopped(t *testing.T) {
	for _, failStop := range []bool{false, true} {
		name := "success"
		failures := 0
		if failStop {
			name, failures = "stop failure", 1
		}
		t.Run(name, func(t *testing.T) {
			h, fake, stops := newCascadePlaybackTestRuntime(t, failures)
			defer cleanupCascadeSubjectRuntime(h)
			req := cascadePlaybackRequest(15060, "1200000001")
			tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
			defer tx.Terminate()
			go h.Handle(req, tx)
			res := cascadeAwaitResponse(t, tx)
			require.Equal(t, 200, res.StatusCode)
			h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx)

			other := req.Clone()
			callID := sip.CallIDHeader("replace-playback-source")
			other.ReplaceHeader(&callID)
			otherTx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(other), make(chan *sip.Response, 10)}
			defer otherTx.Terminate()
			go h.Handle(other, otherTx)
			response := cascadeAwaitResponse(t, otherTx)
			if failStop {
				require.Equal(t, 503, response.StatusCode)
				require.Empty(t, fake.stopped, "failed sender stop must retain old playback source")
				fake.mu.Lock()
				require.Len(t, fake.requests, 1, "replacement must not acquire a new source")
				fake.mu.Unlock()
			} else {
				require.Equal(t, 200, response.StatusCode)
				require.Len(t, fake.stopped, 1, "old source must be released before answering replacement")
				require.Equal(t, "playback-1", <-fake.stopped)
				h.Handle(cascadeDialogRequest(other, response, sip.ACK), otherTx)
				fake.mu.Lock()
				require.Len(t, fake.requests, 2)
				fake.mu.Unlock()
			}
			require.Len(t, stops, 1, "the old sender must be stopped exactly once")
			h.mu.Lock()
			require.Len(t, h.sessions, 1)
			h.mu.Unlock()
		})
	}
}

// 真实报文（EasyGBS 3.7.451，2026-10-26 实测）：
//
//	Subject: 34020000012000000001:1,37010301021180000010:2
//
// 序列号就是 1、2 这种纯序号，没有国标里「0 开头＝实时 / 1 开头＝回放」的前缀；
// 接收者写的是**设备编码**而不是通道编码。严格解析会把 Subject 判成非法，
// 上级 UI 表现为「点了语音没反应」（没有任何报错），所以广播路径必须放宽。
// 视频 INVITE 那条路径仍由 TestCascadeSubjectRejectsMissingOrInvalidSequence 锁死。
func TestCascadeBroadcastSubjectAcceptsPlainSequence(t *testing.T) {
	identity, err := parseCascadeBroadcastSourceIdentity("34020000012000000001:1,37010301021180000010:2")
	require.NoError(t, err)
	require.Equal(t, "34020000012000000001", identity.SenderID)
	require.Equal(t, "1", identity.SenderSequence)
	require.Equal(t, "37010301021180000010", identity.ReceiverID)
	require.Equal(t, "2", identity.ReceiverSequence)

	binding := cascadeBroadcastBinding{
		LocalSourceID: "34020000012000000001",
		DeviceCode:    "37010301021180000010",
		ChannelCode:   "37292800001320003005",
	}
	require.True(t, cascadeAudioSubjectMatches(identity, binding), "接收者写成设备编码也要认")

	// 结构坏掉、编码长度不对、夹带换行的仍然要拒。
	for _, raw := range []string{
		"",
		"34020000012000000001:1",
		"34020000012000000001:1,3701030102118000001:2",
		"3402000001200000001:1,37010301021180000010:2",
		"34020000012000000001:1,37010301021180000010:2\r\nX: y",
	} {
		_, err := parseCascadeBroadcastSourceIdentity(raw)
		require.Error(t, err, raw)
	}
}
