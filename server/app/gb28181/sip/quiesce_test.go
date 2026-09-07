package sip

import (
	"context"
	"testing"
	"time"

	siplib "github.com/emiago/sipgo/sip"
)

type quiesceTransaction struct {
	siplib.ServerTransaction
	response *siplib.Response
}

func (t *quiesceTransaction) Respond(r *siplib.Response) error { t.response = r; return nil }

func TestQuiesceRejectsInviteAndWaitsAcceptedHandler(t *testing.T) {
	s := &Server{}
	if !s.beginInvite() {
		t.Fatal("initial invite rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := s.BeginQuiesce(ctx); err != context.DeadlineExceeded {
		t.Fatalf("accepted handler not awaited: %v", err)
	}
	if s.beginInvite() {
		t.Fatal("invite accepted during quiesce")
	}
	tx := &quiesceTransaction{}
	req := siplib.NewRequest(siplib.INVITE, siplib.Uri{Scheme: "sip", Host: "127.0.0.1"})
	s.handleBroadcastInvite(req, tx)
	if tx.response == nil || tx.response.StatusCode != 503 || tx.response.Reason != "Server Shutting Down" {
		t.Fatalf("response=%v", tx.response)
	}
	s.endInvite()
	if err := s.BeginQuiesce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginQuiesce(context.Background()); err != nil {
		t.Fatal(err)
	}
}
