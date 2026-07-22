package uac

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
)

type fakeTalkDialog struct {
	waitErr    error
	waitBlock  <-chan struct{}
	ackErr     error
	byeErr     error
	statusCode int

	mu         sync.Mutex
	ackCalls   int
	byeCalls   int
	closeCalls int
}

func (d *fakeTalkDialog) WaitAnswer(context.Context) error {
	if d.waitBlock != nil {
		<-d.waitBlock
	}
	return d.waitErr
}

func (d *fakeTalkDialog) Ack(context.Context) error {
	d.mu.Lock()
	d.ackCalls++
	d.mu.Unlock()
	return d.ackErr
}

func (d *fakeTalkDialog) Bye(context.Context) error {
	d.mu.Lock()
	d.byeCalls++
	d.mu.Unlock()
	return d.byeErr
}

func (d *fakeTalkDialog) Close() error {
	d.mu.Lock()
	d.closeCalls++
	d.mu.Unlock()
	return nil
}

func (d *fakeTalkDialog) StatusCode() int { return d.statusCode }

func (d *fakeTalkDialog) ReadBye(req *sip.Request, tx sip.ServerTransaction) error {
	return tx.Respond(sip.NewResponseFromRequest(req, sip.StatusOK, "OK", nil))
}

type fakeTalkDialogTransport struct {
	dialog talkDialog
	req    *sip.Request
}

func (f *fakeTalkDialogTransport) WriteInvite(_ context.Context, req *sip.Request) (talkDialog, error) {
	f.req = req
	return f.dialog, nil
}

func newTalkTestUAC(dialog talkDialog) (*UAC, *fakeTalkDialogTransport) {
	transport := &fakeTalkDialogTransport{dialog: dialog}
	u := &UAC{serverID: "34020000002000000001", domain: "3402000000"}
	u.talkDialogs = newTalkDialogStore(transport)
	return u, transport
}

func validTalkInvite() TalkInviteRequest {
	return TalkInviteRequest{
		DeviceID: "34020000001320000001", ChannelID: "34020000001320000020",
		Destination: "192.0.2.20:5060", Transport: "tcp", SSRC: "0200000001",
		SDP: "v=0\r\ns=Talk\r\n",
	}
}

func TestInviteTalkSuccessStoresIndependentDialog(t *testing.T) {
	dialog := &fakeTalkDialog{statusCode: 200}
	u, transport := newTalkTestUAC(dialog)

	meta, err := u.InviteTalk(context.Background(), validTalkInvite())
	if err != nil {
		t.Fatal(err)
	}
	if meta.CallID == "" || meta.CSeq == 0 || meta.StatusCode != 200 {
		t.Fatalf("metadata=%+v", meta)
	}
	if transport.req.Recipient.User != "34020000001320000020" || transport.req.Destination() != "192.0.2.20:5060" {
		t.Fatalf("unexpected target: %s %s", transport.req.Recipient.User, transport.req.Destination())
	}
	if transport.req.Transport() != "TCP" || string(transport.req.Body()) != validTalkInvite().SDP {
		t.Fatalf("unexpected INVITE transport/body")
	}
	if subject := transport.req.GetHeader("Subject"); subject == nil || subject.Value() != "34020000001320000020:0200000001,34020000002000000001:0" {
		t.Fatalf("Subject=%v", subject)
	}
	if u.talkDialogs.get(meta.CallID) == nil {
		t.Fatal("talk dialog not mapped by Call-ID")
	}
	if err := u.AckTalk(context.Background(), meta.CallID); err != nil || dialog.ackCalls != 1 {
		t.Fatalf("duplicate ACK must be idempotent: err=%v calls=%d", err, dialog.ackCalls)
	}
}

func TestInviteTalkRejectedPreserves486(t *testing.T) {
	dialog := &fakeTalkDialog{statusCode: sip.StatusBusyHere, waitErr: errors.New("486 Busy Here")}
	u, _ := newTalkTestUAC(dialog)
	meta, err := u.InviteTalk(context.Background(), validTalkInvite())
	if err == nil || meta.StatusCode != sip.StatusBusyHere {
		t.Fatalf("metadata=%+v err=%v", meta, err)
	}
	if u.talkDialogs.get(meta.CallID) != nil || dialog.closeCalls != 1 {
		t.Fatal("rejected dialog must be closed and not retained")
	}
}

func TestInviteTalkTimeoutClosesDialog(t *testing.T) {
	blocked := make(chan struct{})
	dialog := &fakeTalkDialog{waitBlock: blocked}
	u, _ := newTalkTestUAC(dialog)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	meta, err := u.InviteTalk(ctx, validTalkInvite())
	close(blocked)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("metadata=%+v err=%v", meta, err)
	}
	if dialog.closeCalls != 1 || u.talkDialogs.get(meta.CallID) != nil {
		t.Fatal("timed out dialog must be closed and not retained")
	}
}

func TestInviteTalkAckFailureDoesNotRetainDialog(t *testing.T) {
	dialog := &fakeTalkDialog{statusCode: 200, ackErr: errors.New("write ACK failed")}
	u, _ := newTalkTestUAC(dialog)
	meta, err := u.InviteTalk(context.Background(), validTalkInvite())
	if err == nil || dialog.ackCalls != 1 || dialog.closeCalls != 1 {
		t.Fatalf("metadata=%+v err=%v ack=%d close=%d", meta, err, dialog.ackCalls, dialog.closeCalls)
	}
	if u.talkDialogs.get(meta.CallID) != nil {
		t.Fatal("ACK-failed dialog must not be retained")
	}
}

func TestByeTalkIsIdempotent(t *testing.T) {
	dialog := &fakeTalkDialog{statusCode: 200}
	u, _ := newTalkTestUAC(dialog)
	meta, err := u.InviteTalk(context.Background(), validTalkInvite())
	if err != nil {
		t.Fatal(err)
	}
	if err := u.ByeTalk(context.Background(), meta.CallID); err != nil {
		t.Fatal(err)
	}
	if err := u.ByeTalk(context.Background(), meta.CallID); err != nil {
		t.Fatal(err)
	}
	if dialog.byeCalls != 1 || u.talkDialogs.get(meta.CallID) != nil {
		t.Fatalf("byeCalls=%d", dialog.byeCalls)
	}
}

func TestHandleTalkByeUnknownResponds481(t *testing.T) {
	u, _ := newTalkTestUAC(&fakeTalkDialog{})
	req := newTalkByeRequest("unknown-call")
	tx := siptest.NewServerTxRecorder(req)

	handled, err := u.HandleTalkBye(req, tx)
	if err != nil || handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	responses := tx.Result()
	if len(responses) != 1 || responses[0].StatusCode != sip.StatusCallTransactionDoesNotExists {
		t.Fatalf("responses=%v", responses)
	}
}

func TestHandleTalkByeKnownResponds200AndNotifies(t *testing.T) {
	dialog := &fakeTalkDialog{statusCode: 200}
	u, _ := newTalkTestUAC(dialog)
	meta, err := u.InviteTalk(context.Background(), validTalkInvite())
	if err != nil {
		t.Fatal(err)
	}
	notified := make(chan TalkDialogMetadata, 1)
	u.SetTalkByeHandler(func(got TalkDialogMetadata) { notified <- got })
	req := newTalkByeRequest(meta.CallID)
	tx := siptest.NewServerTxRecorder(req)

	handled, err := u.HandleTalkBye(req, tx)
	if err != nil || !handled || tx.Result()[0].StatusCode != sip.StatusOK {
		t.Fatalf("handled=%v err=%v responses=%v", handled, err, tx.Result())
	}
	if got := <-notified; got.CallID != meta.CallID {
		t.Fatalf("notified=%+v", got)
	}
	if u.talkDialogs.get(meta.CallID) != nil {
		t.Fatal("remote BYE must remove dialog")
	}
	repeatedTx := siptest.NewServerTxRecorder(req)
	handled, err = u.HandleTalkBye(req, repeatedTx)
	if err != nil || handled || repeatedTx.Result()[0].StatusCode != sip.StatusCallTransactionDoesNotExists {
		t.Fatalf("repeated remote BYE: handled=%v err=%v responses=%v", handled, err, repeatedTx.Result())
	}
}

func newTalkByeRequest(callID string) *sip.Request {
	req := sip.NewRequest(sip.BYE, sip.Uri{User: "platform", Host: "3402000000"})
	req.AppendHeader(sip.NewHeader("Via", "SIP/2.0/TCP 192.0.2.20:5060;branch=z9hG4bK-talk"))
	req.AppendHeader(sip.NewHeader("From", "<sip:device@3402000000>;tag=device-tag"))
	req.AppendHeader(sip.NewHeader("To", "<sip:platform@3402000000>;tag=platform-tag"))
	h := sip.CallIDHeader(callID)
	req.AppendHeader(&h)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 2, MethodName: sip.BYE})
	req.SetBody(nil)
	req.SetTransport("TCP")
	return req
}
