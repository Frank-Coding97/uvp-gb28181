package security

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func unsupportedBanPolicy() Policy {
	policy := DefaultPolicy()
	policy.BanScore = 1
	policy.BanTTLs = []TTLStep{{Score: 1, TTL: 0}}
	return policy
}

func TestRuntimeUnsupportedFirewallKeepsApplicationBanActive(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	runtime := NewRuntime(unsupportedBanPolicy(), clock, NewUnsupportedFirewallClient(), []byte("test"))

	require.NoError(t, runtime.Record(Event{
		Transport: "TCP", SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonUnknownMethod,
	}))
	bans := runtime.Bans()
	require.Len(t, bans, 1)
	require.Equal(t, BanActive, bans[0].Status)
	require.Equal(t, AgentStateUnsupported, bans[0].AgentState)
	require.Zero(t, bans[0].FirewallAppliedAt)
	require.True(t, runtime.Admission().IsBanned("198.51.100.10"))
	require.Equal(t, AgentCapabilityUnsupported, runtime.AgentStatus().Capability)
}

func TestRuntimeUnsupportedFirewallAllowsApplicationUnbanAndPolicyUpdate(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	runtime := NewRuntime(unsupportedBanPolicy(), clock, NewUnsupportedFirewallClient(), []byte("test"))
	require.NoError(t, runtime.Record(Event{
		Transport: "TCP", SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonUnknownMethod,
	}))
	require.NoError(t, runtime.Unban("198.51.100.10", "operator"))
	require.False(t, runtime.Admission().IsBanned("198.51.100.10"))

	next := unsupportedBanPolicy()
	next.Mode = ModeObserve
	require.NoError(t, runtime.UpdatePolicy(next, "operator"))
}

type capabilityCountingAgent struct {
	capability     AgentCapabilityState
	statusCalls    int
	banCalls       int
	unbanCalls     int
	reconcileCalls int
}

func (a *capabilityCountingAgent) Capability() AgentCapabilityState { return a.capability }
func (a *capabilityCountingAgent) Ban(BanDecision) error {
	a.banCalls++
	return ErrFirewallUnsupported
}
func (a *capabilityCountingAgent) Unban(string) error {
	a.unbanCalls++
	return ErrFirewallUnsupported
}
func (a *capabilityCountingAgent) Reconcile([]BanDecision) error {
	a.reconcileCalls++
	return ErrFirewallUnsupported
}
func (a *capabilityCountingAgent) Status() AgentStatus {
	a.statusCalls++
	return AgentStatus{Capability: a.capability}
}

func TestNewRuntimeReadsCapabilityWithoutStatusIO(t *testing.T) {
	agent := &capabilityCountingAgent{capability: AgentCapabilityUnsupported}
	runtime := NewRuntime(DefaultPolicy(), nil, agent, nil)
	require.Zero(t, agent.statusCalls)
	require.Equal(t, AgentCapabilityUnsupported, runtime.AgentStatus().Capability)
}

func TestRuntimeUpdatePolicyAbsorbsTypedUnsupportedReconcileError(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	agent := &capabilityCountingAgent{capability: AgentCapabilityUnknown}
	runtime := NewRuntime(DefaultPolicy(), clock, agent, nil)

	require.NoError(t, runtime.UpdatePolicy(DefaultPolicy(), "operator"))
	require.Equal(t, 1, agent.reconcileCalls)
}

func TestUnsupportedRuntimeReconcileSkipsAgentAndPersistence(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	agent := &capabilityCountingAgent{capability: AgentCapabilityUnsupported}
	store := &countingBanStore{Store: NewGormStore(newSecurityStoreTestDB(t))}
	require.NoError(t, store.SavePolicy(context.Background(), unsupportedBanPolicy(), "system"))
	old := FirewallBan{
		Decision: BanDecision{DecisionID: "old", SourceIP: "198.51.100.10", Score: 1, CreatedAt: clock.Now(), Permanent: true},
		Status:   BanAgentFailed, AgentState: AgentStateFailed, FirewallAppliedAt: clock.Now(),
	}
	require.NoError(t, store.SaveBan(context.Background(), old))
	store.banSaves = 0

	runtime, err := NewPersistentRuntime(context.Background(), store, clock, agent, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, runtime.Close(context.Background())) }()
	require.True(t, runtime.Admission().IsBanned(old.Decision.SourceIP))
	item, ok := runtime.bans.Get(old.Decision.SourceIP, clock.Now())
	require.True(t, ok)
	require.Equal(t, BanActive, item.Status)
	require.Equal(t, AgentStateUnsupported, item.AgentState)
	require.Zero(t, item.FirewallAppliedAt)
	require.Equal(t, 1, store.banSaves)
	require.Zero(t, agent.statusCalls)
	require.Zero(t, agent.reconcileCalls)

	require.NoError(t, runtime.reconcileAgent())
	require.Equal(t, 1, store.banSaves)
	require.Zero(t, agent.reconcileCalls)
	require.Zero(t, agent.statusCalls)

	active, err := store.ActiveBans(context.Background(), clock.Now())
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, BanActive, active[0].Status)
	require.Equal(t, AgentStateUnsupported, active[0].AgentState)
}

type countingBanStore struct {
	Store
	banSaves int
	saveErr  error
}

func (s *countingBanStore) SaveBan(ctx context.Context, item FirewallBan) error {
	s.banSaves++
	if s.saveErr != nil {
		return s.saveErr
	}
	return s.Store.SaveBan(ctx, item)
}

func TestPersistentUnsupportedNormalizationFailureKeepsAdmissionAndReportsPersistenceError(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	base := NewGormStore(newSecurityStoreTestDB(t))
	require.NoError(t, base.SavePolicy(context.Background(), unsupportedBanPolicy(), "system"))
	old := FirewallBan{
		Decision: BanDecision{DecisionID: "old", SourceIP: "198.51.100.10", Score: 1, CreatedAt: clock.Now(), Permanent: true},
		Status:   BanAgentFailed, AgentState: AgentStateFailed, FirewallAppliedAt: clock.Now(),
	}
	require.NoError(t, base.SaveBan(context.Background(), old))
	store := &countingBanStore{Store: base, saveErr: errors.New("injected save failure")}
	agent := &capabilityCountingAgent{capability: AgentCapabilityUnsupported}

	runtime, err := NewPersistentRuntime(context.Background(), store, clock, agent, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, runtime.Close(context.Background())) }()
	require.True(t, runtime.Admission().IsBanned(old.Decision.SourceIP))
	status := runtime.AgentStatus()
	require.Equal(t, startupBanStatePersistenceError, status.LastError)
	require.NotContains(t, status.LastError, "injected save failure")

	persisted, err := base.ActiveBans(context.Background(), clock.Now())
	require.NoError(t, err)
	require.Len(t, persisted, 1)
	require.Equal(t, BanAgentFailed, persisted[0].Status)
	require.Equal(t, AgentStateFailed, persisted[0].AgentState)
}

type unbanErrorAgent struct{}

func (unbanErrorAgent) Ban(BanDecision) error { return nil }
func (unbanErrorAgent) Unban(string) error    { return errors.New("agent disconnected") }
func (unbanErrorAgent) Status() AgentStatus   { return AgentStatus{Capability: AgentCapabilitySupported} }

func TestRuntimeUnbanKeepsApplicationBanOnOrdinaryAgentError(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	runtime := NewRuntime(unsupportedBanPolicy(), clock, unbanErrorAgent{}, nil)
	require.NoError(t, runtime.Record(Event{
		Transport: "TCP", SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonUnknownMethod,
	}))
	require.Error(t, runtime.Unban("198.51.100.10", "operator"))
	require.True(t, runtime.Admission().IsBanned("198.51.100.10"))
}

type policyErrorAgent struct{}

func (policyErrorAgent) Ban(BanDecision) error { return nil }
func (policyErrorAgent) Unban(string) error    { return nil }
func (policyErrorAgent) Status() AgentStatus {
	return AgentStatus{Capability: AgentCapabilitySupported}
}
func (policyErrorAgent) Reconcile([]BanDecision) error { return errors.New("agent reconcile failed") }

func TestRuntimeUpdatePolicyKeepsApplicationBanOnOrdinaryAgentError(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	runtime := NewRuntime(unsupportedBanPolicy(), clock, policyErrorAgent{}, nil)
	require.NoError(t, runtime.Record(Event{
		Transport: "TCP", SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonUnknownMethod,
	}))

	require.Error(t, runtime.UpdatePolicy(unsupportedBanPolicy(), "operator"))
	require.True(t, runtime.Admission().IsBanned("198.51.100.10"))
}
