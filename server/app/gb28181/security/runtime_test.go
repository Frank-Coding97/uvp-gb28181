package security

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRuntimeObserveAggregatesWithoutFirewallSideEffect(t *testing.T) {
	agent := &fakeAgent{}
	r := NewRuntime(DefaultPolicy(), &fakeClock{now: time.Unix(100, 0)}, agent, []byte("secret"))
	require.NoError(t, r.Record(Event{SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonUnknownMethod, Action: ActionDrop}))
	snapshot := r.Snapshot()
	require.Len(t, snapshot.Events, 1)
	require.Empty(t, agent.banCalls)
}

func TestRuntimeProtectPropagatesBanToAdmissionAndAgent(t *testing.T) {
	p := DefaultPolicyWithMode(ModeProtect)
	p.BanScore = 1
	p.BanTTLs = []TTLStep{{Score: 1, TTL: time.Minute}}
	agent := &fakeAgent{}
	r := NewRuntime(p, &fakeClock{now: time.Unix(100, 0)}, agent, []byte("secret"))
	require.NoError(t, r.Record(Event{SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonUnknownMethod, Action: ActionDrop}))
	require.Len(t, agent.banCalls, 1)
	require.True(t, r.Admission().IsBanned("198.51.100.10"))
}

func TestRuntimeAdmissionFeedsScorerAndSnapshot(t *testing.T) {
	p := DefaultPolicyWithMode(ModeProtect)
	p.MaxUDPPerWindow = 1
	p.BanScore = 1
	p.BanTTLs = []TTLStep{{Score: 1, TTL: time.Minute}}
	agent := &fakeAgent{}
	r := NewRuntime(p, &fakeClock{now: time.Unix(100, 0)}, agent, []byte("secret"))

	packet := []byte("INVITE sip:x SIP/2.0\r\n")
	_, err := r.Admission().Filter(readProps(), packet)
	require.NoError(t, err)
	out, err := r.Admission().Filter(readProps(), packet)
	require.NoError(t, err)
	require.Empty(t, out)
	require.Len(t, r.Events(), 1)
	require.Len(t, r.Bans(), 1)
	require.Len(t, agent.banCalls, 1)
}

func TestRuntimeKeepsFailedAgentDecisionVisible(t *testing.T) {
	p := DefaultPolicyWithMode(ModeProtect)
	p.BanScore = 1
	p.BanTTLs = []TTLStep{{Score: 1, TTL: time.Minute}}
	agent := &fakeAgent{banErr: errors.New("socket unavailable")}
	r := NewRuntime(p, &fakeClock{now: time.Unix(100, 0)}, agent, []byte("secret"))

	err := r.Record(Event{SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonUnknownMethod})
	require.Error(t, err)
	require.Len(t, r.Bans(), 1)
	require.Equal(t, BanAgentFailed, r.Bans()[0].Status)
	require.True(t, r.Admission().IsBanned("198.51.100.10"))
}

func TestPersistentRuntimeRestoresPolicyBanAndFlushesEvents(t *testing.T) {
	db := newSecurityStoreTestDB(t)
	store := NewGormStore(db)
	policy := DefaultPolicy()
	policy.Mode = ModeProtect
	require.NoError(t, store.SavePolicy(context.Background(), policy, "system"))
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	ban := FirewallBan{Decision: BanDecision{DecisionID: "restore-1", SourceIP: "203.0.113.10", Reason: ReasonInviteRate, Score: 120, CreatedAt: now, TTL: time.Hour}, Status: BanActive, RuleID: "restore-1", Origin: "auto", AgentState: "applied"}
	require.NoError(t, store.SaveBan(context.Background(), ban))
	agent := &fakeAgent{}
	r, err := NewPersistentRuntime(context.Background(), store, &fakeClock{now: now.Add(time.Minute)}, agent, []byte("secret"))
	require.NoError(t, err)
	require.Equal(t, ModeProtect, r.Policy().Mode)
	require.True(t, r.Admission().IsBanned("203.0.113.10"))
	require.Len(t, agent.reconcileCalls, 1)
	require.Len(t, agent.reconcileCalls[0], 1)

	require.NoError(t, r.Record(Event{SourceIP: "198.51.100.20", Transport: "UDP", Method: "INVITE", Reason: ReasonInviteRate, Action: ActionSample, Occurred: now.Add(time.Minute)}))
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, r.Close(closeCtx))
	events, err := store.RecentEvents(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
}

func TestPersistentRuntimeObserveClearsKernelRulesInsteadOfRestoringBans(t *testing.T) {
	db := newSecurityStoreTestDB(t)
	store := NewGormStore(db)
	policy := DefaultPolicy()
	require.NoError(t, store.SavePolicy(context.Background(), policy, "system"))
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	ban := FirewallBan{Decision: BanDecision{DecisionID: "observe-1", SourceIP: "203.0.113.10", Reason: ReasonInviteRate, Score: 120, CreatedAt: now, TTL: time.Hour}, Status: BanActive, RuleID: "observe-1", Origin: "auto", AgentState: "applied"}
	require.NoError(t, store.SaveBan(context.Background(), ban))
	agent := &fakeAgent{}

	r, err := NewPersistentRuntime(context.Background(), store, &fakeClock{now: now.Add(time.Minute)}, agent, []byte("secret"))
	require.NoError(t, err)
	require.Equal(t, ModeObserve, r.Policy().Mode)
	require.False(t, r.Admission().IsBanned("203.0.113.10"))
	require.Equal(t, [][]BanDecision{nil}, agent.reconcileCalls)
}

func TestRuntimePolicySwitchToObserveClearsAgentRules(t *testing.T) {
	p := DefaultPolicyWithMode(ModeProtect)
	p.BanScore = 1
	p.BanTTLs = []TTLStep{{Score: 1, TTL: time.Minute}}
	agent := &fakeAgent{}
	r := NewRuntime(p, &fakeClock{now: time.Unix(100, 0)}, agent, []byte("secret"))
	require.NoError(t, r.Record(Event{SourceIP: "198.51.100.30", Method: "INVITE", Reason: ReasonInviteRate}))

	next := p
	next.Mode = ModeObserve
	require.NoError(t, r.UpdatePolicy(next, "admin"))
	require.Equal(t, [][]BanDecision{nil}, agent.reconcileCalls)
	require.False(t, r.Admission().IsBanned("198.51.100.30"))
}

func TestRuntimeUnbanAcceptsDecisionIDFromAPI(t *testing.T) {
	p := DefaultPolicyWithMode(ModeProtect)
	p.BanScore = 1
	p.BanTTLs = []TTLStep{{Score: 1, TTL: time.Minute}}
	agent := &fakeAgent{}
	r := NewRuntime(p, &fakeClock{now: time.Unix(100, 0)}, agent, []byte("secret"))
	require.NoError(t, r.Record(Event{SourceIP: "198.51.100.30", Method: "INVITE", Reason: ReasonInviteRate}))
	bans := r.Bans()
	require.Len(t, bans, 1)
	require.NoError(t, r.Unban(bans[0].Decision.DecisionID, "admin"))
	require.Equal(t, []string{"198.51.100.30"}, agent.unbanCalls)
	require.Equal(t, BanUnbanned, r.Bans()[0].Status)
}
