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
	_, decision, err := s.Observe(Event{SourceIP: "198.51.100.10", Reason: ReasonDigestFailure})
	require.NoError(t, err)
	require.Nil(t, decision)
	_, decision, err = s.Observe(Event{SourceIP: "198.51.100.10", Reason: ReasonServerMismatch})
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
	_, decision, err := s.Observe(Event{SourceIP: "198.51.100.10", Reason: ReasonNonceReplay})
	require.NoError(t, err)
	require.Nil(t, decision)
	require.Empty(t, agent.banCalls)
}

func TestTrustedEndpointUpdatesOnlyWithValidAddress(t *testing.T) {
	s := NewScorer(DefaultPolicy(), &fakeClock{now: time.Unix(100, 0)}, nil, []byte("secret"))
	require.Error(t, s.UpdateTrustedEndpoint("34020000001320000001", "udp", "bad", time.Time{}))
	require.NoError(t, s.UpdateTrustedEndpoint("34020000001320000001", "udp", "198.51.100.12", time.Time{}))
	e, ok := s.TrustedEndpoint("34020000001320000001")
	require.True(t, ok)
	require.Equal(t, "198.51.100.12", e.Address)
}

func TestTrustedEndpointClearsSourceScore(t *testing.T) {
	p := DefaultPolicyWithMode(ModeProtect)
	p.BanScore = 30
	p.BanTTLs = []TTLStep{{Score: 30, TTL: time.Minute}}
	s := NewScorer(p, &fakeClock{now: time.Unix(100, 0)}, nil, []byte("secret"))

	_, decision, err := s.Observe(Event{SourceIP: "198.51.100.12", Reason: ReasonDigestFailure})
	require.NoError(t, err)
	require.Nil(t, decision)
	require.NoError(t, s.UpdateTrustedEndpoint("34020000001320000001", "udp", "198.51.100.12", time.Time{}))
	_, decision, err = s.Observe(Event{SourceIP: "198.51.100.12", Reason: ReasonServerMismatch})
	require.NoError(t, err)
	require.Nil(t, decision, "a valid REGISTER starts a fresh score window for that endpoint")
}

func TestNonceIsSignedExpiringAndSingleUse(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	m := NewNonceManager([]byte("secret"), time.Minute, clock)
	nonce, err := m.Issue()
	require.NoError(t, err)
	require.NoError(t, m.Validate(nonce, "00000001"))
	require.ErrorIs(t, m.Validate(nonce, "00000001"), ErrNonceReplay)
	clock.now = clock.now.Add(2 * time.Minute)
	nonce2, err := m.Issue()
	require.NoError(t, err)
	clock.now = clock.now.Add(2 * time.Minute)
	require.ErrorIs(t, m.Validate(nonce2, "00000001"), ErrNonceExpired)
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
