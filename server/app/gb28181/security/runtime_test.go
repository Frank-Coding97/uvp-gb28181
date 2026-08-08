package security

import (
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
