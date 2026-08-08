package security

import (
	"net"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

func readProps() sip.TransportReadProps {
	return sip.TransportReadProps{Transport: "udp", RemoteAddr: &net.UDPAddr{IP: net.ParseIP("198.51.100.10"), Port: 5060}}
}

func TestAdmissionDropsBannedBeforeTrace(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	traceCalls := 0
	a := NewAdmission(DefaultPolicyWithMode(ModeProtect), clock, func(_ sip.TransportReadProps, data []byte) ([]byte, error) {
		traceCalls++
		return data, nil
	}, nil)
	require.NoError(t, a.Ban("198.51.100.10", clock.Now().Add(time.Minute)))
	out, err := a.Filter(readProps(), []byte("REGISTER sip:x SIP/2.0\r\n"))
	require.NoError(t, err)
	require.Empty(t, out)
	require.Zero(t, traceCalls)
}

func TestAdmissionAllowsPartialTCPAndDelegatesOriginalBytes(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	var got []byte
	a := NewAdmission(DefaultPolicyWithMode(ModeProtect), clock, func(_ sip.TransportReadProps, data []byte) ([]byte, error) {
		got = append([]byte(nil), data...)
		return data, nil
	}, nil)
	props := readProps()
	props.Transport = "tcp"
	chunk := []byte("REGIS")
	out, err := a.Filter(props, chunk)
	require.NoError(t, err)
	require.Equal(t, chunk, out)
	require.Equal(t, chunk, got)
}

func TestAdmissionRejectsOversizedPacket(t *testing.T) {
	p := DefaultPolicyWithMode(ModeProtect)
	p.MaxPacketBytes = 4
	a := NewAdmission(p, &fakeClock{now: time.Unix(100, 0)}, func(_ sip.TransportReadProps, data []byte) ([]byte, error) { return data, nil }, nil)
	out, err := a.Filter(readProps(), []byte("12345"))
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestAdmissionExpiresDynamicBan(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	a := NewAdmission(DefaultPolicyWithMode(ModeProtect), clock, nil, nil)
	require.NoError(t, a.Ban("198.51.100.10", clock.Now().Add(time.Second)))
	require.True(t, a.IsBanned("198.51.100.10"))
	clock.now = clock.now.Add(2 * time.Second)
	require.False(t, a.IsBanned("198.51.100.10"))
}

func DefaultPolicyWithMode(mode Mode) Policy {
	p := DefaultPolicy()
	p.Mode = mode
	return p
}
