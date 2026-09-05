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

func TestAdmissionBuffersPartialTCPAndDelegatesCompleteFrame(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	var got []byte
	a := NewAdmission(DefaultPolicyWithMode(ModeProtect), clock, func(_ sip.TransportReadProps, data []byte) ([]byte, error) {
		got = append([]byte(nil), data...)
		return data, nil
	}, nil)
	props := readProps()
	props.Transport = "tcp"
	frame := []byte("REGISTER sip:x SIP/2.0\r\nContent-Length: 0\r\n\r\n")
	out, err := a.Filter(props, frame[:5])
	require.NoError(t, err)
	require.Empty(t, out)
	require.Empty(t, got)

	out, err = a.Filter(props, frame[5:])
	require.NoError(t, err)
	require.Equal(t, frame, out)
	require.Equal(t, frame, got)
}

func TestAdmissionRejectsOversizedPacket(t *testing.T) {
	p := DefaultPolicyWithMode(ModeProtect)
	p.MaxPacketBytes = 4
	a := NewAdmission(p, &fakeClock{now: time.Unix(100, 0)}, func(_ sip.TransportReadProps, data []byte) ([]byte, error) { return data, nil }, nil)
	out, err := a.Filter(readProps(), []byte("12345"))
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestAdmissionObserveSamplesScannerTrafficWithoutDropping(t *testing.T) {
	p := DefaultPolicyWithMode(ModeObserve)
	var events []Event
	traceCalls := 0
	a := NewAdmission(p, &fakeClock{now: time.Unix(100, 0)}, func(_ sip.TransportReadProps, data []byte) ([]byte, error) {
		traceCalls++
		return data, nil
	}, func(event Event) { events = append(events, event) })

	packet := []byte("INVITE sip:x SIP/2.0\r\n")
	for i := 0; i < 2; i++ {
		out, err := a.Filter(readProps(), packet)
		require.NoError(t, err)
		require.Equal(t, packet, out)
	}
	require.Equal(t, 2, traceCalls)
	require.Equal(t, int64(2), a.Sampled())
	require.Len(t, events, 2)
	require.Equal(t, ActionSample, events[0].Action)
	require.Equal(t, "INVITE", events[0].Method)
}

func TestAdmissionProtectDropsFirstUnknownInviteBeforeTrace(t *testing.T) {
	var events []Event
	traceCalls := 0
	a := NewAdmission(DefaultPolicy(), &fakeClock{now: time.Unix(100, 0)}, func(_ sip.TransportReadProps, data []byte) ([]byte, error) {
		traceCalls++
		return data, nil
	}, func(event Event) { events = append(events, event) })

	out, err := a.Filter(readProps(), []byte("INVITE sip:x SIP/2.0\r\nUser-Agent: friendly-scanner\r\n\r\n"))
	require.NoError(t, err)
	require.Empty(t, out)
	require.Zero(t, traceCalls)
	require.Len(t, events, 1)
	require.Equal(t, "INVITE", events[0].Method)
	require.Equal(t, ReasonInviteRate, events[0].Reason)
	require.Equal(t, ActionDrop, events[0].Action)
}

func TestAdmissionProtectAllowsAllowlistedInvite(t *testing.T) {
	p := DefaultPolicy()
	_, network, err := net.ParseCIDR("198.51.100.0/24")
	require.NoError(t, err)
	p.Allowlist = []net.IPNet{*network}
	traceCalls := 0
	a := NewAdmission(p, &fakeClock{now: time.Unix(100, 0)}, func(_ sip.TransportReadProps, data []byte) ([]byte, error) {
		traceCalls++
		return data, nil
	}, nil)
	packet := []byte("INVITE sip:x SIP/2.0\r\n")

	out, err := a.Filter(readProps(), packet)
	require.NoError(t, err)
	require.Equal(t, packet, out)
	require.Equal(t, 1, traceCalls)
}

func TestAdmissionDropsManualUserAgentBlacklistBeforeTrace(t *testing.T) {
	var events []Event
	traceCalls := 0
	a := NewAdmission(DefaultPolicy(), &fakeClock{now: time.Unix(100, 0)}, func(_ sip.TransportReadProps, data []byte) ([]byte, error) {
		traceCalls++
		return data, nil
	}, func(event Event) { events = append(events, event) })
	a.SetAccessRules([]AccessRule{{ID: 9, ListType: ListBlacklist, MatchType: MatchUserAgent, MatchValue: "friendly-scanner*", Status: RuleEnabled}})

	out, err := a.Filter(readProps(), []byte("REGISTER sip:x SIP/2.0\r\nUser-Agent: Friendly-Scanner/1.0\r\n\r\n"))
	require.NoError(t, err)
	require.Empty(t, out)
	require.Zero(t, traceCalls)
	require.Len(t, events, 1)
	require.Equal(t, ReasonManualBlacklist, events[0].Reason)
	require.Equal(t, "Friendly-Scanner/1.0", events[0].UserAgent)
}

func TestAdmissionDoesNotRateLimitNormalRegisterTraffic(t *testing.T) {
	p := DefaultPolicyWithMode(ModeProtect)
	p.MaxUDPPerWindow = 1
	a := NewAdmission(p, &fakeClock{now: time.Unix(100, 0)}, nil, nil)
	packet := []byte("REGISTER sip:x SIP/2.0\r\n")

	for i := 0; i < 3; i++ {
		out, err := a.Filter(readProps(), packet)
		require.NoError(t, err)
		require.Equal(t, packet, out)
	}
}

func TestAdmissionCountsTCPConnectionsByRemoteEndpointNotRead(t *testing.T) {
	p := DefaultPolicyWithMode(ModeStrict)
	p.MaxTCPConnections = 1
	a := NewAdmission(p, &fakeClock{now: time.Unix(100, 0)}, nil, nil)
	props := readProps()
	props.Transport = "tcp"
	props.RemoteAddr = &net.TCPAddr{IP: net.ParseIP("198.51.100.10"), Port: 5060}
	packet := []byte("REGISTER sip:x SIP/2.0\r\nContent-Length: 0\r\n\r\n")

	for i := 0; i < 3; i++ {
		out, err := a.Filter(props, packet)
		require.NoError(t, err)
		require.NotEmpty(t, out)
	}

	second := props
	second.RemoteAddr = &net.TCPAddr{IP: net.ParseIP("198.51.100.10"), Port: 5061}
	out, err := a.Filter(second, packet)
	require.NoError(t, err)
	require.Empty(t, out)

	a.ConnectionClosed(props)
	out, err = a.Filter(second, packet)
	require.NoError(t, err)
	require.NotEmpty(t, out)
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
