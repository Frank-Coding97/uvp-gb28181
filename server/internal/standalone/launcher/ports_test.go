package launcher

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

func TestPreflightChecksMediaTCPAndUDPAndReleasesBindings(t *testing.T) {
	blocked, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer blocked.Close()
	first, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	firstAddress := first.Addr().String()
	require.NoError(t, first.Close())

	err = checkPorts([]string{}, []standalone.MediaListener{
		{Network: "tcp", Address: firstAddress},
		{Network: "udp", Address: firstAddress},
		{Network: "tcp", Address: blocked.Addr().String()},
	})
	require.Error(t, err)

	listener, err := net.Listen("tcp4", firstAddress)
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	packet, err := net.ListenPacket("udp4", firstAddress)
	require.NoError(t, err)
	require.NoError(t, packet.Close())
}

func TestPreflightReleasesSuccessfulMediaBindings(t *testing.T) {
	reserved, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	address := reserved.Addr().String()
	require.NoError(t, reserved.Close())

	require.NoError(t, checkPorts(nil, []standalone.MediaListener{
		{Network: "tcp", Address: address},
		{Network: "udp", Address: address},
	}))

	listener, err := net.Listen("tcp4", address)
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	packet, err := net.ListenPacket("udp4", address)
	require.NoError(t, err)
	require.NoError(t, packet.Close())
}

func TestPreflightRejectsOccupiedMediaUDPPort(t *testing.T) {
	occupied, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer occupied.Close()
	err = checkPorts(nil, []standalone.MediaListener{{Network: "udp", Address: occupied.LocalAddr().String()}})
	require.Error(t, err)
}

func TestPreflightRejectsOccupiedIPv4Wildcard(t *testing.T) {
	for _, network := range []string{"tcp", "udp"} {
		t.Run(network, func(t *testing.T) {
			var address string
			if network == "tcp" {
				listener, err := net.Listen("tcp4", "0.0.0.0:0")
				require.NoError(t, err)
				defer listener.Close()
				address = listener.Addr().String()
			} else {
				listener, err := net.ListenPacket("udp4", "0.0.0.0:0")
				require.NoError(t, err)
				defer listener.Close()
				address = listener.LocalAddr().String()
			}
			err := checkPorts(nil, []standalone.MediaListener{{Network: network, Address: address}})
			require.Error(t, err)
			require.Contains(t, err.Error(), address)
		})

	}
}

func TestPreflightRejectsDuplicateProtocolBinding(t *testing.T) {
	reserved, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	address := reserved.Addr().String()
	require.NoError(t, reserved.Close())

	err = checkPorts(nil, []standalone.MediaListener{
		{Network: "tcp", Address: address},
		{Network: "tcp", Address: address},
	})
	require.Error(t, err)

	listener, err := net.Listen("tcp4", address)
	require.NoError(t, err)
	require.NoError(t, listener.Close())
}
