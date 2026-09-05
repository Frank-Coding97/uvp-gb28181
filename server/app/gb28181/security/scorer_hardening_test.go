package security

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPersistentInviteProbeTriggersPermanentBan(t *testing.T) {
	for _, interval := range []time.Duration{20 * time.Second, 30 * time.Second} {
		t.Run(interval.String(), func(t *testing.T) {
			clock := &fakeClock{now: time.Unix(100, 0)}
			s := NewScorer(DefaultPolicy(), clock, nil, []byte("test"))
			for i := 0; i < 10; i++ {
				observed, d, err := s.Observe(Event{SourceIP: "198.51.100.10", Transport: "UDP", Method: "INVITE", Reason: ReasonInviteRate, TransactionID: fmt.Sprint(i)})
				require.NoError(t, err)
				if i < 9 {
					require.Nil(t, d)
				} else {
					require.NotNil(t, d)
					require.True(t, d.Permanent)
					require.Zero(t, d.TTL)
					require.Equal(t, ReasonInvitePersistent, d.Reason)
					require.Equal(t, d.Reason, observed.Reason)
					require.Equal(t, 600, d.WindowSeconds)
					require.Equal(t, 10, d.TriggerCount)
				}
				clock.now = clock.now.Add(interval)
			}
		})
	}
}

func TestInviteRetransmissionsDoNotTriggerBan(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	s := NewScorer(DefaultPolicy(), clock, nil, []byte("test"))
	for i := 0; i < 30; i++ {
		_, d, err := s.Observe(Event{SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonInviteRate, TransactionID: "same-transaction"})
		require.NoError(t, err)
		require.Nil(t, d)
		clock.now = clock.now.Add(time.Second)
	}
}

func TestInviteWindowExpiresAndIsBounded(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	p := DefaultPolicy()
	p.MaxEventKeys = 20
	s := NewScorer(p, clock, nil, []byte("test"))
	for i := 0; i < 40; i++ {
		_, _, err := s.Observe(Event{SourceIP: fmt.Sprintf("198.51.100.%d", i+1), Method: "INVITE", Reason: ReasonInviteRate})
		require.NoError(t, err)
	}
	require.LessOrEqual(t, len(s.invites), 20)
	require.LessOrEqual(t, len(s.buckets), 20)
	clock.now = clock.now.Add(11 * time.Minute)
	_, d, err := s.Observe(Event{SourceIP: "198.51.100.40", Method: "INVITE", Reason: ReasonInviteRate})
	require.NoError(t, err)
	require.Nil(t, d)
	require.Len(t, s.invites, 1)
}

func TestNonceTTLIncreaseCannotResurrectConsumedNonce(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	m := NewNonceManager([]byte("test"), time.Second, clock)
	n, err := m.Issue()
	require.NoError(t, err)
	require.NoError(t, m.ValidateForTransaction(n, "", "original"))
	clock.now = clock.now.Add(2 * time.Second)
	fresh, err := m.Issue()
	require.NoError(t, err)
	require.NoError(t, m.Validate(fresh, ""))
	m.SetTTL(time.Minute)
	require.Error(t, m.ValidateForTransaction(n, "", "attack"))
	newNonce, err := m.Issue()
	require.NoError(t, err)
	clock.now = clock.now.Add(2 * time.Second)
	require.NoError(t, m.Validate(newNonce, ""))
}

func TestRepeatedIdenticalSlowProbeStillTriggersWithinTenMinutes(t *testing.T) {
	for _, interval := range []time.Duration{20 * time.Second, 30 * time.Second} {
		clock := &fakeClock{now: time.Unix(100, 0)}
		start := clock.now
		s := NewScorer(DefaultPolicy(), clock, nil, []byte("test"))
		var ban *BanDecision
		for clock.now.Sub(start) < 10*time.Minute {
			_, d, err := s.Observe(Event{SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonInviteRate, TransactionID: "same-probe"})
			require.NoError(t, err)
			if d != nil {
				ban = d
				break
			}
			clock.now = clock.now.Add(interval)
		}
		require.NotNil(t, ban, "repeated probe at %s must not evade accumulation", interval)
		require.True(t, ban.Permanent)
	}
}

func TestNonceRejectsAlternateBase64SpellingOfConsumedNonce(t *testing.T) {
	m := NewNonceManager([]byte("test"), time.Minute, &fakeClock{now: time.Unix(100, 0)})
	nonce, err := m.Issue()
	require.NoError(t, err)
	require.NoError(t, m.ValidateForTransaction(nonce, "", "original"))
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	index := strings.IndexByte(alphabet, nonce[len(nonce)-1])
	require.NotEqual(t, -1, index)
	alternate := nonce[:len(nonce)-1] + string(alphabet[index|1])
	require.ErrorIs(t, m.ValidateForTransaction(alternate, "", "replay"), ErrNonceInvalid, "unused base64 bits cannot create a fresh replay-cache key")
}
