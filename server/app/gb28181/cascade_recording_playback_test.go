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
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/play"
	gbplayback "uvplatform.com/uvp-gb28181/app/gb28181/playback"
	"uvplatform.com/uvp-gb28181/app/gb28181/recordquery"
	"uvplatform.com/uvp-gb28181/app/gb28181/routes"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
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
	req.SetBody([]byte(strings.Replace(strings.Replace(string(req.Body()), "s=Play\r\n", "s=Playback\r\nu="+req.Recipient.User+":0\r\n", 1), "t=0 0", "t=1785632400 1785636000", 1)))
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
	previousRecordQueryService := recordQueryService
	previousCascadeVideo := cascadeVideo.Swap(nil)
	t.Cleanup(func() {
		playSvc, playbackService, zlmRegistry = previousPlaySvc, previousPlaybackService, previousRegistry
		recordQueryService = previousRecordQueryService
		cascadeVideo.Store(previousCascadeVideo)
		routes.SetCascadeSourceLeaseChecker(nil)
	})

	playSvc = &play.Service{}
	playbackService = gbplayback.NewService(gbplayback.NewRegistry(gbplayback.RegistryConfig{}), nil, nil, nil, nil, gbplayback.ServiceConfig{})
	zlmRegistry = node.NewRegistry(nil)
	queryService, err := recordquery.NewService(cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
		return uac.TrackedMessageResult{}, nil
	}), recordquery.Options{Timeout: time.Second, MaxActiveQueries: 1, MaxRecordsPerQuery: 10, ResultTTL: time.Minute, Location: time.FixedZone("CST", 8*60*60)})
	require.NoError(t, err)
	t.Cleanup(queryService.Close)
	recordQueryService = queryService
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
	require.Same(t, queryService, runtime.recordings)
	require.Same(t, queryService.Location(), runtime.recordingLocation)
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

func TestCascadeDownloadCreatesDownloadSourceAndCleansOnBye(t *testing.T) {
	h, fake, stops := newCascadePlaybackTestRuntime(t)
	req := cascadePlaybackRequest(15060, "1200000001")
	req.SetBody([]byte(strings.Replace(string(req.Body()), "s=Playback\r\n", "s=Download\r\n", 1) + "a=downloadspeed:2\r\n"))
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	finished := make(chan struct{})
	go func() { h.Handle(req, tx); close(finished) }()
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 200, res.StatusCode)
	require.Contains(t, string(res.Body()), "s=Download\r\n")
	require.True(t, h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx))
	fake.mu.Lock()
	requests := append([]gbplayback.CreateRequest(nil), fake.requests...)
	fake.mu.Unlock()
	require.Len(t, requests, 1)
	require.Equal(t, gbplayback.ModeDownload, requests[0].Mode)
	require.Equal(t, uint32(2), requests[0].DownloadSpeed)
	require.EqualValues(t, 1785632400, requests[0].SegmentStart.Unix())
	require.EqualValues(t, 1785636000, requests[0].SegmentEnd.Unix())
	bye := cascadeDialogRequest(req, res, sip.BYE)
	byeTx := siptest.NewServerTxRecorder(bye)
	defer byeTx.Terminate()
	require.True(t, h.Handle(bye, byeTx))
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("download session did not stop after BYE")
	}
	require.NotEmpty(t, stops)
	require.NotEmpty(t, fake.stopped)
}

func TestCascadeDownloadRejectsDifferentRecordingTarget(t *testing.T) {
	h, fake, _ := newCascadePlaybackTestRuntime(t)
	req := cascadePlaybackRequest(15060, "1200000001")
	req.SetBody([]byte(strings.Replace(strings.Replace(string(req.Body()), "s=Playback", "s=Download", 1), "u="+req.Recipient.User+":0", "u=34020000001320000999:2", 1)))
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)
	require.Equal(t, 403, cascadeAwaitResponse(t, tx).StatusCode)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	require.Empty(t, fake.requests, "unpublished recording must never start a source")
}

func TestCascadeDownloadRequiresRecordingAuthorization(t *testing.T) {
	h, _, _ := newCascadeVideoTestRuntime(t)
	req := cascadePlaybackRequest(15060, "1200000001")
	req.SetBody([]byte(strings.Replace(string(req.Body()), "s=Playback", "s=Download", 1)))
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)
	require.Equal(t, 403, cascadeAwaitResponse(t, tx).StatusCode)
}

func TestCascadeDownloadSendsCompletionOnlyForFileEnd(t *testing.T) {
	for _, cause := range []string{"file-to-end", "media-offline", "playback session expired"} {
		t.Run(cause, func(t *testing.T) {
			h, fake, _ := newCascadePlaybackTestRuntime(t)
			messages := make(chan *sip.Request, 3)
			h.client.TxRequester = &siptest.ClientTxRequester{OnRequest: func(req *sip.Request) *sip.Response {
				messages <- req.Clone()
				return sip.NewResponseFromRequest(req, 200, "OK", nil)
			}}
			req := cascadePlaybackRequest(15060, "1200000001")
			req.SetBody([]byte(strings.Replace(string(req.Body()), "s=Playback", "s=Download", 1)))
			tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
			defer tx.Terminate()
			done := make(chan struct{})
			go func() { h.Handle(req, tx); close(done) }()
			res := cascadeAwaitResponse(t, tx)
			require.Equal(t, 200, res.StatusCode)
			require.True(t, h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx))
			fake.mu.Lock()
			fake.sessions["playback-1"].State = gbplayback.StateEnded
			fake.sessions["playback-1"].EndReason = cause
			fake.mu.Unlock()
			first := awaitCascadeStatusResponse(t, messages)
			if cause == "file-to-end" {
				require.Equal(t, sip.MESSAGE, first.Method)
				notify, err := manscdp.ParseMediaStatusNotify(first.Body())
				require.NoError(t, err)
				require.Equal(t, "121", notify.NotifyType)
				require.Equal(t, req.Recipient.User, notify.DeviceID)
				require.Equal(t, req.CallID().Value(), first.CallID().Value())
				from := first.From().AsTo()
				require.Equal(t, res.To().Value(), from.Value())
				require.NotContains(t, string(first.Body()), "source-only")
				select {
				case unexpected := <-messages:
					t.Fatalf("sender must wait for receiver BYE; got %s", unexpected.Method)
				case <-done:
					t.Fatal("download dialog closed before receiver BYE")
				case <-time.After(50 * time.Millisecond):
				}
				bye := cascadeDialogRequest(req, res, sip.BYE)
				byeTx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(bye), make(chan *sip.Response, 10)}
				defer byeTx.Terminate()
				require.True(t, h.Handle(bye, byeTx))
				require.Equal(t, 200, cascadeAwaitResponse(t, byeTx).StatusCode)
			} else {
				require.Equal(t, sip.BYE, first.Method, "interruptions must not claim successful download")
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("download did not finish")
			}
			require.Empty(t, messages)
		})
	}
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
	mu         sync.Mutex
	last       zlm.GBSendRTPRequest
}

func (f *cascadeMonitorSender) StartGBSendRTP(_ context.Context, request zlm.GBSendRTPRequest) (*zlm.GBSendRTPResult, error) {
	f.mu.Lock()
	f.last = request
	f.mu.Unlock()
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

func TestCascadePlaybackAdvertisesLocalRTPInfoURL(t *testing.T) {
	h, db, _ := newCascadeVideoTestRuntime(t)
	defer cleanupCascadeSubjectRuntime(h)
	require.NoError(t, db.Model(&model.GbCascadePlatform{}).Where("id = 1").Updates(map[string]interface{}{"media_advertise_ip": "192.0.2.10", "recording_enabled": true}).Error)
	fake := &cascadePlaybackFake{sessions: map[string]*gbplayback.Session{}, stopped: make(chan string, 10)}
	h.playback = fake
	monitor := &cascadeMonitorSender{}
	monitor.active.Store(true)
	h.sender = media.NewSender(h.nodes, func(*node.Node) media.GBSenderClient { return monitor })
	h.client.TxRequester = &siptest.ClientTxRequester{OnRequest: func(request *sip.Request) *sip.Response {
		return sip.NewResponseFromRequest(request, 200, "OK", nil)
	}}
	req := cascadePlaybackRequest(15060, "1200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 200, res.StatusCode)
	monitor.mu.Lock()
	got := monitor.last.RTPInfoURL
	monitor.mu.Unlock()
	require.Equal(t, "rtsp://192.0.2.10/rtp/playback-1", got)
	h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx)
	bye := cascadeDialogRequest(req, res, sip.BYE)
	byeTx := siptest.NewServerTxRecorder(bye)
	defer byeTx.Terminate()
	require.True(t, h.Handle(bye, byeTx))
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
	require.Contains(t, string(scaleTx.Result()[0].Body()), "RTP-Info: url=rtsp://192.168.10.220/rtp/playback-1;seq=9;rtptime=9000")
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

type delayedCascadeRTPInfoSender struct {
	mu          sync.Mutex
	reads       int
	resetCalls  int
	firstPacket *zlm.GBRTPInfo
}

func (s *delayedCascadeRTPInfoSender) StartGBSendRTP(context.Context, zlm.GBSendRTPRequest) (*zlm.GBSendRTPResult, error) {
	return &zlm.GBSendRTPResult{LocalPort: 31000}, nil
}
func (*delayedCascadeRTPInfoSender) StopSendRtp(context.Context, string, string, string, string) error {
	return nil
}
func (s *delayedCascadeRTPInfoSender) ResetGBSendRTPInfo(context.Context, zlm.GBSendRTPRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetCalls++
	s.reads = 0
	s.firstPacket = &zlm.GBRTPInfo{URL: "rtsp://192.168.10.220/rtp/playback-1", Seq: 22, RTPTime: 22000}
	return nil
}
func (s *delayedCascadeRTPInfoSender) GetGBSendRTPInfo(context.Context, zlm.GBSendRTPRequest) (*zlm.GBRTPInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	if s.reads < 2 {
		return nil, nil
	}
	return s.firstPacket, nil
}

func TestCascadePlaybackScaleRefreshesFirstPacketRTPInfo(t *testing.T) {
	h, db, _ := newCascadeVideoTestRuntime(t)
	defer cleanupCascadeSubjectRuntime(h)
	require.NoError(t, db.Model(&model.GbCascadePlatform{}).Where("id = 1").Updates(map[string]interface{}{"media_advertise_ip": "192.0.2.10", "recording_enabled": true}).Error)
	fake := &cascadePlaybackFake{sessions: map[string]*gbplayback.Session{}, stopped: make(chan string, 10)}
	h.playback = fake
	sender := &delayedCascadeRTPInfoSender{}
	h.sender = media.NewSender(h.nodes, func(*node.Node) media.GBSenderClient { return sender })
	h.client.TxRequester = &siptest.ClientTxRequester{OnRequest: func(request *sip.Request) *sip.Response {
		return sip.NewResponseFromRequest(request, 200, "OK", nil)
	}}
	req := cascadePlaybackRequest(15060, "1200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)
	res := cascadeAwaitResponse(t, tx)
	h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx)

	info := cascadeDialogRequest(req, res, sip.INFO)
	info.CSeq().SeqNo = 2
	info.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSRTSP"))
	info.SetBody([]byte("PLAY RTSP/1.0\r\nCSeq: 10\r\nScale: 2\r\n\r\n"))
	infoTx := siptest.NewServerTxRecorder(info)
	defer infoTx.Terminate()
	require.True(t, h.Handle(info, infoTx))
	body := string(infoTx.Result()[0].Body())
	require.Contains(t, body, "RTP-Info: url=rtsp://192.168.10.220/rtp/playback-1;seq=22;rtptime=22000")
	sender.mu.Lock()
	defer sender.mu.Unlock()
	require.Equal(t, 1, sender.resetCalls)
	require.GreaterOrEqual(t, sender.reads, 2, "must wait for the post-PLAY packet sample")
}

func TestCascadePlaybackLegacySenderDoesNotWaitForOptionalRTPInfo(t *testing.T) {
	h, db, _ := newCascadeVideoTestRuntime(t)
	defer cleanupCascadeSubjectRuntime(h)
	require.NoError(t, db.Model(&model.GbCascadePlatform{}).Where("id = 1").Updates(map[string]interface{}{"media_advertise_ip": "192.0.2.10", "recording_enabled": true}).Error)
	fake := &cascadePlaybackFake{sessions: map[string]*gbplayback.Session{}, stopped: make(chan string, 10)}
	h.playback = fake
	sender := &cascadeMonitorSender{}
	h.sender = media.NewSender(h.nodes, func(*node.Node) media.GBSenderClient { return sender })
	h.client.TxRequester = &siptest.ClientTxRequester{OnRequest: func(request *sip.Request) *sip.Response {
		return sip.NewResponseFromRequest(request, 200, "OK", nil)
	}}
	req := cascadePlaybackRequest(15060, "1200000001")
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)
	res := cascadeAwaitResponse(t, tx)
	h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx)

	info := cascadeDialogRequest(req, res, sip.INFO)
	info.CSeq().SeqNo = 2
	info.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSRTSP"))
	info.SetBody([]byte("PLAY RTSP/1.0\r\nCSeq: 10\r\nScale: 2\r\n\r\n"))
	infoTx := siptest.NewServerTxRecorder(info)
	defer infoTx.Terminate()
	start := time.Now()
	require.True(t, h.Handle(info, infoTx))
	require.Less(t, time.Since(start), 500*time.Millisecond)
	require.NotContains(t, string(infoTx.Result()[0].Body()), "RTP-Info:")
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

// Keep the real registry's stopping/finalization semantics while replacing only
// the downstream network/media provisioning in this local cascade test.
type cascadeRegistryPlayback struct {
	*gbplayback.Service
	registry  *gbplayback.Registry
	resources gbplayback.CleanupResources
}

func (s *cascadeRegistryPlayback) Create(ctx context.Context, request gbplayback.CreateRequest) (gbplayback.CreateResult, error) {
	request.Resources = s.resources
	created, err := s.registry.Create(ctx, request)
	if err != nil {
		return created, err
	}
	err = s.registry.Update(created.Session.ID, func(current *gbplayback.Session) error {
		current.NodeID, current.StreamID, current.State = "1", "download-registry", gbplayback.StatePlaying
		return nil
	})
	created.Session, _ = s.registry.Get(created.Session.ID)
	return created, err
}

type cascadeDownloadCleanup struct {
	started chan struct{}
	finish  <-chan struct{}
}

func (c *cascadeDownloadCleanup) Teardown(ctx context.Context) error {
	close(c.started)
	select {
	case <-c.finish:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (*cascadeDownloadCleanup) CloseRTP(context.Context) error { return nil }
func (*cascadeDownloadCleanup) Unbind(context.Context) error   { return nil }

func TestCascadeDownloadWaitsForRegistryFinalizationBeforeCompletion(t *testing.T) {
	h, _, stops := newCascadePlaybackTestRuntime(t)
	finish := make(chan struct{})
	cleanup := &cascadeDownloadCleanup{started: make(chan struct{}), finish: finish}
	registry := gbplayback.NewRegistry(gbplayback.RegistryConfig{})
	h.playback = &cascadeRegistryPlayback{Service: gbplayback.NewService(registry, nil, nil, nil, nil, gbplayback.ServiceConfig{}), registry: registry, resources: cleanup}
	messages := make(chan *sip.Request, 3)
	h.client.TxRequester = &siptest.ClientTxRequester{OnRequest: func(req *sip.Request) *sip.Response {
		messages <- req.Clone()
		return sip.NewResponseFromRequest(req, 200, "OK", nil)
	}}
	req := cascadePlaybackRequest(15060, "1200000001")
	req.SetBody([]byte(strings.Replace(string(req.Body()), "s=Playback", "s=Download", 1)))
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	done := make(chan struct{})
	go func() { h.Handle(req, tx); close(done) }()
	res := cascadeAwaitResponse(t, tx)
	require.Equal(t, 200, res.StatusCode)
	require.True(t, h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx))
	h.mu.Lock()
	var lease *cascadePlaybackLease
	for _, session := range h.sessions {
		lease = session.playback
	}
	h.mu.Unlock()
	require.NotNil(t, lease)
	finalized := make(chan error, 1)
	go func() { finalized <- registry.Finalize(lease.id, gbplayback.StateEnded, "file-to-end") }()
	select {
	case <-cleanup.started:
	case <-time.After(3 * time.Second):
		t.Fatal("source cleanup did not start")
	}
	current, ok := registry.Get(lease.id)
	require.True(t, ok)
	require.Equal(t, gbplayback.StateStopping, current.State)
	select {
	case <-stops:
	default:
		t.Fatal("sender must stop before releasing source")
	}
	select {
	case early := <-messages:
		t.Fatalf("notification before source cleanup outcome: %s", early.Method)
	case <-done:
		t.Fatal("sender guard canceled download before finalization")
	case <-time.After(50 * time.Millisecond):
	}
	close(finish)
	require.NoError(t, <-finalized)
	notify := awaitCascadeStatusResponse(t, messages)
	require.Equal(t, sip.MESSAGE, notify.Method)
	body, err := manscdp.ParseMediaStatusNotify(notify.Body())
	require.NoError(t, err)
	require.Equal(t, "121", body.NotifyType)
	bye := cascadeDialogRequest(req, res, sip.BYE)
	byeTx := siptest.NewServerTxRecorder(bye)
	defer byeTx.Terminate()
	require.True(t, h.Handle(bye, byeTx))
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("download did not close after receiver BYE")
	}
	require.Empty(t, messages)
	require.Empty(t, stops, "final cleanup must not stop the sender twice")
	require.Zero(t, registry.ActiveCount())
}

func TestCascadeDownloadCompletionFailureAndMissingByeReleaseDialog(t *testing.T) {
	for _, notifyStatus := range []int{200, 500} {
		t.Run(fmt.Sprint(notifyStatus), func(t *testing.T) {
			h, fake, _ := newCascadePlaybackTestRuntime(t)
			h.downloadByeTimeout = 60 * time.Millisecond
			messages := make(chan *sip.Request, 3)
			h.client.TxRequester = &siptest.ClientTxRequester{OnRequest: func(req *sip.Request) *sip.Response {
				messages <- req.Clone()
				status := 200
				if req.Method == sip.MESSAGE {
					status = notifyStatus
				}
				return sip.NewResponseFromRequest(req, status, "test", nil)
			}}
			req := cascadePlaybackRequest(15060, "1200000001")
			req.SetBody([]byte(strings.Replace(string(req.Body()), "s=Playback", "s=Download", 1)))
			tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
			defer tx.Terminate()
			done := make(chan struct{})
			go func() { h.Handle(req, tx); close(done) }()
			res := cascadeAwaitResponse(t, tx)
			require.Equal(t, 200, res.StatusCode)
			require.True(t, h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx))
			fake.mu.Lock()
			fake.sessions["playback-1"].State, fake.sessions["playback-1"].EndReason = gbplayback.StateEnded, "file-to-end"
			fake.mu.Unlock()
			first := awaitCascadeStatusResponse(t, messages)
			require.Equal(t, sip.MESSAGE, first.Method)
			bye := awaitCascadeStatusResponse(t, messages)
			require.Equal(t, sip.BYE, bye.Method)
			require.Greater(t, bye.CSeq().SeqNo, first.CSeq().SeqNo)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("download retained after completion timeout/failure")
			}
			require.Empty(t, messages)
		})
	}
}

func TestCascadeDownloadRewritesRecordingURIWithoutLosingSelector(t *testing.T) {
	for _, suffix := range []string{":2", "/records/one.ps"} {
		t.Run(suffix, func(t *testing.T) {
			h, fake, _ := newCascadePlaybackTestRuntime(t)
			published := "34020000001320000099"
			sourceCode := "34020000001320000010"
			require.NoError(t, app.DB().Table("gb_cascade_channel_projection").Where("platform_id = ?", 1).Update("published_channel_id", published).Error)
			req := cascadePlaybackRequest(15060, "1200000001")
			req.Recipient.User, req.To().Address.User = published, published
			subject := req.GetHeader("Subject").Value()
			req.RemoveHeader("Subject")
			req.AppendHeader(sip.NewHeader("Subject", strings.Replace(subject, sourceCode, published, 1)))
			uri := published + suffix
			wantURI := sourceCode + suffix
			if strings.HasPrefix(suffix, "/") {
				uri = "http://" + uri
				wantURI = "http://37010301021180000007" + suffix
				h.recordingLocation = time.FixedZone("CST", 8*60*60)
				h.recordings = cascadeRecordingQueryFunc(func(_ context.Context, request recordquery.CascadeQueryRequest) (recordquery.QueryResult, error) {
					require.Equal(t, sourceCode, request.ChannelCode)
					require.Equal(t, "37010301021180000007", request.DeviceCode)
					return recordquery.QueryResult{Records: []recordquery.Record{{RecordInfoItem: manscdp.RecordInfoItem{
						DeviceID: sourceCode, FilePath: wantURI,
						StartTime: recordquery.FormatMANSCDPWallClock(request.StartTime, h.recordingLocation),
						EndTime:   recordquery.FormatMANSCDPWallClock(request.EndTime, h.recordingLocation),
					}}}}, nil
				})
			}
			body := strings.Replace(string(req.Body()), "s=Playback", "s=Download", 1)
			body = strings.Replace(body, "u="+sourceCode+":0", "u="+uri, 1)
			req.SetBody([]byte(body))
			tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
			defer tx.Terminate()
			done := make(chan struct{})
			go func() { h.Handle(req, tx); close(done) }()
			res := cascadeAwaitResponse(t, tx)
			require.Equal(t, 200, res.StatusCode)
			require.Contains(t, string(res.Body()), "u="+uri+"\r\n")
			require.NotContains(t, string(res.Body()), sourceCode)
			fake.mu.Lock()
			actualURI := fake.requests[0].RecordingURI
			fake.mu.Unlock()
			require.Equal(t, wantURI, actualURI)
			require.True(t, h.Handle(cascadeDialogRequest(req, res, sip.ACK), tx))
			bye := cascadeDialogRequest(req, res, sip.BYE)
			byeTx := siptest.NewServerTxRecorder(bye)
			defer byeTx.Terminate()
			require.True(t, h.Handle(bye, byeTx))
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("download did not release")
			}
		})
	}
}

func TestCascadeDownloadFileURIRequiresAuthorizedRecordingMatch(t *testing.T) {
	h, fake, _ := newCascadePlaybackTestRuntime(t)
	req := cascadePlaybackRequest(15060, "1200000001")
	req.SetBody([]byte(strings.Replace(strings.Replace(string(req.Body()), "s=Playback", "s=Download", 1), "u="+req.Recipient.User+":0", "u=http://"+req.Recipient.User+"/another-channel/secret.ps", 1)))
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)
	res := cascadeAwaitResponse(t, tx)
	require.NotEqual(t, 200, res.StatusCode, "a file URI must be tied to records of the authorized channel")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	require.Empty(t, fake.requests)
}

type cascadeRecordingQueryFunc func(context.Context, recordquery.CascadeQueryRequest) (recordquery.QueryResult, error)

func (f cascadeRecordingQueryFunc) QueryForCascade(ctx context.Context, req recordquery.CascadeQueryRequest) (recordquery.QueryResult, error) {
	return f(ctx, req)
}

func TestCascadeDownloadFileURIRejectsUnmatchedAndIncompleteResults(t *testing.T) {
	for _, mismatch := range []string{"path", "channel", "range", "incomplete"} {
		t.Run(mismatch, func(t *testing.T) {
			h, fake, _ := newCascadePlaybackTestRuntime(t)
			h.recordingLocation = time.FixedZone("CST", 8*60*60)
			h.recordings = cascadeRecordingQueryFunc(func(_ context.Context, request recordquery.CascadeQueryRequest) (recordquery.QueryResult, error) {
				item := manscdp.RecordInfoItem{DeviceID: request.ChannelCode, FilePath: "http://37010301021180000007/records/one.ps",
					StartTime: recordquery.FormatMANSCDPWallClock(request.StartTime, h.recordingLocation),
					EndTime:   recordquery.FormatMANSCDPWallClock(request.EndTime, h.recordingLocation)}
				switch mismatch {
				case "path":
					item.FilePath = "http://37010301021180000007/another-channel/other.ps"
				case "channel":
					item.DeviceID = "another-channel"
				case "range":
					item.EndTime = recordquery.FormatMANSCDPWallClock(request.StartTime.Add(time.Second), h.recordingLocation)
				}
				return recordquery.QueryResult{Incomplete: mismatch == "incomplete", Records: []recordquery.Record{{RecordInfoItem: item}}}, nil
			})
			req := cascadePlaybackRequest(15060, "1200000001")
			req.SetBody([]byte(strings.Replace(strings.Replace(string(req.Body()), "s=Playback", "s=Download", 1), "u="+req.Recipient.User+":0", "u=http://"+req.Recipient.User+"/records/one.ps", 1)))
			tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
			defer tx.Terminate()
			go h.Handle(req, tx)
			require.NotEqual(t, 200, cascadeAwaitResponse(t, tx).StatusCode)
			fake.mu.Lock()
			defer fake.mu.Unlock()
			require.Empty(t, fake.requests, "unverified file must never provision a download source")
		})
	}
}
