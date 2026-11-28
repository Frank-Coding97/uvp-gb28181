package sip

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type countedUDPPacketConn struct {
	net.PacketConn
	closes atomic.Int32
	err    error
}

func (c *countedUDPPacketConn) Close() error {
	c.closes.Add(1)
	return errors.Join(c.PacketConn.Close(), c.err)
}

func udpCloseFixture(t *testing.T, refs int, listener bool) (*UDPConnection, *countedUDPPacketConn) {
	t.Helper()
	socket, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = socket.Close() })
	counted := &countedUDPPacketConn{PacketConn: socket}
	return &UDPConnection{PacketConn: counted, PacketAddr: socket.LocalAddr().String(), Listener: listener, refcount: refs}, counted
}

func TestUDPForceClosePreservesOutstandingReferences(t *testing.T) {
	// Caller + reader are independent owners. Hard shutdown closes the socket,
	// not those owners; each still releases exactly once afterwards.
	conn, socket := udpCloseFixture(t, 2, false)
	pool := newConnectionPool()
	pool.Add(conn.PacketAddr, conn)
	pool.Add("127.0.0.1:5099", conn) // pool aliases do not own extra refs
	require.NoError(t, pool.Clear())
	require.Equal(t, 2, conn.Ref(0), "hard close erased outstanding owner references")
	require.Equal(t, int32(1), socket.closes.Load())
	ref, err := conn.TryClose()
	require.NoError(t, err)
	require.Equal(t, 1, ref)
	require.NoError(t, pool.CloseAndDelete(conn, conn.PacketAddr))
	require.Zero(t, conn.Ref(0))
	require.Equal(t, int32(1), socket.closes.Load(), "reader cleanup must not close the socket again")
	_, err = socket.WriteTo([]byte("closed"), socket.LocalAddr())
	require.ErrorIs(t, err, net.ErrClosed)
}

func TestUDPForceCloseConcurrentOwnerRelease(t *testing.T) {
	const owners = 32
	conn, socket := udpCloseFixture(t, owners, false)
	var wg sync.WaitGroup
	start := make(chan struct{})
	errorsSeen := make(chan error, owners*2)
	for i := 0; i < owners; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); <-start; errorsSeen <- conn.Close() }()
		go func() { defer wg.Done(); <-start; _, err := conn.TryClose(); errorsSeen <- err }()
	}
	close(start)
	wg.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		require.NoError(t, err)
	}
	require.Zero(t, conn.Ref(0))
	require.Equal(t, int32(1), socket.closes.Load())
}

func TestUDPForceCloseRetainsError(t *testing.T) {
	conn, socket := udpCloseFixture(t, 1, false)
	failure := errors.New("injected close result unknown")
	socket.err = failure
	require.ErrorIs(t, conn.Close(), failure)
	ref, err := conn.TryClose()
	require.Zero(t, ref)
	require.ErrorIs(t, err, failure, "last owner cannot turn failed hard close into success")
	require.ErrorIs(t, conn.Close(), failure)
	require.Equal(t, int32(1), socket.closes.Load(), "unknown close is not blindly retried")
}

func TestUDPListenerSocketRemainsCallerOwned(t *testing.T) {
	conn, socket := udpCloseFixture(t, 2, true)
	require.NoError(t, conn.Close())
	require.Equal(t, 2, conn.Ref(0))
	for expected := 1; expected >= 0; expected-- {
		ref, err := conn.TryClose()
		require.NoError(t, err)
		require.Equal(t, expected, ref)
	}
	require.Zero(t, socket.closes.Load())
	require.NoError(t, socket.SetDeadline(time.Now().Add(time.Second)))
	_, err := socket.WriteTo([]byte("still owned"), socket.LocalAddr())
	require.NoError(t, err)
	buf := make([]byte, 64)
	n, _, err := socket.ReadFrom(buf)
	require.NoError(t, err)
	require.Equal(t, "still owned", string(buf[:n]))
	require.NoError(t, socket.Close())
}

func TestUDPExcessReleaseRemainsDetectable(t *testing.T) {
	conn, socket := udpCloseFixture(t, 1, false)
	_, err := conn.TryClose()
	require.NoError(t, err)
	ref, err := conn.TryClose()
	require.NoError(t, err)
	require.Equal(t, -1, ref, "report the actual invalid reference count")
	require.Equal(t, -1, conn.Ref(0), "do not hide a genuine duplicate owner release")
	require.Equal(t, int32(1), socket.closes.Load())
}

func TestUDPForceCloseJoinsActualReaderCleanup(t *testing.T) {
	// Match the default transport policy without changing a process-global
	// setting: caller + reader + idle retention. Idle is not a live owner.
	conn, socket := udpCloseFixture(t, 3, false)
	transport := &TransportUDP{}
	transport.init(NewParser())
	remote := "127.0.0.1:5099"
	transport.pool.Add(conn.PacketAddr, conn)
	transport.pool.Add(remote, conn)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		transport.readUDPConnection(conn, remote, conn.PacketAddr, func(Message) {})
	}()
	ref, err := conn.TryClose() // transaction owner exits first
	require.NoError(t, err)
	require.Equal(t, 2, ref)
	require.NoError(t, transport.Close())
	select {
	case <-readerDone: // wait past all reader defers, not merely ReadFrom error
	case <-time.After(3 * time.Second):
		t.Fatal("hard close did not finish UDP reader cleanup")
	}
	require.Equal(t, 1, conn.Ref(0), "only the default idle retention remains")
	require.Zero(t, transport.pool.Size())
	require.Equal(t, int32(1), socket.closes.Load())
	_, _, err = socket.ReadFrom(make([]byte, 1))
	require.ErrorIs(t, err, net.ErrClosed)
	// Demonstrate local port release. This is not a remote SIP dialog receipt.
	rebound, err := net.ListenPacket("udp4", conn.PacketAddr)
	require.NoError(t, err)
	require.NoError(t, rebound.Close())
}
