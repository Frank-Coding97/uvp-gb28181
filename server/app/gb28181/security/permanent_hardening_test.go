package security

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPermanentBanManualUnbanResetsScores(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	r := NewRuntime(DefaultPolicy(), clock, &fakeAgent{}, []byte("test"))
	for i := 0; i < 5; i++ {
		require.NoError(t, r.Record(Event{Transport: "TCP", SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonInviteRate}))
	}
	first := r.Bans()[0].Decision
	require.True(t, first.Permanent)
	clock.now = clock.now.Add(365 * 24 * time.Hour)
	require.True(t, r.Admission().IsBanned(first.SourceIP))
	require.NoError(t, r.Unban(first.SourceIP, "operator"))
	require.False(t, r.Admission().IsBanned(first.SourceIP))
	for i := 0; i < 4; i++ {
		require.NoError(t, r.Record(Event{Transport: "TCP", SourceIP: first.SourceIP, Method: "INVITE", Reason: ReasonInviteRate}))
		require.False(t, r.Admission().IsBanned(first.SourceIP))
	}
	require.NoError(t, r.Record(Event{Transport: "TCP", SourceIP: first.SourceIP, Method: "INVITE", Reason: ReasonInviteRate}))
	require.True(t, r.Admission().IsBanned(first.SourceIP))
	require.NotEqual(t, first.DecisionID, r.Bans()[0].Decision.DecisionID)
}
func TestPermanentBanRestoresAndLegacyBanKeepsExpiry(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	store := NewGormStore(newSecurityStoreTestDB(t))
	agent := &fakeAgent{}
	r := NewRuntime(DefaultPolicy(), clock, agent, []byte("test"))
	r.store = store
	for i := 0; i < 5; i++ {
		require.NoError(t, r.Record(Event{Transport: "TCP", SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonInviteRate}))
	}
	legacy := FirewallBan{Decision: BanDecision{DecisionID: "legacy", SourceIP: "198.51.100.11", CreatedAt: clock.now, TTL: time.Hour}, Status: BanActive, Origin: "auto"}
	require.NoError(t, store.SaveBan(context.Background(), legacy))
	restored, err := NewPersistentRuntime(context.Background(), store, clock, agent, []byte("test"))
	require.NoError(t, err)
	defer restored.Close(context.Background())
	require.True(t, restored.Admission().IsBanned("198.51.100.10"))
	item, ok := restored.bans.Get("198.51.100.11", clock.now)
	require.True(t, ok)
	require.False(t, item.Decision.Permanent)
	require.Equal(t, legacy.Decision.ExpiresAt().Unix(), item.Decision.ExpiresAt().Unix())
}

type failBanStore struct {
	Store
	fail bool
}

func (s *failBanStore) SaveBan(ctx context.Context, b FirewallBan) error {
	if s.fail {
		return fmt.Errorf("database unavailable")
	}
	return s.Store.SaveBan(ctx, b)
}
func TestBanPersistenceFailureRetriesBeforeEnforcement(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	agent := &fakeAgent{}
	store := &failBanStore{Store: NewGormStore(newSecurityStoreTestDB(t)), fail: true}
	r := NewRuntime(DefaultPolicy(), clock, agent, []byte("test"))
	r.store = store
	for i := 0; i < 4; i++ {
		require.NoError(t, r.Record(Event{Transport: "TCP", SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonInviteRate}))
	}
	require.Error(t, r.Record(Event{Transport: "TCP", SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonInviteRate}))
	require.Empty(t, agent.banCalls)
	require.True(t, r.Admission().IsBanned("198.51.100.10"))
	store.fail = false
	require.NoError(t, r.reconcileAgent())
	require.Len(t, agent.reconcileCalls, 1)
	require.Len(t, agent.reconcileCalls[0], 1)
	active, err := store.ActiveBans(context.Background(), clock.now)
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.True(t, active[0].Decision.Permanent)
	require.NoError(t, r.Unban("198.51.100.10", "operator"))
	require.NoError(t, r.reconcileAgent())
	require.Empty(t, agent.reconcileCalls[len(agent.reconcileCalls)-1])
}
func TestPersisterBacklogIsBoundedOnDatabaseFailure(t *testing.T) {
	p := &eventPersister{clock: &fakeClock{now: time.Now()}}
	pending := map[string]EventAggregate{}
	for i := 0; i < securityEventQueueCapacity+50; i++ {
		p.aggregate(pending, Event{Transport: "TCP", SourceIP: "198.51.100.10", Method: fmt.Sprint(i)})
	}
	require.Len(t, pending, securityEventQueueCapacity)
	require.EqualValues(t, 50, p.dropped.Load())
}

func TestRepeatPermanentBanAtSameTimestampRestoresNewestActiveDecision(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	store := NewGormStore(newSecurityStoreTestDB(t))
	agent := &fakeAgent{}
	r := NewRuntime(DefaultPolicy(), clock, agent, []byte("test"))
	r.store = store
	probe := Event{Transport: "TCP", SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonInviteRate}
	for i := 0; i < 5; i++ {
		require.NoError(t, r.Record(probe))
	}
	first := r.Bans()[0].Decision.DecisionID
	require.NoError(t, r.Unban(probe.SourceIP, "operator"))
	for i := 0; i < 5; i++ {
		require.NoError(t, r.Record(probe))
	}
	second := r.Bans()[0].Decision.DecisionID
	require.NotEqual(t, first, second)
	restored, err := NewPersistentRuntime(context.Background(), store, clock, agent, []byte("test"))
	require.NoError(t, err)
	defer restored.Close(context.Background())
	item, ok := restored.bans.Get(probe.SourceIP, clock.now)
	require.True(t, ok)
	require.Equal(t, second, item.Decision.DecisionID)
	require.Equal(t, BanActive, item.Status)
	history, err := store.RecentBans(context.Background(), 10, clock.now)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, second, history[0].Decision.DecisionID)
	require.Equal(t, BanUnbanned, history[1].Status)
}

func TestRuntimeAdmissionLowRateInviteAndTrustedDevice(t *testing.T) {
	for _, transport := range []string{"UDP", "TCP"} {
		for _, interval := range []time.Duration{20 * time.Second, 30 * time.Second} {
			t.Run(transport+interval.String(), func(t *testing.T) {
				clock := &fakeClock{now: time.Unix(100, 0)}
				r := NewRuntime(DefaultPolicy(), clock, &fakeAgent{}, []byte("test"))
				props := readProps()
				props.Transport = transport
				for i := 0; i < 10; i++ {
					packet := admissionSIPFrame(transport, "INVITE", "unregistered", fmt.Sprint(i), "")
					out, err := r.Admission().Filter(props, packet)
					require.NoError(t, err)
					require.Empty(t, out)
					require.Equal(t, transport == "TCP" && i == 9, r.Admission().IsBanned("198.51.100.10"))
					clock.now = clock.now.Add(interval)
				}
				if transport == "TCP" {
					require.Equal(t, ReasonInvitePersistent, r.Bans()[0].Decision.Reason)
					require.True(t, r.Bans()[0].Decision.Permanent)
				} else {
					require.Empty(t, r.Bans())
				}
			})
		}
	}
	clock := &fakeClock{now: time.Unix(100, 0)}
	r := NewRuntime(DefaultPolicy(), clock, &fakeAgent{}, []byte("test"))
	require.NoError(t, r.TrustEndpoint("registered", "UDP", "198.51.100.10", time.Hour))
	for i := 0; i < 20; i++ {
		packet := admissionSIPFrame("UDP", "INVITE", "registered", fmt.Sprint(i), "")
		out, err := r.Admission().Filter(readProps(), packet)
		require.NoError(t, err)
		require.Equal(t, packet, out)
		clock.now = clock.now.Add(30 * time.Second)
	}
	require.Empty(t, r.Bans())
	require.Empty(t, r.scorer.invites)
}

type blockingReconcileAgent struct {
	fakeAgent
	entered chan struct{}
	release chan struct{}
	block   bool
}

func (a *blockingReconcileAgent) Reconcile(ds []BanDecision) error {
	if a.block {
		close(a.entered)
		<-a.release
	}
	return a.fakeAgent.Reconcile(ds)
}
func TestManualUnbanSerializesWithPendingAgentReconcile(t *testing.T) {
	agent := &blockingReconcileAgent{entered: make(chan struct{}), release: make(chan struct{}), block: true}
	r := NewRuntime(DefaultPolicy(), RealClock(), agent, []byte("test"))
	for i := 0; i < 5; i++ {
		require.NoError(t, r.Record(Event{Transport: "TCP", SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonInviteRate}))
	}
	syncDone := make(chan error, 1)
	go func() { syncDone <- r.reconcileAgent() }()
	<-agent.entered
	unbanDone := make(chan error, 1)
	go func() { unbanDone <- r.Unban("198.51.100.10", "operator") }()
	close(agent.release)
	require.NoError(t, <-syncDone)
	require.NoError(t, <-unbanDone)
	agent.block = false
	require.NoError(t, r.reconcileAgent())
	require.Empty(t, agent.reconcileCalls[len(agent.reconcileCalls)-1])
	require.False(t, r.Admission().IsBanned("198.51.100.10"))
	require.Empty(t, r.scorer.invites)
}

func TestPolicyUpdatePreservesTCPFrameForPreviouslyUnbannedSource(t *testing.T) {
	r := NewRuntime(DefaultPolicy(), RealClock(), &fakeAgent{}, []byte("test"))
	for i := 0; i < 5; i++ {
		require.NoError(t, r.Record(Event{Transport: "TCP", SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonInviteRate}))
	}
	require.NoError(t, r.Unban("198.51.100.10", "operator"))
	props := admissionTCPProps(5060)
	packet := admissionSIPFrame("TCP", "REGISTER", "registered", "register-call", "")
	out, err := r.Admission().Filter(props, packet[:12])
	require.NoError(t, err)
	require.Empty(t, out)
	p := r.Policy()
	p.NonceTTL += time.Second
	require.NoError(t, r.UpdatePolicy(p))
	out, err = r.Admission().Filter(props, packet[12:])
	require.NoError(t, err)
	require.Equal(t, packet, out)
}
