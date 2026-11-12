package sip

import (
	"context"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	siplib "github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/handler"
)

const (
	broadcastTestCallID = "broadcast-nat-dialog"
	broadcastDeviceURI  = "sip:device@192.168.124.5:5060"
	broadcastPeerSource = "203.0.113.7:51030"
)

type broadcastRoutingProcessor struct{}

func (broadcastRoutingProcessor) PrepareBroadcastInvite(context.Context, handler.BroadcastInviteRequest) (handler.BroadcastInviteResponse, error) {
	return handler.BroadcastInviteResponse{
		SessionID: "broadcast-session",
		AnswerSDP: "v=0\r\no=platform 1 1 IN IP4 192.0.2.10\r\ns=Broadcast\r\nc=IN IP4 192.0.2.10\r\nt=0 0\r\nm=audio 40000 RTP/AVP 0\r\n",
	}, nil
}

func (broadcastRoutingProcessor) OnBroadcastAck(context.Context, string) error { return nil }
func (broadcastRoutingProcessor) OnBroadcastBye(context.Context, string) error { return nil }

func newBroadcastRoutingServer(t *testing.T) (*Server, **siplib.Request) {
	t.Helper()
	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ua.Close() })
	client, err := sipgo.NewClient(ua)
	require.NoError(t, err)
	var sent *siplib.Request
	client.TxRequester = &siptest.ClientTxRequester{OnRequest: func(req *siplib.Request) *siplib.Response {
		sent = req.Clone()
		return siplib.NewResponseFromRequest(req, siplib.StatusOK, "OK", nil)
	}}
	server := &Server{
		broadcastDialogs: sipgo.NewDialogServerCache(client, siplib.ContactHeader{
			Address: siplib.Uri{Scheme: "sip", User: "platform", Host: "192.0.2.10", Port: 5060},
		}),
		broadcastProcessor: broadcastRoutingProcessor{},
	}
	return server, &sent
}

func newBroadcastRoutingInvite(t *testing.T, recordRoute string) (*siplib.Request, *siptest.ServerTxRecorder) {
	t.Helper()
	request := siplib.NewRequest(siplib.INVITE, siplib.Uri{Scheme: "sip", User: "platform", Host: "3402000000"})
	request.SetTransport("UDP")
	request.SetSource(broadcastPeerSource)
	request.AppendHeader(siplib.NewHeader("Via", "SIP/2.0/UDP 192.168.124.5:5060;branch=z9hG4bK-broadcast; rport"))
	request.AppendHeader(siplib.NewHeader("From", "<sip:device@3402000000>;tag=device-tag"))
	request.AppendHeader(siplib.NewHeader("To", "<sip:platform@3402000000>"))
	callID := siplib.CallIDHeader(broadcastTestCallID)
	request.AppendHeader(&callID)
	request.AppendHeader(&siplib.CSeqHeader{SeqNo: 1, MethodName: siplib.INVITE})
	request.AppendHeader(&siplib.ContactHeader{Address: siplib.Uri{
		Scheme: "sip", User: "device", Host: "192.168.124.5", Port: 5060,
	}})
	if recordRoute != "" {
		request.AppendHeader(siplib.NewHeader("Record-Route", recordRoute))
	}
	request.SetBody([]byte("v=0\r\no=device 1 1 IN IP4 192.168.124.5\r\ns=Broadcast\r\nc=IN IP4 192.168.124.5\r\nt=0 0\r\nm=audio 40002 RTP/AVP 0\r\n"))
	return request, siptest.NewServerTxRecorder(request)
}

func establishBroadcastRoutingDialog(t *testing.T, server *Server, recordRoute string) *sipgo.DialogServerSession {
	t.Helper()
	request, tx := newBroadcastRoutingInvite(t, recordRoute)
	completed := make(chan struct{})
	go func() {
		server.handleBroadcastInvite(request, tx)
		close(completed)
	}()
	var value any
	require.Eventually(t, func() bool {
		var ok bool
		value, ok = server.broadcastSessions.Load(broadcastTestCallID)
		return ok
	}, time.Second, 10*time.Millisecond, "Broadcast INVITE should create a session")
	entry := value.(struct {
		sessionID string
		dialog    *sipgo.DialogServerSession
	})
	ack := siplib.NewRequest(siplib.ACK, request.Recipient)
	ack.AppendHeader(&siplib.CSeqHeader{SeqNo: 1, MethodName: siplib.ACK})
	require.NoError(t, entry.dialog.ReadAck(ack, tx))
	select {
	case <-completed:
	case <-time.After(time.Second):
		require.FailNow(t, "Broadcast INVITE handler did not finish after ACK")
	}
	return entry.dialog
}

func TestBroadcastByeUsesInviteSourceWithoutRewritingRequestURI(t *testing.T) {
	server, sent := newBroadcastRoutingServer(t)
	establishBroadcastRoutingDialog(t, server, "")

	require.NoError(t, server.ByeBroadcast(context.Background(), broadcastTestCallID))
	require.NotNil(t, *sent, "Broadcast BYE should be sent")
	require.Equal(t, "192.168.124.5", (*sent).Recipient.Host, "dialog Request-URI must remain the device Contact")
	require.Equal(t, broadcastPeerSource, (*sent).Destination(), "transport destination should use the observed INVITE source")
	require.Empty(t, (*sent).GetHeaders("Route"), "source routing must not invent or change a Route set")
}

func TestBroadcastByePreservesRecordRoute(t *testing.T) {
	server, sent := newBroadcastRoutingServer(t)
	establishBroadcastRoutingDialog(t, server, "<sip:proxy.example.test:5070;lr>")

	require.NoError(t, server.ByeBroadcast(context.Background(), broadcastTestCallID))
	require.NotNil(t, *sent, "Broadcast BYE should be sent")
	require.Equal(t, "192.168.124.5", (*sent).Recipient.Host, "dialog Request-URI must remain the device Contact")
	require.Equal(t, "proxy.example.test:5070", (*sent).Destination(), "Record-Route should continue to determine the next hop")
	routes := (*sent).GetHeaders("Route")
	require.Len(t, routes, 1)
	require.Contains(t, routes[0].Value(), "proxy.example.test:5070;lr")
}
