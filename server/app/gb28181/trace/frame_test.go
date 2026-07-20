package trace

import (
	"net"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

func TestFrameAssemblerUDPDatagramIsOneExactFrame(t *testing.T) {
	assembler := NewFrameAssembler(1024)
	props := testReadProps("UDP", 5060, 15060)
	raw := sipMessage("MESSAGE", "udp-1", []byte("hello"))

	frames := assembler.Push(props, raw)
	require.Len(t, frames, 1)
	require.Equal(t, raw, frames[0].Data)
	require.False(t, frames[0].Malformed)
}

func TestFrameAssemblerTCPHandlesPartialAndStickyFrames(t *testing.T) {
	assembler := NewFrameAssembler(4096)
	props := testReadProps("TCP", 5060, 15060)
	first := sipMessage("MESSAGE", "tcp-1", []byte("first-body"))
	second := sipMessage("BYE", "tcp-2", nil)
	cut := len(first) - 4

	require.Empty(t, assembler.Push(props, first[:cut]))
	frames := assembler.Push(props, append(append([]byte(nil), first[cut:]...), second...))
	require.Len(t, frames, 2)
	require.Equal(t, first, frames[0].Data)
	require.Equal(t, second, frames[1].Data)
}

func TestFrameAssemblerTCPKeepsConnectionsIsolated(t *testing.T) {
	assembler := NewFrameAssembler(4096)
	left := testReadProps("TCP", 5060, 15061)
	right := testReadProps("TCP", 5060, 15062)
	leftRaw := sipMessage("INVITE", "left", []byte("left"))
	rightRaw := sipMessage("SUBSCRIBE", "right", []byte("right"))

	require.Empty(t, assembler.Push(left, leftRaw[:len(leftRaw)/2]))
	require.Empty(t, assembler.Push(right, rightRaw[:len(rightRaw)/2]))
	require.Equal(t, leftRaw, assembler.Push(left, leftRaw[len(leftRaw)/2:])[0].Data)
	require.Equal(t, rightRaw, assembler.Push(right, rightRaw[len(rightRaw)/2:])[0].Data)
}

func TestFrameAssemblerMalformedLengthResynchronizes(t *testing.T) {
	assembler := NewFrameAssembler(4096)
	props := testReadProps("TCP", 5060, 15060)
	malformed := []byte("MESSAGE sip:test SIP/2.0\r\nCall-ID: bad\r\nContent-Length: nope\r\n\r\n")
	valid := sipMessage("REGISTER", "after-bad", nil)

	frames := assembler.Push(props, append(append([]byte(nil), malformed...), valid...))
	require.Len(t, frames, 2)
	require.True(t, frames[0].Malformed)
	require.Equal(t, malformed, frames[0].Data)
	require.Equal(t, valid, frames[1].Data)
}

func TestFrameAssemblerBufferLimitDoesNotBlockLaterMessage(t *testing.T) {
	assembler := NewFrameAssembler(128)
	props := testReadProps("TCP", 5060, 15060)
	oversized := []byte("MESSAGE sip:test SIP/2.0\r\nCall-ID: huge\r\nContent-Length: 9999\r\n\r\n")
	valid := sipMessage("BYE", "after-huge", nil)

	frames := assembler.Push(props, append(append([]byte(nil), oversized...), valid...))
	require.Len(t, frames, 2)
	require.True(t, frames[0].Malformed)
	require.Equal(t, valid, frames[1].Data)
}

func TestFrameAssemblerForgetDropsPartialConnection(t *testing.T) {
	assembler := NewFrameAssembler(4096)
	props := testReadProps("TCP", 5060, 15060)
	raw := sipMessage("MESSAGE", "forgotten", []byte("body"))
	require.Empty(t, assembler.Push(props, raw[:len(raw)/2]))
	assembler.Forget(props)
	frames := assembler.Push(props, raw[len(raw)/2:])
	require.Len(t, frames, 1)
	require.True(t, frames[0].Malformed)
}

func testReadProps(transport string, localPort, remotePort int) sip.TransportReadProps {
	return sip.TransportReadProps{
		Transport:  transport,
		LocalAddr:  &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: localPort},
		RemoteAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: remotePort},
	}
}

func sipMessage(method, callID string, body []byte) []byte {
	return []byte(method + " sip:34020000002000000001@3402000000 SIP/2.0\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 " + method + "\r\n" +
		"Content-Length: " + intString(len(body)) + "\r\n\r\n" + string(body))
}

func intString(n int) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	return string(buf[i:])
}
