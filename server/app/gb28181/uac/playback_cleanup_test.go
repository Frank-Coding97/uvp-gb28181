package uac

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	gbplayback "uvplatform.cn/uvp-gb28181/app/gb28181/playback"
)

type playbackTxRequestFunc func(context.Context, *sip.Request) (sip.ClientTransaction, error)

func (f playbackTxRequestFunc) Request(ctx context.Context, req *sip.Request) (sip.ClientTransaction, error) {
	return f(ctx, req)
}

// A completed transaction without a final response exercises sipgo's
// tx.Done()/tx.Err()==nil branch, not a successful SIP exchange.
type playbackDoneTx struct {
	sip.ClientTransaction
	done chan struct{}
}

func (tx playbackDoneTx) Done() <-chan struct{}           { return tx.done }
func (tx playbackDoneTx) Err() error                      { return nil }
func (tx playbackDoneTx) Responses() <-chan *sip.Response { return nil }
func (tx playbackDoneTx) Terminate()                      {}

type playbackResponseCaptureTx struct {
	sip.ServerTransaction
	response *sip.Response
}

func (tx *playbackResponseCaptureTx) Respond(response *sip.Response) error {
	tx.response = response.Clone()
	return nil
}

func TestSipgoPlaybackByeRequiresAttributableConfirmation(t *testing.T) {
	for _, mode := range []string{"200", "retry", "transaction-done", "ended-cause-gap", "ack-retransmission-failure"} {
		t.Run(mode, func(t *testing.T) {
			ua, err := sipgo.NewUA()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = ua.Close() })
			client, err := sipgo.NewClient(ua)
			if err != nil {
				t.Fatal(err)
			}
			var session *sipgo.DialogClientSession
			var inviteTx *sip.ClientTx
			var inviteResponse *sip.Response
			ackFailure := false
			var byeRequests []*sip.Request
			requester := &siptest.ClientTxRequester{OnRequest: func(req *sip.Request) *sip.Response {
				response := sip.NewResponseFromRequest(req, sip.StatusOK, "OK", nil)
				if req.Method == sip.BYE && mode == "retry" && len(byeRequests) == 1 {
					response.StatusCode = sip.StatusInternalServerError
				}
				if req.Method == sip.INVITE {
					response.To().Params.Add("tag", "original-device-tag")
					response.AppendHeader(sip.NewHeader("Contact", "<sip:original@192.0.2.20:5060>"))
					inviteResponse = response.Clone()
				}
				return response
			}}
			client.TxRequester = playbackTxRequestFunc(func(ctx context.Context, req *sip.Request) (sip.ClientTransaction, error) {
				switch req.Method {
				case sip.ACK:
					if ackFailure {
						return nil, context.Canceled
					}
					return nil, nil
				case sip.BYE:
					byeRequests = append(byeRequests, req.Clone())
					if req.CallID().Value() != session.InviteRequest.CallID().Value() || req.Recipient.String() != "sip:original@192.0.2.20:5060" {
						t.Error("BYE did not retain the original dialog/target")
					}
					if mode != "200" && mode != "retry" {
						if mode == "ended-cause-gap" {
							// Inject the observable state of the state.Swap/cancel
							// publication gap, without modifying the protocol stack.
							session.InitWithState(sip.DialogStateEnded)
						}
						if mode == "ack-retransmission-failure" {
							ackFailure = true
							inviteTx.Receive(inviteResponse.Clone())
						}
						done := make(chan struct{})
						close(done)
						return playbackDoneTx{done: done}, nil
					}
				}
				tx, err := requester.Request(ctx, req)
				if req.Method == sip.INVITE && err == nil {
					inviteTx = tx.(*sip.ClientTx)
				}
				return tx, err
			})
			contact := sip.ContactHeader{Address: sip.Uri{User: "platform", Host: "192.0.2.10", Port: 5060}}
			cache := sipgo.NewDialogClientCache(client, contact)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			session, err = cache.Invite(ctx, sip.Uri{User: "original", Host: "192.0.2.20", Port: 5060}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer inviteTx.Terminate()
			if err := session.WaitAnswer(ctx, sipgo.AnswerOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := session.Ack(ctx); err != nil {
				t.Fatal(err)
			}
			dialog := &sipgoPlaybackDialog{session: session}
			err = dialog.Bye(ctx)
			if mode == "retry" {
				if err == nil || session.LoadState() != sip.DialogStateConfirmed {
					t.Fatalf("first failure=%v", err)
				}
				err = dialog.Bye(ctx)
				if len(byeRequests) != 2 || byeRequests[1].CSeq().SeqNo != byeRequests[0].CSeq().SeqNo+1 {
					t.Fatal("original session was not retryable with increasing CSeq")
				}
			}
			if mode == "200" || mode == "retry" {
				if err != nil {
					t.Fatalf("normal BYE: %v", err)
				}
			} else if !errors.Is(err, ErrPlaybackCleanupUnknown) {
				t.Fatalf("unconfirmed BYE=%v", err)
			}
			if mode == "ack-retransmission-failure" {
				cause := context.Cause(session.Context())
				if session.LoadState() != sip.DialogStateEnded || !errors.Is(cause, context.Canceled) || cause == context.Canceled {
					t.Fatalf("real ACK failure not reproduced: cause=%v", cause)
				}
			}
		})
	}
}

func TestPlaybackCleanupResultDoesNotTurnMissingIntoEvidence(t *testing.T) {
	dialog := establishedPlaybackDialog()
	u, _ := newPlaybackTestUAC(dialog)
	metadata, err := u.InvitePlayback(context.Background(), validPlaybackInvite())
	if err != nil {
		t.Fatal(err)
	}
	dialog.byeErr = context.DeadlineExceeded
	if result, err := u.TeardownPlaybackResult(context.Background(), metadata.CallID); result != PlaybackTeardownPending || err == nil {
		t.Fatalf("unconfirmed=%s %v", result, err)
	}
	dialog.byeErr = nil
	if result, err := u.TeardownPlaybackResult(context.Background(), metadata.CallID); result != PlaybackTeardownClosed || err != nil {
		t.Fatalf("confirmed=%s %v", result, err)
	}
	for _, callID := range []string{metadata.CallID, "unknown-after-restart", ""} {
		if result, err := u.TeardownPlaybackResult(context.Background(), callID); result != PlaybackTeardownMissing || err != nil {
			t.Fatalf("missing=%s %v", result, err)
		}
		if err := u.TeardownPlayback(context.Background(), callID); err != nil {
			t.Fatal(err)
		}
	}
	var unavailable *UAC
	if result, err := unavailable.TeardownPlaybackResult(context.Background(), metadata.CallID); result != PlaybackTeardownMissing || !errors.Is(err, ErrPlaybackUnavailable) {
		t.Fatalf("unavailable=%s %v", result, err)
	}
}

func TestPlaybackInboundUnknownRequiresExactRetry(t *testing.T) {
	for _, sequence := range []uint32{0, 17} {
		dialog := establishedPlaybackDialog()
		u, _ := newPlaybackTestUAC(dialog)
		metadata, err := u.InvitePlayback(context.Background(), validPlaybackInvite())
		if err != nil {
			t.Fatal(err)
		}
		request := newTalkByeRequest(metadata.CallID)
		request.CSeq().SeqNo = sequence
		var ended int
		u.SetPlaybackEndHook(func(ctx context.Context, m PlaybackDialogMetadata, _ string) error {
			ended++
			// The production hook reenters via registry cleanup. No record lock
			// may be held while this callback runs.
			return u.TeardownPlayback(ctx, m.CallID)
		})
		dialog.readByeErr = errors.New("response transport failed")
		if handled, err := u.HandlePlaybackBye(request, siptest.NewServerTxRecorder(request)); !handled || err == nil {
			t.Fatal("expected inbound failure")
		}
		if result, err := u.TeardownPlaybackResult(context.Background(), metadata.CallID); result != PlaybackTeardownPending || !errors.Is(err, ErrPlaybackCleanupUnknown) || dialog.byeCalls != 0 || len(dialog.requests) != 0 {
			t.Fatalf("inbound unknown converted to outbound: %s %v", result, err)
		}
		for _, mutate := range []func(*sip.Request){
			func(r *sip.Request) { r.From().Params.Add("tag", "other-remote") },
			func(r *sip.Request) { r.To().Params.Add("tag", "other-local") },
			func(r *sip.Request) { r.CSeq().SeqNo++ },
			func(r *sip.Request) { r.Method = sip.INFO },
			func(r *sip.Request) { r.CSeq().MethodName = sip.INFO },
			func(r *sip.Request) { r.RemoveHeader("CSeq") },
			func(r *sip.Request) { r.RemoveHeader("From") },
		} {
			wrong := request.Clone()
			mutate(wrong)
			tx := &playbackResponseCaptureTx{}
			handled, err := u.HandlePlaybackBye(wrong, tx)
			if err != nil || handled || tx.response == nil || tx.response.StatusCode != sip.StatusCallTransactionDoesNotExists {
				t.Fatalf("wrong identity: handled=%v err=%v", handled, err)
			}
		}
		dialog.readByeErr = nil
		dialog.closeErr = errors.New("local close failed")
		if handled, err := u.HandlePlaybackBye(request, siptest.NewServerTxRecorder(request)); !handled || err == nil || ended != 0 {
			t.Fatal("local close failure lost")
		}
		if record := u.playbackDialogs.get(metadata.CallID); record == nil || !record.byeConfirmed {
			t.Fatal("confirmed BYE material lost")
		}
		dialog.closeErr = nil
		done := make(chan error, 1)
		go func() { _, err := u.HandlePlaybackBye(request, siptest.NewServerTxRecorder(request)); done <- err }()
		select {
		case err := <-done:
			if err != nil || ended != 1 {
				t.Fatalf("exact retry: ended=%d err=%v", ended, err)
			}
		case <-time.After(time.Second):
			t.Fatal("end hook reentry deadlocked")
		}
		if u.playbackDialogs.get(metadata.CallID) != nil || dialog.byeCalls != 0 {
			t.Fatal("inbound cleanup did not complete independently")
		}
	}
}

func TestPlaybackCleanupConcurrentInboundAndOutbound(t *testing.T) {
	for i := 0; i < 20; i++ {
		dialog := establishedPlaybackDialog()
		u, _ := newPlaybackTestUAC(dialog)
		metadata, err := u.InvitePlayback(context.Background(), validPlaybackInvite())
		if err != nil {
			t.Fatal(err)
		}
		var ended atomic.Int32
		u.SetPlaybackEndHook(func(context.Context, PlaybackDialogMetadata, string) error { ended.Add(1); return nil })
		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make(chan error, 2)
		wg.Add(2)
		go func() { defer wg.Done(); <-start; results <- u.TeardownPlayback(context.Background(), metadata.CallID) }()
		go func() {
			defer wg.Done()
			<-start
			request := newTalkByeRequest(metadata.CallID)
			_, err := u.HandlePlaybackBye(request, siptest.NewServerTxRecorder(request))
			results <- err
		}()
		close(start)
		wg.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatal(err)
			}
		}
		if dialog.closeCalls != 1 || dialog.byeCalls > 1 || ended.Load() > 1 || u.playbackDialogs.get(metadata.CallID) != nil {
			t.Fatalf("duplicate cleanup: close=%d bye=%d ended=%d", dialog.closeCalls, dialog.byeCalls, ended.Load())
		}
	}
}

func TestPlaybackOutboundCloseRetryPreservesInboundEndNotification(t *testing.T) {
	dialog := establishedPlaybackDialog()
	u, _ := newPlaybackTestUAC(dialog)
	metadata, err := u.InvitePlayback(context.Background(), validPlaybackInvite())
	if err != nil {
		t.Fatal(err)
	}
	var ended int
	u.SetPlaybackEndHook(func(ctx context.Context, m PlaybackDialogMetadata, _ string) error {
		ended++
		return u.TeardownPlayback(ctx, m.CallID)
	})
	dialog.closeErr = errors.New("local close failed")
	request := newTalkByeRequest(metadata.CallID)
	if _, err := u.HandlePlaybackBye(request, siptest.NewServerTxRecorder(request)); err == nil {
		t.Fatal("expected local close error")
	}
	dialog.closeErr = nil
	done := make(chan error, 1)
	go func() { done <- u.TeardownPlayback(context.Background(), metadata.CallID) }()
	select {
	case err := <-done:
		if err != nil || ended != 1 || dialog.byeCalls != 0 {
			t.Fatalf("retry notification: err=%v ended=%d bye=%d", err, ended, dialog.byeCalls)
		}
	case <-time.After(time.Second):
		t.Fatal("outbound retry notification reentry deadlocked")
	}
}

func TestPlaybackACKFailureRetainsCleanupOnlyDialog(t *testing.T) {
	dialog := establishedPlaybackDialog()
	dialog.ackErr = errors.New("ACK write unknown")
	u, _ := newPlaybackTestUAC(dialog)
	metadata, err := u.InvitePlayback(context.Background(), validPlaybackInvite())
	if !errors.Is(err, dialog.ackErr) || !errors.Is(err, ErrPlaybackACKPending) || metadata.CallID == "" {
		t.Fatalf("invite: %+v %v", metadata, err)
	}
	if u.playbackDialogs.get(metadata.CallID) == nil || dialog.closeCalls != 0 {
		t.Fatal("ACK failure discarded cleanup material")
	}
	if _, err := u.SendPlaybackInfo(context.Background(), metadata.CallID, PlaybackInfoRequest{Action: PlaybackInfoResume}); !errors.Is(err, ErrPlaybackClosed) {
		t.Fatalf("unacknowledged dialog accepted control: %v", err)
	}
	if result, err := u.TeardownPlaybackResult(context.Background(), metadata.CallID); result != PlaybackTeardownPending || !errors.Is(err, ErrPlaybackCleanupUnknown) {
		t.Fatalf("cleanup=%s %v", result, err)
	}
	if dialog.ackCalls != 1 || dialog.byeCalls != 0 || dialog.closeCalls != 0 || len(dialog.requests) != 0 {
		t.Fatal("unknown ACK silently retried or became BYE confirmation")
	}
	request := newTalkByeRequest(metadata.CallID)
	if handled, err := u.HandlePlaybackBye(request, siptest.NewServerTxRecorder(request)); !handled || err != nil {
		t.Fatalf("exact remote BYE: %v %v", handled, err)
	}
}

func TestPlaybackAdapterKeepsRetainedFailureHandle(t *testing.T) {
	dialog := establishedPlaybackDialog()
	dialog.ackErr = errors.New("ACK failed")
	u, _ := newPlaybackTestUAC(dialog)
	in := validPlaybackInvite()
	result, err := NewPlaybackAdapter(u).Invite(context.Background(), gbplayback.UACInvite{
		DeviceID: in.DeviceID, ChannelID: in.ChannelID, Destination: in.Destination,
		Transport: in.Transport, SSRC: in.SSRC, SDP: in.SDP,
	})
	if !errors.Is(err, dialog.ackErr) || !errors.Is(err, ErrPlaybackACKPending) || !result.CleanupRequired || result.CallID == "" || u.playbackDialogs.get(result.CallID) == nil {
		t.Fatalf("adapter lost retained failure: %+v %v", result, err)
	}
}

func TestPlaybackAdapterDoesNotInventCleanupHandleForOtherErrors(t *testing.T) {
	for _, mode := range []string{"rejected", "wait-error", "send-error"} {
		t.Run(mode, func(t *testing.T) {
			dialog := establishedPlaybackDialog()
			u, transport := newPlaybackTestUAC(dialog)
			switch mode {
			case "rejected":
				dialog.statusCode, dialog.waitErr = sip.StatusBusyHere, errors.New("rejected")
			case "wait-error":
				dialog.waitErr = context.DeadlineExceeded
			case "send-error":
				transport.err = errors.New("send failed")
			}
			in := validPlaybackInvite()
			result, err := NewPlaybackAdapter(u).Invite(context.Background(), gbplayback.UACInvite{
				DeviceID: in.DeviceID, ChannelID: in.ChannelID, Destination: in.Destination,
				Transport: in.Transport, SSRC: in.SSRC, SDP: in.SDP,
			})
			if err == nil || errors.Is(err, ErrPlaybackACKPending) || result.CleanupRequired || result.CallID != "" {
				t.Fatalf("unretained failure became cleanup evidence: %+v %v", result, err)
			}
		})
	}
}
