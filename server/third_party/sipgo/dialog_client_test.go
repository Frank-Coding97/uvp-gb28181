package sipgo

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testClient(t testing.TB, f func(req *sip.Request) *sip.Response) *Client {
	ua, _ := NewUA()
	client, err := NewClient(ua)
	require.NoError(t, err)
	client.TxRequester = &siptest.ClientTxRequester{
		OnRequest: f,
	}
	return client
}

func testClientResponder(t testing.TB, f func(req *sip.Request, w *siptest.ClientTxResponder)) *Client {
	ua, _ := NewUA()
	client, err := NewClient(ua)
	require.NoError(t, err)
	client.TxRequester = &siptest.ClientTxRequesterResponder{
		OnRequest: f,
	}
	return client
}

func TestDialogClientRequestRecordRouteHeaders(t *testing.T) {
	client := testClient(t, func(req *sip.Request) *sip.Response {
		return sip.NewResponseFromRequest(req, 200, "OK", nil)
	})

	invite := sip.NewRequest(sip.INVITE, sip.Uri{User: "test", Host: "localhost"})
	invite.AppendHeader(sip.NewHeader("Contact", "<sip:uac@uac.p1.com>"))
	err := clientRequestBuildReq(client, invite)
	require.NoError(t, err)
	// assert.Equal(t, "localhost:5060", invite.Source())
	assert.Equal(t, "localhost:5060", invite.Destination())

	t.Run("LooseRouting", func(t *testing.T) {

		resp := sip.NewResponseFromRequest(invite, 200, "OK", nil)
		resp.AppendHeader(sip.NewHeader("Contact", "<sip:uas@uas.p2.com>"))
		// Fake some proxy headers
		resp.AppendHeader(sip.NewHeader("Record-Route", "<sip:p2.com;lr>"))
		resp.AppendHeader(sip.NewHeader("Record-Route", "<sip:p1.com;lr>"))

		s := DialogClientSession{
			UA: &DialogUA{
				Client: client,
			},
			Dialog: Dialog{
				InviteRequest:  invite,
				InviteResponse: resp,
			},
			inviteTx: sip.NewClientTx("test", invite, nil, slog.Default()),
		}
		// Send canceled request
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		ack := newAckRequestUAC(s.InviteRequest, s.InviteResponse, nil)
		assert.Equal(t, "uas.p2.com:5060", ack.Destination())
		s.WriteAck(ctx, ack)
		assert.Equal(t, "sip:uas@uas.p2.com", ack.Recipient.String())
		assert.Equal(t, "<sip:p1.com;lr>", ack.Route().Value())
		assert.Equal(t, "<sip:p2.com;lr>", ack.GetHeaders("Route")[1].Value())

		bye := newByeRequestUAC(s.InviteRequest, s.InviteResponse, nil)
		s.Do(ctx, bye)
		assert.Equal(t, "sip:uas@uas.p2.com", bye.Recipient.String())
		assert.Equal(t, "<sip:p1.com;lr>", bye.Route().Value())
		assert.Equal(t, "<sip:p2.com;lr>", bye.GetHeaders("Route")[1].Value())
	})

	t.Run("StrictRouting", func(t *testing.T) {

		resp := sip.NewResponseFromRequest(invite, 200, "OK", nil)
		resp.AppendHeader(sip.NewHeader("Contact", "<sip:uas@uas.p2.com>"))
		// Fake some proxy headers
		resp.AppendHeader(sip.NewHeader("Record-Route", "<sip:p2.com;lr>"))
		resp.AppendHeader(sip.NewHeader("Record-Route", "<sip:p1.com>"))

		s := DialogClientSession{
			UA: &DialogUA{
				Client: client,
			},
			Dialog: Dialog{
				InviteRequest:  invite,
				InviteResponse: resp,
			},
			inviteTx: sip.NewClientTx("test", invite, nil, slog.Default()),
		}

		// Send canceled request
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		ack := newAckRequestUAC(s.InviteRequest, s.InviteResponse, nil)
		assert.Equal(t, "uas.p2.com:5060", ack.Destination())
		s.WriteAck(ctx, ack)
		assert.Equal(t, "sip:p1.com", ack.Recipient.String())
		assert.Equal(t, "<sip:p1.com>", ack.Route().Value())
		assert.Equal(t, "<sip:p2.com;lr>", ack.GetHeaders("Route")[1].Value())

		bye := newByeRequestUAC(s.InviteRequest, s.InviteResponse, nil)
		s.Do(ctx, bye)
		assert.Equal(t, "sip:p1.com", bye.Recipient.String())
		assert.Equal(t, "<sip:p1.com>", bye.Route().Value())
		assert.Equal(t, "<sip:p2.com;lr>", bye.GetHeaders("Route")[1].Value())
	})

}

func TestNewAckRequestUACKeepsAdvertisedViaAddress(t *testing.T) {
	invite := sip.NewRequest(sip.INVITE, sip.Uri{User: "device", Host: "192.0.2.20", Port: 5060})
	client := testClient(t, func(req *sip.Request) *sip.Response {
		return sip.NewResponseFromRequest(req, sip.StatusOK, "OK", nil)
	})
	require.NoError(t, clientRequestBuildReq(client, invite))
	invite.RemoveHeader("Via")
	params := sip.NewParams()
	params.Add("branch", "z9hG4bK.original")
	invite.AppendHeader(&sip.ViaHeader{
		ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "UDP",
		Host: "192.0.2.10", Port: 5062, Params: params,
	})
	response := sip.NewResponseFromRequest(invite, sip.StatusOK, "OK", nil)

	ack := newAckRequestUAC(invite, response, nil)

	require.NotNil(t, ack.Via())
	assert.Equal(t, "192.0.2.10", ack.Via().Host)
	assert.Equal(t, 5062, ack.Via().Port)
	assert.Equal(t, "UDP", ack.Via().Transport)
	assert.NotEqual(t, "z9hG4bK.original", ack.Via().Params.GetOr("branch", ""))
}

func TestInviteSentByViaKeepsAddressAndRotatesBranch(t *testing.T) {
	invite := sip.NewRequest(sip.INVITE, sip.Uri{User: "device", Host: "192.0.2.20", Port: 5060})
	params := sip.NewParams()
	params.Add("branch", "z9hG4bK.original")
	invite.AppendHeader(&sip.ViaHeader{
		ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "TCP",
		Host: "192.0.2.10", Port: 5062, Params: params,
	})

	via := inviteSentByVia(invite)

	require.NotNil(t, via)
	assert.Equal(t, "192.0.2.10", via.Host)
	assert.Equal(t, 5062, via.Port)
	assert.Equal(t, "TCP", via.Transport)
	assert.NotEqual(t, "z9hG4bK.original", via.Params.GetOr("branch", ""))
	// 原始 INVITE 的 Via 不得被就地改写。
	assert.Equal(t, "z9hG4bK.original", invite.Via().Params.GetOr("branch", ""))
	assert.Nil(t, inviteSentByVia(sip.NewRequest(sip.INVITE, sip.Uri{Host: "192.0.2.20"})), "没有 Via 时不得伪造 sent-by")
}

// TestDialogClientSessionByeKeepsAdvertisedViaAddress 端到端钉住 BYE 的 sent-by。
//
// 真实报文实测:拖动时间轴触发的拆除 INFO/BYE 曾带着 `[::]:5062` 出网,而设备
// 按 RFC 3261 §18.2.1 把应答发往 sent-by,于是每条都只能靠重传超时收场(实测
// 该设备 2890 条出向 BYE 零应答)。INVITE 由应用层显式写地址、ACK 克隆 INVITE,
// 只有 BYE 需要在这里补齐。
func TestDialogClientSessionByeKeepsAdvertisedViaAddress(t *testing.T) {
	peer, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	port := peer.LocalAddr().(*net.UDPAddr).Port

	var mu sync.Mutex
	var bye *sip.Request
	go func() {
		buf := make([]byte, 8192)
		for {
			n, addr, readErr := peer.ReadFrom(buf)
			if readErr != nil {
				return
			}
			message, parseErr := sip.ParseMessage(buf[:n])
			if parseErr != nil {
				continue
			}
			request, ok := message.(*sip.Request)
			if !ok {
				continue
			}
			var response *sip.Response
			switch request.Method {
			case sip.INVITE:
				response = cleanupBranchResponse(request, port)
			case sip.BYE:
				mu.Lock()
				bye = request
				mu.Unlock()
				response = sip.NewResponseFromRequest(request, sip.StatusOK, "OK", nil)
			default:
				continue
			}
			_, _ = peer.WriteTo([]byte(response.String()), addr)
		}
	}()

	dua, invite := ownedInviteRequest(t, peer.LocalAddr().String())
	session, err := dua.WriteInvite(context.Background(), invite)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, session.WaitAnswer(ctx, AnswerOptions{}))
	require.NoError(t, session.Ack(ctx))
	require.NoError(t, session.Bye(ctx))

	mu.Lock()
	defer mu.Unlock()
	require.NotNil(t, bye, "BYE 必须真的发到设备")
	require.Len(t, bye.GetHeaders("Via"), 1, "BYE 只能有一个 Via")
	assert.Equal(t, invite.Via().Host, bye.Via().Host)
	assert.Equal(t, invite.Via().Port, bye.Via().Port)
	assert.NotEqual(t, invite.Via().Params.GetOr("branch", ""), bye.Via().Params.GetOr("branch", ""))
}

// TestNewBranchCleanupKeepsSingleVia 钉住 NewBranchCleanup 的单 Via 不变式。
//
// 它自己会用 INVITE 的 sent-by 覆盖 BYE 的 Via；若 newByeRequestUAC 也塞一个，
// 就会变成两个 Via，被 validFixedCleanupRequest 判为非法，整条定点拆除路径直接
// 不发 BYE（实测 download-final-SQL 用例 byes=0）。这个不变式同时也是
// NewFixedBranchCleanup 的前提：ACK/BYE 除 branch 外必须逐字节相同。
func TestNewBranchCleanupKeepsSingleVia(t *testing.T) {
	dua, invite := ownedInviteRequest(t, "127.0.0.1:5060")
	owner, err := dua.NewBranchCleanup(invite, cleanupBranchResponse(invite, 5060), invite.CSeq().SeqNo+1)
	require.NoError(t, err)
	defer owner.Terminate()

	ack, bye := owner.ACKRequest(), owner.BYERequest()
	require.Len(t, bye.GetHeaders("Via"), 1, "BYE 只能有一个 Via")
	require.Equal(t, invite.Via().Host, bye.Via().Host)
	require.Equal(t, invite.Via().Port, bye.Via().Port)
	require.NotEqual(t, ack.Via().Params.GetOr("branch", ""), bye.Via().Params.GetOr("branch", ""))
}

func TestDialogClientMultiRequest(t *testing.T) {
	var sentReq *sip.Request
	client := testClient(t, func(req *sip.Request) *sip.Response {
		sentReq = req
		return sip.NewResponseFromRequest(req, 200, "OK", nil)
	})

	dua := DialogUA{
		Client: client,
	}
	d, err := dua.Invite(context.TODO(), sip.Uri{User: "test", Host: "localhost"}, nil)
	require.NoError(t, err)
	assert.NotNil(t, d.InviteRequest.From())
	assert.NotNil(t, d.InviteRequest.To())
	assert.NotNil(t, d.InviteRequest.Contact())
	assert.NotEmpty(t, d.InviteRequest.CallID())
	assert.NotEmpty(t, d.InviteRequest.MaxForwards())

	err = d.WaitAnswer(context.TODO(), AnswerOptions{})
	require.NoError(t, err)
	d.Ack(context.TODO())
	assert.Equal(t, d.InviteRequest.CSeq().SeqNo, sentReq.CSeq().SeqNo)

	_, err = d.Do(context.Background(), sip.NewRequest(sip.INVITE, sip.Uri{User: "reinvite", Host: "localhost"}))
	require.NoError(t, err)

	assert.Equal(t, d.InviteRequest.CSeq().SeqNo+1, sentReq.CSeq().SeqNo)
}

func TestDialogClientMultiResponses(t *testing.T) {

	t.Run("ProvisionalLoop", func(t *testing.T) {
		client := testClient(t, func(req *sip.Request) *sip.Response {
			return sip.NewResponseFromRequest(req, 100, "Trying", nil)
		})

		dua := DialogUA{
			Client: client,
		}
		d, err := dua.Invite(context.TODO(), sip.Uri{User: "test", Host: "localhost"}, nil)
		require.NoError(t, err)
		go func() {
			// Receive more provisional
			for i := 0; i < 10; i++ {
				d.inviteTx.(*sip.ClientTx).Receive(sip.NewResponseFromRequest(d.InviteRequest, 100, "Trying", nil))
			}
		}()
		err = d.WaitAnswer(context.TODO(), AnswerOptions{})
		require.Error(t, err)
	})
	t.Run("ProxyAuthLoop", func(t *testing.T) {
		var sentReq *sip.Request
		client := testClient(t, func(req *sip.Request) *sip.Response {
			sentReq = req
			res := sip.NewResponseFromRequest(req, 407, "Unauthorized", nil)
			challenge := `Digest username="user", realm="test", nonce="662d65a084b88c6d2a745a9de086fa91", uri="sip:+user@example.com", algorithm=sha-256, response="3681b63e5d9c3bb80e5350e2783d7b88"`
			res.AppendHeader(sip.NewHeader("Proxy-Authenticate", challenge))
			return res
		})

		dua := DialogUA{
			Client: client,
		}
		d, err := dua.Invite(context.TODO(), sip.Uri{User: "test", Host: "localhost"}, nil)
		require.NoError(t, err)

		err = d.WaitAnswer(context.TODO(), AnswerOptions{Password: "secret"})
		require.Error(t, err)
		assert.Equal(t, d.InviteRequest.CSeq().SeqNo, sentReq.CSeq().SeqNo)
	})

	t.Run("AuthLoop", func(t *testing.T) {
		var sentReq *sip.Request
		client := testClient(t, func(req *sip.Request) *sip.Response {
			sentReq = req
			res := sip.NewResponseFromRequest(req, 401, "Unauthorized", nil)
			challenge := `Digest username="user", realm="test", nonce="662d65a084b88c6d2a745a9de086fa91", uri="sip:+user@example.com", algorithm=sha-256, response="3681b63e5d9c3bb80e5350e2783d7b88"`
			res.AppendHeader(sip.NewHeader("WWW-Authenticate", challenge))
			return res
		})

		dua := DialogUA{
			Client: client,
		}
		d, err := dua.Invite(context.TODO(), sip.Uri{User: "test", Host: "localhost"}, nil)
		require.NoError(t, err)

		err = d.WaitAnswer(context.TODO(), AnswerOptions{Password: "secret"})
		require.Error(t, err)
		assert.Equal(t, d.InviteRequest.CSeq().SeqNo, sentReq.CSeq().SeqNo)
	})

}

func TestDialogClientACKRetransmission(t *testing.T) {
	var acks int32
	client := testClientResponder(t, func(req *sip.Request, w *siptest.ClientTxResponder) {
		if req.IsAck() {
			atomic.AddInt32(&acks, 1)
			return
		}

		res := sip.NewResponseFromRequest(req, 200, "OK", nil)
		w.Receive(res)
		time.Sleep(sip.T1)
		w.Receive(res)
		time.Sleep(sip.T1)
		w.Receive(res)
	})

	dua := DialogUA{
		Client: client,
	}
	d, err := dua.Invite(context.TODO(), sip.Uri{User: "test", Host: "localhost"}, nil)
	require.NoError(t, err)
	err = d.WaitAnswer(context.TODO(), AnswerOptions{})
	require.NoError(t, err)

	// We will keep receiving retransmission
	if err := d.Ack(context.TODO()); err != nil {
		t.Error(err)
	}
	time.Sleep(4 * sip.T1)
	// It should retransmit
	state := d.LoadState()
	assert.Equal(t, sip.DialogStateConfirmed, state)
	assert.EqualValues(t, 3, atomic.LoadInt32(&acks))
}

func BenchmarkDialogDo(b *testing.B) {
	ua, _ := NewUA()
	cli, _ := NewClient(ua)
	cli.TxRequester = &siptest.ClientTxRequester{
		OnRequest: func(req *sip.Request) *sip.Response {
			return sip.NewResponseFromRequest(req, 200, "OK", nil)
		},
	}
	dua := &DialogUA{
		Client: cli,
	}

	dialog, err := dua.Invite(context.TODO(), sip.Uri{User: "test", Host: "localhost"}, nil)
	require.NoError(b, err)
	dialog.WaitAnswer(context.TODO(), AnswerOptions{})

	b.Run("ACK", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			dialog.Ack(context.TODO())
		}
	})
	b.Run("NotSupported", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := sip.NewRequest(sip.REFER, sip.Uri{User: "refer", Host: "localhost"})
			dialog.Do(context.TODO(), req)
		}
	})

}
