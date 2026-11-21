package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"uvplatform.com/uvp-gb28181/app/gb28181/sdp"
)

// packetSocket prepares a media endpoint before SIP negotiation. For TCP active,
// port 9 is advertised per RFC 4145; the connection uses an ephemeral local port.
type packetSocket struct {
	ctx                context.Context
	bindIP             net.IP
	transport, setup   string
	udp                *net.UDPConn
	rtcpUDP            *net.UDPConn
	listener           *net.TCPListener
	port               int
	rtcpPort           int
	mu                 sync.Mutex
	writes             sync.Mutex
	wg                 sync.WaitGroup
	configured, closed bool
	remote             *net.UDPAddr
	rtcpRemote         *net.UDPAddr
	conn               net.Conn
	connectErr         error
	ready              chan struct{}
}

func openPacketSocket(ctx context.Context, bindIP, transport, setup string) (*packetSocket, error) {
	ip := net.ParseIP(bindIP)
	if ip == nil || ip.To4() == nil || ip.IsMulticast() || ip.Equal(net.IPv4bcast) {
		return nil, fmt.Errorf("invalid audio bind address")
	}
	s := &packetSocket{ctx: ctx, bindIP: ip, transport: transport, setup: setup, ready: make(chan struct{})}
	var err error
	switch transport {
	case "RTP/AVP":
		if setup != "" {
			return nil, fmt.Errorf("TCP role on UDP audio")
		}
		s.udp, s.rtcpUDP, err = listenUDPPortPair(ip)
		if err == nil {
			s.port = s.udp.LocalAddr().(*net.UDPAddr).Port
			s.rtcpPort = s.rtcpUDP.LocalAddr().(*net.UDPAddr).Port
		}
	case "TCP/RTP/AVP":
		switch setup {
		case "active":
			s.port = 9
		case "passive":
			s.listener, err = net.ListenTCP("tcp4", &net.TCPAddr{IP: ip})
			if err == nil {
				s.port = s.listener.Addr().(*net.TCPAddr).Port
			}
		default:
			return nil, fmt.Errorf("invalid local audio TCP role")
		}
	default:
		return nil, fmt.Errorf("unsupported audio transport")
	}
	return s, err
}

func listenUDPPortPair(ip net.IP) (*net.UDPConn, *net.UDPConn, error) {
	for attempt := 0; attempt < 32; attempt++ {
		rtp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip})
		if err != nil {
			return nil, nil, err
		}
		port := rtp.LocalAddr().(*net.UDPAddr).Port
		if port == 65535 {
			_ = rtp.Close()
			continue
		}
		rtcp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip, Port: port + 1})
		if err == nil {
			return rtp, rtcp, nil
		}
		_ = rtp.Close()
	}
	return nil, nil, fmt.Errorf("unable to reserve adjacent RTP/RTCP UDP ports")
}

func validIP(ip net.IP) bool {
	return ip != nil && ip.To4() != nil && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.Equal(net.IPv4bcast)
}

func (s *packetSocket) setPeer(offer sdp.CascadeAudioOffer) error {
	ip := net.ParseIP(offer.RemoteIP)
	if !validIP(ip) || offer.RemotePort < 1 || offer.RemotePort > 65535 || offer.Transport != s.transport || offer.Setup != s.setup {
		return fmt.Errorf("audio peer does not match prepared transport")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ctx.Err() != nil {
		return context.Canceled
	}
	if s.configured {
		return fmt.Errorf("audio peer already negotiated")
	}
	s.remote = &net.UDPAddr{IP: ip, Port: offer.RemotePort}
	if s.udp != nil {
		rtcpIP := net.ParseIP(offer.RTCPIP)
		if rtcpIP == nil {
			rtcpIP = ip
		}
		rtcpPort := offer.RTCPPort
		if rtcpPort == 0 {
			rtcpPort = offer.RemotePort + 1
		}
		if !validIP(rtcpIP) || rtcpPort < 1 || rtcpPort > 65535 {
			return fmt.Errorf("invalid audio RTCP peer")
		}
		s.rtcpRemote = &net.UDPAddr{IP: rtcpIP, Port: rtcpPort}
	}
	s.configured = true
	if s.udp != nil {
		close(s.ready)
		return nil
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		var conn net.Conn
		var err error
		if s.setup == "active" {
			dialer := net.Dialer{Timeout: 5 * time.Second, LocalAddr: &net.TCPAddr{IP: s.bindIP}}
			for attempt := 1; attempt <= 4; attempt++ {
				if attempt > 1 {
					timer := time.NewTimer(time.Second)
					select {
					case <-s.ctx.Done():
						timer.Stop()
						err = s.ctx.Err()
					case <-timer.C:
					}
				}
				if s.ctx.Err() != nil {
					err = s.ctx.Err()
					break
				}
				conn, err = dialer.DialContext(s.ctx, "tcp4", net.JoinHostPort(ip.String(), strconv.Itoa(offer.RemotePort)))
				if err == nil {
					break
				}
				if attempt == 4 {
					err = fmt.Errorf("audio TCP connection failed after 4 attempts: %w", err)
				}
			}
		} else {
			defer s.listener.Close()
			for {
				conn, err = s.listener.Accept()
				if err != nil {
					break
				}
				// An active TCP peer's source port need not equal its SDP discard port.
				if addr, ok := conn.RemoteAddr().(*net.TCPAddr); ok && addr.IP.Equal(ip) {
					break
				}
				_ = conn.Close()
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closed && conn != nil {
			_ = conn.Close()
			conn = nil
			err = context.Canceled
		}
		s.conn, s.connectErr = conn, err
		close(s.ready)
	}()
	return nil
}

func (s *packetSocket) waitReady() (net.Conn, error) {
	select {
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case <-s.ready:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, context.Canceled
	}
	return s.conn, s.connectErr
}

func (s *packetSocket) readPacket(buf []byte) (int, error) {
	conn, err := s.waitReady()
	if err != nil {
		return 0, err
	}
	if s.udp != nil {
		for {
			n, from, err := s.udp.ReadFromUDP(buf)
			if err != nil {
				return 0, err
			}
			if from.IP.Equal(s.remote.IP) && from.Port == s.remote.Port {
				return n, nil
			}
		}
	}
	var header [2]byte
	for {
		if _, err = io.ReadFull(conn, header[:]); err != nil {
			return 0, err
		}
		length := int(binary.BigEndian.Uint16(header[:]))
		if length == 0 {
			continue
		} // RFC 4571 null packet.
		if length > len(buf) {
			return 0, fmt.Errorf("audio TCP frame exceeds buffer")
		}
		return io.ReadFull(conn, buf[:length])
	}
}

func (s *packetSocket) readRTCP(buf []byte) (int, error) {
	if s.rtcpUDP == nil {
		return 0, fmt.Errorf("RTCP UDP socket is unavailable")
	}
	if _, err := s.waitReady(); err != nil {
		return 0, err
	}
	for {
		n, from, err := s.rtcpUDP.ReadFromUDP(buf)
		if err != nil {
			return 0, err
		}
		if from.IP.Equal(s.rtcpRemote.IP) && from.Port == s.rtcpRemote.Port {
			return n, nil
		}
	}
}

func (s *packetSocket) writePacket(packet []byte, rtcpPacket bool) error {
	conn, err := s.waitReady()
	if err != nil {
		return err
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	if s.udp != nil {
		conn, remote := s.udp, s.remote
		if rtcpPacket {
			conn, remote = s.rtcpUDP, s.rtcpRemote
		}
		if err = conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return err
		}
		_, err = conn.WriteToUDP(packet, remote)
		return err
	}
	if len(packet) > 65535 {
		return fmt.Errorf("audio TCP frame exceeds RFC 4571 length")
	}
	if err = conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	frame := make([]byte, 2+len(packet))
	binary.BigEndian.PutUint16(frame, uint16(len(packet)))
	copy(frame[2:], packet)
	for len(frame) > 0 {
		n, writeErr := conn.Write(frame)
		if writeErr != nil {
			return writeErr
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	return nil
}

func (s *packetSocket) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	if s.udp != nil {
		_ = s.udp.Close()
	}
	if s.rtcpUDP != nil {
		_ = s.rtcpUDP.Close()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
}
