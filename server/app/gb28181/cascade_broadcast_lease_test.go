package gb28181

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/talk"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

func broadcastLeaseRequest(t *testing.T, p model.GbCascadePlatform) *sip.Request {
	t.Helper()
	body, err := manscdp.BuildBroadcastNotifyWithProfile(protocol.ProfileFor(protocol.Version2022), manscdp.BroadcastNotify{SN: 97, SourceID: p.UpstreamServerID, TargetID: "34020000001320000012"})
	require.NoError(t, err)
	return newCascadeStatusRequest(p, p.LocalDeviceID, p.LocalDeviceID, body)
}

func broadcastLeaseStatus(t *testing.T, runtime *cascadeBroadcastRuntime, req *sip.Request) int {
	t.Helper()
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	require.True(t, runtime.Handle(req, tx))
	require.Len(t, tx.Result(), 1)
	return tx.Result()[0].StatusCode
}

func TestCascadeBroadcastLeaseExistsBeforeNotifyAndReplayDoesNotSend(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, p, store := prepareCascadeBroadcastFixture(t)
	var sends atomic.Int32
	sender := cascadeRecordSenderFunc(func(ctx context.Context, _, _, _ string, _ []byte) (uac.TrackedMessageResult, error) {
		sends.Add(1)
		leases, err := talk.NewGormRepo(db).ListCascadeLeases(ctx)
		require.NoError(t, err)
		require.Len(t, leases, 1, "notification must already own the shared channel")
		rows, err := store.ListNonterminalBroadcastSessions(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		var binding cascadeBroadcastBinding
		require.NoError(t, json.Unmarshal([]byte(rows[0].SDPSummary), &binding))
		require.Equal(t, leases[0].SessionID, binding.AudioLeaseID)
		return uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, nil
	})
	runtime := newCascadeBroadcastRuntime(db, store, newCascadePlatformClientFactory(&cascadeStatusTransport{requests: make(chan *sip.Request, 1)}, nil, time.Second), sender)
	req := broadcastLeaseRequest(t, p)
	require.Equal(t, 200, broadcastLeaseStatus(t, runtime, req))
	require.Equal(t, 200, broadcastLeaseStatus(t, runtime, req.Clone()))
	require.EqualValues(t, 1, sends.Load())
	changed := req.Clone()
	changed.SetBody([]byte(string(req.Body()) + "\n"))
	require.Equal(t, 409, broadcastLeaseStatus(t, runtime, changed))
	require.EqualValues(t, 1, sends.Load())
}

func TestCascadeBroadcastLeaseRejectsBusyBeforeNotify(t *testing.T) {
	for _, cascade := range []bool{false, true} {
		t.Run(map[bool]string{false: "web", true: "another cascade"}[cascade], func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			db, p, store := prepareCascadeBroadcastFixture(t)
			repo := talk.NewGormRepo(db)
			if cascade {
				_, err := repo.ReserveCascadeLease(t.Context(), "cascade-audio-existing", 20, "34020000001320000001", "34020000001320000002", time.Now().Add(time.Minute))
				require.NoError(t, err)
			} else {
				row := gbmodels.GbTalkSession{SessionID: "web-existing", ChannelID: 20, DeviceID: "34020000001320000001", State: gbmodels.TalkSessionReserved, ExpiresAt: time.Now().Add(time.Minute)}
				require.NoError(t, repo.Create(t.Context(), &row, "web-token"))
			}
			sender := cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
				t.Fatal("occupied channel must not receive Notify")
				return uac.TrackedMessageResult{}, nil
			})
			runtime := newCascadeBroadcastRuntime(db, store, newCascadePlatformClientFactory(&cascadeStatusTransport{}, nil, time.Second), sender)
			require.Equal(t, 486, broadcastLeaseStatus(t, runtime, broadcastLeaseRequest(t, p)))
			rows, err := store.ListNonterminalBroadcastSessions(t.Context())
			require.NoError(t, err)
			require.Empty(t, rows, "failed reservation must roll back the new broadcast task")
		})
	}
}

func TestCascadeBroadcastLeaseDownstreamFailureReleasesAndReplaysFailure(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, p, store := prepareCascadeBroadcastFixture(t)
	var sends atomic.Int32
	sender := cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
		sends.Add(1)
		return uac.TrackedMessageResult{StatusCode: 500, Attempted: true}, errors.New("device unavailable")
	})
	runtime := newCascadeBroadcastRuntime(db, store, newCascadePlatformClientFactory(&cascadeStatusTransport{}, nil, time.Second), sender)
	req := broadcastLeaseRequest(t, p)
	require.Equal(t, 502, broadcastLeaseStatus(t, runtime, req))
	leases, err := talk.NewGormRepo(db).ListCascadeLeases(t.Context())
	require.NoError(t, err)
	require.Empty(t, leases)
	require.Equal(t, 502, broadcastLeaseStatus(t, runtime, req.Clone()))
	require.EqualValues(t, 1, sends.Load())
}

func TestCascadeBroadcastLeaseBusinessFailureReleasesBeforeUpstreamDelivery(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, p, store := prepareCascadeBroadcastFixture(t)
	var sn int
	var sends atomic.Int32
	sender := cascadeRecordSenderFunc(func(_ context.Context, _, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
		sends.Add(1)
		notify, err := manscdp.ParseCascadeBroadcastNotify(protocol.ProfileFor(protocol.Version2022), body)
		require.NoError(t, err)
		sn = notify.SN
		return uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, nil
	})
	failureTransport := cascadeSIPFailureTransport{}
	runtime := newCascadeBroadcastRuntime(db, store, newCascadePlatformClientFactory(failureTransport, nil, time.Second), sender)
	require.Equal(t, 200, broadcastLeaseStatus(t, runtime, broadcastLeaseRequest(t, p)))
	body, err := manscdp.BuildBroadcastResponseWithProfile(protocol.ProfileFor(protocol.Version2022), sn, "34020000001320000001", "ERROR")
	require.NoError(t, err)
	handled, err := runtime.RouteBroadcastMessage(t.Context(), "34020000001320000001", body)
	require.True(t, handled)
	require.Error(t, err, "the original upstream is unavailable")
	leases, err := talk.NewGormRepo(db).ListCascadeLeases(t.Context())
	require.NoError(t, err)
	require.Empty(t, leases, "a refused broadcast must not retain the channel while upstream delivery fails")
	rows, err := store.ListNonterminalBroadcastSessions(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	row := &rows[0]
	var pending cascadeBroadcastBinding
	require.NoError(t, json.Unmarshal([]byte(row.SDPSummary), &pending))
	require.Equal(t, "ERROR", pending.DeviceResult)
	require.False(t, pending.ResultDelivered, "upstream delivery failure must remain retryable")
	// The retry uses the persisted result and must never notify the device again.
	old := time.Now().Add(-time.Minute)
	pending.ResultAttemptAt = &old
	payload, err := json.Marshal(pending)
	require.NoError(t, err)
	require.NoError(t, db.Model(row).Update("sdp_summary", string(payload)).Error)
	retryTransport := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
	runtime.clients.transport = retryTransport
	handled, err = runtime.RouteBroadcastMessage(t.Context(), "34020000001320000001", body)
	require.True(t, handled)
	require.NoError(t, err)
	select {
	case upstream := <-retryTransport.requests:
		response, parseErr := manscdp.ParseBroadcastResponse(upstream.Body())
		require.NoError(t, parseErr)
		require.Equal(t, "ERROR", response.Result)
	case <-time.After(time.Second):
		t.Fatal("persisted device rejection was not retried upstream")
	}
	require.EqualValues(t, 1, sends.Load())
}

func TestCascadeBroadcastBusinessFailureWaitsForAudioCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	cleaned := make(chan struct{})
	runtime := &cascadeBroadcastRuntime{audioSessions: map[string]*cascadeAudioSession{
		"device-dialog": {key: "broadcast-key", ctx: ctx, cancel: cancel, done: done},
	}}
	context.AfterFunc(ctx, func() {
		time.Sleep(40 * time.Millisecond)
		close(cleaned)
		close(done)
	})

	started := time.Now()
	require.NoError(t, runtime.stopCascadeAudioForBusinessEnd("broadcast-key"))
	require.GreaterOrEqual(t, time.Since(started), 30*time.Millisecond)
	select {
	case <-cleaned:
	default:
		t.Fatal("broadcast business result advanced before audio cleanup completed")
	}
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.True(t, runtime.audioSessions["device-dialog"].businessEnd)
}

type cascadeSIPFailureTransport struct{}

func (cascadeSIPFailureTransport) Do(context.Context, *sip.Request) (*sip.Response, error) {
	return nil, errors.New("upstream unavailable")
}
func (cascadeSIPFailureTransport) DoDigestAuth(context.Context, *sip.Request, *sip.Response, sipclient.Credentials) (*sip.Response, error) {
	return nil, errors.New("upstream unavailable")
}

func TestCascadeBroadcastLeaseSweepStopsExpiredAndRevokedReservations(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "revoked"}[revoke], func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			db, p, store := prepareCascadeBroadcastFixture(t)
			sender := cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
				return uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, nil
			})
			runtime := newCascadeBroadcastRuntime(db, store, newCascadePlatformClientFactory(&cascadeStatusTransport{}, nil, time.Second), sender)
			require.Equal(t, 200, broadcastLeaseStatus(t, runtime, broadcastLeaseRequest(t, p)))
			now := time.Now()
			if revoke {
				require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET enabled=0").Error)
			} else {
				now = now.Add(time.Minute)
			}
			require.NoError(t, runtime.Sweep(t.Context(), now))
			leases, err := talk.NewGormRepo(db).ListCascadeLeases(t.Context())
			require.NoError(t, err)
			require.Empty(t, leases)
			rows, err := store.ListNonterminalBroadcastSessions(t.Context())
			require.NoError(t, err)
			require.Empty(t, rows)
		})
	}
}

func TestCascadeBroadcastLeaseRecoveryPreservesWebAndVideo(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, p, store := prepareCascadeBroadcastFixture(t)
	sender := cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
		return uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, nil
	})
	runtime := newCascadeBroadcastRuntime(db, store, newCascadePlatformClientFactory(&cascadeStatusTransport{}, nil, time.Second), sender)
	require.Equal(t, 200, broadcastLeaseStatus(t, runtime, broadcastLeaseRequest(t, p)))
	repo := talk.NewGormRepo(db)
	_, err := repo.ReserveCascadeLease(t.Context(), "cascade-audio-orphan", 21, "34020000001320000001", "34020000001320000003", time.Now().Add(time.Minute))
	require.NoError(t, err)
	web := gbmodels.GbTalkSession{SessionID: "web-remains", ChannelID: 22, DeviceID: "34020000001320000001", State: gbmodels.TalkSessionReserved, ExpiresAt: time.Now().Add(time.Minute)}
	require.NoError(t, repo.Create(t.Context(), &web, "token"))
	video := model.GbCascadeMediaSession{Kind: model.CascadeMediaSessionKindVideo, PlatformID: p.ID, DialogKey: "video-remains", State: model.CascadeMediaSessionStateActive}
	require.NoError(t, store.CreateMediaSession(t.Context(), &video))
	restarted := newCascadeBroadcastRuntime(db, store, runtime.clients, sender)
	require.NoError(t, restarted.Recover(t.Context()))
	leases, err := repo.ListCascadeLeases(t.Context())
	require.NoError(t, err)
	require.Empty(t, leases)
	current, err := repo.FindBySession(t.Context(), web.SessionID)
	require.NoError(t, err)
	require.Equal(t, gbmodels.TalkSessionReserved, current.State)
	currentVideo, err := store.FindMediaSessionByDialog(t.Context(), video.DialogKey)
	require.NoError(t, err)
	require.Equal(t, model.CascadeMediaSessionStateActive, currentVideo.State)
}

func TestCascadeBroadcastLeaseShutdownCancelsInFlightNotify(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, p, store := prepareCascadeBroadcastFixture(t)
	entered := make(chan struct{})
	sender := cascadeRecordSenderFunc(func(ctx context.Context, _, _, _ string, _ []byte) (uac.TrackedMessageResult, error) {
		close(entered)
		<-ctx.Done()
		return uac.TrackedMessageResult{}, ctx.Err()
	})
	runtime := newCascadeBroadcastRuntime(db, store, newCascadePlatformClientFactory(&cascadeStatusTransport{}, nil, time.Second), sender)
	req := broadcastLeaseRequest(t, p)
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	done := make(chan struct{})
	go func() { runtime.Handle(req, tx); close(done) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("Notify not entered")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, runtime.Shutdown(ctx))
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("handler survived shutdown")
	}
	leases, err := talk.NewGormRepo(db).ListCascadeLeases(t.Context())
	require.NoError(t, err)
	require.Empty(t, leases)
	require.Equal(t, 503, broadcastLeaseStatus(t, runtime, req.Clone()))
}
