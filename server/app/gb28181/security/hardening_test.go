package security

import (
	"context"
	"fmt"
	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestSecurityHardeningLegitimateResponsesMustNotCauseBan(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	agent := &fakeAgent{}
	r := NewRuntime(DefaultPolicy(), clock, agent, []byte("review-test"))
	require.NoError(t, r.TrustEndpoint("registered-device", "UDP", "198.51.100.10", time.Hour))
	packet := []byte("SIP/2.0 200 OK\r\nVia: SIP/2.0/UDP 198.51.100.10:5060;branch=z9hG4bK-review\r\nFrom: <sip:x@example.com>;tag=a\r\nTo: <sip:y@example.com>;tag=b\r\nCall-ID: review\r\nCSeq: 1 MESSAGE\r\nContent-Length: 0\r\n\r\n")
	drops := 0
	for i := 0; i < 130; i++ {
		out, err := r.Admission().Filter(readProps(), packet)
		require.NoError(t, err)
		if len(out) == 0 {
			drops++
		}
	}
	t.Logf("responses=130 drops=%d bans=%d", drops, len(r.Bans()))
	require.False(t, r.Admission().IsBanned("198.51.100.10"), "legitimate responses must not trigger a source ban")
}
func TestSecurityHardeningTCPAllMessagesMustPassAdmission(t *testing.T) {
	a := NewAdmission(DefaultPolicy(), &fakeClock{now: time.Unix(100, 0)}, nil, nil)
	p := readProps()
	p.Transport = "tcp"
	packet := func(method string) string {
		return fmt.Sprintf("%s sip:x@example.com SIP/2.0\r\nVia: SIP/2.0/TCP 198.51.100.10:5060;branch=z9hG4bK-%s\r\nFrom: <sip:a@example.com>;tag=a\r\nTo: <sip:x@example.com>\r\nCall-ID: review-%s\r\nCSeq: 1 %s\r\nContent-Length: 0\r\n\r\n", method, method, method, method)
	}
	out, err := a.Filter(p, []byte(packet("REGISTER")+packet("INVITE")))
	require.NoError(t, err)
	methods := []string{}
	if len(out) > 0 {
		err = sip.NewParser().NewSIPStream().ParseSIPStream(out, func(msg sip.Message) {
			if req, ok := msg.(*sip.Request); ok {
				methods = append(methods, string(req.Method))
			}
		})
		require.NoError(t, err)
	}
	t.Logf("forwarded methods=%v", methods)
	require.NotContains(t, methods, "INVITE", "protect must reject unauthorized INVITE even when coalesced after REGISTER")
}
func TestSecurityHardeningBanDecisionIDsMustIdentifyEachDecision(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	p := DefaultPolicy()
	p.BanScore = 10
	p.BanTTLs = []TTLStep{{Score: 10, TTL: time.Minute}}
	s := NewScorer(p, clock, nil, []byte("review-test"))
	_, first, err := s.Observe(Event{SourceIP: "198.51.100.10", Reason: ReasonUnknownMethod})
	require.NoError(t, err)
	require.NotNil(t, first)
	clock.now = clock.now.Add(2 * time.Minute)
	_, second, err := s.Observe(Event{SourceIP: "198.51.100.10", Reason: ReasonUnknownMethod})
	require.NoError(t, err)
	require.NotNil(t, second)
	require.NotEqual(t, first.DecisionID, second.DecisionID, "new ban must not overwrite expired decision")
}
func TestSecurityHardeningAggregateMustRecoverAfterCapacityReached(t *testing.T) {
	p := DefaultPolicy()
	p.Mode = ModeObserve
	p.MaxEventKeys = 2
	clock := &fakeClock{now: time.Unix(120, 0)}
	r := NewRuntime(p, clock, nil, []byte("review-test"))
	for i := 0; i < 3; i++ {
		require.NoError(t, r.Record(Event{SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonInviteRate}))
		clock.now = clock.now.Add(time.Minute)
	}
	events := r.Events()
	t.Logf("events=%d last=%s now=%s dropped=%d", len(events), events[len(events)-1].LastSeenAt, clock.now, r.Snapshot().Dropped)
	require.Equal(t, clock.now.Add(-time.Minute), events[len(events)-1].LastSeenAt, "events must keep advancing after bounded store is full")
}
func TestSecurityHardeningPolicyNonceTTLActuallyChangesRuntime(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	r := NewRuntime(DefaultPolicy(), clock, nil, []byte("review-test"))
	p := r.Policy()
	p.NonceTTL = time.Second
	require.NoError(t, r.UpdatePolicy(p, "review"))
	n, err := r.IssueNonce()
	require.NoError(t, err)
	clock.now = clock.now.Add(2 * time.Second)
	require.ErrorIs(t, r.ValidateNonceForTransaction(n, "", "review"), ErrNonceExpired, "published nonce TTL must take effect")
}

func TestSecurityHardeningRepeatBanMustRemainActiveInPersistentStore(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	p := DefaultPolicy()
	p.BanScore = 10
	p.BanTTLs = []TTLStep{{Score: 10, TTL: time.Minute}}
	store := NewGormStore(newSecurityStoreTestDB(t))
	r := NewRuntime(p, clock, &fakeAgent{}, []byte("review-test"))
	r.store = store
	require.NoError(t, r.Record(Event{SourceIP: "198.51.100.10", Reason: ReasonUnknownMethod}))
	clock.now = clock.now.Add(2 * time.Minute)
	require.NoError(t, r.Record(Event{SourceIP: "198.51.100.10", Reason: ReasonUnknownMethod}))
	require.True(t, r.Admission().IsBanned("198.51.100.10"))
	active, err := store.ActiveBans(context.Background(), clock.now)
	require.NoError(t, err)
	t.Logf("admission_banned=true database_active_bans=%d", len(active))
	require.Len(t, active, 1, "second ban must survive restart/reconcile")
}
func TestSecurityHardeningExpiredAdmissionRateWindowsMustBeCollected(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	a := NewAdmission(DefaultPolicy(), clock, nil, nil)
	for i := 0; i < 5000; i++ {
		_, err := a.Filter(readProps(), []byte(fmt.Sprintf("X%06d sip:x@example.com SIP/2.0\r\n", i)))
		require.NoError(t, err)
	}
	clock.now = clock.now.Add(time.Hour)
	_, err := a.Filter(readProps(), []byte("LAST sip:x@example.com SIP/2.0\r\n"))
	require.NoError(t, err)
	t.Logf("retained_rate_windows_after_one_hour=%d dropped=%d", len(a.windows), a.Dropped())
	require.LessOrEqual(t, len(a.windows), DefaultPolicy().MaxEventKeys, "expired keys must not grow forever with attacker-chosen methods")
}

type reviewStatusAgent struct {
	fakeAgent
	statusCalls int
}

func (a *reviewStatusAgent) Status() AgentStatus {
	a.statusCalls++
	return AgentStatus{Connected: true}
}
func TestSecurityHardeningAdmissionMustNotPollAgentStatusPerPacket(t *testing.T) {
	agent := &reviewStatusAgent{}
	r := NewRuntime(DefaultPolicy(), &fakeClock{now: time.Unix(100, 0)}, agent, []byte("review-test"))
	for i := 0; i < 4; i++ {
		out, err := r.Admission().Filter(readProps(), []byte("INVITE sip:x@example.com SIP/2.0\r\n"))
		require.NoError(t, err)
		require.Empty(t, out)
	}
	t.Logf("packets=4 subscribers=0 synchronous_status_calls=%d", agent.statusCalls)
	require.Zero(t, agent.statusCalls, "transport filtering must not depend on synchronous agent health RPCs")
}
