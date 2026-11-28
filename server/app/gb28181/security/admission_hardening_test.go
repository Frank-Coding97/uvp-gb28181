package security

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

func TestAdmissionHardeningTCPFiltersStickyFramesWithoutLeakingDroppedTail(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	props := admissionTCPProps(5060)
	var forwarded []byte
	var events []Event
	a := NewAdmission(DefaultPolicy(), clock, func(_ sip.TransportReadProps, data []byte) ([]byte, error) {
		forwarded = append(forwarded, data...)
		return data, nil
	}, func(event Event) { events = append(events, event) })

	register := admissionSIPFrame("TCP", "REGISTER", "device-register", "register-call", "register-body")
	invite := admissionSIPFrame("TCP", "INVITE", "device-invite", "invite-call", "drop-body")
	message := admissionSIPFrame("TCP", "MESSAGE", "device-message", "message-call", "allow-body")
	headerEnd := bytes.Index(register, []byte("\r\n\r\n")) + 4
	cut := headerEnd + 3
	require.Greater(t, cut, headerEnd)
	require.Less(t, cut, len(register))

	out, err := a.Filter(props, register[:cut])
	require.NoError(t, err)
	require.Empty(t, out)
	require.Empty(t, forwarded)

	sticky := append(append(append([]byte(nil), register[cut:]...), invite...), message...)
	out, err = a.Filter(props, sticky)
	require.NoError(t, err)
	want := append(append([]byte(nil), register...), message...)
	require.Equal(t, want, out)
	require.Equal(t, want, forwarded)
	require.Len(t, events, 1)
	require.Equal(t, ReasonInviteRate, events[0].Reason)
	require.Equal(t, "INVITE", events[0].Method)
}

func TestAdmissionHardeningTCPKeepsCRLFKeepalivePath(t *testing.T) {
	var forwarded []byte
	props := admissionTCPProps(5060)
	a := NewAdmission(DefaultPolicy(), &fakeClock{now: time.Unix(100, 0)}, func(_ sip.TransportReadProps, data []byte) ([]byte, error) {
		forwarded = append(forwarded, data...)
		return data, nil
	}, nil)

	keepalive := []byte("\r\n\r\n")
	out, err := a.Filter(props, keepalive)
	require.NoError(t, err)
	require.Equal(t, keepalive, out)
	require.Equal(t, keepalive, forwarded)
}

func TestAdmissionHardeningGlobalTCPCapacityDropsWithoutRiskEvent(t *testing.T) {
	p := DefaultPolicyWithMode(ModeStrict)
	p.MaxEventKeys = 1
	p.MaxTCPConnections = 1
	var events []Event
	a := NewAdmission(p, &fakeClock{now: time.Unix(100, 0)}, nil, func(event Event) { events = append(events, event) })
	partial := []byte("REGISTER sip:x SIP/2.0\r\n")

	_, err := a.Filter(admissionTCPPropsAt("198.51.100.10", 5060), partial)
	require.NoError(t, err)
	out, err := a.Filter(admissionTCPPropsAt("198.51.100.11", 5061), partial)
	require.NoError(t, err)
	require.Empty(t, out)
	require.EqualValues(t, 1, a.Dropped())
	require.Empty(t, events)
}

func TestAdmissionHardeningStrictTCPSourceCapacityEmitsSourceRisk(t *testing.T) {
	p := DefaultPolicyWithMode(ModeStrict)
	p.MaxEventKeys = 4
	p.MaxTCPConnections = 1
	var events []Event
	a := NewAdmission(p, &fakeClock{now: time.Unix(100, 0)}, nil, func(event Event) { events = append(events, event) })
	partial := []byte("REGISTER sip:x SIP/2.0\r\n")

	_, err := a.Filter(admissionTCPPropsAt("198.51.100.10", 5060), partial)
	require.NoError(t, err)
	out, err := a.Filter(admissionTCPPropsAt("198.51.100.10", 5061), partial)
	require.NoError(t, err)
	require.Empty(t, out)
	require.Len(t, events, 1)
	require.Equal(t, ReasonConnectionRate, events[0].Reason)
	require.Equal(t, ScopeSource, events[0].RiskScope)
}

func TestAdmissionHardeningPolicyKeepsPartialFrameUnlessMaxPacketChanges(t *testing.T) {
	props := admissionTCPProps(5060)
	frame := admissionSIPFrame("TCP", "REGISTER", "device-register", "policy-call", "body")
	a := NewAdmission(DefaultPolicy(), &fakeClock{now: time.Unix(100, 0)}, nil, nil)
	cut := len(frame) - 1

	out, err := a.Filter(props, frame[:cut])
	require.NoError(t, err)
	require.Empty(t, out)
	a.framerMu.Lock()
	before := a.framer
	a.framerMu.Unlock()

	p := DefaultPolicy()
	p.Mode = ModeObserve
	p.NonceTTL = 2 * time.Minute
	a.SetPolicy(p)
	a.framerMu.Lock()
	afterPolicy := a.framer
	a.framerMu.Unlock()
	require.Same(t, before, afterPolicy)

	out, err = a.Filter(props, frame[cut:])
	require.NoError(t, err)
	require.Equal(t, frame, out)

	p.MaxPacketBytes--
	a.SetPolicy(p)
	a.framerMu.Lock()
	afterMaxPacket := a.framer
	streamCount := afterMaxPacket.StreamCount()
	a.framerMu.Unlock()
	require.NotSame(t, afterPolicy, afterMaxPacket)
	require.Zero(t, streamCount)
}

func TestAdmissionHardeningResponsesDoNotUseUnknownMethodRateWindow(t *testing.T) {
	p := DefaultPolicy()
	p.MaxUDPPerWindow = 1
	var events []Event
	a := NewAdmission(p, &fakeClock{now: time.Unix(100, 0)}, nil, func(event Event) { events = append(events, event) })
	response := []byte("SIP/2.0 200 OK\r\nVia: SIP/2.0/UDP 198.51.100.10:5060;branch=z9hG4bK-response\r\nFrom: <sip:device-response@example.com>;tag=from\r\nTo: <sip:platform@example.com>;tag=to\r\nCall-ID: response-call\r\nCSeq: 1 MESSAGE\r\nContent-Length: 0\r\n\r\n")

	for i := 0; i < 130; i++ {
		out, err := a.Filter(readProps(), response)
		require.NoError(t, err)
		require.Equal(t, response, out)
	}
	require.Empty(t, events)
	require.Empty(t, a.windows)
}

func TestAdmissionHardeningUnknownMethodsUseFiniteRateClassification(t *testing.T) {
	p := DefaultPolicy()
	p.MaxUDPPerWindow = 1
	var events []Event
	a := NewAdmission(p, &fakeClock{now: time.Unix(100, 0)}, nil, func(event Event) { events = append(events, event) })

	for i := 0; i < 5000; i++ {
		packet := []byte(fmt.Sprintf("X%04d sip:x@example.com SIP/2.0\r\n", i))
		_, err := a.Filter(readProps(), packet)
		require.NoError(t, err)
	}
	require.Len(t, a.windows, 1)
	require.NotEmpty(t, events)
	for _, event := range events {
		require.Equal(t, "UNKNOWN", event.Method)
		require.Equal(t, ReasonUnknownMethod, event.Reason)
		require.Empty(t, event.DeviceID)
		require.Empty(t, event.TransactionID)
	}
}

func TestAdmissionHardeningBoundsPartialTCPStateAndUnbanResetsIt(t *testing.T) {
	p := DefaultPolicy()
	p.MaxEventKeys = 2
	p.MaxUDPPerWindow = 1
	clock := &fakeClock{now: time.Unix(100, 0)}
	a := NewAdmission(p, clock, nil, nil)

	for port := 5060; port < 5070; port++ {
		_, err := a.Filter(admissionTCPProps(port), []byte("REGISTER sip:x SIP/2.0\r\n"))
		require.NoError(t, err)
	}
	require.LessOrEqual(t, len(a.conns["198.51.100.10"]), p.MaxEventKeys)
	require.LessOrEqual(t, a.framer.StreamCount(), p.MaxEventKeys)

	_, err := a.Filter(readProps(), []byte("X1 sip:x@example.com SIP/2.0\r\n"))
	require.NoError(t, err)
	_, err = a.Filter(readProps(), []byte("X2 sip:x@example.com SIP/2.0\r\n"))
	require.NoError(t, err)
	require.NotEmpty(t, a.windows)

	a.ConnectionClosed(admissionTCPProps(5060))
	require.LessOrEqual(t, a.framer.StreamCount(), p.MaxEventKeys-1)
	a.Unban("198.51.100.10")
	require.Empty(t, a.windows)
	require.Empty(t, a.conns)
	require.Zero(t, a.framer.StreamCount())

	out, err := a.Filter(readProps(), []byte("X3 sip:x@example.com SIP/2.0\r\n"))
	require.NoError(t, err)
	require.NotEmpty(t, out)
}

func TestAdmissionHardeningTrustedSourceUsesFromDeviceAndTransport(t *testing.T) {
	props := readProps()
	props.Transport = "udp"
	var gotSource, gotTransport, gotDevice string
	traceCalls := 0
	var events []Event
	a := NewAdmission(DefaultPolicy(), &fakeClock{now: time.Unix(100, 0)}, func(_ sip.TransportReadProps, data []byte) ([]byte, error) {
		traceCalls++
		return data, nil
	}, func(event Event) { events = append(events, event) })
	a.SetTrustedSource(func(sourceIP, transport, deviceID string) bool {
		gotSource, gotTransport, gotDevice = sourceIP, transport, deviceID
		return sourceIP == "198.51.100.10" && transport == "UDP" && deviceID == "device-trusted"
	})

	packet := admissionSIPFrame("UDP", "INVITE", "device-trusted", "trusted-call", "body")
	out, err := a.Filter(props, packet)
	require.NoError(t, err)
	require.Equal(t, packet, out)
	require.Equal(t, 1, traceCalls)
	require.Empty(t, events)
	require.Equal(t, "198.51.100.10", gotSource)
	require.Equal(t, "UDP", gotTransport)
	require.Equal(t, "device-trusted", gotDevice)
}

func TestAdmissionHardeningInviteEventIdentityIsStableAndSafe(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	var events []Event
	a := NewAdmission(DefaultPolicy(), clock, nil, func(event Event) { events = append(events, event) })
	packet := admissionSIPFrame("UDP", "INVITE", "device-invite", "stable-call", "body")
	for i := 0; i < 2; i++ {
		out, err := a.Filter(readProps(), packet)
		require.NoError(t, err)
		require.Empty(t, out)
	}
	require.Len(t, events, 2)
	require.Equal(t, "device-invite", events[0].DeviceID)
	require.Equal(t, ScopeSource, events[0].RiskScope)
	require.NotEmpty(t, events[0].TransactionID)
	require.Len(t, events[0].TransactionID, 64)
	require.Equal(t, events[0].TransactionID, events[1].TransactionID)

	unknownPolicy := DefaultPolicy()
	unknownPolicy.MaxUDPPerWindow = 1
	var unknownEvents []Event
	unknownAdmission := NewAdmission(unknownPolicy, clock, nil, func(event Event) { unknownEvents = append(unknownEvents, event) })
	unknown := admissionSIPFrame("UDP", "X-ATTACK", "198.51.100.10", "unknown-call", "body")
	_, err := unknownAdmission.Filter(readProps(), unknown)
	require.NoError(t, err)
	_, err = unknownAdmission.Filter(readProps(), unknown)
	require.NoError(t, err)
	require.NotEmpty(t, unknownEvents)
	require.Empty(t, unknownEvents[len(unknownEvents)-1].DeviceID)
	require.Empty(t, unknownEvents[len(unknownEvents)-1].TransactionID)
	require.Equal(t, ScopeSource, unknownEvents[len(unknownEvents)-1].RiskScope)

	overlargePolicy := DefaultPolicy()
	overlargePolicy.MaxPacketBytes = 32
	var overlargeEvents []Event
	overlargeAdmission := NewAdmission(overlargePolicy, clock, nil, func(event Event) { overlargeEvents = append(overlargeEvents, event) })
	_, err = overlargeAdmission.Filter(readProps(), packet)
	require.NoError(t, err)
	require.Len(t, overlargeEvents, 1)
	require.Equal(t, ReasonPacketTooLarge, overlargeEvents[0].Reason)
	require.Empty(t, overlargeEvents[0].DeviceID)
	require.Empty(t, overlargeEvents[0].TransactionID)
	require.Equal(t, ScopeSource, overlargeEvents[0].RiskScope)
}

func TestAdmissionHardeningDeviceAndUserAgentMetadataStayBounded(t *testing.T) {
	longDevice := strings.Repeat("d", 65)
	var inviteEvents []Event
	a := NewAdmission(DefaultPolicy(), &fakeClock{now: time.Unix(100, 0)}, nil, func(event Event) { inviteEvents = append(inviteEvents, event) })
	_, err := a.Filter(readProps(), admissionSIPFrame("UDP", "INVITE", longDevice, "long-device-call", "body"))
	require.NoError(t, err)
	require.Len(t, inviteEvents, 1)
	require.Empty(t, inviteEvents[0].DeviceID)
	require.Equal(t, ScopeSource, inviteEvents[0].RiskScope)

	longUserAgent := "scanner-" + strings.Repeat("x", 300)
	var userAgentEvents []Event
	uaAdmission := NewAdmission(DefaultPolicy(), &fakeClock{now: time.Unix(100, 0)}, nil, func(event Event) { userAgentEvents = append(userAgentEvents, event) })
	uaAdmission.SetAccessRules([]AccessRule{{ListType: ListBlacklist, MatchType: MatchUserAgent, MatchValue: "scanner*", Status: RuleEnabled}})
	packet := []byte("REGISTER sip:x SIP/2.0\r\nUser-Agent: " + longUserAgent + "\r\nContent-Length: 0\r\n\r\n")
	_, err = uaAdmission.Filter(readProps(), packet)
	require.NoError(t, err)
	require.Len(t, userAgentEvents, 1)
	require.Equal(t, longUserAgent[:255], userAgentEvents[0].UserAgent)
	require.Equal(t, ScopeSource, userAgentEvents[0].RiskScope)
}

func admissionTCPProps(port int) sip.TransportReadProps {
	return admissionTCPPropsAt("198.51.100.10", port)
}

func admissionTCPPropsAt(source string, port int) sip.TransportReadProps {
	return sip.TransportReadProps{
		Transport:  "tcp",
		RemoteAddr: &net.TCPAddr{IP: net.ParseIP(source), Port: port},
	}
}

func admissionSIPFrame(transport, method, deviceID, callID, body string) []byte {
	return []byte(fmt.Sprintf("%s sip:platform@example.com SIP/2.0\r\n"+
		"Via: SIP/2.0/%s 198.51.100.10:5060;branch=z9hG4bK-%s\r\n"+
		"From: <sip:%s@example.com>;tag=from-%s\r\n"+
		"To: <sip:platform@example.com>;tag=to-%s\r\n"+
		"Call-ID: %s\r\n"+
		"CSeq: 1 %s\r\n"+
		"Content-Length: %d\r\n\r\n%s", method, transport, callID, deviceID, callID, callID, callID, method, len(body), body))
}
