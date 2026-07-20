package sip

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	siplib "github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

type observedWrite struct {
	props siplib.TransportWriteProps
	data  []byte
}

func TestTransportWriteObserverMatchesUDPRequestBytes(t *testing.T) {
	methods := []siplib.RequestMethod{
		siplib.REGISTER,
		siplib.MESSAGE,
		siplib.INVITE,
		siplib.ACK,
		siplib.BYE,
		siplib.SUBSCRIBE,
	}

	for _, method := range methods {
		t.Run(method.String(), func(t *testing.T) {
			receiver, err := net.ListenPacket("udp4", "127.0.0.1:0")
			require.NoError(t, err)
			defer receiver.Close()

			writes := make(chan observedWrite, 1)
			ua, err := sipgo.NewUA(
				sipgo.WithUserAgent("trace-test"),
				sipgo.WithUserAgentTransportLayerOptions(
					siplib.WithTransportLayerWriteObserver(func(props siplib.TransportWriteProps, data []byte) {
						writes <- observedWrite{props: props, data: append([]byte(nil), data...)}
					}),
				),
			)
			require.NoError(t, err)
			defer ua.Close()

			client, err := sipgo.NewClient(ua, sipgo.WithClientHostname("127.0.0.1"))
			require.NoError(t, err)

			req := newObservedRequest(method, receiver.LocalAddr().String(), "UDP")
			require.NoError(t, client.WriteRequest(req))

			require.NoError(t, receiver.SetReadDeadline(time.Now().Add(2*time.Second)))
			buf := make([]byte, 64*1024)
			n, remote, err := receiver.ReadFrom(buf)
			require.NoError(t, err)
			actual := append([]byte(nil), buf[:n]...)

			write := receiveObservedWrite(t, writes)
			require.Equal(t, actual, write.data)
			require.Equal(t, "UDP", write.props.Transport)
			requireSamePort(t, remote, write.props.LocalAddr)
			require.Equal(t, receiver.LocalAddr().String(), write.props.RemoteAddr.String())
			require.NotNil(t, req.Via())
			require.NotNil(t, req.CallID())
			require.NotNil(t, req.CSeq())
		})
	}
}

func requireSamePort(t *testing.T, expected, actual net.Addr) {
	t.Helper()
	_, expectedPort, err := net.SplitHostPort(expected.String())
	require.NoError(t, err)
	_, actualPort, err := net.SplitHostPort(actual.String())
	require.NoError(t, err)
	require.Equal(t, expectedPort, actualPort)
}

func TestTransportWriteObserverMatchesTCPRequestBytes(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	received := make(chan []byte, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		defer conn.Close()
		data, err := readSIPFrame(conn)
		if err != nil {
			acceptErr <- err
			return
		}
		received <- data
	}()

	writes := make(chan observedWrite, 1)
	ua, err := sipgo.NewUA(sipgo.WithUserAgentTransportLayerOptions(
		siplib.WithTransportLayerWriteObserver(func(props siplib.TransportWriteProps, data []byte) {
			writes <- observedWrite{props: props, data: append([]byte(nil), data...)}
		}),
	))
	require.NoError(t, err)
	defer ua.Close()
	client, err := sipgo.NewClient(ua, sipgo.WithClientHostname("127.0.0.1"))
	require.NoError(t, err)

	req := newObservedRequest(siplib.INVITE, listener.Addr().String(), "TCP")
	require.NoError(t, client.WriteRequest(req))

	var actual []byte
	select {
	case actual = <-received:
	case err := <-acceptErr:
		t.Fatalf("read TCP SIP frame: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for TCP SIP frame")
	}

	write := receiveObservedWrite(t, writes)
	require.Equal(t, actual, write.data)
	require.Equal(t, "TCP", write.props.Transport)
	require.Equal(t, listener.Addr().String(), write.props.RemoteAddr.String())
}

func TestTransportWriteObserverMatchesServerResponseBytes(t *testing.T) {
	for _, status := range []int{siplib.StatusUnauthorized, siplib.StatusOK} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			serverConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
			require.NoError(t, err)
			defer serverConn.Close()

			writes := make(chan observedWrite, 1)
			ua, err := sipgo.NewUA(sipgo.WithUserAgentTransportLayerOptions(
				siplib.WithTransportLayerWriteObserver(func(props siplib.TransportWriteProps, data []byte) {
					writes <- observedWrite{props: props, data: append([]byte(nil), data...)}
				}),
			))
			require.NoError(t, err)
			defer ua.Close()
			server, err := sipgo.NewServer(ua)
			require.NoError(t, err)
			server.OnRegister(func(req *siplib.Request, tx siplib.ServerTransaction) {
				reason := "Unauthorized"
				if status == siplib.StatusOK {
					reason = "OK"
				}
				res := siplib.NewResponseFromRequest(req, status, reason, nil)
				require.NoError(t, tx.Respond(res))
			})
			go func() { _ = server.ServeUDP(serverConn) }()

			clientConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
			require.NoError(t, err)
			defer clientConn.Close()
			require.NoError(t, clientConn.SetDeadline(time.Now().Add(2*time.Second)))

			raw := rawRegister(serverConn.LocalAddr().String(), clientConn.LocalAddr().String(), "UDP", status)
			_, err = clientConn.WriteTo(raw, serverConn.LocalAddr())
			require.NoError(t, err)
			buf := make([]byte, 64*1024)
			n, _, err := clientConn.ReadFrom(buf)
			require.NoError(t, err)

			write := receiveObservedWrite(t, writes)
			require.Equal(t, buf[:n], write.data)
			require.Equal(t, "UDP", write.props.Transport)
		})
	}
}

func TestTransportWriteObserverMatchesTCPServerResponseBytes(t *testing.T) {
	for _, status := range []int{siplib.StatusUnauthorized, siplib.StatusOK} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()

			writes := make(chan observedWrite, 1)
			ua, err := sipgo.NewUA(sipgo.WithUserAgentTransportLayerOptions(
				siplib.WithTransportLayerWriteObserver(func(props siplib.TransportWriteProps, data []byte) {
					writes <- observedWrite{props: props, data: append([]byte(nil), data...)}
				}),
			))
			require.NoError(t, err)
			defer ua.Close()
			server, err := sipgo.NewServer(ua)
			require.NoError(t, err)
			server.OnRegister(func(req *siplib.Request, tx siplib.ServerTransaction) {
				reason := "Unauthorized"
				if status == siplib.StatusOK {
					reason = "OK"
				}
				res := siplib.NewResponseFromRequest(req, status, reason, nil)
				require.NoError(t, tx.Respond(res))
			})
			go func() { _ = server.ServeTCP(listener) }()

			clientConn, err := net.DialTimeout("tcp4", listener.Addr().String(), 2*time.Second)
			require.NoError(t, err)
			defer clientConn.Close()
			raw := rawRegister(listener.Addr().String(), clientConn.LocalAddr().String(), "TCP", status)
			_, err = clientConn.Write(raw)
			require.NoError(t, err)
			actual, err := readSIPFrame(clientConn)
			require.NoError(t, err)

			write := receiveObservedWrite(t, writes)
			require.Equal(t, actual, write.data)
			require.Equal(t, "TCP", write.props.Transport)
			require.Equal(t, listener.Addr().String(), write.props.LocalAddr.String())
			require.Equal(t, clientConn.LocalAddr().String(), write.props.RemoteAddr.String())
		})
	}
}

func TestTransportWriteObserverPanicDoesNotAffectSend(t *testing.T) {
	receiver, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer receiver.Close()

	ua, err := sipgo.NewUA(sipgo.WithUserAgentTransportLayerOptions(
		siplib.WithTransportLayerWriteObserver(func(siplib.TransportWriteProps, []byte) {
			panic("observer failure")
		}),
	))
	require.NoError(t, err)
	defer ua.Close()
	client, err := sipgo.NewClient(ua, sipgo.WithClientHostname("127.0.0.1"))
	require.NoError(t, err)

	req := newObservedRequest(siplib.MESSAGE, receiver.LocalAddr().String(), "UDP")
	require.NotPanics(t, func() {
		require.NoError(t, client.WriteRequest(req))
	})
	require.NoError(t, receiver.SetReadDeadline(time.Now().Add(2*time.Second)))
	buf := make([]byte, 64*1024)
	_, _, err = receiver.ReadFrom(buf)
	require.NoError(t, err)
}

func newObservedRequest(method siplib.RequestMethod, destination, transport string) *siplib.Request {
	host, portText, _ := net.SplitHostPort(destination)
	port, _ := strconv.Atoi(portText)
	req := siplib.NewRequest(method, siplib.Uri{Scheme: "sip", User: "34020000001320000001", Host: host, Port: port})
	req.SetDestination(destination)
	req.SetTransport(transport)
	if method == siplib.MESSAGE || method == siplib.INVITE || method == siplib.SUBSCRIBE {
		req.SetBody([]byte("trace-body"))
	}
	return req
}

func receiveObservedWrite(t *testing.T, writes <-chan observedWrite) observedWrite {
	t.Helper()
	select {
	case write := <-writes:
		return write
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for transport write observer")
		return observedWrite{}
	}
}

func readSIPFrame(conn net.Conn) ([]byte, error) {
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	var raw bytes.Buffer
	contentLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		raw.WriteString(line)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		name, value, ok := strings.Cut(trimmed, ":")
		if ok && strings.EqualFold(name, "Content-Length") {
			contentLength, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if contentLength > 0 {
		body := make([]byte, contentLength)
		if _, err := io.ReadFull(reader, body); err != nil {
			return nil, err
		}
		raw.Write(body)
	}
	return raw.Bytes(), nil
}

func rawRegister(serverAddr, clientAddr, transport string, suffix int) []byte {
	return []byte(fmt.Sprintf("REGISTER sip:34020000002000000001@%s SIP/2.0\r\n"+
		"Via: SIP/2.0/%s %s;branch=z9hG4bK-%d;rport\r\n"+
		"From: <sip:34020000001320000001@3402000000>;tag=from-%d\r\n"+
		"To: <sip:34020000001320000001@3402000000>\r\n"+
		"Call-ID: trace-response-%d\r\n"+
		"CSeq: 1 REGISTER\r\n"+
		"Contact: <sip:34020000001320000001@%s>\r\n"+
		"Max-Forwards: 70\r\n"+
		"Content-Length: 0\r\n\r\n",
		serverAddr, transport, clientAddr, suffix, suffix, suffix, clientAddr))
}
