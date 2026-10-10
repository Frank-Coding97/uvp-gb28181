package sipgo

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

func TestFixedDialogINFOUsesExplicitSequenceAndOriginalIdentity(t *testing.T) {
	for _, routes := range [][]string{nil, {"sip:first.example;lr", "sip:second.example:5070;lr"}, {"sip:strict.example:5070"}} {
		t.Run(strings.Join(routes, ","), func(t *testing.T) {
			dua, invite := ownedInviteRequest(t, "127.0.0.1:5060")
			response := cleanupBranchResponse(invite, 5062)
			for _, route := range routes {
				var uri sip.Uri
				require.NoError(t, sip.ParseUri(route, &uri))
				response.AppendHeader(&sip.RecordRouteHeader{Address: uri})
			}
			seq := invite.CSeq().SeqNo + 9 // Caller reserves the next durable sequence.
			body := []byte(fmt.Sprintf("PAUSE RTSP/1.0\r\nCSeq: %d\r\nPauseTime: now\r\n\r\n", seq))
			r, err := dua.BuildDialogINFO(invite, response, seq, "Application/MANSRTSP", body)
			require.NoError(t, err)
			require.Equal(t, sip.INFO, r.Method)
			require.Equal(t, seq, r.CSeq().SeqNo)
			require.Equal(t, sip.INFO, r.CSeq().MethodName)
			require.Equal(t, body, r.Body())
			require.Equal(t, invite.From().Value(), r.From().Value())
			require.Equal(t, response.To().Value(), r.To().Value())
			require.Equal(t, invite.Contact().Value(), r.Contact().Value())
			require.Equal(t, invite.Via().Host, r.Via().Host)
			require.Equal(t, invite.Via().Port, r.Via().Port)
			require.NotEqual(t, invite.Via().Params.GetOr("branch", ""), r.Via().Params.GetOr("branch", ""))
			if len(routes) > 0 {
				require.Equal(t, routes[len(routes)-1], r.Route().Address.String())
			}
			before := r.String()
			body[0] = 'X'
			*invite.CallID() = "mutated"
			response.To().Params.Add("tag", "mutated")
			require.Equal(t, before, r.String(), "fixed request cannot alias caller input")
		})
	}
}

func TestFixedDialogINFOActualUDPPrepareDoesNotRewriteOrSend(t *testing.T) {
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	dua, invite := ownedInviteRequest(t, peer.LocalAddr().String())
	body := []byte("fixture control body")
	r, err := dua.BuildDialogINFO(invite, cleanupBranchResponse(invite, peer.LocalAddr().(*net.UDPAddr).Port), invite.CSeq().SeqNo+1, "Application/MANSRTSP", body)
	require.NoError(t, err)
	before := r.String()
	tx, err := dua.Client.TransactionLayer().NewClientTransaction(context.Background(), r)
	require.NoError(t, err)
	defer tx.Terminate()
	require.Equal(t, before, r.String())
	buffer := make([]byte, 8192)
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	_, _, err = peer.ReadFrom(buffer)
	require.Error(t, err, "pure build and tx preparation have no SIP write")
	require.NoError(t, tx.Init())
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	n, _, err := peer.ReadFrom(buffer)
	require.NoError(t, err)
	require.Equal(t, before, string(buffer[:n]))
	tx.Terminate()
	waitOwnedInvite(t, tx.Quiesced())
}

func TestFixedDialogINFORejectsInvalidMaterial(t *testing.T) {
	for _, fault := range []string{"invite", "response", "sequence", "rewrite", "content-type", "empty-body", "huge-body", "contact", "route"} {
		t.Run(fault, func(t *testing.T) {
			dua, invite := ownedInviteRequest(t, "127.0.0.1:5060")
			response := cleanupBranchResponse(invite, 5062)
			seq, ct, body := invite.CSeq().SeqNo+1, "Application/MANSRTSP", []byte("fixture")
			switch fault {
			case "invite":
				invite = nil
			case "response":
				response = nil
			case "sequence":
				seq--
			case "rewrite":
				dua.RewriteContact = true
			case "content-type":
				ct += "\r\nAuthorization: secret"
			case "empty-body":
				body = nil
			case "huge-body":
				body = make([]byte, 4097)
			case "contact":
				response.RemoveHeader("Contact")
			case "route":
				response.AppendHeader(sip.NewHeader("Record-Route", "<sip:proxy.example;lr>;secret=value"))
			}
			r, err := dua.BuildDialogINFO(invite, response, seq, ct, body)
			require.Error(t, err)
			require.Nil(t, r)
		})
	}
}
