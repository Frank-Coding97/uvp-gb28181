package security

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type activationAgent struct {
	failure error
	calls   int
	bans    int
	writes  []int
}

func (a *activationAgent) Ban(BanDecision) error {
	a.bans++
	return nil
}
func (*activationAgent) Unban(string) error  { return nil }
func (*activationAgent) Status() AgentStatus { return AgentStatus{Connected: true} }

func (a *activationAgent) Reconcile(decisions []BanDecision) error {
	a.calls++
	a.writes = append(a.writes, len(decisions))
	return a.failure
}

type failingActivationStore struct {
	Store
}

func (failingActivationStore) SavePolicy(context.Context, Policy, string) error {
	return errors.New("policy write failed")
}

func TestRuntimeRevertsFirewallRulesWhenPolicyWriteFails(t *testing.T) {
	ctx := context.Background()
	store := NewGormStore(newSecurityStoreTestDB(t))
	previous := DefaultPolicy()
	previous.Mode = ModeObserve
	require.NoError(t, store.SavePolicy(ctx, previous, "operator"))
	agent := &activationAgent{}
	runtime, err := NewPersistentRuntime(ctx, failingActivationStore{store}, nil, agent, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, runtime.Close(ctx)) }()
	runtime.bans.Upsert(BanDecision{
		DecisionID: "active-from-before-observe",
		SourceIP:   "198.51.100.10",
		Permanent:  true,
		CreatedAt:  time.Now(),
	}, "auto")

	before := len(agent.writes)
	require.ErrorContains(t, runtime.UpdatePolicy(DefaultPolicy(), "operator"), "policy write failed")
	require.Equal(t, []int{1, 0}, agent.writes[before:])
	require.Equal(t, ModeObserve, runtime.Policy().Mode)
	require.False(t, runtime.Admission().IsBanned("198.51.100.10"))
}

func TestApplicationProtectionPersistsAndDoesNotSendBansToFirewall(t *testing.T) {
	ctx := context.Background()
	store := NewGormStore(newSecurityStoreTestDB(t))
	policy := unsupportedBanPolicy()
	policy.Mode = ModeObserve
	require.NoError(t, store.SavePolicy(ctx, policy, "operator"))
	agent := &activationAgent{}
	runtime, err := NewPersistentRuntime(ctx, store, nil, agent, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, runtime.Close(ctx)) }()

	policy.Mode = ModeApplication
	require.NoError(t, runtime.UpdatePolicy(policy, "operator"))
	err = runtime.Record(Event{Transport: "TCP", SourceIP: "198.51.100.10", Reason: ReasonUnknownMethod})
	require.NoError(t, err)
	require.Zero(t, agent.bans)
	require.True(t, runtime.Admission().IsBanned("198.51.100.10"))
	saved, err := store.LoadPolicy(ctx)
	require.NoError(t, err)
	require.Equal(t, ModeApplication, saved.Mode)
}

func TestRuntimeOnlyCommitsProtectionAfterFirewallReconcile(t *testing.T) {
	for _, failure := range []error{ErrFirewallUnsupported, errors.New("nftables unavailable"), nil} {
		name := "ready"
		if failure != nil {
			name = failure.Error()
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			store := NewGormStore(newSecurityStoreTestDB(t))
			policy := DefaultPolicy()
			policy.Mode = ModeObserve
			require.NoError(t, store.SavePolicy(ctx, policy, "operator"))
			agent := &activationAgent{}
			runtime, err := NewPersistentRuntime(ctx, store, nil, agent, nil)
			require.NoError(t, err)
			defer func() { require.NoError(t, runtime.Close(ctx)) }()
			agent.failure = failure
			before := agent.calls

			err = runtime.UpdatePolicy(DefaultPolicy(), "operator")
			require.Greater(t, agent.calls, before, "an empty ban list must still be reconciled")
			expected := ModeProtect
			if failure != nil {
				require.Error(t, err)
				expected = ModeObserve
			} else {
				require.NoError(t, err)
			}
			loaded, err := store.LoadPolicy(ctx)
			require.NoError(t, err)
			require.Equal(t, expected, loaded.Mode)
			require.Equal(t, expected, runtime.Policy().Mode)
		})
	}
}
