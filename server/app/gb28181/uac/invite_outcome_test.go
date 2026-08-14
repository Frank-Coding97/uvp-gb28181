package uac

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
)

type inviteOutcomeDialog struct {
	waitErr    error
	ackErr     error
	statusCode int

	byeErr   error
	closeErr error

	closeCalls int
	byeCalls   int
}

func (d *inviteOutcomeDialog) WaitAnswer(context.Context) error { return d.waitErr }
func (d *inviteOutcomeDialog) Ack(context.Context) error        { return d.ackErr }
func (d *inviteOutcomeDialog) Bye(context.Context) error {
	d.byeCalls++
	return d.byeErr
}
func (d *inviteOutcomeDialog) Close() error {
	d.closeCalls++
	return d.closeErr
}
func (d *inviteOutcomeDialog) FinalStatus() int { return d.statusCode }

type inviteOutcomeTransport struct {
	dialog inviteDialog
	err    error
	req    *sip.Request
}

func (t *inviteOutcomeTransport) WriteInvite(_ context.Context, req *sip.Request) (inviteDialog, error) {
	t.req = req
	return t.dialog, t.err
}

type inviteOutcomeRecorder struct {
	begin   int
	end     int
	status  int
	success bool
}

func (r *inviteOutcomeRecorder) Begin(metrics.Transaction) { r.begin++ }
func (r *inviteOutcomeRecorder) End(_, _ string, statusCode int, success bool) {
	r.end++
	r.status = statusCode
	r.success = success
}

func newInviteOutcomeTestUAC(transport inviteDialogTransport) *UAC {
	u := &UAC{
		serverID:        testPlatformID,
		domain:          "3402000000",
		sipPort:         5061,
		advertiseIP:     "192.0.2.1",
		inviteTransport: transport,
	}
	return u
}

func validInviteOutcomeSession() *Session {
	return &Session{
		RequestID: "play-request-1",
		DeviceID:  testDeviceID,
		ChannelID: "34020000001320000020",
		SSRC:      "0200000001",
		StreamID:  "stream-1",
		Dest:      "192.0.2.20:5060",
		Transport: "udp",
	}
}

func TestInviteTrackedConstructionFailureIsUnsent(t *testing.T) {
	transport := &inviteOutcomeTransport{}
	u := newInviteOutcomeTestUAC(transport)
	u.dynamicAdvertise = true
	u.resolveLocalIP = func(string) (string, error) { return "", errors.New("no route") }

	outcome, err := u.InviteTracked(context.Background(), NewSessionManager(), validInviteOutcomeSession(), "v=0\r\n")

	if err == nil || !strings.Contains(err.Error(), "构造 INVITE 失败") {
		t.Fatalf("err=%v, want construction error", err)
	}
	if outcome.RequestID == "" || outcome.RequestSent || outcome.CallID != "" || outcome.CSeq != "" {
		t.Fatalf("construction failure should be explicitly unsent: %+v", outcome)
	}
	if outcome.Error == nil {
		t.Fatal("outcome should retain original error")
	}
	if transport.req != nil {
		t.Fatal("construction failure must not call transport")
	}
}

func TestInviteTrackedWriteFailureRetainsCorrelation(t *testing.T) {
	transportErr := errors.New("write failed")
	transport := &inviteOutcomeTransport{err: transportErr}
	u := newInviteOutcomeTestUAC(transport)

	outcome, err := u.InviteTracked(context.Background(), NewSessionManager(), validInviteOutcomeSession(), "v=0\r\n")

	if !errors.Is(err, transportErr) {
		t.Fatalf("err=%v, want write error", err)
	}
	if outcome.RequestID == "" || outcome.RequestSent || outcome.CallID == "" || outcome.CSeq == "" {
		t.Fatalf("write failure lost request correlation: %+v", outcome)
	}
	if outcome.FinalStatus != 0 || !outcome.FinalResponseAt.IsZero() || outcome.AckSucceeded || !outcome.AckAt.IsZero() {
		t.Fatalf("write failure should have no response/ACK: %+v", outcome)
	}
	if !errors.Is(outcome.Error, transportErr) {
		t.Fatalf("outcome error=%v, want write error", outcome.Error)
	}
}

func TestInviteTrackedTimeoutHasSentRequestWithoutFinalResponse(t *testing.T) {
	transport := &inviteOutcomeTransport{dialog: &inviteOutcomeDialog{waitErr: context.DeadlineExceeded}}
	u := newInviteOutcomeTestUAC(transport)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	outcome, err := u.InviteTracked(ctx, NewSessionManager(), validInviteOutcomeSession(), "v=0\r\n")

	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v, want deadline error", err)
	}
	if !outcome.RequestSent || outcome.SentAt.IsZero() || outcome.FinalStatus != 0 || !outcome.FinalResponseAt.IsZero() {
		t.Fatalf("timeout outcome=%+v", outcome)
	}
	if outcome.AckSucceeded || !outcome.AckAt.IsZero() {
		t.Fatalf("timeout must not report ACK: %+v", outcome)
	}
}

func TestInviteTrackedRejectedCapturesFinalStatus(t *testing.T) {
	dialog := &inviteOutcomeDialog{waitErr: errors.New("486 Busy Here"), statusCode: sip.StatusBusyHere}
	transport := &inviteOutcomeTransport{dialog: dialog}
	u := newInviteOutcomeTestUAC(transport)
	m := NewSessionManager()
	s := validInviteOutcomeSession()

	outcome, err := u.InviteTracked(context.Background(), m, s, "v=0\r\n")

	if err == nil || outcome.FinalStatus != sip.StatusBusyHere || outcome.FinalResponseAt.IsZero() {
		t.Fatalf("outcome=%+v err=%v, want final 486", outcome, err)
	}
	if outcome.AckSucceeded || !outcome.AckAt.IsZero() || s.State != StateIdle || m.Get(s.StreamID) != nil {
		t.Fatalf("rejected invite retained state: outcome=%+v state=%v stored=%v", outcome, s.State, m.Get(s.StreamID))
	}
	if dialog.closeCalls != 1 {
		t.Fatalf("rejected dialog close calls=%d, want 1", dialog.closeCalls)
	}
}

func TestInviteTrackedSuccessCapturesResponseAndACK(t *testing.T) {
	dialog := &inviteOutcomeDialog{statusCode: sip.StatusOK}
	transport := &inviteOutcomeTransport{dialog: dialog}
	u := newInviteOutcomeTestUAC(transport)
	recorder := &inviteOutcomeRecorder{}
	u.SetRecorder(recorder)
	m := NewSessionManager()
	s := validInviteOutcomeSession()

	outcome, err := u.InviteTracked(context.Background(), m, s, "v=0\r\n")

	if err != nil || outcome.FinalStatus != sip.StatusOK || outcome.FinalResponseAt.IsZero() {
		t.Fatalf("outcome=%+v err=%v, want successful response", outcome, err)
	}
	if !outcome.RequestSent || !outcome.AckSucceeded || outcome.AckAt.IsZero() || !outcome.AckAt.After(outcome.FinalResponseAt) {
		t.Fatalf("success outcome=%+v", outcome)
	}
	if s.State != StateEstablished || m.Get(s.StreamID) != s {
		t.Fatalf("successful invite not stored: state=%v stored=%v", s.State, m.Get(s.StreamID))
	}
	if recorder.begin != 1 || recorder.end != 1 || recorder.status != sip.StatusOK || !recorder.success {
		t.Fatalf("metrics=(begin:%d end:%d status:%d success:%v)", recorder.begin, recorder.end, recorder.status, recorder.success)
	}
	if transport.req == nil || outcome.CallID == "" || outcome.CSeq == "" {
		t.Fatalf("request correlation missing: req=%v outcome=%+v", transport.req, outcome)
	}
}

func TestInviteTrackedACKFailureCapturesFinalResponseWithoutSession(t *testing.T) {
	ackErr := errors.New("ACK write failed")
	dialog := &inviteOutcomeDialog{statusCode: sip.StatusOK, ackErr: ackErr}
	transport := &inviteOutcomeTransport{dialog: dialog}
	u := newInviteOutcomeTestUAC(transport)
	m := NewSessionManager()
	s := validInviteOutcomeSession()

	outcome, err := u.InviteTracked(context.Background(), m, s, "v=0\r\n")

	if err == nil || !errors.Is(err, ackErr) || outcome.FinalStatus != sip.StatusOK || outcome.FinalResponseAt.IsZero() {
		t.Fatalf("outcome=%+v err=%v, want ACK failure after 200", outcome, err)
	}
	if outcome.AckSucceeded || !outcome.AckAt.IsZero() || s.State != StateIdle || m.Get(s.StreamID) != nil {
		t.Fatalf("ACK failure outcome/state=%+v state=%v stored=%v", outcome, s.State, m.Get(s.StreamID))
	}
	if dialog.closeCalls != 1 {
		t.Fatalf("ACK failure dialog close calls=%d, want 1", dialog.closeCalls)
	}
}

func TestInviteTrackedStaleGenerationReleasesDialog(t *testing.T) {
	dialog := &inviteOutcomeDialog{statusCode: sip.StatusOK}
	transport := &inviteOutcomeTransport{dialog: dialog}
	u := newInviteOutcomeTestUAC(transport)
	m := NewSessionManager()

	// 较新的代次已建立并写入:同 streamID、generation=2
	live := validInviteOutcomeSession()
	live.Generation = 2
	m.PutIfCurrent(live)

	s := validInviteOutcomeSession()
	s.Generation = 1

	outcome, err := u.InviteTracked(context.Background(), m, s, "v=0\r\n")

	if !errors.Is(err, ErrStaleInviteGeneration) {
		t.Fatalf("err=%v, want ErrStaleInviteGeneration", err)
	}
	if outcome.FinalStatus != sip.StatusOK || !outcome.AckSucceeded {
		t.Fatalf("outcome=%+v, ACK 本身成功,只是写入被拒", outcome)
	}
	if dialog.byeCalls != 1 {
		t.Fatalf("stale dialog bye calls=%d, want 1(设备端会话必须回收)", dialog.byeCalls)
	}
	if dialog.closeCalls != 1 {
		t.Fatalf("stale dialog close calls=%d, want 1", dialog.closeCalls)
	}
	if s.State != StateIdle || s.dialog != nil {
		t.Fatalf("stale session state=%v dialog=%v, want idle/nil", s.State, s.dialog)
	}
	if m.Get(s.StreamID) != live {
		t.Fatal("stale invite must not replace the live session")
	}
}

func TestInviteTrackedStaleGenerationSurfacesCleanupFailure(t *testing.T) {
	byeErr := errors.New("BYE write failed")
	dialog := &inviteOutcomeDialog{statusCode: sip.StatusOK, byeErr: byeErr}
	transport := &inviteOutcomeTransport{dialog: dialog}
	u := newInviteOutcomeTestUAC(transport)
	m := NewSessionManager()

	live := validInviteOutcomeSession()
	live.Generation = 2
	m.PutIfCurrent(live)

	s := validInviteOutcomeSession()
	s.Generation = 1

	_, err := u.InviteTracked(context.Background(), m, s, "v=0\r\n")

	// 主错误仍是 stale generation,清理失败必须可观测(错误链含 BYE 错误)
	if !errors.Is(err, ErrStaleInviteGeneration) {
		t.Fatalf("err=%v, want ErrStaleInviteGeneration", err)
	}
	if !errors.Is(err, byeErr) {
		t.Fatalf("err=%v, want cleanup BYE failure in error chain", err)
	}
	if dialog.closeCalls != 1 {
		t.Fatalf("stale dialog close calls=%d, want 1(Close 仍必须执行)", dialog.closeCalls)
	}
}

func TestInviteCompatibilityWrapperPreservesErrorContract(t *testing.T) {
	ackErr := errors.New("ACK write failed")
	transport := &inviteOutcomeTransport{dialog: &inviteOutcomeDialog{statusCode: sip.StatusOK, ackErr: ackErr}}
	u := newInviteOutcomeTestUAC(transport)

	err := u.Invite(context.Background(), NewSessionManager(), validInviteOutcomeSession(), "v=0\r\n")

	if err == nil || !errors.Is(err, ackErr) {
		t.Fatalf("legacy Invite error=%v, want wrapped ACK error", err)
	}
}
