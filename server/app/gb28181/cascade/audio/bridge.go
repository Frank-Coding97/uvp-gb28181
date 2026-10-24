// Package audio bridges the PCMA RTP legs of a cascade audio dialog. SIP dialog
// ownership and authorization remain with the caller; Activate is its ACK gate.
package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"uvplatform.com/uvp-gb28181/app/gb28181/sdp"
)

type Bridge struct {
	ctx              context.Context
	cancel           context.CancelFunc
	source, receiver *packetSocket
	receiverOffer    sdp.CascadeAudioOffer
	mu               sync.Mutex
	configured       bool
	err              error
	active           atomic.Bool
	lastRTPUnixNano  atomic.Int64
	workers          sync.WaitGroup
	done             chan struct{}
}

// NewBridge reserves the receiving and sending endpoints before either SIP
// answer is sent. Source SDP becomes available later through SetSource.
func NewBridge(ctx context.Context, bindIP, sourceTransport, sourceSetup string, receiver sdp.CascadeAudioOffer) (*Bridge, error) {
	if _, err := audioSSRC(receiver.SSRC); err != nil {
		return nil, err
	}
	if !validDirection(receiver.Direction) {
		return nil, fmt.Errorf("invalid receiver audio direction")
	}
	ctx, cancel := context.WithCancel(ctx)
	source, err := openPacketSocket(ctx, bindIP, sourceTransport, sourceSetup)
	if err != nil {
		cancel()
		return nil, err
	}
	output, err := openPacketSocket(ctx, bindIP, receiver.Transport, receiver.Setup)
	if err != nil {
		cancel()
		source.stop()
		return nil, err
	}
	if err = output.setPeer(receiver); err != nil {
		cancel()
		source.stop()
		output.stop()
		return nil, err
	}
	b := &Bridge{ctx: ctx, cancel: cancel, source: source, receiver: output, receiverOffer: receiver, done: make(chan struct{})}
	go func() {
		<-ctx.Done()
		// Serialize with SetSource's worker registration before waiting.
		b.mu.Lock()
		source.stop()
		output.stop()
		b.mu.Unlock()
		source.wg.Wait()
		output.wg.Wait()
		b.workers.Wait()
		close(b.done)
	}()
	return b, nil
}

func (b *Bridge) SourcePort() int       { return b.source.port }
func (b *Bridge) SourceRTCPPort() int   { return b.source.rtcpPort }
func (b *Bridge) ReceiverPort() int     { return b.receiver.port }
func (b *Bridge) ReceiverRTCPPort() int { return b.receiver.rtcpPort }
func (b *Bridge) Done() <-chan struct{} { return b.done }
func (b *Bridge) Close()                { b.cancel(); <-b.done }
func (b *Bridge) Err() error            { b.mu.Lock(); defer b.mu.Unlock(); return b.err }
func (b *Bridge) LastRTPAt() time.Time {
	if last := b.lastRTPUnixNano.Load(); last != 0 {
		return time.Unix(0, last)
	}
	return time.Time{}
}

func audioSSRC(value string) (uint32, error) {
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil || n == 0 || len(value) != 10 || value[0] != '0' {
		return 0, fmt.Errorf("invalid negotiated audio SSRC")
	}
	return uint32(n), nil
}
func validDirection(direction string) bool {
	return direction == "sendrecv" || direction == "sendonly" || direction == "recvonly"
}
func sends(direction string) bool    { return direction == "sendrecv" || direction == "sendonly" }
func receives(direction string) bool { return direction == "sendrecv" || direction == "recvonly" }

func (b *Bridge) SetSource(offer sdp.CascadeAudioOffer) error {
	sourceSSRC, err := audioSSRC(offer.SSRC)
	if err != nil {
		return err
	}
	receiverSSRC, _ := audioSSRC(b.receiverOffer.SSRC)
	if !validDirection(offer.Direction) {
		return fmt.Errorf("invalid source audio direction")
	}
	forward := sends(offer.Direction) && receives(b.receiverOffer.Direction)
	reverse := sends(b.receiverOffer.Direction) && receives(offer.Direction)
	if !forward && !reverse {
		return fmt.Errorf("incompatible audio media directions")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ctx.Err() != nil {
		return b.ctx.Err()
	}
	if b.configured {
		return fmt.Errorf("source audio already negotiated")
	}
	if err = b.source.setPeer(offer); err != nil {
		return err
	}
	b.configured = true
	b.startForwarders(b.source, b.receiver, sourceSSRC, receiverSSRC, forward)
	b.startForwarders(b.receiver, b.source, receiverSSRC, sourceSSRC, reverse)
	return nil
}

func (b *Bridge) startForwarders(from, to *packetSocket, fromSSRC, toSSRC uint32, allowed bool) {
	if from.udp == nil {
		b.workers.Add(1)
		go b.copyTCPPackets(from, to, fromSSRC, toSSRC, allowed)
		return
	}
	b.workers.Add(2)
	go b.copyRTP(from, to, fromSSRC, toSSRC, allowed)
	go b.copyRTCP(from, to, fromSSRC, toSSRC, allowed)
}

func (b *Bridge) Activate() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ctx.Err() != nil {
		return b.ctx.Err()
	}
	if !b.configured {
		return fmt.Errorf("audio source not negotiated")
	}
	b.lastRTPUnixNano.Store(time.Now().UnixNano())
	b.active.Store(true)
	return nil
}

func (b *Bridge) copyRTP(from, to *packetSocket, expectedSSRC, targetSSRC uint32, allowed bool) {
	defer b.workers.Done()
	packet := make([]byte, 65535)
	for {
		n, err := from.readPacket(packet)
		if err != nil {
			b.fail(err)
			return
		}
		if !allowed || !b.active.Load() || !validRTP(packet[:n], expectedSSRC) {
			continue
		}
		b.lastRTPUnixNano.Store(time.Now().UnixNano())
		binary.BigEndian.PutUint32(packet[8:12], targetSSRC)
		if err = to.writePacket(packet[:n], false); err != nil {
			b.fail(err)
			return
		}
	}
}

func (b *Bridge) copyRTCP(from, to *packetSocket, fromSSRC, toSSRC uint32, allowed bool) {
	defer b.workers.Done()
	packet := make([]byte, 65535)
	for {
		n, err := from.readRTCP(packet)
		if err != nil {
			b.fail(err)
			return
		}
		if !allowed || !b.active.Load() {
			continue
		}
		translated, err := rewriteRTCP(packet[:n], fromSSRC, toSSRC)
		if err != nil {
			continue
		}
		if err = to.writePacket(translated, true); err != nil {
			b.fail(err)
			return
		}
	}
}

func (b *Bridge) copyTCPPackets(from, to *packetSocket, fromSSRC, toSSRC uint32, allowed bool) {
	defer b.workers.Done()
	packet := make([]byte, 65535)
	for {
		n, err := from.readPacket(packet)
		if err != nil {
			b.fail(err)
			return
		}
		if !allowed || !b.active.Load() {
			continue
		}
		if isRTCPPacket(packet[:n]) {
			translated, rewriteErr := rewriteRTCP(packet[:n], fromSSRC, toSSRC)
			if rewriteErr != nil {
				continue
			}
			if err = to.writePacket(translated, true); err != nil {
				b.fail(err)
				return
			}
			continue
		}
		if !validRTP(packet[:n], fromSSRC) {
			continue
		}
		binary.BigEndian.PutUint32(packet[8:12], toSSRC)
		if err = to.writePacket(packet[:n], false); err != nil {
			b.fail(err)
			return
		}
	}
}

func isRTCPPacket(packet []byte) bool {
	return len(packet) >= 4 && packet[0]>>6 == 2 && packet[1] >= 192 && packet[1] <= 223
}

func (b *Bridge) fail(err error) {
	b.mu.Lock()
	if b.ctx.Err() == nil && b.err == nil {
		b.err = err
	}
	b.cancel()
	b.mu.Unlock()
}

// validRTP validates the full header before touching SSRC or forwarding payload.
func validRTP(packet []byte, ssrc uint32) bool {
	if len(packet) < 12 || packet[0]>>6 != 2 || packet[1]&0x7f != 8 || binary.BigEndian.Uint32(packet[8:12]) != ssrc {
		return false
	}
	header := 12 + 4*int(packet[0]&15)
	if header > len(packet) {
		return false
	}
	if packet[0]&0x10 != 0 {
		if header+4 > len(packet) {
			return false
		}
		header += 4 + 4*int(binary.BigEndian.Uint16(packet[header+2:header+4]))
		if header > len(packet) {
			return false
		}
	}
	end := len(packet)
	if packet[0]&0x20 != 0 {
		padding := int(packet[end-1])
		if padding == 0 || padding > end-header {
			return false
		}
		end -= padding
	}
	return end > header
}
