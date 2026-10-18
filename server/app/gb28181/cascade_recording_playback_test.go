package gb28181

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/media"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/play"
	gbplayback "uvplatform.com/uvp-gb28181/app/gb28181/playback"
	"uvplatform.com/uvp-gb28181/app/gb28181/routes"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type cascadePlaybackFake struct {
	mu        sync.Mutex
	requests  []gbplayback.CreateRequest
	actions   []gbplayback.ActionRequest
	actionErr error
	sessions  map[string]*gbplayback.Session
	stopped   chan string
}

func (f *cascadePlaybackFake) Create(_ context.Context, req gbplayback.CreateRequest) (gbplayback.CreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	id := fmt.Sprintf("playback-%d", len(f.requests))
	s := &gbplayback.Session{ID: id, OwnerID: req.OwnerID, NodeID: "1", StreamID: id, State: gbplayback.StatePlaying,
		SegmentStart: req.SegmentStart, SegmentEnd: req.SegmentEnd, BeforeCleanup: req.BeforeCleanup}
	f.sessions[id] = s
	return gbplayback.CreateResult{Session: s}, nil
}
func (f *cascadePlaybackFake) PeekForOwner(id, owner string) (*gbplayback.Session, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[id]
	if !ok || s.OwnerID != owner {
		return nil, false
	}
	copy := *s
	return &copy, true
}
func (f *cascadePlaybackFake) GetForOwner(id, owner string) (*gbplayback.Session, bool) {
	return f.PeekForOwner(id, owner)
}
func (f *cascadePlaybackFake) StopForOwner(ctx context.Context, id, owner, _ string) error {
	s, ok := f.PeekForOwner(id, owner)
	if !ok {
		return gbplayback.ErrPlaybackNotFound
	}
	if s.BeforeCleanup != nil {
		if err := s.BeforeCleanup(ctx); err != nil {
			return err
		}
	}
	f.mu.Lock()
	f.sessions[id].State = gbplayback.StateStopped
	f.mu.Unlock()
	f.stopped <- id
	return nil
}
func (f *cascadePlaybackFake) Action(_ context.Context, id, owner string, request gbplayback.ActionRequest) (*gbplayback.Session, error) {
	s, ok := f.PeekForOwner(id, owner)
	if !ok {
		return nil, gbplayback.ErrPlaybackNotFound
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, request)
	if f.actionErr != nil {
		return nil, f.actionErr
	}
	if request.Action == "pause" {
		f.sessions[id].State = gbplayback.StatePaused
	}
	if request.Action == "resume" {
		f.sessions[id].State = gbplayback.StatePlaying
	}
	return s, nil
}

func cascadePlaybackRequest(port int, ssrc string) *sip.Request {
	req := cascadeTestRequest(port, ssrc)
	req.SetBody([]byte(strings.Replace(strings.Replace(string(req.Body()), "s=Play\r\n", "s=Playback\r\n", 1), "t=0 0", "t=1785632400 1785636000", 1)))
	return req
}

func TestCascadePlaybackBootstrapWiresPlaybackBeforeVideoRuntime(t *testing.T) {
	body, err := os.ReadFile("bootstrap.go")
	require.NoError(t, err)
	source := string(body)
	cascadeRuntimeAt := strings.Index(source, "startCascadeRuntime(cfg, srv, credentialWarningReported)")
	playbackAt := strings.Index(source, "setupPlaybackRuntime(cfg, srv.UAC(), deviceOperations, deviceIntents)")
	cascadeAt := strings.Index(source, "setupCascadeVideoRuntime(srv)")
	require.Positive(t, cascadeRuntimeAt)
	require.Positive(t, playbackAt)
	require.Positive(t, cascadeAt)
	require.Less(t, playbackAt, cascadeAt, "cascade runtime must receive the initialized playback service")
	require.Less(t, cascadeAt, cascadeRuntimeAt, "upstream registration must start only after cascade INVITE handlers are ready")
}

type cascadeVideoAssemblyServer struct {
	*fakeSIPRuntimeServer
	client  *sipgo.Client
	handler func(*sip.Request, sip.ServerTransaction) bool
}

func (s *cascadeVideoAssemblyServer) NewCascadeClient() (*sipgo.Client, error) {
	return s.client, nil
}

func (s *cascadeVideoAssemblyServer) SetCascadeDialogHandler(handler func(*sip.Request, sip.ServerTransaction) bool) {
	s.handler = handler
}

func TestCascadeVideoInitialAssemblyCapturesReadyPlaybackService(t *testing.T) {
	db := cascadeInviteTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.GbCascadeMediaSession{}))

	previousPlaySvc, previousPlaybackService, previousRegistry := playSvc, playbackService, zlmRegistry
	previousCascadeVideo := cascadeVideo.Swap(nil)
	t.Cleanup(func() {
		playSvc, playbackService, zlmRegistry = previousPlaySvc, previousPlaybackService, previousRegistry
		cascadeVideo.Store(previousCascadeVideo)
		routes.SetCascadeSourceLeaseChecker(nil)
	})

	playSvc = &play.Service{}
	playbackService = gbplayback.NewService(gbplayback.NewRegistry(gbplayback.RegistryConfig{}), nil, nil, nil, nil, gbplayback.ServiceConfig{})
	zlmRegistry = node.NewRegistry(nil)
	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ua.Close() })
	client, err := sipgo.NewClient(ua)
	require.NoError(t, err)
	server := &cascadeVideoAssemblyServer{fakeSIPRuntimeServer: &fakeSIPRuntimeServer{}, client: client}

	require.NoError(t, setupCascadeVideoRuntime(server))
	runtime := cascadeVideo.Load()
	require.NotNil(t, runtime)
	require.Same(t, playbackService, runtime.playback, "first cascade runtime must capture the playback service assembled by bootstrap")
	require.NotNil(t, server.handler)
}

func TestCascadePlaybackRequiresRecordingAuthorization(t *testing.T) {
	h, _, _ := newCascadeVideoTestRuntime(t)
	req := cascadePlaybackRequest(15060, "1200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)
	require.Equal(t, 403, cascadeAwaitResponse(t, tx).StatusCode)
}

func TestCascadePlaybackSameChannelUsesIndependentSourcesPerDialog(t *testing.T) {
	h, fake, _ := newCascadePlaybackTestRuntime(t)
	requests := []*sip.Request{cascadePlaybackRequest(15060, "1200000001"), cascadePlaybackRequest(15060, "1200000002")}
	responses := make([]*sip.Response, len(requests))
	finished := make([]chan struct{}, len(requests))
	for index, req := range requests {
		id := sip.CallIDHeader(fmt.Sprintf("playback-call-%d", index))
		req.ReplaceHeader(&id)
		tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
		defer tx.Terminate()
		finished[index] = make(chan struct{})
		go func() { h.Handle(req, tx); close(finished[index]) }()
		responses[index] = cascadeAwaitResponse(t, tx)
		require.Equal(t, 200, responses[index].StatusCode)
		require.True(t, h.Handle(cascadeDialogRequest(req, responses[index], sip.ACK), tx))
	}
	for index, req := range requests {
		bye := cascadeDialogRequest(req, responses[index], sip.BYE)
		tx := siptest.NewServerTxRecorder(bye)
		defer tx.Terminate()
		require.True(t, h.Handle(bye, tx))
		select {
		case <-finished[index]:
		case <-time.After(3 * time.Second):
			t.Fatal("independent playback not stopped")
		}
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	require.Len(t, fake.requests, 2)
	require.NotEqual(t, fake.requests[0].OwnerID, fake.requests[1].OwnerID)
	require.Equal(t, fake.requests[0].ChannelID, fake.requests[1].ChannelID)
	require.False(t, h.sources.HasLease("shared-source"))
}

func newCascadePlaybackTestRuntime(t *testing.T, stopFailures ...int) (*cascadeVideoRuntime, *cascadePlaybackFake, chan string) {
	h, db, stops := newCascadeVideoTestRuntime(t, stopFailures...)
	require.NoError(t, db.Model(&model.GbCascadePlatform{}).Where("id = 1").Update("recording_enabled", true).Error)
	fake := &cascadePlaybackFake{sessions: map[string]*gbplayback.Session{}, stopped: make(chan string, 10)}
	h.playback = fake
	h.client.TxRequester = &siptest.ClientTxRequester{OnRequest: func(request *sip.Request) *sip.Response { return sip.NewResponseFromRequest(request, 200, "OK", nil) }}
	h.monitorInterval = 10 * time.Millisecond
	return h, fake, stops
}

func TestCascadePlaybackUnacknowledgedAnswerReleasesSource(t *testing.T) {
	oldT1 := sip.T1
	sip.T1 = 2 * time.Millisecond
	defer func() { sip.T1 = oldT1 }()
	h, fake, stops := newCascadePlaybackTestRuntime(t)
	req := cascadePlaybackRequest(15060, "1200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 32)}
	defer tx.Terminate()
	finished := make(chan struct{})
	go func() { h.Handle(req, tx); close(finished) }()
	require.Equal(t, 200, cascadeAwaitResponse(t, tx).StatusCode)
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("unacknowledged playback leaked")
	}
	select {
	case <-stops:
	default:
		t.Fatal("unacknowledged sender not stopped")
	}
	select {
	case <-fake.stopped:
	default:
		t.Fatal("unacknowledged source not stopped")
	}
}

func TestCascadePlaybackSourceEndedAndSharingRevocationCleanSessions(t *testing.T) {
	for _, cause := range []string{"source-ended", "sharing-revoked", "permission-revoked"} {
		t.Run(cause, func(t *testing.T) {
			h, fake, stops := newCascadePlaybackTestRuntime(t)
			var byes atomic.Int32
			h.client.TxRequester = &siptest.ClientTxRequester{OnRequest: func(request *sip.Request) *sip.Response {
				if request.Method == sip.BYE {
					byes.Add(1)
				}
				return sip.NewResponseFromRequest(request, 200, "OK", nil)
			}}
			req := cascadePlaybackRequest(15060, "1200000001")
			tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
			defer tx.Terminate()
			finished := make(chan struct{})
			go func() { h.Handle(req, tx); close(finished) }()
			res := cascadeAwaitResponse(t, tx)
			require.Equal(t, 200, res.StatusCode)
			require.True(t, h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx))
			if cause == "source-ended" {
				fake.mu.Lock()
				fake.sessions["playback-1"].State = gbplayback.StateEnded
				fake.mu.Unlock()
			} else {
				p, err := h.store.FindPlatform(context.Background(), 1)
				require.NoError(t, err)
				if cause == "sharing-revoked" {
					p.ProjectionRevision++
				} else {
					p.RecordingEnabled = false
					p.ConfigRevision++
				}
				require.NoError(t, app.DB().Model(&model.GbCascadePlatform{}).Where("id = 1").Updates(map[string]interface{}{
					"projection_revision": p.ProjectionRevision, "config_revision": p.ConfigRevision, "recording_enabled": p.RecordingEnabled,
				}).Error)
				require.NoError(t, h.revalidate(context.Background()))
			}
			select {
			case <-finished:
			case <-time.After(3 * time.Second):
				t.Fatal("obsolete playback not cleaned")
			}
			select {
			case <-stops:
			default:
				t.Fatal("sender not stopped")
			}
			select {
			case <-fake.stopped:
			default:
				t.Fatal("source not stopped")
			}
			require.EqualValues(t, 1, byes.Load(), "confirmed cascade dialog must send exactly one BYE")
		})
	}
}

type cascadeMonitorSender struct {
	active     atomic.Bool
	stopCalls  atomic.Int32
	errorsLeft atomic.Int32
}

func (f *cascadeMonitorSender) StartGBSendRTP(context.Context, zlm.GBSendRTPRequest) (*zlm.GBSendRTPResult, error) {
	return &zlm.GBSendRTPResult{LocalPort: 31000}, nil
}
func (f *cascadeMonitorSender) StopSendRtp(context.Context, string, string, string, string) error {
	f.stopCalls.Add(1)
	return nil
}
func (f *cascadeMonitorSender) IsGBSendRTPActive(context.Context, zlm.GBSendRTPRequest) (bool, error) {
	if f.errorsLeft.Load() > 0 {
		f.errorsLeft.Add(-1)
		return false, fmt.Errorf("temporary sender probe failure")
	}
	return f.active.Load(), nil
}

func TestCascadeRTCPInactiveSenderEndsUpstreamDialog(t *testing.T) {
	h, db, stops := newCascadeVideoTestRuntime(t)
	require.NoError(t, db.Model(&model.GbCascadePlatform{}).Where("id = 1").Update("rtcp_keepalive", true).Error)
	h.monitorInterval = 10 * time.Millisecond
	var byes atomic.Int32
	h.client.TxRequester = &siptest.ClientTxRequester{OnRequest: func(request *sip.Request) *sip.Response {
		if request.Method == sip.BYE {
			byes.Add(1)
		}
		return sip.NewResponseFromRequest(request, 200, "OK", nil)
	}}
	check := &cascadeMonitorSender{}
	check.active.Store(true)
	h.sender = media.NewSender(h.nodes, func(*node.Node) media.GBSenderClient { return check })
	req := cascadeTestRequest(15060, "0200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	finished := make(chan struct{})
	go func() { h.Handle(req, tx); close(finished) }()
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 200, res.StatusCode)
	require.True(t, h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx))
	check.active.Store(false)
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("inactive sender not cleaned")
	}
	require.EqualValues(t, 1, byes.Load())
	require.False(t, h.sources.HasLease("shared-source"))
	select {
	case <-stops:
	default:
		t.Fatal("sender stop not confirmed")
	}
}

func TestCascadeSenderMonitoringCoversRTCPDisabledUDPAndTCP(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tcp   bool
		setup string
	}{
		{name: "udp without rtcp", tcp: false},
		{name: "tcp passive sender without rtcp", tcp: true, setup: "active"},
		{name: "tcp active sender without rtcp", tcp: true, setup: "passive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, stops := newCascadeVideoTestRuntime(t)
			h.monitorInterval = 10 * time.Millisecond
			var byes atomic.Int32
			h.client.TxRequester = &siptest.ClientTxRequester{OnRequest: func(request *sip.Request) *sip.Response {
				if request.Method == sip.BYE {
					byes.Add(1)
				}
				return sip.NewResponseFromRequest(request, 200, "OK", nil)
			}}
			check := &cascadeMonitorSender{}
			check.active.Store(true)
			h.sender = media.NewSender(h.nodes, func(*node.Node) media.GBSenderClient { return check })
			req := cascadeTestRequest(15060, "0200000001")
			if tc.tcp {
				body := strings.Replace(string(req.Body()), "RTP/AVP", "TCP/RTP/AVP", 1)
				body = strings.Replace(body, "a=recvonly\r\n", "a=recvonly\r\na=setup:"+tc.setup+"\r\na=connection:new\r\n", 1)
				req.SetBody([]byte(body))
			}
			tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
			defer tx.Terminate()
			finished := make(chan struct{})
			go func() { h.Handle(req, tx); close(finished) }()
			res := cascadeAwaitResponse(t, tx)
			require.Equal(t, 200, res.StatusCode)
			require.True(t, h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx))
			check.active.Store(false)
			select {
			case <-finished:
			case <-time.After(3 * time.Second):
				t.Fatal("inactive sender not cleaned")
			}
			require.EqualValues(t, 1, byes.Load())
			select {
			case <-stops:
			default:
				t.Fatal("sender stop not confirmed")
			}
		})
	}
}

func TestCascadeSenderProbeTemporaryErrorsDoNotEndSessionImmediately(t *testing.T) {
	h, _, _ := newCascadeVideoTestRuntime(t)
	h.monitorInterval = 10 * time.Millisecond
	check := &cascadeMonitorSender{}
	check.active.Store(true)
	check.errorsLeft.Store(2)
	h.sender = media.NewSender(h.nodes, func(*node.Node) media.GBSenderClient { return check })
	req := cascadeTestRequest(15060, "0200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	finished := make(chan struct{})
	go func() { h.Handle(req, tx); close(finished) }()
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 200, res.StatusCode)
	require.True(t, h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx))
	select {
	case <-finished:
		t.Fatal("temporary sender probe errors ended the session")
	case <-time.After(80 * time.Millisecond):
	}
	check.active.Store(false)
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("inactive sender was not cleaned after probe recovered")
	}
}

func TestCascadePlaybackIndependentSourceControlsAndBye(t *testing.T) {
	h, fake, stops := newCascadePlaybackTestRuntime(t)
	req := cascadePlaybackRequest(15060, "1200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	finished := make(chan struct{})
	go func() { h.Handle(req, tx); close(finished) }()
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 200, res.StatusCode)
	require.Contains(t, string(res.Body()), "s=Playback\r\n")
	require.True(t, h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx))
	info := cascadeDialogRequest(req, res, sip.INFO)
	info.CSeq().SeqNo = 2
	info.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSRTSP"))
	info.SetBody([]byte("PAUSE RTSP/1.0\r\nCSeq: 7\r\nPauseTime: now\r\n\r\n"))
	wrong := info.Clone()
	wrong.SetSource("10.8.0.2:15060")
	wrongTx := siptest.NewServerTxRecorder(wrong)
	defer wrongTx.Terminate()
	require.True(t, h.Handle(wrong, wrongTx))
	require.Equal(t, 403, wrongTx.Result()[0].StatusCode)
	infoTx := siptest.NewServerTxRecorder(info)
	defer infoTx.Terminate()
	require.True(t, h.Handle(info, infoTx))
	require.Equal(t, 200, infoTx.Result()[0].StatusCode)
	require.Contains(t, string(infoTx.Result()[0].Body()), "CSeq: 7\r\n")
	bye := cascadeDialogRequest(req, res, sip.BYE)
	bye.CSeq().SeqNo = 3
	byeTx := siptest.NewServerTxRecorder(bye)
	defer byeTx.Terminate()
	require.True(t, h.Handle(bye, byeTx))
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("playback not cleaned")
	}
	select {
	case ssrc := <-stops:
		require.Equal(t, "1200000001", ssrc)
	default:
		t.Fatal("sender not stopped before source")
	}
	select {
	case <-fake.stopped:
	default:
		t.Fatal("playback source not stopped")
	}
	require.Empty(t, stops, "playback source cleanup must not stop a confirmed sender a second time")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	require.Len(t, fake.requests, 1)
	require.True(t, strings.HasPrefix(fake.requests[0].OwnerID, "cascade:1:"))
	require.EqualValues(t, 1785632400, fake.requests[0].SegmentStart.Unix())
	require.Equal(t, "pause", fake.actions[0].Action)
	require.False(t, h.sources.HasLease("shared-source"))
}

func TestCascadePlaybackRangeReachesServiceAndReportsControlFailure(t *testing.T) {
	h, fake, _ := newCascadePlaybackTestRuntime(t)
	defer cleanupCascadeSubjectRuntime(h)
	req := cascadePlaybackRequest(15060, "1200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 200, res.StatusCode)
	h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx)
	scaleInfo := cascadeDialogRequest(req, res, sip.INFO)
	scaleInfo.CSeq().SeqNo = 2
	scaleInfo.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSRTSP"))
	scaleInfo.SetBody([]byte("PLAY RTSP/1.0\r\nCSeq: 10\r\nScale: 2\r\n\r\n"))
	scaleTx := siptest.NewServerTxRecorder(scaleInfo)
	defer scaleTx.Terminate()
	require.True(t, h.Handle(scaleInfo, scaleTx))
	require.Contains(t, string(scaleTx.Result()[0].Body()), "RTP-Info: url=rtsp://192.0.2.20/rtp/source-1;seq=9;rtptime=9000")
	pauseInfo := cascadeDialogRequest(req, res, sip.INFO)
	pauseInfo.CSeq().SeqNo = 3
	pauseInfo.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSRTSP"))
	pauseInfo.SetBody([]byte("PAUSE RTSP/1.0\r\nCSeq: 11\r\nPauseTime: now\r\n\r\n"))
	pauseTx := siptest.NewServerTxRecorder(pauseInfo)
	defer pauseTx.Terminate()
	require.True(t, h.Handle(pauseInfo, pauseTx))
	require.NotContains(t, string(pauseTx.Result()[0].Body()), "RTP-Info:")
	for i, value := range []string{"npt=12.5-30", "smpte=0:00:12:15-0:00:30:00"} {
		info := cascadeDialogRequest(req, res, sip.INFO)
		info.CSeq().SeqNo = uint32(i + 4)
		info.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSRTSP"))
		info.SetBody([]byte(fmt.Sprintf("PLAY RTSP/1.0\r\nCSeq: %d\r\nRange: %s\r\n\r\n", i+7, value)))
		infoTx := siptest.NewServerTxRecorder(info)
		defer infoTx.Terminate()
		require.True(t, h.Handle(info, infoTx))
		require.Contains(t, string(infoTx.Result()[0].Body()), "RTSP/1.0 200 OK")
		fake.mu.Lock()
		got := fake.actions[i+2].RangeValue
		fake.mu.Unlock()
		require.Equal(t, value, got)
	}
	fake.mu.Lock()
	fake.actionErr = gbplayback.ErrPlaybackRangeUnsupported
	fake.mu.Unlock()
	info := cascadeDialogRequest(req, res, sip.INFO)
	info.CSeq().SeqNo = 5
	info.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSRTSP"))
	info.SetBody([]byte("PLAY RTSP/1.0\r\nCSeq: 9\r\nRange: npt=1-30\r\n\r\n"))
	infoTx := siptest.NewServerTxRecorder(info)
	defer infoTx.Terminate()
	require.True(t, h.Handle(info, infoTx))
	require.Contains(t, string(infoTx.Result()[0].Body()), "RTSP/1.0 501 Range Not Supported")
}

func TestCascadePlaybackCleanupStopFailureKeepsDialogAndSource(t *testing.T) {
	h, fake, stops := newCascadePlaybackTestRuntime(t, 1)
	defer cleanupCascadeSubjectRuntime(h)
	var byes atomic.Int32
	h.client.TxRequester = &siptest.ClientTxRequester{OnRequest: func(r *sip.Request) *sip.Response {
		if r.Method == sip.BYE {
			byes.Add(1)
		}
		return sip.NewResponseFromRequest(r, 200, "OK", nil)
	}}
	req := cascadePlaybackRequest(15060, "1200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 200, res.StatusCode)
	h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx)
	source, ok := fake.PeekForOwner("playback-1", "cascade:1:"+dialogIDForCascadeTest(t, req, res))
	require.True(t, ok)
	require.Error(t, source.BeforeCleanup(context.Background()))
	require.Len(t, stops, 1)
	require.Zero(t, byes.Load())
	require.Empty(t, fake.stopped)
	h.mu.Lock()
	for _, session := range h.sessions {
		require.NoError(t, session.context.Err())
	}
	h.mu.Unlock()
}

func dialogIDForCascadeTest(t *testing.T, req *sip.Request, res *sip.Response) string {
	t.Helper()
	ack := cascadeDialogRequest(req, res, sip.ACK)
	id, err := sip.DialogIDFromRequestUAS(ack)
	require.NoError(t, err)
	return id
}

func TestCascadeSenderRepeatedProbeErrorsEndSession(t *testing.T) {
	h, _, stops := newCascadeVideoTestRuntime(t)
	defer cleanupCascadeSubjectRuntime(h)
	h.monitorInterval = 10 * time.Millisecond
	check := &cascadeMonitorSender{}
	check.active.Store(true)
	check.errorsLeft.Store(3)
	h.sender = media.NewSender(h.nodes, func(*node.Node) media.GBSenderClient { return check })
	h.client.TxRequester = &siptest.ClientTxRequester{OnRequest: func(r *sip.Request) *sip.Response { return sip.NewResponseFromRequest(r, 200, "OK", nil) }}
	req := cascadeTestRequest(15060, "0200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	done := make(chan struct{})
	go func() { h.Handle(req, tx); close(done) }()
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 200, res.StatusCode)
	h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("repeated probe errors left sender leased")
	}
	require.Len(t, stops, 1)
	require.False(t, h.sources.HasLease("shared-source"))
}
