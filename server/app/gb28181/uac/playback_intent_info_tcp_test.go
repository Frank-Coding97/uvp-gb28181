package uac

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func TestPlaybackIntentINFOActualTCPKeepsMANSRTSPResultSeparate(t *testing.T) {
	u, db, store, id, _ := playbackIntentStoreFixture(t)
	u.client.TxRequester = nil
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	barrier := playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db))
	peer, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	in := validPlaybackInvite()
	in.Destination, in.Transport = peer.Addr().String(), "TCP"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	op, err := u.beginPlaybackIntentOperation(ctx, store, barrier, id, 2, strings.Repeat("b", 32), in)
	require.NoError(t, err)
	defer op.CloseLocal(context.Background())
	require.NoError(t, peer.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second)))
	conn, err := peer.Accept()
	require.NoError(t, err)
	defer conn.Close()
	parser := sip.NewParser().NewSIPStream()
	defer parser.Close()
	var pending []sip.Message
	read := func() *sip.Request {
		t.Helper()
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
		buffer := make([]byte, 8192)
		for len(pending) == 0 {
			n, err := conn.Read(buffer)
			require.NoError(t, err)
			err = parser.ParseSIPStream(buffer[:n], func(m sip.Message) { pending = append(pending, m) })
			if !errors.Is(err, sip.ErrParseSipPartial) {
				require.NoError(t, err)
			}
		}
		r := pending[0].(*sip.Request)
		pending = pending[1:]
		return r
	}
	require.NoError(t, op.Start(ctx))
	invite := read()
	response := sip.NewResponseFromRequest(invite, 200, "OK", nil)
	response.To().Params.Add("tag", "info-tcp")
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "device", Host: "127.0.0.1", Port: peer.Addr().(*net.TCPAddr).Port}})
	_, err = conn.Write([]byte(response.String()))
	require.NoError(t, err)
	_, err = op.ReadAndAccept(ctx)
	require.NoError(t, err)
	require.Equal(t, sip.ACK, read().Method)
	for n, status := range []int{400, 200} {
		finished := make(chan error, 1)
		go func() { finished <- op.SendINFO(ctx, playauth.DeviceSIPINFOCommand{Action: "scale", Scale: 2}) }()
		info := read()
		require.Equal(t, sip.INFO, info.Method)
		require.Equal(t, invite.CSeq().SeqNo+uint32(n)+1, info.CSeq().SeqNo)
		r := sip.NewResponseFromRequest(info, 200, "OK", []byte(fmt.Sprintf("RTSP/1.0 %d Result\r\nCSeq: %d\r\n\r\n", status, info.CSeq().SeqNo)))
		r.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSRTSP"))
		_, err = conn.Write([]byte(r.String()))
		require.NoError(t, err)
		err = <-finished
		if status == 400 {
			require.ErrorIs(t, err, ErrPlaybackRejected)
		} else {
			require.NoError(t, err)
		}
	}
	require.NoError(t, op.CloseLocal(ctx))
	require.NoError(t, barrier.WaitBefore(ctx, 1, 2))
	stored, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	steps := stored.Steps[0].KnownBranch.InfoSteps
	require.Len(t, steps, 2)
	require.Equal(t, 400, steps[0].Response.MANSRTSPStatus)
	require.Equal(t, 200, steps[1].Response.MANSRTSPStatus)
	require.Equal(t, playauth.IntentDispatched, stored.Intent.State)
}
