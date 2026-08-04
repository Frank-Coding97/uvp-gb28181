package uac

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"uvplatform.cn/uvp-gb28181/app/gb28181/mansrtsp"
)

type fakePlaybackDialog struct {
	waitErr, ackErr, byeErr, doErr error
	waitBlock                      <-chan struct{}
	statusCode                     int
	metadata                       PlaybackDialogMetadata
	responseStatus                 int
	responseBody                   func(uint32) []byte

	mu                             sync.Mutex
	ackCalls, byeCalls, closeCalls int
	requests                       []*sip.Request
}

func (d *fakePlaybackDialog) WaitAnswer(context.Context) error {
	if d.waitBlock != nil {
		<-d.waitBlock
	}
	return d.waitErr
}
func (d *fakePlaybackDialog) Ack(context.Context) error {
	d.mu.Lock()
	d.ackCalls++
	d.mu.Unlock()
	return d.ackErr
}
func (d *fakePlaybackDialog) Bye(context.Context) error {
	d.mu.Lock()
	d.byeCalls++
	d.mu.Unlock()
	return d.byeErr
}
func (d *fakePlaybackDialog) Close() error                     { d.mu.Lock(); d.closeCalls++; d.mu.Unlock(); return nil }
func (d *fakePlaybackDialog) StatusCode() int                  { return d.statusCode }
func (d *fakePlaybackDialog) Metadata() PlaybackDialogMetadata { return d.metadata }
func (d *fakePlaybackDialog) ReadBye(req *sip.Request, tx sip.ServerTransaction) error {
	return tx.Respond(sip.NewResponseFromRequest(req, sip.StatusOK, "OK", nil))
}
func (d *fakePlaybackDialog) Do(_ context.Context, req *sip.Request) (*sip.Response, error) {
	d.mu.Lock()
	d.requests = append(d.requests, req.Clone())
	d.mu.Unlock()
	if d.doErr != nil {
		return nil, d.doErr
	}
	status := d.responseStatus
	if status == 0 {
		status = sip.StatusOK
	}
	response := sip.NewResponse(status, "control")
	if d.responseBody != nil {
		response.SetBody(d.responseBody(req.CSeq().SeqNo))
	}
	return response, nil
}

type fakePlaybackTransport struct {
	dialog playbackDialog
	req    *sip.Request
	err    error
}

func (t *fakePlaybackTransport) WriteInvite(_ context.Context, req *sip.Request) (playbackDialog, error) {
	t.req = req
	return t.dialog, t.err
}

func newPlaybackTestUAC(dialog *fakePlaybackDialog) (*UAC, *fakePlaybackTransport) {
	transport := &fakePlaybackTransport{dialog: dialog}
	u := &UAC{serverID: "34020000002000000001", domain: "3402000000", sipPort: 5061, advertiseIP: "192.0.2.1"}
	u.playbackDialogs = NewPlaybackDialogStore(transport)
	return u, transport
}

func validPlaybackInvite() PlaybackInviteRequest {
	return PlaybackInviteRequest{
		DeviceID: "34020000002000000010", ChannelID: "34020000001320000001",
		Destination: "192.0.2.20:5060", Transport: "tcp", SSRC: "1000000001",
		SDP: "v=0\r\ns=Playback\r\n",
	}
}

func acceptedMANSRTSP(cseq uint32) []byte {
	return []byte(fmt.Sprintf("RTSP/1.0 200 OK\r\nCSeq: %d\r\n\r\n", cseq))
}

func establishedPlaybackDialog() *fakePlaybackDialog {
	return &fakePlaybackDialog{
		statusCode: 200, responseBody: acceptedMANSRTSP,
		metadata: PlaybackDialogMetadata{
			LocalTag: "platform-tag", RemoteTag: "device-tag",
			RemoteTarget: "sip:device@192.0.2.20:5060",
			RouteSet:     []string{"<sip:proxy-a:5060;lr>", "<sip:proxy-b:5060;lr>"},
		},
	}
}

func TestInvitePlaybackStoresIndependentDialogMetadataAndACKs(t *testing.T) {
	dialog := establishedPlaybackDialog()
	u, transport := newPlaybackTestUAC(dialog)
	metadata, err := u.InvitePlayback(context.Background(), validPlaybackInvite())
	if err != nil {
		t.Fatal(err)
	}
	if metadata.CallID == "" || metadata.CSeq == 0 || metadata.StatusCode != 200 || metadata.LocalTag != "platform-tag" || metadata.RemoteTag != "device-tag" {
		t.Fatalf("metadata=%+v", metadata)
	}
	if len(metadata.RouteSet) != 2 || metadata.RemoteTarget != "sip:device@192.0.2.20:5060" {
		t.Fatalf("metadata=%+v", metadata)
	}
	if transport.req.Recipient.User != validPlaybackInvite().ChannelID || transport.req.Transport() != "TCP" || transport.req.Destination() != validPlaybackInvite().Destination {
		t.Fatalf("request=%s", transport.req.String())
	}
	if got := transport.req.GetHeader("Content-Type"); got == nil || got.Value() != "application/sdp" {
		t.Fatalf("Content-Type=%v", got)
	}
	if dialog.ackCalls != 1 {
		t.Fatalf("ackCalls=%d", dialog.ackCalls)
	}
}

func TestInvitePlaybackRejectAndTimeoutDoNotRetainDialog(t *testing.T) {
	rejected := establishedPlaybackDialog()
	rejected.statusCode, rejected.waitErr = sip.StatusBusyHere, errors.New("486 Busy Here")
	u, _ := newPlaybackTestUAC(rejected)
	metadata, err := u.InvitePlayback(context.Background(), validPlaybackInvite())
	if err == nil || metadata.StatusCode != sip.StatusBusyHere || rejected.closeCalls != 1 {
		t.Fatalf("metadata=%+v err=%v", metadata, err)
	}

	blocked := make(chan struct{})
	timed := establishedPlaybackDialog()
	timed.waitBlock = blocked
	u, _ = newPlaybackTestUAC(timed)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = u.InvitePlayback(ctx, validPlaybackInvite())
	close(blocked)
	if !errors.Is(err, context.DeadlineExceeded) || timed.closeCalls != 1 {
		t.Fatalf("err=%v close=%d", err, timed.closeCalls)
	}
}

func TestSendPlaybackInfoBuildsAllActionsInOriginalDialog(t *testing.T) {
	dialog := establishedPlaybackDialog()
	u, _ := newPlaybackTestUAC(dialog)
	metadata, err := u.InvitePlayback(context.Background(), validPlaybackInvite())
	if err != nil {
		t.Fatal(err)
	}
	actions := []PlaybackInfoRequest{
		{Action: PlaybackInfoPlay}, {Action: PlaybackInfoPause}, {Action: PlaybackInfoResume},
		{Action: PlaybackInfoSeek, Position: 30 * time.Second, SegmentDuration: time.Minute},
		{Action: PlaybackInfoScale, Scale: 2},
	}
	for _, action := range actions {
		result, sendErr := u.SendPlaybackInfo(context.Background(), metadata.CallID, action)
		if sendErr != nil || result.Status != mansrtsp.ResultAccepted {
			t.Fatalf("action=%s result=%+v err=%v", action.Action, result, sendErr)
		}
	}
	dialog.mu.Lock()
	defer dialog.mu.Unlock()
	if len(dialog.requests) != len(actions) {
		t.Fatalf("requests=%d", len(dialog.requests))
	}
	for index, request := range dialog.requests {
		wantCSeq := metadata.CSeq + uint32(index) + 1
		if request.CSeq().SeqNo != wantCSeq {
			t.Fatalf("request %d cseq=%d want=%d", index, request.CSeq().SeqNo, wantCSeq)
		}
		if request.CallID() == nil || string(*request.CallID()) != metadata.CallID {
			t.Fatalf("request %d call-id", index)
		}
		if header := request.GetHeader("Content-Type"); header == nil || header.Value() != mansrtsp.ContentType {
			t.Fatalf("request %d content-type=%v", index, header)
		}
	}
}

func TestSendPlaybackInfoConcurrentCSeqIsStrictlyMonotonic(t *testing.T) {
	dialog := establishedPlaybackDialog()
	u, _ := newPlaybackTestUAC(dialog)
	metadata, err := u.InvitePlayback(context.Background(), validPlaybackInvite())
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errCh := make(chan error, 5)
	for index := 0; index < 5; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, sendErr := u.SendPlaybackInfo(context.Background(), metadata.CallID, PlaybackInfoRequest{Action: PlaybackInfoResume})
			errCh <- sendErr
		}()
	}
	wait.Wait()
	close(errCh)
	for sendErr := range errCh {
		if sendErr != nil {
			t.Fatal(sendErr)
		}
	}
	dialog.mu.Lock()
	seqs := make([]int, 0, len(dialog.requests))
	for _, request := range dialog.requests {
		seqs = append(seqs, int(request.CSeq().SeqNo))
	}
	dialog.mu.Unlock()
	sort.Ints(seqs)
	for index, seq := range seqs {
		if seq != int(metadata.CSeq)+index+1 {
			t.Fatalf("seqs=%v", seqs)
		}
	}
}

func TestSendPlaybackInfoReturnsRejectedProtocolAndTimeoutErrors(t *testing.T) {
	dialog := establishedPlaybackDialog()
	u, _ := newPlaybackTestUAC(dialog)
	metadata, err := u.InvitePlayback(context.Background(), validPlaybackInvite())
	if err != nil {
		t.Fatal(err)
	}
	dialog.responseStatus = sip.StatusBadRequest
	result, err := u.SendPlaybackInfo(context.Background(), metadata.CallID, PlaybackInfoRequest{Action: PlaybackInfoPause})
	if !errors.Is(err, ErrPlaybackRejected) || result.Status != mansrtsp.ResultRejected || result.StatusCode != sip.StatusBadRequest {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	dialog.responseStatus = sip.StatusOK
	dialog.responseBody = func(uint32) []byte { return []byte("broken") }
	_, err = u.SendPlaybackInfo(context.Background(), metadata.CallID, PlaybackInfoRequest{Action: PlaybackInfoResume})
	if !errors.Is(err, mansrtsp.ErrProtocol) {
		t.Fatalf("err=%v", err)
	}
	dialog.responseBody = acceptedMANSRTSP
	dialog.doErr = context.DeadlineExceeded
	_, err = u.SendPlaybackInfo(context.Background(), metadata.CallID, PlaybackInfoRequest{Action: PlaybackInfoResume})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestSendPlaybackInfoAcceptsEmptySIP2xxBody(t *testing.T) {
	dialog := establishedPlaybackDialog()
	dialog.responseBody = nil
	u, _ := newPlaybackTestUAC(dialog)
	metadata, err := u.InvitePlayback(context.Background(), validPlaybackInvite())
	if err != nil {
		t.Fatal(err)
	}
	result, err := u.SendPlaybackInfo(context.Background(), metadata.CallID, PlaybackInfoRequest{Action: PlaybackInfoPause})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != mansrtsp.ResultAccepted || result.StatusCode != sip.StatusOK || result.CSeq != metadata.CSeq+1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestTeardownPlaybackAcceptsEmptySIP2xxBody(t *testing.T) {
	dialog := establishedPlaybackDialog()
	dialog.responseBody = nil
	u, _ := newPlaybackTestUAC(dialog)
	metadata, err := u.InvitePlayback(context.Background(), validPlaybackInvite())
	if err != nil {
		t.Fatal(err)
	}
	if err := u.TeardownPlayback(context.Background(), metadata.CallID); err != nil {
		t.Fatal(err)
	}
	if dialog.byeCalls != 1 || dialog.closeCalls != 1 {
		t.Fatalf("bye=%d close=%d", dialog.byeCalls, dialog.closeCalls)
	}
}

func TestTeardownPlaybackAlwaysBYEsAndIsIdempotent(t *testing.T) {
	dialog := establishedPlaybackDialog()
	u, _ := newPlaybackTestUAC(dialog)
	metadata, err := u.InvitePlayback(context.Background(), validPlaybackInvite())
	if err != nil {
		t.Fatal(err)
	}
	dialog.responseStatus = sip.StatusBadRequest
	err = u.TeardownPlayback(context.Background(), metadata.CallID)
	if !errors.Is(err, ErrPlaybackRejected) || dialog.byeCalls != 1 {
		t.Fatalf("err=%v bye=%d", err, dialog.byeCalls)
	}
	if err := u.TeardownPlayback(context.Background(), metadata.CallID); err != nil || dialog.byeCalls != 1 {
		t.Fatalf("repeat err=%v bye=%d", err, dialog.byeCalls)
	}
	_, err = u.SendPlaybackInfo(context.Background(), metadata.CallID, PlaybackInfoRequest{Action: PlaybackInfoResume})
	if !errors.Is(err, ErrPlaybackClosed) {
		t.Fatalf("err=%v", err)
	}
}

func TestHandlePlaybackByeUsesExactCallIDAndIsIdempotent(t *testing.T) {
	dialog := establishedPlaybackDialog()
	u, _ := newPlaybackTestUAC(dialog)
	var ended int
	u.SetPlaybackEndHook(func(_ context.Context, got PlaybackDialogMetadata, reason string) error {
		if got.CallID == "" || reason != "bye" {
			t.Fatalf("metadata=%+v reason=%q", got, reason)
		}
		ended++
		return nil
	})
	metadata, err := u.InvitePlayback(context.Background(), validPlaybackInvite())
	if err != nil {
		t.Fatal(err)
	}
	request := newTalkByeRequest(metadata.CallID)
	tx := siptest.NewServerTxRecorder(request)
	handled, err := u.HandlePlaybackBye(request, tx)
	if err != nil || !handled || tx.Result()[0].StatusCode != sip.StatusOK {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if ended != 1 {
		t.Fatalf("end hook calls=%d", ended)
	}
	repeated := siptest.NewServerTxRecorder(request)
	handled, err = u.HandlePlaybackBye(request, repeated)
	if err != nil || handled || repeated.Result()[0].StatusCode != sip.StatusCallTransactionDoesNotExists {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if ended != 1 {
		t.Fatalf("late BYE called end hook again: %d", ended)
	}
}
