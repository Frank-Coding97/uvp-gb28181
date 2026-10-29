package gb28181

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	"uvplatform.com/uvp-gb28181/app/gb28181/sdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/talk"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

func TestCascadeAudioMediaInactiveUsesConfiguredBoundary(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	require.False(t, cascadeAudioMediaInactive(time.Time{}, now), "uninitialized media activity is not treated as an expired stream")
	require.False(t, cascadeAudioMediaInactive(now.Add(-cascadeAudioMediaInactivityTimeout+time.Nanosecond), now))
	require.True(t, cascadeAudioMediaInactive(now.Add(-cascadeAudioMediaInactivityTimeout), now))
}

func TestWaitCascadeAudioDeviceACKGatesActivation(t *testing.T) {
	session := &cascadeAudioSession{deviceACK: make(chan struct{})}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, waitCascadeAudioDeviceACK(ctx, session), context.DeadlineExceeded,
		"the bridge must not proceed before the device confirms its 200 response")

	close(session.deviceACK)
	require.NoError(t, waitCascadeAudioDeviceACK(t.Context(), session))
	require.Error(t, waitCascadeAudioDeviceACK(t.Context(), &cascadeAudioSession{}))
}

func TestCascadeAudioCleanupDoesNotRepeatRemoteBYE(t *testing.T) {
	require.True(t, shouldCascadeAudioSendBye("", "upstream"))
	require.True(t, shouldCascadeAudioSendBye("device", "upstream"))
	require.False(t, shouldCascadeAudioSendBye("upstream", "upstream"))
	require.False(t, shouldCascadeAudioSendBye("device", "device"))
}

type cascadeAudioTestUAS struct {
	id       string
	ctx      context.Context
	cancel   context.CancelFunc
	toTag    string
	mu       sync.Mutex
	state    sip.DialogState
	answer   chan []byte
	byeCount int
}

func newCascadeAudioTestUAS(req *sip.Request) (*cascadeAudioTestUAS, error) {
	if req.To().Params == nil {
		req.To().Params = sip.NewParams()
	}
	req.To().Params.Add("tag", "uvp-audio-local-tag")
	id, err := sip.DialogIDFromRequestUAS(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &cascadeAudioTestUAS{id: id, ctx: ctx, cancel: cancel, toTag: "uvp-audio-local-tag", state: sip.DialogStateEstablished, answer: make(chan []byte, 1)}, nil
}
func (d *cascadeAudioTestUAS) DialogID() string                  { return d.id }
func (d *cascadeAudioTestUAS) Context() context.Context          { return d.ctx }
func (d *cascadeAudioTestUAS) Respond(int, string, []byte) error { return nil }
func (d *cascadeAudioTestUAS) RespondSDP(body []byte) error {
	d.mu.Lock()
	d.state = sip.DialogStateEstablished
	d.mu.Unlock()
	d.answer <- append([]byte(nil), body...)
	return nil
}
func (d *cascadeAudioTestUAS) LoadState() sip.DialogState {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state
}
func (d *cascadeAudioTestUAS) ReadAck(*sip.Request, sip.ServerTransaction) error {
	d.mu.Lock()
	d.state = sip.DialogStateConfirmed
	d.mu.Unlock()
	return nil
}
func (d *cascadeAudioTestUAS) ReadBye(*sip.Request, sip.ServerTransaction) error { return nil }
func (d *cascadeAudioTestUAS) Bye(context.Context) error {
	d.mu.Lock()
	d.byeCount++
	d.mu.Unlock()
	return nil
}
func (d *cascadeAudioTestUAS) Close() error { d.cancel(); return nil }

type cascadeAudioTestDialog struct {
	answer       []byte
	callID       string
	acked        chan struct{}
	closed       chan struct{}
	byeCount     int
	readByeCount int
	once         sync.Once
}

func (d *cascadeAudioTestDialog) WaitAnswer(context.Context) error { return nil }
func (d *cascadeAudioTestDialog) AnswerSDP() []byte                { return append([]byte(nil), d.answer...) }
func (d *cascadeAudioTestDialog) Ack(context.Context) error        { d.acked <- struct{}{}; return nil }
func (d *cascadeAudioTestDialog) Bye(context.Context) error        { d.byeCount++; return nil }
func (d *cascadeAudioTestDialog) Close() error                     { d.once.Do(func() { close(d.closed) }); return nil }
func (d *cascadeAudioTestDialog) MatchesRequest(req *sip.Request) bool {
	return req != nil && req.Method == sip.BYE && req.CallID() != nil && req.CallID().Value() == d.callID
}
func (d *cascadeAudioTestDialog) ReadBye(req *sip.Request, tx sip.ServerTransaction) error {
	d.readByeCount++
	return tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
}

type cascadeAudioTestTransport struct {
	dialog     *cascadeAudioTestDialog
	sourcePort int
	sourceSSRC string
}

func (f *cascadeAudioTestTransport) Do(_ context.Context, req *sip.Request) (*sip.Response, error) {
	return sip.NewResponseFromRequest(req, 200, "OK", nil), nil
}
func (f *cascadeAudioTestTransport) DoDigestAuth(ctx context.Context, req *sip.Request, _ *sip.Response, _ sipclient.Credentials) (*sip.Response, error) {
	return f.Do(ctx, req)
}
func (f *cascadeAudioTestTransport) WriteAudioDialog(_ context.Context, req *sip.Request, _ sipclient.Credentials) (sipclient.AudioDialog, error) {
	offer, err := sdp.ParseCascadeAudioOffer(req.Body())
	if err != nil {
		return nil, err
	}
	f.sourceSSRC = offer.SSRC
	answer, err := sdp.BuildCascadeAudioAnswer(offer, "34020000002000000002", "127.0.0.1", f.sourcePort)
	if err != nil {
		return nil, err
	}
	f.dialog.answer = answer
	return f.dialog, nil
}

func TestCascadeAudioRuntimeCompletesBothDialogsBeforeMediaAndCleansUp(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, store, row, req := cascadeAudioBindingFixture(t)
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET local_sip_ip='127.0.0.1', media_advertise_ip='127.0.0.1'").Error)
	deviceMedia, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	defer deviceMedia.Close()
	req.SetBody([]byte(strings.ReplaceAll(strings.ReplaceAll(string(req.Body()), "192.0.2.30", "127.0.0.1"), "30000", strconv.Itoa(deviceMedia.LocalAddr().(*net.UDPAddr).Port))))
	upstreamMedia, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	defer upstreamMedia.Close()
	match, err := resolveCascadeBroadcastPull(t.Context(), db, store, req, time.Now())
	require.NoError(t, err)
	require.NoError(t, match.Claim(t.Context(), db, req, time.Now()))
	upstream := &cascadeAudioTestDialog{acked: make(chan struct{}, 1), closed: make(chan struct{})}
	transport := &cascadeAudioTestTransport{dialog: upstream, sourcePort: upstreamMedia.LocalAddr().(*net.UDPAddr).Port}
	runtime := newCascadeBroadcastRuntime(db, store, newCascadePlatformClientFactory(transport, nil, time.Second), nil)
	var uas *cascadeAudioTestUAS
	createdUAS := make(chan struct{})
	runtime.audioDialogFactory = func(request *sip.Request, _ sip.ServerTransaction, _ model.GbCascadePlatform) (cascadeAudioServerDialog, error) {
		var factoryErr error
		uas, factoryErr = newCascadeAudioTestUAS(request)
		close(createdUAS)
		return uas, factoryErr
	}
	inviteTx := siptest.NewServerTxRecorder(req)
	defer inviteTx.Terminate()
	handlerDone := make(chan struct{})
	go func() { runtime.HandleCascadeDialog(req, inviteTx); close(handlerDone) }()
	select {
	case <-createdUAS:
	case <-time.After(3 * time.Second):
		t.Fatal("device dialog was not created")
	}
	var deviceAnswer []byte
	select {
	case deviceAnswer = <-uas.answer:
	case <-time.After(3 * time.Second):
		t.Fatal("device SDP answer was not produced")
	}
	require.Contains(t, string(deviceAnswer), "m=audio ")
	select {
	case <-upstream.acked:
		t.Fatal("upstream ACK must wait for device ACK")
	default:
	}
	mediaRow, err := store.FindMediaSessionByDialog(t.Context(), row.DialogKey)
	require.NoError(t, err)
	require.NotEqual(t, model.CascadeMediaSessionStateActive, mediaRow.State)
	sourcePort, err := currentCascadeAudioSourcePort(runtime)
	require.NoError(t, err)
	deviceMedia.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	upstreamSSRC, err := strconv.ParseUint(transport.sourceSSRC, 10, 32)
	require.NoError(t, err)
	_, err = upstreamMedia.WriteToUDP(testCascadePCMARtp(1, uint32(upstreamSSRC)), &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: sourcePort})
	require.NoError(t, err)
	_, _, err = deviceMedia.ReadFromUDP(make([]byte, 1500))
	require.Error(t, err, "RTP arriving before device ACK must not cross the bridge")

	ack := req.Clone()
	ack.Method = sip.ACK
	ack.CSeq().MethodName = sip.ACK
	ack.To().Params = sip.NewParams()
	ack.To().Params.Add("tag", uas.toTag)
	ackTx := siptest.NewServerTxRecorder(ack)
	defer ackTx.Terminate()
	require.True(t, runtime.HandleCascadeDialog(ack, ackTx))
	select {
	case <-upstream.acked:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream dialog was not ACKed after device ACK")
	}
	require.Eventually(t, func() bool {
		current, findErr := store.FindMediaSessionByDialog(t.Context(), row.DialogKey)
		return findErr == nil && current.State == model.CascadeMediaSessionStateActive
	}, time.Second, 10*time.Millisecond)
	deviceMedia.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = upstreamMedia.WriteToUDP(testCascadePCMARtp(2, uint32(upstreamSSRC)), &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: sourcePort})
	require.NoError(t, err)
	forwarded := make([]byte, 1500)
	n, _, err := deviceMedia.ReadFromUDP(forwarded)
	require.NoError(t, err, "active audio bridge must forward upstream RTP to the device")
	deviceOffer, err := sdp.ParseCascadeAudioOffer(deviceAnswer)
	require.NoError(t, err)
	deviceSSRC, err := strconv.ParseUint(deviceOffer.SSRC, 10, 32)
	require.NoError(t, err)
	require.Equal(t, uint32(deviceSSRC), binary.BigEndian.Uint32(forwarded[8:12]))
	require.Equal(t, byte(8), forwarded[1]&0x7f)
	require.Equal(t, 14, n)

	bye := ack.Clone()
	bye.Method = sip.BYE
	bye.CSeq().SeqNo++
	bye.CSeq().MethodName = sip.BYE
	byeTx := siptest.NewServerTxRecorder(bye)
	defer byeTx.Terminate()
	require.True(t, runtime.HandleCascadeDialog(bye, byeTx))
	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("device BYE did not finish audio session cleanup")
	}
	select {
	case <-upstream.closed:
	default:
		t.Fatal("upstream dialog was not closed")
	}
	require.Equal(t, 1, upstream.byeCount)
	leases, err := talk.NewGormRepo(db).ListCascadeLeases(t.Context())
	require.NoError(t, err)
	require.Empty(t, leases)
	closed, err := store.FindMediaSessionByDialog(t.Context(), row.DialogKey)
	require.NoError(t, err)
	require.Equal(t, model.CascadeMediaSessionStateClosed, closed.State)
}

func TestCascadeAudioUpstreamBYEClosesDeviceAndReleasesCascadeLease(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, store, row, deviceInvite := cascadeAudioBindingFixture(t)
	match, err := resolveCascadeBroadcastPull(t.Context(), db, store, deviceInvite, time.Now())
	require.NoError(t, err)
	require.NoError(t, match.Claim(t.Context(), db, deviceInvite, time.Now()))

	platform := match.Authorization.Platform
	upstream := &cascadeAudioTestDialog{answer: []byte("answer"), callID: "upstream-audio-dialog", acked: make(chan struct{}, 1), closed: make(chan struct{})}
	uas, err := newCascadeAudioTestUAS(deviceInvite.Clone())
	require.NoError(t, err)
	require.NoError(t, uas.RespondSDP([]byte("device answer")))
	require.NoError(t, uas.ReadAck(nil, nil))
	sessionCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	session := &cascadeAudioSession{key: row.DialogKey, platform: platform, pull: match, peer: match.Peer, uas: uas, uac: upstream, ctx: sessionCtx, cancel: cancel, done: make(chan struct{}), endedBy: "", businessEnd: false}
	runtime := newCascadeBroadcastRuntime(db, store, nil, nil)
	runtime.audioDialogFactory = func(*sip.Request, sip.ServerTransaction, model.GbCascadePlatform) (cascadeAudioServerDialog, error) {
		return uas, nil
	}
	runtime.audioSessions[uas.DialogID()] = session

	bye := deviceInvite.Clone()
	bye.Method = sip.BYE
	bye.CSeq().SeqNo++
	bye.CSeq().MethodName = sip.BYE
	bye.From().Address.User = platform.UpstreamServerID
	bye.From().Params = sip.NewParams()
	bye.From().Params.Add("tag", "upstream-audio-tag")
	bye.To().Address.User = platform.LocalDeviceID
	bye.To().Params = sip.NewParams()
	bye.To().Params.Add("tag", "local-audio-tag")
	byeID := sip.CallIDHeader(upstream.callID)
	bye.ReplaceHeader(&byeID)
	bye.SetSource(platform.Host + ":" + strconv.Itoa(platform.Port))
	bye.SetTransport(platform.Transport)
	byeTx := siptest.NewServerTxRecorder(bye)
	defer byeTx.Terminate()
	require.True(t, runtime.HandleCascadeDialog(bye, byeTx))
	require.Equal(t, 200, byeTx.Result()[0].StatusCode)
	require.Equal(t, "upstream", session.endedBy)
	require.ErrorIs(t, session.ctx.Err(), context.Canceled)

	runtime.cleanupCascadeAudio(session, "upstream ended audio dialog")
	require.Equal(t, 1, upstream.readByeCount)
	require.Zero(t, upstream.byeCount, "do not send a second BYE to the upstream that ended the dialog")
	require.Equal(t, 1, uas.byeCount, "send BYE to the remaining device dialog")
	select {
	case <-upstream.closed:
	default:
		t.Fatal("upstream dialog was not closed")
	}
	leases, err := talk.NewGormRepo(db).ListCascadeLeases(t.Context())
	require.NoError(t, err)
	require.Empty(t, leases)
	closed, err := store.FindMediaSessionByDialog(t.Context(), row.DialogKey)
	require.NoError(t, err)
	require.Equal(t, model.CascadeMediaSessionStateClosed, closed.State)
}

func TestCascadeAudioShutdownClosesBothDialogsAndReleasesCascadeLease(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, store, row, req := cascadeAudioBindingFixture(t)
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET local_sip_ip='127.0.0.1', media_advertise_ip='127.0.0.1'").Error)
	deviceMedia, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	defer deviceMedia.Close()
	req.SetBody([]byte(strings.ReplaceAll(strings.ReplaceAll(string(req.Body()), "192.0.2.30", "127.0.0.1"), "30000", strconv.Itoa(deviceMedia.LocalAddr().(*net.UDPAddr).Port))))
	upstreamMedia, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	defer upstreamMedia.Close()
	match, err := resolveCascadeBroadcastPull(t.Context(), db, store, req, time.Now())
	require.NoError(t, err)
	require.NoError(t, match.Claim(t.Context(), db, req, time.Now()))
	upstream := &cascadeAudioTestDialog{acked: make(chan struct{}, 1), closed: make(chan struct{})}
	transport := &cascadeAudioTestTransport{dialog: upstream, sourcePort: upstreamMedia.LocalAddr().(*net.UDPAddr).Port}
	runtime := newCascadeBroadcastRuntime(db, store, newCascadePlatformClientFactory(transport, nil, time.Second), nil)
	createdUAS := make(chan *cascadeAudioTestUAS, 1)
	runtime.audioDialogFactory = func(request *sip.Request, _ sip.ServerTransaction, _ model.GbCascadePlatform) (cascadeAudioServerDialog, error) {
		uas, factoryErr := newCascadeAudioTestUAS(request)
		if factoryErr == nil {
			createdUAS <- uas
		}
		return uas, factoryErr
	}
	ctx, release, admitted := runtime.acquire(t.Context(), 0)
	require.True(t, admitted)
	inviteTx := siptest.NewServerTxRecorder(req)
	defer inviteTx.Terminate()
	registerDone := make(chan error, 1)
	go func() {
		defer release()
		registerDone <- runtime.registerCascadeAudioSession(ctx, req, inviteTx, match)
	}()
	var uas *cascadeAudioTestUAS
	select {
	case uas = <-createdUAS:
	case <-time.After(3 * time.Second):
		t.Fatal("device dialog was not created")
	}
	select {
	case <-uas.answer:
	case <-time.After(3 * time.Second):
		t.Fatal("device SDP answer was not produced")
	}
	ack := req.Clone()
	ack.Method = sip.ACK
	ack.CSeq().MethodName = sip.ACK
	ack.To().Params = sip.NewParams()
	ack.To().Params.Add("tag", uas.toTag)
	ackTx := siptest.NewServerTxRecorder(ack)
	defer ackTx.Terminate()
	require.True(t, runtime.HandleCascadeDialog(ack, ackTx))
	require.Eventually(t, func() bool {
		current, findErr := store.FindMediaSessionByDialog(t.Context(), row.DialogKey)
		return findErr == nil && current.State == model.CascadeMediaSessionStateActive
	}, time.Second, 10*time.Millisecond)

	shutdownCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, runtime.Shutdown(shutdownCtx))
	select {
	case registerErr := <-registerDone:
		require.NoError(t, registerErr)
	case <-time.After(time.Second):
		t.Fatal("audio session did not finish after runtime shutdown")
	}
	require.Equal(t, 1, upstream.byeCount)
	require.Equal(t, 1, uas.byeCount)
	select {
	case <-upstream.closed:
	default:
		t.Fatal("upstream dialog was not closed during shutdown")
	}
	leases, err := talk.NewGormRepo(db).ListCascadeLeases(t.Context())
	require.NoError(t, err)
	require.Empty(t, leases)
	closed, err := store.FindMediaSessionByDialog(t.Context(), row.DialogKey)
	require.NoError(t, err)
	require.Equal(t, model.CascadeMediaSessionStateClosed, closed.State)
}

func TestCascadeAudioAuthorizationRevocationClosesBothDialogsAndReleasesLease(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, store, row, req := cascadeAudioBindingFixture(t)
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET local_sip_ip='127.0.0.1', media_advertise_ip='127.0.0.1'").Error)
	deviceMedia, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	defer deviceMedia.Close()
	req.SetBody([]byte(strings.ReplaceAll(strings.ReplaceAll(string(req.Body()), "192.0.2.30", "127.0.0.1"), "30000", strconv.Itoa(deviceMedia.LocalAddr().(*net.UDPAddr).Port))))
	upstreamMedia, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	defer upstreamMedia.Close()
	match, err := resolveCascadeBroadcastPull(t.Context(), db, store, req, time.Now())
	require.NoError(t, err)
	require.NoError(t, match.Claim(t.Context(), db, req, time.Now()))
	upstream := &cascadeAudioTestDialog{acked: make(chan struct{}, 1), closed: make(chan struct{})}
	transport := &cascadeAudioTestTransport{dialog: upstream, sourcePort: upstreamMedia.LocalAddr().(*net.UDPAddr).Port}
	runtime := newCascadeBroadcastRuntime(db, store, newCascadePlatformClientFactory(transport, nil, time.Second), nil)
	createdUAS := make(chan *cascadeAudioTestUAS, 1)
	runtime.audioDialogFactory = func(request *sip.Request, _ sip.ServerTransaction, _ model.GbCascadePlatform) (cascadeAudioServerDialog, error) {
		uas, factoryErr := newCascadeAudioTestUAS(request)
		if factoryErr == nil {
			createdUAS <- uas
		}
		return uas, factoryErr
	}
	ctx, release, admitted := runtime.acquire(t.Context(), 0)
	require.True(t, admitted)
	inviteTx := siptest.NewServerTxRecorder(req)
	defer inviteTx.Terminate()
	registerDone := make(chan error, 1)
	go func() {
		defer release()
		registerDone <- runtime.registerCascadeAudioSession(ctx, req, inviteTx, match)
	}()
	var uas *cascadeAudioTestUAS
	select {
	case uas = <-createdUAS:
	case <-time.After(3 * time.Second):
		t.Fatal("device dialog was not created")
	}
	select {
	case <-uas.answer:
	case <-time.After(3 * time.Second):
		t.Fatal("device SDP answer was not produced")
	}
	ack := req.Clone()
	ack.Method = sip.ACK
	ack.CSeq().MethodName = sip.ACK
	ack.To().Params = sip.NewParams()
	ack.To().Params.Add("tag", uas.toTag)
	ackTx := siptest.NewServerTxRecorder(ack)
	defer ackTx.Terminate()
	require.True(t, runtime.HandleCascadeDialog(ack, ackTx))
	require.Eventually(t, func() bool {
		current, findErr := store.FindMediaSessionByDialog(t.Context(), row.DialogKey)
		return findErr == nil && current.State == model.CascadeMediaSessionStateActive
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, db.Model(&model.GbCascadePlatform{}).Where("id = ?", match.Authorization.Platform.ID).Update("enabled", false).Error)

	select {
	case sessionErr := <-registerDone:
		require.NoError(t, sessionErr)
	case <-time.After(7 * time.Second):
		t.Fatal("audio session did not stop after publication authorization was revoked")
	}
	require.Equal(t, 1, upstream.byeCount)
	require.Equal(t, 1, uas.byeCount)
	select {
	case <-upstream.closed:
	default:
		t.Fatal("upstream dialog was not closed after revocation")
	}
	leases, err := talk.NewGormRepo(db).ListCascadeLeases(t.Context())
	require.NoError(t, err)
	require.Empty(t, leases)
	closed, err := store.FindMediaSessionByDialog(t.Context(), row.DialogKey)
	require.NoError(t, err)
	require.Equal(t, model.CascadeMediaSessionStateClosed, closed.State)
}

func TestCascadeAudioMediaInactivityClosesBothDialogsAndReleasesLease(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, store, row, req := cascadeAudioBindingFixture(t)
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET local_sip_ip='127.0.0.1', media_advertise_ip='127.0.0.1'").Error)
	deviceMedia, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	defer deviceMedia.Close()
	req.SetBody([]byte(strings.ReplaceAll(strings.ReplaceAll(string(req.Body()), "192.0.2.30", "127.0.0.1"), "30000", strconv.Itoa(deviceMedia.LocalAddr().(*net.UDPAddr).Port))))
	upstreamMedia, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	defer upstreamMedia.Close()
	match, err := resolveCascadeBroadcastPull(t.Context(), db, store, req, time.Now())
	require.NoError(t, err)
	require.NoError(t, match.Claim(t.Context(), db, req, time.Now()))
	upstream := &cascadeAudioTestDialog{acked: make(chan struct{}, 1), closed: make(chan struct{})}
	transport := &cascadeAudioTestTransport{dialog: upstream, sourcePort: upstreamMedia.LocalAddr().(*net.UDPAddr).Port}
	runtime := newCascadeBroadcastRuntime(db, store, newCascadePlatformClientFactory(transport, nil, time.Second), nil)
	createdUAS := make(chan *cascadeAudioTestUAS, 1)
	runtime.audioDialogFactory = func(request *sip.Request, _ sip.ServerTransaction, _ model.GbCascadePlatform) (cascadeAudioServerDialog, error) {
		uas, factoryErr := newCascadeAudioTestUAS(request)
		if factoryErr == nil {
			createdUAS <- uas
		}
		return uas, factoryErr
	}
	ctx, release, admitted := runtime.acquire(t.Context(), 0)
	require.True(t, admitted)
	inviteTx := siptest.NewServerTxRecorder(req)
	defer inviteTx.Terminate()
	registerDone := make(chan error, 1)
	go func() {
		defer release()
		registerDone <- runtime.registerCascadeAudioSession(ctx, req, inviteTx, match)
	}()
	var uas *cascadeAudioTestUAS
	select {
	case uas = <-createdUAS:
	case <-time.After(3 * time.Second):
		t.Fatal("device dialog was not created")
	}
	select {
	case <-uas.answer:
	case <-time.After(3 * time.Second):
		t.Fatal("device SDP answer was not produced")
	}
	ack := req.Clone()
	ack.Method = sip.ACK
	ack.CSeq().MethodName = sip.ACK
	ack.To().Params = sip.NewParams()
	ack.To().Params.Add("tag", uas.toTag)
	ackTx := siptest.NewServerTxRecorder(ack)
	defer ackTx.Terminate()
	require.True(t, runtime.HandleCascadeDialog(ack, ackTx))
	require.Eventually(t, func() bool {
		current, findErr := store.FindMediaSessionByDialog(t.Context(), row.DialogKey)
		return findErr == nil && current.State == model.CascadeMediaSessionStateActive
	}, time.Second, 10*time.Millisecond)
	select {
	case sessionErr := <-registerDone:
		require.NoError(t, sessionErr)
	case <-time.After(cascadeAudioMediaInactivityTimeout + 12*time.Second):
		t.Fatal("audio session did not stop after the media inactivity timeout")
	}
	require.Equal(t, 1, upstream.byeCount)
	require.Equal(t, 1, uas.byeCount)
	select {
	case <-upstream.closed:
	default:
		t.Fatal("upstream dialog was not closed after media inactivity")
	}
	leases, err := talk.NewGormRepo(db).ListCascadeLeases(t.Context())
	require.NoError(t, err)
	require.Empty(t, leases)
	closed, err := store.FindMediaSessionByDialog(t.Context(), row.DialogKey)
	require.NoError(t, err)
	require.Equal(t, model.CascadeMediaSessionStateClosed, closed.State)
}

func currentCascadeAudioSourcePort(runtime *cascadeBroadcastRuntime) (int, error) {
	runtime.audioMu.Lock()
	defer runtime.audioMu.Unlock()
	for _, session := range runtime.audioSessions {
		if session.bridge != nil {
			return session.bridge.SourcePort(), nil
		}
	}
	return 0, errors.New("cascade audio bridge not registered")
}

func testCascadePCMARtp(sequence uint16, ssrc uint32) []byte {
	packet := make([]byte, 14)
	packet[0], packet[1] = 0x80, 8
	binary.BigEndian.PutUint16(packet[2:4], sequence)
	binary.BigEndian.PutUint32(packet[4:8], uint32(sequence)*160)
	binary.BigEndian.PutUint32(packet[8:12], ssrc)
	packet[12], packet[13] = 0xd5, 0xd5
	return packet
}
