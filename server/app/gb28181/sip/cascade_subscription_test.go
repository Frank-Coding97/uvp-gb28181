package sip

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
)

func TestCascadeSubscriptionDispatch(t *testing.T) {
	s := &Server{}
	req := sip.NewRequest(sip.SUBSCRIBE, sip.Uri{User: "34020000002000000001", Host: "127.0.0.1"})
	req.AppendHeader(&sip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "UDP", Host: "127.0.0.1", Port: 5060, Params: sip.NewParams()})
	req.Via().Params.Add("branch", sip.GenerateBranch())
	req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: "34020000002000000002"}, Params: sip.NewParams()})
	req.AppendHeader(&sip.ToHeader{Address: req.Recipient, Params: sip.NewParams()})
	id := sip.CallIDHeader("dispatch-test")
	req.AppendHeader(&id)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: sip.SUBSCRIBE})
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	s.dispatchCascadeSubscription(req, tx)
	require.Equal(t, 489, tx.Result()[0].StatusCode)
	called := false
	s.SetCascadeSubscribeHandler(func(got *sip.Request, gotTx sip.ServerTransaction) bool {
		called = true
		require.Same(t, req, got)
		require.Same(t, tx, gotTx)
		return true
	})
	s.dispatchCascadeSubscription(req, tx)
	require.True(t, called)
	require.Len(t, tx.Result(), 1)
}

func TestCascadeSubscriptionRegisteredOnSIPServer(t *testing.T) {
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.LocalAddr().(*net.UDPAddr).Port
	require.NoError(t, listener.Close())
	cfg := testConfig()
	cfg.SIP.Port = port
	cfg.SIP.Transport = []string{"udp"}
	server, err := NewServer(cfg)
	require.NoError(t, err)
	received := make(chan struct{}, 1)
	server.SetCascadeSubscribeHandler(func(req *sip.Request, tx sip.ServerTransaction) bool {
		received <- struct{}{}
		require.Equal(t, sip.SUBSCRIBE, req.Method)
		_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
		return true
	})
	require.NoError(t, server.Start())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, server.Shutdown(ctx))
	})
	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	defer ua.Close()
	client, err := sipgo.NewClient(ua, sipgo.WithClientHostname("127.0.0.1"))
	require.NoError(t, err)
	time.Sleep(30 * time.Millisecond)
	req := sip.NewRequest(sip.SUBSCRIBE, sip.Uri{Scheme: "sip", User: cfg.SIP.ServerID, Host: "127.0.0.1", Port: port})
	req.SetTransport("UDP")
	req.AppendHeader(sip.NewHeader("Event", "Catalog;id=1"))
	req.AppendHeader(sip.NewHeader("Expires", "600"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := client.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 200, response.StatusCode)
	select {
	case <-received:
	default:
		t.Fatal("SUBSCRIBE handler was not invoked")
	}
}
