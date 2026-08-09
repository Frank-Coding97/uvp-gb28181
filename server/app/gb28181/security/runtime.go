package security

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

type RuntimeSnapshot struct {
	Mode    Mode
	Dropped int64
	Sampled int64
	Events  []EventAggregate
	Bans    []FirewallBan
	Agent   AgentStatus
	AsOf    time.Time
}

// Runtime is the application-side composition root for security state. It
// is safe for transport callbacks and controller reads to run concurrently.
type Runtime struct {
	mu             sync.RWMutex
	policy         Policy
	clock          Clock
	admit          *Admission
	scorer         *Scorer
	events         *AggregateStore
	bans           *BanStore
	agent          FirewallAgentClient
	store          Store
	persist        *eventPersister
	persistDropped atomic.Int64
	subsMu         sync.Mutex
	subs           map[chan RuntimeSnapshot]struct{}
}

func NewRuntime(policy Policy, clock Clock, agent FirewallAgentClient, nonceSecret []byte) *Runtime {
	if policy.Validate() != nil {
		policy = DefaultPolicy()
	}
	if clock == nil {
		clock = RealClock()
	}
	r := &Runtime{policy: policy, clock: clock, scorer: NewScorer(policy, clock, nil, nonceSecret), events: NewAggregateStore(policy.MaxEventKeys), bans: NewBanStore(), agent: agent, subs: make(map[chan RuntimeSnapshot]struct{})}
	r.admit = NewAdmission(policy, clock, nil, func(event Event) { _ = r.Record(event) })
	return r
}

// NewPersistentRuntime restores policy and recent state before exposing the
// admission gate. Event writes are aggregated on a bounded background queue so
// scanner traffic cannot turn the database into the next bottleneck.
func NewPersistentRuntime(ctx context.Context, store Store, clock Clock, agent FirewallAgentClient, nonceSecret []byte) (*Runtime, error) {
	if store == nil {
		return nil, errors.New("security store unavailable")
	}
	policy, err := store.LoadPolicy(ctx)
	if err != nil {
		return nil, err
	}
	r := NewRuntime(policy, clock, agent, nonceSecret)
	r.store = store
	if events, loadErr := store.RecentEvents(ctx, 500); loadErr != nil {
		return nil, loadErr
	} else {
		r.events.Seed(events)
	}
	if bans, loadErr := store.RecentBans(ctx, 500, r.clock.Now()); loadErr != nil {
		return nil, loadErr
	} else {
		r.bans.Seed(bans)
	}
	active, err := store.ActiveBans(ctx, r.clock.Now())
	if err != nil {
		return nil, err
	}
	decisions := enforcementDecisions(policy, active, r.clock.Now())
	for _, item := range decisions {
		_ = r.admit.Ban(item.SourceIP, item.CreatedAt.Add(item.TTL))
	}
	if reconciler, ok := agent.(interface{ Reconcile([]BanDecision) error }); ok {
		_ = reconciler.Reconcile(decisions)
	}
	r.persist = newEventPersister(store, r.clock)
	return r, nil
}

func (r *Runtime) Admission() *Admission { return r.admit }
func (r *Runtime) IssueNonce() (string, error) {
	return r.scorer.Nonce().Issue()
}
func (r *Runtime) ValidateNonce(nonce, nonceCount string) error {
	return r.scorer.Nonce().Validate(nonce, nonceCount)
}
func (r *Runtime) TrustEndpoint(deviceID, transport, address string, expires time.Duration) error {
	return r.scorer.UpdateTrustedEndpoint(deviceID, transport, address, r.clock.Now().Add(expires))
}
func (r *Runtime) TrustedEndpoint(deviceID string) (Endpoint, bool) {
	return r.scorer.TrustedEndpoint(deviceID)
}
func (r *Runtime) Record(event Event) error {
	event, decision, err := r.scorer.Observe(event)
	if err != nil {
		return err
	}
	recorded := r.events.Record(event)
	if recorded && r.persist != nil && !r.persist.Enqueue(event) {
		r.persistDropped.Add(1)
	}
	if !recorded && decision == nil {
		return nil
	}
	if decision != nil {
		item := r.bans.Upsert(*decision, "auto")
		_ = r.admit.Ban(decision.SourceIP, decision.CreatedAt.Add(decision.TTL))
		if r.agent != nil {
			if err := r.agent.Ban(*decision); err != nil {
				r.bans.MarkAgentFailed(decision.SourceIP, err.Error())
				if r.store != nil {
					failed, _ := r.bans.Get(decision.SourceIP, r.clock.Now())
					_ = r.store.SaveBan(context.Background(), failed)
				}
				r.publish(r.Snapshot())
				return err
			}
			r.bans.MarkApplied(decision.SourceIP)
			item, _ = r.bans.Get(decision.SourceIP, r.clock.Now())
		} else {
			r.bans.MarkAgentFailed(decision.SourceIP, "agent unavailable")
			item, _ = r.bans.Get(decision.SourceIP, r.clock.Now())
		}
		if r.store != nil {
			if err := r.store.SaveBan(context.Background(), item); err != nil {
				return err
			}
		}
	}
	r.publish(r.Snapshot())
	return nil
}

func (r *Runtime) Snapshot() RuntimeSnapshot {
	r.mu.RLock()
	policy, clock := r.policy, r.clock
	r.mu.RUnlock()
	return RuntimeSnapshot{Mode: policy.Mode, Dropped: r.admit.Dropped() + r.events.Dropped() + r.persistDropped.Load(), Sampled: r.admit.Sampled(), Events: r.events.Snapshot(), Bans: r.bans.List(clock.Now()), Agent: agentStatus(r.agent), AsOf: clock.Now()}
}

func (r *Runtime) Events() []EventAggregate { return r.events.Snapshot() }
func (r *Runtime) Bans() []FirewallBan      { return r.bans.List(r.clock.Now()) }
func (r *Runtime) Policy() Policy           { r.mu.RLock(); defer r.mu.RUnlock(); return r.policy }
func (r *Runtime) UpdatePolicy(policy Policy, actors ...string) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	actor := "system"
	if len(actors) > 0 {
		actor = actorOrSystem(actors[0])
	}
	if r.store != nil {
		if err := r.store.SavePolicy(context.Background(), policy, actor); err != nil {
			return err
		}
	}
	r.mu.Lock()
	r.policy = policy
	r.mu.Unlock()
	r.admit.SetPolicy(policy)
	r.scorer.SetPolicy(policy)
	active := r.bans.List(r.clock.Now())
	decisions := enforcementDecisions(policy, active, r.clock.Now())
	for _, item := range active {
		r.admit.Unban(item.Decision.SourceIP)
	}
	for _, item := range decisions {
		_ = r.admit.Ban(item.SourceIP, item.CreatedAt.Add(item.TTL))
	}
	if reconciler, ok := r.agent.(interface{ Reconcile([]BanDecision) error }); ok {
		if err := reconciler.Reconcile(decisions); err != nil {
			return err
		}
	}
	return nil
}

func enforcementDecisions(policy Policy, bans []FirewallBan, now time.Time) []BanDecision {
	if policy.Mode == ModeObserve {
		return nil
	}
	decisions := make([]BanDecision, 0, len(bans))
	for _, item := range bans {
		if item.Status != BanActive && item.Status != BanAgentFailed {
			continue
		}
		if item.Decision.CreatedAt.Add(item.Decision.TTL).After(now) && !policy.IsAllowlisted(item.Decision.SourceIP) {
			decisions = append(decisions, item.Decision)
		}
	}
	return decisions
}
func (r *Runtime) Unban(identifier, actor string) error {
	item, ok := r.bans.Find(identifier, r.clock.Now())
	if !ok {
		return errors.New("ban not found")
	}
	if r.agent != nil {
		if err := r.agent.Unban(item.Decision.SourceIP); err != nil {
			return err
		}
	}
	if r.store != nil {
		if err := r.store.Unban(context.Background(), item.Decision.SourceIP, actor, r.clock.Now()); err != nil {
			return err
		}
	}
	r.admit.Unban(item.Decision.SourceIP)
	r.bans.Unban(item.Decision.SourceIP, actor, r.clock.Now())
	return nil
}

func (r *Runtime) Close(ctx context.Context) error {
	if r.persist == nil {
		return nil
	}
	return r.persist.Close(ctx)
}
func (r *Runtime) AgentStatus() AgentStatus { return agentStatus(r.agent) }
func (r *Runtime) Stream() (<-chan RuntimeSnapshot, func()) {
	ch := make(chan RuntimeSnapshot, 8)
	r.subsMu.Lock()
	r.subs[ch] = struct{}{}
	r.subsMu.Unlock()
	return ch, func() {
		r.subsMu.Lock()
		if _, ok := r.subs[ch]; ok {
			delete(r.subs, ch)
			close(ch)
		}
		r.subsMu.Unlock()
	}
}
func (r *Runtime) publish(snapshot RuntimeSnapshot) {
	r.subsMu.Lock()
	defer r.subsMu.Unlock()
	for ch := range r.subs {
		select {
		case ch <- snapshot:
		default:
		}
	}
}
func agentStatus(agent FirewallAgentClient) AgentStatus {
	if agent == nil {
		return AgentStatus{Connected: false, LastError: "agent unavailable"}
	}
	return agent.Status()
}
