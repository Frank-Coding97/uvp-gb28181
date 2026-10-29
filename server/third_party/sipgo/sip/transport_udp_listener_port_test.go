package sip

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTransportUDPGetListenerConnectionMatchesServedSocketByPort pins the UVP
// fork behaviour that lets a client originating from a pinned local port reuse
// the shared serving socket (see UVP_PATCH.md, "Originate from the pinned local
// port"). Served sockets are pooled under their real local address, so a client
// that only knows the port cannot reach them through GetConnection.
func TestTransportUDPGetListenerConnectionMatchesServedSocketByPort(t *testing.T) {
	transport := &TransportUDP{}
	transport.init(NewParser())

	listener := &UDPConnection{PacketAddr: "[::]:5060", Listener: true}
	dialed := &UDPConnection{PacketAddr: "192.0.2.20:40000"}
	transport.pool.Add(listener.PacketAddr, listener)
	transport.pool.Add(dialed.PacketAddr, dialed)

	require.Same(t, listener, transport.GetListenerConnection(5060))
	require.Nil(t, transport.GetListenerConnection(5061))
	require.Nil(t, transport.GetListenerConnection(0))

	// 参考计数：GetListenerConnection 加一，TryClose 减一，始终不会关掉服务 socket。
	before := listener.Ref(0)
	got := transport.GetListenerConnection(5060)
	require.Equal(t, before+1, got.Ref(0))
	_, err := got.TryClose()
	require.NoError(t, err)
	require.Equal(t, before, listener.Ref(0))
	require.False(t, listener.PacketConn != nil)
}
