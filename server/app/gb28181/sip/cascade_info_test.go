package sip

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

func TestServerRoutesCascadePlaybackInfo(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := probe.Addr().(*net.TCPAddr).Port
	require.NoError(t, probe.Close())
	config := testConfig()
	config.SIP.Port = port
	server, err := NewServer(config)
	require.NoError(t, err)
	server.SetCascadeDialogHandler(func(request *sip.Request, tx sip.ServerTransaction) bool {
		if request.Method != sip.INFO {
			return false
		}
		response := sip.NewResponseFromRequest(request, 200, "OK", []byte("RTSP/1.0 200 OK\r\nCSeq: 7\r\n\r\n"))
		response.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSRTSP"))
		_ = tx.Respond(response)
		return true
	})
	require.NoError(t, server.Start())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, server.Shutdown(ctx))
	})
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", address, 20*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, time.Second, 10*time.Millisecond)
	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ua.Close() })
	client, err := sipgo.NewClient(ua, sipgo.WithClientHostname("127.0.0.1"))
	require.NoError(t, err)
	request := sip.NewRequest(sip.INFO, sip.Uri{User: config.SIP.ServerID, Host: "127.0.0.1", Port: port})
	request.SetTransport("TCP")
	request.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSRTSP"))
	request.SetBody([]byte("PAUSE RTSP/1.0\r\nCSeq: 7\r\nPauseTime: now\r\n\r\n"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := client.Do(ctx, request)
	require.NoError(t, err)
	require.Equal(t, 200, response.StatusCode)
	require.True(t, strings.Contains(string(response.Body()), "CSeq: 7"))
}
