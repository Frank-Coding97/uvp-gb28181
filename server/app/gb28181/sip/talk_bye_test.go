package sip

import (
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
)

func TestServerHandlesUnknownTalkByeWith481(t *testing.T) {
	server, err := NewServer(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	req := sip.NewRequest(sip.BYE, sip.Uri{User: "platform", Host: "3402000000"})
	req.AppendHeader(sip.NewHeader("Via", "SIP/2.0/TCP 192.0.2.20:5060;branch=z9hG4bK-talk"))
	req.AppendHeader(sip.NewHeader("From", "<sip:device@3402000000>;tag=device-tag"))
	req.AppendHeader(sip.NewHeader("To", "<sip:platform@3402000000>;tag=platform-tag"))
	callID := sip.CallIDHeader("talk-unknown")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 2, MethodName: sip.BYE})
	tx := siptest.NewServerTxRecorder(req)

	server.handleBye(req, tx)
	responses := tx.Result()
	if len(responses) != 1 || responses[0].StatusCode != sip.StatusCallTransactionDoesNotExists {
		t.Fatalf("responses=%v", responses)
	}
}
