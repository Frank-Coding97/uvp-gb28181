package firewall

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/security"
)

func TestAgentBanIsIdempotentAndRejectsAllowlist(t *testing.T) {
	clock := &testClock{now: time.Unix(100, 0)}
	_, network, err := net.ParseCIDR("198.51.100.0/24")
	require.NoError(t, err)
	a := New(NewMemoryBackend(), clock, []net.IPNet{*network})
	err = a.Ban(security.BanDecision{DecisionID: "d1", SourceIP: "198.51.100.7", TTL: time.Minute})
	require.ErrorIs(t, err, ErrAllowlisted)
	a = New(NewMemoryBackend(), clock, nil)
	d := security.BanDecision{DecisionID: "d1", SourceIP: "203.0.113.7", TTL: time.Minute}
	require.NoError(t, a.Ban(d))
	require.NoError(t, a.Ban(d))
	require.Equal(t, 1, a.Status().AppliedRules)
}

func TestAgentReconcileSkipsExpiredRules(t *testing.T) {
	clock := &testClock{now: time.Unix(100, 0)}
	a := New(NewMemoryBackend(), clock, nil)
	require.NoError(t, a.Reconcile([]security.BanDecision{{DecisionID: "expired", SourceIP: "203.0.113.1", CreatedAt: clock.now.Add(-time.Minute), TTL: time.Second}, {DecisionID: "active", SourceIP: "203.0.113.2", CreatedAt: clock.now, TTL: time.Minute}}))
	require.Equal(t, 1, a.Status().AppliedRules)
}

func TestAgentAcceptsPermanentRulesAndKeepsThemDuringReconcile(t *testing.T) {
	clock := &testClock{now: time.Unix(100, 0)}
	backend := NewMemoryBackend()
	a := New(backend, clock, nil)
	decision := security.BanDecision{DecisionID: "permanent", SourceIP: "203.0.113.3", CreatedAt: clock.now, Permanent: true}
	require.NoError(t, a.Ban(decision))
	require.True(t, backend.rules[decision.SourceIP].IsZero())
	clock.now = clock.now.Add(365 * 24 * time.Hour)
	require.NoError(t, a.Reconcile([]security.BanDecision{decision}))
	require.True(t, backend.rules[decision.SourceIP].IsZero())
	require.Equal(t, 1, a.Status().AppliedRules)
}

func TestAgentReconcileRemovesRulesThatNowOverlapAllowlist(t *testing.T) {
	clock := &testClock{now: time.Unix(100, 0)}
	backend := NewMemoryBackend()
	require.NoError(t, backend.Add("198.51.100.7", clock.now.Add(time.Hour)))
	_, network, err := net.ParseCIDR("198.51.100.0/24")
	require.NoError(t, err)
	a := New(backend, clock, []net.IPNet{*network})

	err = a.Reconcile([]security.BanDecision{{DecisionID: "old", SourceIP: "198.51.100.7", CreatedAt: clock.now, TTL: time.Hour}})
	require.NoError(t, err)
	rules, err := backend.List()
	require.NoError(t, err)
	require.Empty(t, rules)
}

type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time { return c.now }
