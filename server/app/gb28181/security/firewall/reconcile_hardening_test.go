package firewall

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/security"
)

func TestAgentReconcileSkipsStableRulesWithoutReadding(t *testing.T) {
	clock := &testClock{now: time.Unix(100, 0)}
	backend := newReconcileHardeningBackend()
	agent := New(backend, clock, nil)
	permanent := security.BanDecision{
		DecisionID: "permanent", SourceIP: "203.0.113.10", CreatedAt: clock.now, Permanent: true,
	}
	finite := security.BanDecision{
		DecisionID: "finite", SourceIP: "203.0.113.11", CreatedAt: clock.now, TTL: time.Minute,
	}
	require.NoError(t, agent.Ban(permanent))
	require.NoError(t, agent.Ban(finite))
	backend.resetCalls()

	require.NoError(t, agent.Reconcile([]security.BanDecision{permanent, finite}))

	require.Equal(t, 1, backend.listCalls)
	require.Zero(t, backend.addCalls)
	require.Zero(t, backend.removeCalls)
	require.ElementsMatch(t, []string{permanent.SourceIP, finite.SourceIP}, backend.ruleList())
}

func TestAgentReconcileRestoresMissingKernelRule(t *testing.T) {
	clock := &testClock{now: time.Unix(100, 0)}
	backend := newReconcileHardeningBackend()
	agent := New(backend, clock, nil)
	decision := security.BanDecision{
		DecisionID: "recover", SourceIP: "203.0.113.12", CreatedAt: clock.now, TTL: time.Minute,
	}
	require.NoError(t, agent.Ban(decision))
	delete(backend.rules, decision.SourceIP)
	backend.resetCalls()

	require.NoError(t, agent.Reconcile([]security.BanDecision{decision}))

	require.Equal(t, 1, backend.listCalls)
	require.Equal(t, 1, backend.addCalls)
	require.Zero(t, backend.removeCalls)
	require.Contains(t, backend.rules, decision.SourceIP)
}

func TestAgentReconcileReappliesChangedDecisionIDOrExpiry(t *testing.T) {
	clock := &testClock{now: time.Unix(100, 0)}
	backend := newReconcileHardeningBackend()
	agent := New(backend, clock, nil)
	decision := security.BanDecision{
		DecisionID: "original", SourceIP: "203.0.113.13", CreatedAt: clock.now, TTL: time.Minute,
	}
	require.NoError(t, agent.Ban(decision))

	changedID := decision
	changedID.DecisionID = "replacement"
	backend.resetCalls()
	require.NoError(t, agent.Reconcile([]security.BanDecision{changedID}))
	require.Equal(t, 1, backend.listCalls)
	require.Equal(t, 1, backend.addCalls)
	require.Equal(t, changedID.ExpiresAt(), backend.added[len(backend.added)-1].expiresAt)

	changedExpiry := changedID
	changedExpiry.TTL = 2 * time.Minute
	backend.resetCalls()
	require.NoError(t, agent.Reconcile([]security.BanDecision{changedExpiry}))
	require.Equal(t, 1, backend.listCalls)
	require.Equal(t, 1, backend.addCalls)
	require.Equal(t, changedExpiry.ExpiresAt(), backend.added[len(backend.added)-1].expiresAt)
}

func TestAgentReconcileRemovesDeletedSourceWithoutReaddingStableRules(t *testing.T) {
	clock := &testClock{now: time.Unix(100, 0)}
	backend := newReconcileHardeningBackend()
	agent := New(backend, clock, nil)
	kept := security.BanDecision{
		DecisionID: "kept", SourceIP: "203.0.113.14", CreatedAt: clock.now, TTL: time.Minute,
	}
	removed := security.BanDecision{
		DecisionID: "removed", SourceIP: "203.0.113.15", CreatedAt: clock.now, Permanent: true,
	}
	require.NoError(t, agent.Ban(kept))
	require.NoError(t, agent.Ban(removed))
	backend.resetCalls()

	require.NoError(t, agent.Reconcile([]security.BanDecision{kept}))

	require.Equal(t, 1, backend.listCalls)
	require.Zero(t, backend.addCalls)
	require.Equal(t, 1, backend.removeCalls)
	require.Equal(t, []string{removed.SourceIP}, backend.removed)
	require.Equal(t, []string{kept.SourceIP}, backend.ruleList())
}

func TestAgentReconcileRepairsAfterAgentRestartOnce(t *testing.T) {
	clock := &testClock{now: time.Unix(100, 0)}
	backend := newReconcileHardeningBackend()
	previousAgent := New(backend, clock, nil)
	decision := security.BanDecision{
		DecisionID: "after-restart", SourceIP: "203.0.113.16", CreatedAt: clock.now, Permanent: true,
	}
	require.NoError(t, previousAgent.Ban(decision))

	agent := New(backend, clock, nil)
	backend.resetCalls()
	require.NoError(t, agent.Reconcile([]security.BanDecision{decision}))
	require.Equal(t, 1, backend.listCalls)
	require.Equal(t, 1, backend.addCalls)

	backend.resetCalls()
	require.NoError(t, agent.Reconcile([]security.BanDecision{decision}))
	require.Equal(t, 1, backend.listCalls)
	require.Zero(t, backend.addCalls)
}

type reconcileHardeningBackend struct {
	rules       map[string]time.Time
	listCalls   int
	addCalls    int
	removeCalls int
	added       []reconcileHardeningAdd
	removed     []string
}

type reconcileHardeningAdd struct {
	sourceIP  string
	expiresAt time.Time
}

func newReconcileHardeningBackend() *reconcileHardeningBackend {
	return &reconcileHardeningBackend{rules: make(map[string]time.Time)}
}

func (backend *reconcileHardeningBackend) Add(sourceIP string, expiresAt time.Time) error {
	backend.addCalls++
	backend.rules[sourceIP] = expiresAt
	backend.added = append(backend.added, reconcileHardeningAdd{sourceIP: sourceIP, expiresAt: expiresAt})
	return nil
}

func (backend *reconcileHardeningBackend) Remove(sourceIP string) error {
	backend.removeCalls++
	delete(backend.rules, sourceIP)
	backend.removed = append(backend.removed, sourceIP)
	return nil
}

func (backend *reconcileHardeningBackend) List() ([]string, error) {
	backend.listCalls++
	return backend.ruleList(), nil
}

func (backend *reconcileHardeningBackend) ruleList() []string {
	result := make([]string, 0, len(backend.rules))
	for sourceIP := range backend.rules {
		result = append(result, sourceIP)
	}
	sort.Strings(result)
	return result
}

func (backend *reconcileHardeningBackend) resetCalls() {
	backend.listCalls = 0
	backend.addCalls = 0
	backend.removeCalls = 0
	backend.added = nil
	backend.removed = nil
}
