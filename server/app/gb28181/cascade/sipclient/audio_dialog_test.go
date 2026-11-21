package sipclient

import (
	"testing"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

func TestAudioDialogMatchesOnlyItsEstablishedRemoteBye(t *testing.T) {
	invite := sip.NewRequest(sip.INVITE, sip.Uri{Scheme: "sip", User: "upstream", Host: "192.0.2.10"})
	callID := sip.CallIDHeader("audio-call")
	invite.AppendHeader(&callID)
	fromParams := sip.NewParams()
	fromParams.Add("tag", "local-tag")
	invite.AppendHeader(&sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: "local"}, Params: fromParams})
	invite.AppendHeader(&sip.ToHeader{Address: sip.Uri{Scheme: "sip", User: "upstream"}, Params: sip.NewParams()})
	invite.AppendHeader(&sip.CSeqHeader{SeqNo: 10, MethodName: sip.INVITE})
	answer := sip.NewResponseFromRequest(invite, sip.StatusOK, "OK", []byte("answer"))
	answer.To().Params.Add("tag", "remote-tag")
	session := &sipgo.DialogClientSession{Dialog: sipgo.Dialog{InviteRequest: invite, InviteResponse: answer}}
	dialog := &sipgoAudioDialog{session: session}
	bye := sip.NewRequest(sip.BYE, invite.Recipient)
	bye.AppendHeader(&callID)
	bye.AppendHeader(&sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: "upstream"}, Params: params("remote-tag")})
	bye.AppendHeader(&sip.ToHeader{Address: sip.Uri{Scheme: "sip", User: "local"}, Params: params("local-tag")})
	bye.AppendHeader(&sip.CSeqHeader{SeqNo: 11, MethodName: sip.BYE})
	if !dialog.MatchesRequest(bye) {
		t.Fatal("matching upstream BYE was rejected")
	}
	changedTag := bye.Clone()
	changedTag.From().Params = params("other")
	if dialog.MatchesRequest(changedTag) {
		t.Fatal("foreign dialog tag was accepted")
	}
	changedMethod := bye.Clone()
	changedMethod.CSeq().MethodName = sip.INFO
	if dialog.MatchesRequest(changedMethod) {
		t.Fatal("non-BYE request was accepted")
	}
	answer.StatusCode = sip.StatusBusyHere
	if got := dialog.AnswerSDP(); got != nil {
		t.Fatalf("non-success response exposed SDP: %q", got)
	}
}

func params(tag string) sip.HeaderParams {
	value := sip.NewParams()
	value.Add("tag", tag)
	return value
}
