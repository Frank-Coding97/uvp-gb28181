package uac

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

// Observe the initial request on real local UDP/TCP sockets without a
// TxRequester override. No device, DB, INVITE response, ACK, CANCEL, retry or
// dialog recovery is exercised; this is only the first-send identity gate.
func TestPlaybackIntentSnapshotMatchesInitialLoopbackWire(t *testing.T) {
	for _, transport := range []string{"UDP", "TCP"} {
		t.Run(transport, func(t *testing.T) {
			var destination string
			var receive func() []byte
			if transport == "UDP" {
				conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
				require.NoError(t, err)
				defer conn.Close()
				destination = conn.LocalAddr().String()
				receive = func() []byte {
					require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
					buffer := make([]byte, 65536)
					n, _, err := conn.ReadFrom(buffer)
					require.NoError(t, err)
					return buffer[:n]
				}
			} else {
				listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
				require.NoError(t, err)
				defer listener.Close()
				destination = listener.Addr().String()
				receive = func() []byte {
					require.NoError(t, listener.SetDeadline(time.Now().Add(3*time.Second)))
					conn, err := listener.Accept()
					require.NoError(t, err)
					defer conn.Close()
					require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
					reader := bufio.NewReader(io.LimitReader(conn, 65536))
					var headers strings.Builder
					for {
						line, err := reader.ReadString('\n')
						require.NoError(t, err)
						headers.WriteString(line)
						if line == "\r\n" {
							break
						}
					}
					// Read exactly the known fixture body, not until connection
					// close: a real SIP TCP connection may remain reusable.
					body := make([]byte, len(validPlaybackInvite().SDP))
					_, err = io.ReadFull(reader, body)
					require.NoError(t, err)
					return append([]byte(headers.String()), body...)
				}
			}
			ua, err := sipgo.NewUA()
			require.NoError(t, err)
			defer ua.Close()
			u, err := New(ua, "34020000002000000001", "3402000000", "192.0.2.1", 5061, false)
			require.NoError(t, err)
			require.Nil(t, u.client.TxRequester)
			input := validPlaybackInvite()
			input.Destination, input.Transport = destination, transport
			request, _, err := u.buildPlaybackInviteRequest(input)
			require.NoError(t, err)
			prepared, snapshot, err := u.preparePlaybackIntentSnapshot(request)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			tx, err := u.client.TransactionRequest(ctx, prepared)
			require.NoError(t, err)
			tx.Terminate() // no protocol retransmission experiment in this gate
			wire := receive()
			message, err := sip.ParseMessage(wire)
			require.NoError(t, err)
			observed, ok := message.(*sip.Request)
			require.True(t, ok)
			// Routing fields are network metadata, not SIP payload fields.
			// Take them from the actual listener/protocol used above.
			observed.SetDestination(destination)
			observed.SetTransport(transport)
			actual, err := snapshotPlaybackIntentRequest(observed)
			require.NoError(t, err)
			require.Equal(t, snapshot, actual, "initial loopback transport changed prepared identity")
			t.Logf("%s actual initial INVITE matches snapshot; no peer response or dialog completion claimed", transport)
		})
	}
}
