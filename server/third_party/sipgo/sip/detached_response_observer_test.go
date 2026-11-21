package sip

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func detachedResponseFixture(t *testing.T) (DetachedInviteResponseSelector, *Response) {
	t.Helper()
	req, _, _ := testCreateInvite(t, "sip:127.0.0.99:5060", "tcp", "127.0.0.2:5060")
	req.From().Params.Add("tag", "local-fixed")
	req.Via().Transport = "TCP"
	req.Via().Port = 5060
	response := NewResponseFromRequest(req, 200, "OK", nil)
	response.SetTransport("TCP")
	response.SetSource("127.0.0.99:5060")
	return DetachedInviteResponseSelector{Branch: req.Via().Params.GetOr("branch", ""), CallID: string(*req.CallID()), CSeq: req.CSeq().SeqNo,
		FromURI: req.From().Address.String(), LocalTag: "local-fixed", ToURI: req.To().Address.String(),
		ViaHost: req.Via().Host, ViaPort: req.Via().Port, Transport: "TCP", Destination: "127.0.0.99:5060"}, response
}

func TestDetachedResponseObserverExactSelector(t *testing.T) {
	for _, fault := range []string{"none", "call-id", "cseq", "method", "from", "to", "tag", "via-host", "via-port", "via-transport", "transport", "source-ip", "source-port", "source-missing", "duplicate-via", "duplicate-branch", "duplicate-call-id"} {
		t.Run(fault, func(t *testing.T) {
			layer := responseObserverLayer(t)
			selector, response := detachedResponseFixture(t)
			sink := &responseCaptureFixture{}
			owner, err := layer.ObserveDetachedClientResponses(selector, sink)
			require.NoError(t, err)
			defer owner.Close()
			selector.CallID = "caller-mutated"
			switch fault {
			case "call-id":
				*response.CallID() = "other"
			case "cseq":
				response.CSeq().SeqNo++
			case "method":
				response.CSeq().MethodName = ACK
			case "from":
				response.From().Address.User = "other"
			case "to":
				response.To().Address.User = "other"
			case "tag":
				response.From().Params.Add("tag", "other")
			case "via-host":
				response.Via().Host = "127.0.0.88"
			case "via-port":
				response.Via().Port++
			case "via-transport":
				response.Via().Transport = "UDP"
			case "transport":
				response.SetTransport("UDP")
			case "source-ip":
				response.SetSource("127.0.0.88:5060")
			case "source-port":
				response.SetSource("127.0.0.99:5061")
			case "source-missing":
				response.SetSource("")
			case "duplicate-via":
				response.AppendHeader(response.Via().Clone())
			case "duplicate-branch":
				response.Via().Params = append(response.Via().Params, HeaderKV{K: "branch", V: response.Via().Params.GetOr("branch", "")})
			case "duplicate-call-id":
				response.AppendHeader(NewHeader("Call-ID", string(*response.CallID())))
			}
			layer.captureClientResponse(response)
			if fault == "none" {
				require.EqualValues(t, 1, sink.captured.Load())
				require.Zero(t, sink.lost.Load())
			} else {
				require.Zero(t, sink.captured.Load())
				require.EqualValues(t, 1, sink.lost.Load())
			}
		})
	}
}

func TestDetachedResponseObserverRejectsUnsafeSelector(t *testing.T) {
	for _, fault := range []string{"branch", "call-id", "cseq", "from", "to", "tag", "via-port", "transport", "dns-source", "no-port"} {
		t.Run(fault, func(t *testing.T) {
			layer := responseObserverLayer(t)
			selector, _ := detachedResponseFixture(t)
			switch fault {
			case "branch":
				selector.Branch = "not-rfc-branch"
			case "call-id":
				selector.CallID = "bad\r\nheader"
			case "cseq":
				selector.CSeq = 0
			case "from":
				selector.FromURI = "bad URI"
			case "to":
				selector.ToURI = ""
			case "tag":
				selector.LocalTag = ""
			case "via-port":
				selector.ViaPort = 0
			case "transport":
				selector.Transport = "WS"
			case "dns-source":
				selector.Destination = "device.example:5060"
			case "no-port":
				selector.Destination = "127.0.0.99"
			}
			sink := &responseCaptureFixture{}
			owner, err := layer.ObserveDetachedClientResponses(selector, sink)
			require.ErrorIs(t, err, ErrClientResponseObservation)
			require.Nil(t, owner)
			require.EqualValues(t, 1, sink.lost.Load())
			require.Empty(t, layer.responseObservers)
		})
	}
}

func TestDetachedResponseObserverSharesCapacityAndLifetime(t *testing.T) {
	layer := responseObserverLayer(t)
	selector, response := detachedResponseFixture(t)
	sink := &responseCaptureFixture{}
	owner, err := layer.ObserveDetachedClientResponses(selector, sink)
	require.NoError(t, err)
	duplicate := &responseCaptureFixture{}
	_, err = layer.ObserveDetachedClientResponses(selector, duplicate)
	require.Error(t, err)
	require.EqualValues(t, 1, duplicate.lost.Load())
	require.EqualValues(t, 1, sink.lost.Load())
	for n := 1; n < maxClientResponseObservers; n++ {
		other := selector
		other.Branch = GenerateBranch()
		_, err := layer.ObserveDetachedClientResponses(other, &responseCaptureFixture{})
		require.NoError(t, err)
	}
	other := selector
	other.Branch = GenerateBranch()
	_, err = layer.ObserveDetachedClientResponses(other, &responseCaptureFixture{})
	require.Error(t, err)
	layer.captureClientResponse(response)
	require.EqualValues(t, 1, sink.captured.Load())
	require.Len(t, layer.responseObservers, maxClientResponseObservers)
	owner.Close()
	<-owner.Done()
	layer.captureClientResponse(response)
	require.EqualValues(t, 1, sink.captured.Load())
	layer.Close()
	_, err = layer.ObserveDetachedClientResponses(other, &responseCaptureFixture{})
	require.Error(t, err)
}

func TestDetachedResponseObserverActualTCPSource(t *testing.T) {
	layer := responseObserverLayer(t)
	peer, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	req, _, _ := testCreateInvite(t, "sip:"+peer.Addr().String(), "tcp", "127.0.0.2:5060")
	req.From().Params.Add("tag", "local-fixed")
	req.Via().Transport, req.Via().Port = "TCP", 5060
	req.SetTransport("TCP")
	req.SetDestination(peer.Addr().String())
	selector := DetachedInviteResponseSelector{Branch: req.Via().Params.GetOr("branch", ""), CallID: string(*req.CallID()), CSeq: req.CSeq().SeqNo,
		FromURI: req.From().Address.String(), LocalTag: "local-fixed", ToURI: req.To().Address.String(), ViaHost: req.Via().Host, ViaPort: req.Via().Port,
		Transport: "TCP", Destination: peer.Addr().String()}
	sink := &responseCaptureFixture{}
	owner, err := layer.ObserveDetachedClientResponses(selector, sink)
	require.NoError(t, err)
	defer owner.Close()
	// Test-only transport connection supplies actual TCP receive metadata. The
	// observer itself neither opens it nor writes an INVITE to it.
	conn, err := layer.tpl.ClientRequestConnection(context.Background(), req)
	require.NoError(t, err)
	defer conn.TryClose()
	require.NoError(t, peer.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second)))
	remote, err := peer.Accept()
	require.NoError(t, err)
	defer remote.Close()
	response := NewResponseFromRequest(req, 200, "OK", nil)
	_, err = remote.Write([]byte(response.String()))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return sink.captured.Load() == 1 }, time.Second, time.Millisecond)
	require.Zero(t, sink.lost.Load(), "actual socket source matches the original numeric destination")
	require.NoError(t, remote.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	n, err := remote.Read(make([]byte, 1024))
	require.Zero(t, n)
	require.Error(t, err, "observation sends no bytes")
}
