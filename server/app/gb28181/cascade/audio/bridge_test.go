package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/pion/rtcp"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/sdp"
)

type testPeer struct {
	udp              *net.UDPConn
	rtcp             *net.UDPConn
	listener         *net.TCPListener
	conn             net.Conn
	transport, setup string
}

func newTestPeer(t *testing.T, transport, localSetup string) *testPeer {
	t.Helper()
	p := &testPeer{transport: transport, setup: localSetup}
	var err error
	if transport == "RTP/AVP" {
		p.udp, p.rtcp, err = listenUDPPortPair(net.IPv4(127, 0, 0, 1))
	} else if localSetup == "active" {
		p.listener, err = net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	}
	require.NoError(t, err)
	t.Cleanup(func() {
		if p.udp != nil {
			_ = p.udp.Close()
		}
		if p.rtcp != nil {
			_ = p.rtcp.Close()
		}
		if p.conn != nil {
			_ = p.conn.Close()
		}
		if p.listener != nil {
			_ = p.listener.Close()
		}
	})
	return p
}
func (p *testPeer) offer(ssrc, direction string) sdp.CascadeAudioOffer {
	port := 9
	if p.udp != nil {
		port = p.udp.LocalAddr().(*net.UDPAddr).Port
	}
	if p.listener != nil {
		port = p.listener.Addr().(*net.TCPAddr).Port
	}
	offer := sdp.CascadeAudioOffer{SessionName: "Play", RemoteIP: "127.0.0.1", RemotePort: port, Transport: p.transport, Setup: p.setup, Direction: direction, SSRC: ssrc}
	if p.rtcp != nil {
		offer.RTCPIP = "127.0.0.1"
		offer.RTCPPort = p.rtcp.LocalAddr().(*net.UDPAddr).Port
	}
	return offer
}
func (p *testPeer) connect(t *testing.T, port int) {
	t.Helper()
	if p.transport == "RTP/AVP" {
		return
	}
	var err error
	if p.setup == "passive" {
		p.conn, err = net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	} else {
		require.NoError(t, p.listener.SetDeadline(time.Now().Add(time.Second)))
		p.conn, err = p.listener.Accept()
	}
	require.NoError(t, err)
}
func (p *testPeer) send(t *testing.T, port int, packet []byte) {
	t.Helper()
	if p.udp != nil {
		_, err := p.udp.WriteToUDP(packet, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
		require.NoError(t, err)
		return
	}
	frame := make([]byte, 2+len(packet))
	binary.BigEndian.PutUint16(frame, uint16(len(packet)))
	copy(frame[2:], packet)
	// Exercise partial RFC 4571 headers and bodies instead of assuming one write/frame.
	for _, part := range [][]byte{frame[:1], frame[1:5], frame[5:]} {
		_, err := p.conn.Write(part)
		require.NoError(t, err)
	}
}
func (p *testPeer) receive(timeout time.Duration) ([]byte, error) {
	if p.udp != nil {
		_ = p.udp.SetReadDeadline(time.Now().Add(timeout))
		buf := make([]byte, 65535)
		n, _, err := p.udp.ReadFromUDP(buf)
		return buf[:n], err
	}
	_ = p.conn.SetReadDeadline(time.Now().Add(timeout))
	var header [2]byte
	if _, err := io.ReadFull(p.conn, header[:]); err != nil {
		return nil, err
	}
	buf := make([]byte, int(binary.BigEndian.Uint16(header[:])))
	_, err := io.ReadFull(p.conn, buf)
	return buf, err
}
func (p *testPeer) sendRTCP(t *testing.T, port int, packet []byte) {
	t.Helper()
	_, err := p.rtcp.WriteToUDP(packet, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	require.NoError(t, err)
}
func (p *testPeer) receiveRTCP(timeout time.Duration) ([]byte, error) {
	_ = p.rtcp.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 65535)
	n, _, err := p.rtcp.ReadFromUDP(buf)
	return buf[:n], err
}
func rtpPacket(ssrc uint32) []byte {
	p := []byte{0x80, 0x88, 0x12, 0x34, 0, 0, 0x45, 0x67, 0, 0, 0, 0, 0xd5, 0x55, 0xa1}
	binary.BigEndian.PutUint32(p[8:], ssrc)
	return p
}

func TestBridgeTransportsAndAckGate(t *testing.T) {
	modes := []struct{ name, transport, setup string }{{"UDP", "RTP/AVP", ""}, {"TCPActive", "TCP/RTP/AVP", "active"}, {"TCPPassive", "TCP/RTP/AVP", "passive"}}
	for _, srcMode := range modes {
		for _, dstMode := range modes {
			t.Run(srcMode.name+"To"+dstMode.name, func(t *testing.T) {
				src := newTestPeer(t, srcMode.transport, srcMode.setup)
				dst := newTestPeer(t, dstMode.transport, dstMode.setup)
				bridge, err := NewBridge(context.Background(), "127.0.0.1", srcMode.transport, srcMode.setup, dst.offer("0100000002", "recvonly"))
				require.NoError(t, err)
				t.Cleanup(bridge.Close)
				require.Error(t, bridge.Activate(), "cannot activate before source negotiation")
				require.NoError(t, bridge.SetSource(src.offer("0100000001", "sendonly")))
				src.connect(t, bridge.SourcePort())
				dst.connect(t, bridge.ReceiverPort())
				src.send(t, bridge.SourcePort(), rtpPacket(100000001))
				_, err = dst.receive(50 * time.Millisecond)
				require.Error(t, err, "no audio before ACK")
				require.NoError(t, bridge.Activate())
				src.send(t, bridge.SourcePort(), rtpPacket(100000001))
				packet, err := dst.receive(time.Second)
				require.NoError(t, err)
				require.Equal(t, rtpPacket(100000002), packet)
				require.Error(t, bridge.SetSource(src.offer("0100000003", "sendonly")), "source cannot migrate after negotiation")
				bridge.Close()
				select {
				case <-bridge.Done():
				case <-time.After(time.Second):
					t.Fatal("bridge did not stop")
				}
				require.NoError(t, bridge.Err())
			})
		}
	}
}

func TestBridgeDropsForeignAndInvalidRTP(t *testing.T) {
	src := newTestPeer(t, "RTP/AVP", "")
	dst := newTestPeer(t, "RTP/AVP", "")
	foreign := newTestPeer(t, "RTP/AVP", "")
	bridge, err := NewBridge(context.Background(), "127.0.0.1", "RTP/AVP", "", dst.offer("0100000002", "recvonly"))
	require.NoError(t, err)
	t.Cleanup(bridge.Close)
	require.NoError(t, bridge.SetSource(src.offer("0100000001", "sendonly")))
	require.NoError(t, bridge.Activate())
	foreign.send(t, bridge.SourcePort(), rtpPacket(100000001))
	wrongCodec := rtpPacket(100000001)
	wrongCodec[1] = 0x80
	wrongVersion := rtpPacket(100000001)
	wrongVersion[0] = 0x40
	truncatedCSRC := rtpPacket(100000001)
	truncatedCSRC[0] = 0x8f
	badExtension := rtpPacket(100000001)
	badExtension[0] = 0x90
	badPadding := rtpPacket(100000001)
	badPadding[0] = 0xa0
	badPadding[len(badPadding)-1] = 0
	for _, packet := range [][]byte{rtpPacket(100000003), wrongCodec, wrongVersion, truncatedCSRC, badExtension, badPadding, {0x80, 8}, {}} {
		src.send(t, bridge.SourcePort(), packet)
	}
	// A recvonly receiver is not allowed to inject reverse audio.
	dst.send(t, bridge.ReceiverPort(), rtpPacket(100000002))
	_, err = src.receive(50 * time.Millisecond)
	require.Error(t, err)
	src.send(t, bridge.SourcePort(), rtpPacket(100000001))
	packet, err := dst.receive(time.Second)
	require.NoError(t, err)
	require.Equal(t, rtpPacket(100000002), packet)
	_, err = dst.receive(50 * time.Millisecond)
	require.Error(t, err, "only the final valid packet may arrive")
}

func TestBridgeCancelReleasesPendingTCPListeners(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	receiver := sdp.CascadeAudioOffer{SessionName: "Play", RemoteIP: "127.0.0.1", RemotePort: 9, Transport: "TCP/RTP/AVP", Setup: "passive", Direction: "recvonly", SSRC: "0100000001"}
	bridge, err := NewBridge(ctx, "127.0.0.1", "TCP/RTP/AVP", "passive", receiver)
	require.NoError(t, err)
	source := receiver
	source.Direction = "sendonly"
	require.NoError(t, bridge.SetSource(source))
	ports := []int{bridge.SourcePort(), bridge.ReceiverPort()}
	cancel()
	select {
	case <-bridge.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel left blocked accept/read tasks")
	}
	for _, port := range ports {
		listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
		require.NoError(t, err)
		require.NoError(t, listener.Close())
	}
	bridge.Close()
}

func TestBridgeDuplexAndCoalescedTCPFrames(t *testing.T) {
	src := newTestPeer(t, "TCP/RTP/AVP", "passive")
	dst := newTestPeer(t, "TCP/RTP/AVP", "passive")
	bridge, err := NewBridge(context.Background(), "127.0.0.1", "TCP/RTP/AVP", "passive", dst.offer("0100000002", "sendrecv"))
	require.NoError(t, err)
	t.Cleanup(bridge.Close)
	require.NoError(t, bridge.SetSource(src.offer("0100000001", "sendrecv")))
	src.connect(t, bridge.SourcePort())
	dst.connect(t, bridge.ReceiverPort())
	require.NoError(t, bridge.Activate())
	frame := func(packet []byte) []byte {
		buf := make([]byte, 2+len(packet))
		binary.BigEndian.PutUint16(buf, uint16(len(packet)))
		copy(buf[2:], packet)
		return buf
	}
	first, second := rtpPacket(100000001), rtpPacket(100000001)
	second[3]++
	combined := []byte{0, 0} // RFC 4571 null frame followed by coalesced frames.
	combined = append(combined, frame(first)...)
	combined = append(combined, frame(second)...)
	_, err = src.conn.Write(combined)
	require.NoError(t, err)
	for _, want := range [][]byte{first, second} {
		binary.BigEndian.PutUint32(want[8:], 100000002)
		got, err := dst.receive(time.Second)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	dst.send(t, bridge.ReceiverPort(), rtpPacket(100000002))
	got, err := src.receive(time.Second)
	require.NoError(t, err)
	require.Equal(t, rtpPacket(100000001), got)
	require.NoError(t, dst.conn.Close())
	select {
	case <-bridge.Done():
	case <-time.After(time.Second):
		t.Fatal("TCP EOF did not close the other leg")
	}
	require.Error(t, bridge.Err(), "unexpected peer loss must be visible to the signaling owner")
}

func TestBridgeForwardsUDPAndTCPRTCPWithNegotiatedSSRCs(t *testing.T) {
	const sourceSSRC, receiverSSRC = uint32(100000001), uint32(100000002)
	sourceReport, err := rtcp.Marshal([]rtcp.Packet{&rtcp.SenderReport{SSRC: sourceSSRC, Reports: []rtcp.ReceptionReport{{SSRC: receiverSSRC}}}})
	require.NoError(t, err)
	for _, mode := range []struct{ name, transport, setup string }{{"udp", "RTP/AVP", ""}, {"tcp", "TCP/RTP/AVP", "passive"}} {
		t.Run(mode.name, func(t *testing.T) {
			src := newTestPeer(t, mode.transport, mode.setup)
			dst := newTestPeer(t, mode.transport, mode.setup)
			receiverOffer := dst.offer("0100000002", "sendrecv")
			bridge, err := NewBridge(context.Background(), "127.0.0.1", mode.transport, mode.setup, receiverOffer)
			require.NoError(t, err)
			t.Cleanup(bridge.Close)
			require.NoError(t, bridge.SetSource(src.offer("0100000001", "sendrecv")))
			src.connect(t, bridge.SourcePort())
			dst.connect(t, bridge.ReceiverPort())
			require.NoError(t, bridge.Activate())
			if mode.transport == "RTP/AVP" {
				src.sendRTCP(t, bridge.SourceRTCPPort(), sourceReport)
			} else {
				src.send(t, bridge.SourcePort(), sourceReport)
			}
			var packet []byte
			if mode.transport == "RTP/AVP" {
				packet, err = dst.receiveRTCP(time.Second)
			} else {
				packet, err = dst.receive(time.Second)
			}
			require.NoError(t, err)
			packets, err := rtcp.Unmarshal(packet)
			require.NoError(t, err)
			sr := packets[0].(*rtcp.SenderReport)
			require.Equal(t, receiverSSRC, sr.SSRC)
			require.Equal(t, sourceSSRC, sr.Reports[0].SSRC)
			receiverReport, err := rtcp.Marshal([]rtcp.Packet{&rtcp.ReceiverReport{SSRC: receiverSSRC, Reports: []rtcp.ReceptionReport{{SSRC: sourceSSRC}}}})
			require.NoError(t, err)
			if mode.transport == "RTP/AVP" {
				dst.sendRTCP(t, bridge.ReceiverRTCPPort(), receiverReport)
				packet, err = src.receiveRTCP(time.Second)
			} else {
				dst.send(t, bridge.ReceiverPort(), receiverReport)
				packet, err = src.receive(time.Second)
			}
			require.NoError(t, err)
			packets, err = rtcp.Unmarshal(packet)
			require.NoError(t, err)
			rr := packets[0].(*rtcp.ReceiverReport)
			require.Equal(t, sourceSSRC, rr.SSRC)
			require.Equal(t, receiverSSRC, rr.Reports[0].SSRC)
		})
	}
}

func TestBridgeValidRTPHeadersArePreserved(t *testing.T) {
	src := newTestPeer(t, "RTP/AVP", "")
	dst := newTestPeer(t, "RTP/AVP", "")
	bridge, err := NewBridge(context.Background(), "127.0.0.1", "RTP/AVP", "", dst.offer("0100000002", "recvonly"))
	require.NoError(t, err)
	t.Cleanup(bridge.Close)
	require.NoError(t, bridge.SetSource(src.offer("0100000001", "sendonly")))
	require.NoError(t, bridge.Activate())
	// One CSRC, an extension with one 32-bit word, payload, and two padding bytes.
	packet := append([]byte(nil), rtpPacket(100000001)[:12]...)
	packet[0] = 0xb1
	packet = append(packet, 0, 0, 0, 7, 0xbe, 0xde, 0, 1, 1, 2, 3, 4, 0xd5, 0x55, 0, 2)
	src.send(t, bridge.SourcePort(), packet)
	got, err := dst.receive(time.Second)
	require.NoError(t, err)
	binary.BigEndian.PutUint32(packet[8:], 100000002)
	require.Equal(t, packet, got)
}

func TestBridgeCancelAndConcurrentClose(t *testing.T) {
	for _, transport := range []string{"RTP/AVP", "TCP/RTP/AVP"} {
		t.Run(transport, func(t *testing.T) {
			setup := ""
			if transport == "TCP/RTP/AVP" {
				setup = "passive"
			}
			src := newTestPeer(t, transport, setup)
			dst := newTestPeer(t, transport, setup)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			bridge, err := NewBridge(ctx, "127.0.0.1", transport, setup, dst.offer("0100000002", "recvonly"))
			require.NoError(t, err)
			require.NoError(t, bridge.SetSource(src.offer("0100000001", "sendonly")))
			src.connect(t, bridge.SourcePort())
			dst.connect(t, bridge.ReceiverPort())
			closed := make(chan struct{}, 8)
			for i := 0; i < 8; i++ {
				go func() { bridge.Close(); closed <- struct{}{} }()
			}
			cancel()
			for i := 0; i < 8; i++ {
				select {
				case <-closed:
				case <-time.After(time.Second):
					t.Fatal("close deadlocked")
				}
			}
			require.Error(t, bridge.Activate())
			require.NoError(t, bridge.Err())
		})
	}
}

func TestBridgeTCPRejectsOtherSourceIP(t *testing.T) {
	src := newTestPeer(t, "TCP/RTP/AVP", "passive")
	dst := newTestPeer(t, "RTP/AVP", "")
	bridge, err := NewBridge(context.Background(), "127.0.0.1", "TCP/RTP/AVP", "passive", dst.offer("0100000002", "recvonly"))
	require.NoError(t, err)
	t.Cleanup(bridge.Close)
	require.NoError(t, bridge.SetSource(src.offer("0100000001", "sendonly")))
	require.NoError(t, bridge.Activate())
	dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 2)}, Timeout: time.Second}
	foreign, err := dialer.Dial("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(bridge.SourcePort())))
	if err != nil {
		t.Skipf("secondary loopback address unavailable: %v", err)
	}
	defer foreign.Close()
	_ = foreign.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	_, err = foreign.Read(b[:])
	require.Error(t, err)
	var timeout net.Error
	require.False(t, errors.As(err, &timeout) && timeout.Timeout(), "untrusted connection must be closed, not left pending")
	src.connect(t, bridge.SourcePort())
	src.send(t, bridge.SourcePort(), rtpPacket(100000001))
	got, err := dst.receive(time.Second)
	require.NoError(t, err)
	require.Equal(t, rtpPacket(100000002), got)
}

func TestBridgeRejectsInvalidNegotiation(t *testing.T) {
	receiver := sdp.CascadeAudioOffer{SessionName: "Play", RemoteIP: "127.0.0.1", RemotePort: 40000, Transport: "RTP/AVP", Direction: "recvonly", SSRC: "0100000001"}
	for name, change := range map[string]func(*sdp.CascadeAudioOffer){
		"bad SSRC":          func(p *sdp.CascadeAudioOffer) { p.SSRC = "bad" },
		"bad direction":     func(p *sdp.CascadeAudioOffer) { p.Direction = "inactive" },
		"bad port":          func(p *sdp.CascadeAudioOffer) { p.RemotePort = 65536 },
		"bad address":       func(p *sdp.CascadeAudioOffer) { p.RemoteIP = "239.0.0.1" },
		"bad transport":     func(p *sdp.CascadeAudioOffer) { p.Transport = "UDP/TLS/RTP/SAVPF" },
		"UDP with TCP role": func(p *sdp.CascadeAudioOffer) { p.Setup = "active" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := receiver
			change(&changed)
			bridge, err := NewBridge(context.Background(), "127.0.0.1", "RTP/AVP", "", changed)
			if bridge != nil {
				bridge.Close()
			}
			require.Error(t, err)
		})
	}
	bridge, err := NewBridge(context.Background(), "127.0.0.1", "RTP/AVP", "", receiver)
	require.NoError(t, err)
	defer bridge.Close()
	source := receiver
	source.Direction = "recvonly"
	require.Error(t, bridge.SetSource(source))
	source.Direction = "sendonly"
	source.Transport = "TCP/RTP/AVP"
	source.Setup = "passive"
	require.Error(t, bridge.SetSource(source))
	source.Transport = "RTP/AVP"
	source.Setup = ""
	require.NoError(t, bridge.SetSource(source), "invalid attempts must not consume source negotiation")
}

func TestBridgeWildcardBindDoesNotPermitWildcardPeer(t *testing.T) {
	src := newTestPeer(t, "RTP/AVP", "")
	dst := newTestPeer(t, "RTP/AVP", "")
	bridge, err := NewBridge(context.Background(), "0.0.0.0", "RTP/AVP", "", dst.offer("0100000002", "recvonly"))
	require.NoError(t, err)
	defer bridge.Close()
	invalid := src.offer("0100000001", "sendonly")
	invalid.RemoteIP = "0.0.0.0"
	require.Error(t, bridge.SetSource(invalid))
	require.NoError(t, bridge.SetSource(src.offer("0100000001", "sendonly")))
	require.NoError(t, bridge.Activate())
	src.send(t, bridge.SourcePort(), rtpPacket(100000001))
	got, err := dst.receive(time.Second)
	require.NoError(t, err)
	require.Equal(t, rtpPacket(100000002), got)
}

func TestBridgeTCPInitialConnectionRetriesAndCanBeCanceled(t *testing.T) {
	t.Run("server becomes available", func(t *testing.T) {
		src := newTestPeer(t, "TCP/RTP/AVP", "active")
		dst := newTestPeer(t, "RTP/AVP", "")
		source := src.offer("0100000001", "sendonly")
		require.NoError(t, src.listener.Close())
		bridge, err := NewBridge(context.Background(), "127.0.0.1", "TCP/RTP/AVP", "active", dst.offer("0100000002", "recvonly"))
		require.NoError(t, err)
		t.Cleanup(bridge.Close)
		started := time.Now()
		require.NoError(t, bridge.SetSource(source))
		require.NoError(t, bridge.Activate())
		select {
		case <-bridge.Done():
			t.Fatalf("first connection refusal terminated session: %v", bridge.Err())
		case <-time.After(250 * time.Millisecond):
		}
		src.listener, err = net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: source.RemotePort})
		require.NoError(t, err)
		require.NoError(t, src.listener.SetDeadline(time.Now().Add(3*time.Second)))
		src.conn, err = src.listener.Accept()
		require.NoError(t, err)
		require.GreaterOrEqual(t, time.Since(started), 900*time.Millisecond)
		src.send(t, bridge.SourcePort(), rtpPacket(100000001))
		got, err := dst.receive(time.Second)
		require.NoError(t, err)
		require.Equal(t, rtpPacket(100000002), got)
	})
	t.Run("four failures", func(t *testing.T) {
		src := newTestPeer(t, "TCP/RTP/AVP", "active")
		dst := newTestPeer(t, "RTP/AVP", "")
		source := src.offer("0100000001", "sendonly")
		require.NoError(t, src.listener.Close())
		bridge, err := NewBridge(context.Background(), "127.0.0.1", "TCP/RTP/AVP", "active", dst.offer("0100000002", "recvonly"))
		require.NoError(t, err)
		defer bridge.Close()
		started := time.Now()
		require.NoError(t, bridge.SetSource(source))
		select {
		case <-bridge.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("connection retries unbounded")
		}
		require.GreaterOrEqual(t, time.Since(started), 3*time.Second)
		require.ErrorContains(t, bridge.Err(), "after 4 attempts")
	})
	t.Run("cancel retry delay", func(t *testing.T) {
		src := newTestPeer(t, "TCP/RTP/AVP", "active")
		dst := newTestPeer(t, "RTP/AVP", "")
		source := src.offer("0100000001", "sendonly")
		require.NoError(t, src.listener.Close())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		bridge, err := NewBridge(ctx, "127.0.0.1", "TCP/RTP/AVP", "active", dst.offer("0100000002", "recvonly"))
		require.NoError(t, err)
		defer bridge.Close()
		require.NoError(t, bridge.SetSource(source))
		select {
		case <-bridge.Done():
			t.Fatal("connection failure bypassed retries")
		case <-time.After(100 * time.Millisecond):
		}
		cancel()
		select {
		case <-bridge.Done():
		case <-time.After(500 * time.Millisecond):
			t.Fatal("cancellation waited for retry delay")
		}
		require.NoError(t, bridge.Err())
	})
}
