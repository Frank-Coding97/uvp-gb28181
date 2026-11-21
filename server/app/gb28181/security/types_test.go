package security

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultPolicyUsesPermanentAutoBan(t *testing.T) {
	p := DefaultPolicy()
	require.NoError(t, p.Validate())
	require.Equal(t, ModeProtect, p.Mode)
	require.Equal(t, 10*time.Second, p.Window)
	ttl, matched := p.BanForScore(100)
	require.True(t, matched)
	require.Zero(t, ttl)
	require.Zero(t, p.TTLForScore(200))
	require.Zero(t, p.TTLForScore(500))
	require.True(t, p.PermanentForScore(500))
	require.Zero(t, p.TTLForScore(99))
}

func TestPolicyRejectsInvalidValues(t *testing.T) {
	p := DefaultPolicy()
	p.BanTTLs = []TTLStep{{Score: 100, TTL: -time.Second}}
	require.ErrorIs(t, p.Validate(), ErrInvalidPolicy)
	p.BanTTLs = []TTLStep{{Score: 100, TTL: 0}, {Score: 200, TTL: time.Minute}}
	require.ErrorIs(t, p.Validate(), ErrInvalidPolicy)
}

func TestPolicyAllowlistAndSourceValidation(t *testing.T) {
	_, network, err := net.ParseCIDR("203.0.113.0/24")
	require.NoError(t, err)
	p := DefaultPolicy()
	p.Allowlist = []net.IPNet{*network}
	require.True(t, p.IsAllowlisted("203.0.113.8"))
	require.False(t, p.IsAllowlisted("198.51.100.8"))
	_, err = ValidateSource("224.0.0.1")
	require.ErrorIs(t, err, ErrInvalidAddress)
	_, err = ValidateSource("0.0.0.0")
	require.ErrorIs(t, err, ErrInvalidAddress)
	_, err = ValidateSource("not-an-ip")
	require.ErrorIs(t, err, ErrInvalidAddress)
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeAgent struct {
	banCalls       []BanDecision
	unbanCalls     []string
	reconcileCalls [][]BanDecision
	status         AgentStatus
	banErr         error
}

func (a *fakeAgent) Ban(d BanDecision) error { a.banCalls = append(a.banCalls, d); return a.banErr }
func (a *fakeAgent) Unban(sourceIP string) error {
	a.unbanCalls = append(a.unbanCalls, sourceIP)
	return nil
}
func (a *fakeAgent) Status() AgentStatus { return a.status }
func (a *fakeAgent) Reconcile(items []BanDecision) error {
	a.reconcileCalls = append(a.reconcileCalls, append([]BanDecision(nil), items...))
	return nil
}

func TestFakeAgentRecordsDecisionAndStatus(t *testing.T) {
	agent := &fakeAgent{status: AgentStatus{Connected: true}}
	d := BanDecision{DecisionID: "decision-1", SourceIP: "198.51.100.10", TTL: 10 * time.Minute}
	require.NoError(t, agent.Ban(d))
	require.Len(t, agent.banCalls, 1)
	require.Equal(t, d.DecisionID, agent.banCalls[0].DecisionID)
	require.True(t, agent.Status().Connected)
}

func TestReasonScoresNonceStaleWithoutBanWeight(t *testing.T) {
	require.Zero(t, reasonScore(ReasonNonceStale))
	require.Equal(t, 20, reasonScore(ReasonNonceReplay))
}

func TestTTLForScoreUsesPermanentAutomaticBan(t *testing.T) {
	p := DefaultPolicy()
	require.Zero(t, p.TTLForScore(10000))
	require.True(t, p.PermanentForScore(10000))
	for _, step := range p.BanTTLs {
		require.Zero(t, step.TTL)
	}
}

func TestSecurityContractsUseStableJSONFieldNames(t *testing.T) {
	payload, err := json.Marshal(BanDecision{DecisionID: "d1", SourceIP: "198.51.100.10"})
	require.NoError(t, err)
	require.Contains(t, string(payload), `"decisionId"`)
	require.Contains(t, string(payload), `"sourceIp"`)
}
