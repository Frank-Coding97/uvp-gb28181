package security

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScorerAccumulatesAndBansOnlyAfterThreshold(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	p := DefaultPolicyWithMode(ModeProtect)
	p.BanScore = 30
	p.BanTTLs = []TTLStep{{Score: 30, TTL: time.Minute}}
	agent := &fakeAgent{}
	s := NewScorer(p, clock, agent, []byte("test-secret"))
	_, decision, err := s.Observe(Event{Transport: "TCP", SourceIP: "198.51.100.10", Reason: ReasonDigestFailure})
	require.NoError(t, err)
	require.Nil(t, decision)
	_, decision, err = s.Observe(Event{Transport: "TCP", SourceIP: "198.51.100.10", Reason: ReasonServerMismatch})
	require.NoError(t, err)
	require.NotNil(t, decision)
	require.Equal(t, 25+10, decision.Score)
	require.Empty(t, agent.banCalls)
}

func TestScorerObserveModeNeverCallsAgent(t *testing.T) {
	agent := &fakeAgent{}
	p := DefaultPolicy()
	p.BanScore = 1
	s := NewScorer(p, &fakeClock{now: time.Unix(100, 0)}, agent, []byte("secret"))
	_, decision, err := s.Observe(Event{Transport: "TCP", SourceIP: "198.51.100.10", Reason: ReasonNonceReplay})
	require.NoError(t, err)
	require.Nil(t, decision)
	require.Empty(t, agent.banCalls)
}

func TestProtectPolicyProducesFiniteAutoBan(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	p := DefaultPolicyWithMode(ModeProtect)
	p.BanScore = 40
	p.BanTTLs = []TTLStep{{Score: 40, TTL: time.Minute}}
	s := NewScorer(p, clock, nil, []byte("secret"))
	_, decision, err := s.Observe(Event{Transport: "TCP", SourceIP: "198.51.100.11", Method: "INVITE", Reason: ReasonInviteRate})
	require.NoError(t, err)
	// A single INVITE is below the default repeated-risk threshold.
	require.Nil(t, decision)
	_, decision, err = s.Observe(Event{Transport: "TCP", SourceIP: "198.51.100.11", Method: "INVITE", Reason: ReasonInviteRate})
	require.NoError(t, err)
	require.NotNil(t, decision)
	require.False(t, decision.Permanent)
	require.Equal(t, clock.Now().Add(time.Minute), decision.ExpiresAt())
}

func TestTrustedEndpointUpdatesOnlyWithValidAddress(t *testing.T) {
	s := NewScorer(DefaultPolicy(), &fakeClock{now: time.Unix(100, 0)}, nil, []byte("secret"))
	require.Error(t, s.UpdateTrustedEndpoint("34020000001320000001", "udp", "bad", time.Time{}))
	require.NoError(t, s.UpdateTrustedEndpoint("34020000001320000001", "udp", "198.51.100.12", time.Time{}))
	e, ok := s.TrustedEndpoint("34020000001320000001")
	require.True(t, ok)
	require.Equal(t, "198.51.100.12", e.Address)
}

func TestTrustedEndpointProtectsSharedSourceFromAutomaticBan(t *testing.T) {
	p := DefaultPolicyWithMode(ModeProtect)
	p.BanScore = 30
	p.BanTTLs = []TTLStep{{Score: 30, TTL: time.Minute}}
	s := NewScorer(p, &fakeClock{now: time.Unix(100, 0)}, nil, []byte("secret"))

	_, decision, err := s.Observe(Event{Transport: "TCP", SourceIP: "198.51.100.12", Reason: ReasonDigestFailure})
	require.NoError(t, err)
	require.Nil(t, decision)
	require.NoError(t, s.UpdateTrustedEndpoint("34020000001320000001", "udp", "198.51.100.12", time.Time{}))
	_, decision, err = s.Observe(Event{Transport: "TCP", SourceIP: "198.51.100.12", Reason: ReasonServerMismatch})
	require.NoError(t, err)
	require.Nil(t, decision, "authenticated shared egress must not be automatically IP-banned")
}

func TestNonceIsSignedExpiringAndSingleUse(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	m := NewNonceManager([]byte("secret"), time.Minute, clock)
	nonce, err := m.Issue()
	require.NoError(t, err)
	tampered := []byte(nonce)
	if tampered[len(tampered)-1] == 'A' {
		tampered[len(tampered)-1] = 'B'
	} else {
		tampered[len(tampered)-1] = 'A'
	}
	require.ErrorIs(t, m.Validate(string(tampered), "00000001"), ErrNonceInvalid)
	require.NoError(t, m.Validate(nonce, "00000001"))
	require.ErrorIs(t, m.Validate(nonce, "00000001"), ErrNonceReplay)
	clock.now = clock.now.Add(2 * time.Minute)
	nonce2, err := m.Issue()
	require.NoError(t, err)
	clock.now = clock.now.Add(2 * time.Minute)
	require.ErrorIs(t, m.Validate(nonce2, "00000001"), ErrNonceExpired)
}

func TestNonceFitsLegacyDeviceBuffer(t *testing.T) {
	m := NewNonceManager([]byte("secret"), time.Minute, &fakeClock{now: time.Unix(100, 0)})

	nonce, err := m.Issue()

	require.NoError(t, err)
	require.LessOrEqual(t, len(nonce), 64)
	require.NoError(t, m.Validate(nonce, "00000001"))
}

func TestNonceReplayStateExpiresWithNonceTTL(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	m := NewNonceManager([]byte("secret"), time.Minute, clock)
	nonce, err := m.Issue()
	require.NoError(t, err)
	require.NoError(t, m.Validate(nonce, "00000001"))
	require.Len(t, m.used, 1)

	clock.now = clock.now.Add(2 * time.Minute)
	fresh, err := m.Issue()
	require.NoError(t, err)
	require.NoError(t, m.Validate(fresh, "00000001"))
	require.Len(t, m.used, 1)
}

func TestNonceValidationIsIdempotentWithinSameTransaction(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	m := NewNonceManager([]byte("secret"), time.Minute, clock)
	nonce, err := m.Issue()
	require.NoError(t, err)

	require.NoError(t, m.ValidateForTransaction(nonce, "00000001", "device|call|1|branch"))
	require.NoError(t, m.ValidateForTransaction(nonce, "00000001", "device|call|1|branch"))
	require.Len(t, m.used, 1)
}

func TestNonceValidationRejectsReplayAcrossTransactions(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	m := NewNonceManager([]byte("secret"), time.Minute, clock)
	nonce, err := m.Issue()
	require.NoError(t, err)

	require.NoError(t, m.ValidateForTransaction(nonce, "00000001", "device|call-a|1|branch-a"))
	require.ErrorIs(t, m.ValidateForTransaction(nonce, "00000001", "device|call-b|2|branch-b"), ErrNonceReplay)
}

func TestKnownDeviceRiskDoesNotCreateNATSourceBan(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	p := DefaultPolicyWithMode(ModeProtect)
	p.BanScore = 40
	p.BanTTLs = []TTLStep{{Score: 40, TTL: time.Minute}}
	s := NewScorer(p, clock, nil, []byte("secret"))
	for _, deviceID := range []string{"device-a", "device-b"} {
		for i := 0; i < 3; i++ {
			_, decision, err := s.Observe(Event{Transport: "TCP", SourceIP: "198.51.100.10", DeviceID: deviceID, Reason: ReasonNonceReplay})
			require.NoError(t, err)
			require.Nil(t, decision)
		}
	}
	require.Empty(t, s.decisions)
	events := []Event{{SourceIP: "198.51.100.10", DeviceID: "device-a", Reason: ReasonNonceReplay}, {SourceIP: "198.51.100.10", DeviceID: "device-b", Reason: ReasonNonceReplay}}
	for _, event := range events {
		require.Equal(t, ScopeDevice, riskScopeForEvent(event))
	}
}

func TestTransportRiskCanStillBanSourceIP(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	p := DefaultPolicyWithMode(ModeProtect)
	p.BanScore = 20
	p.BanTTLs = []TTLStep{{Score: 20, TTL: time.Minute}}
	s := NewScorer(p, clock, nil, []byte("secret"))
	_, decision, err := s.Observe(Event{Transport: "TCP", SourceIP: "198.51.100.11", Reason: ReasonPacketTooLarge})
	require.NoError(t, err)
	require.Nil(t, decision)
	_, decision, err = s.Observe(Event{Transport: "TCP", SourceIP: "198.51.100.11", Reason: ReasonPacketTooLarge})
	require.NoError(t, err)
	require.NotNil(t, decision)
	require.Equal(t, ScopeSource, decision.RiskScope)
	require.Empty(t, decision.DeviceID)
}
