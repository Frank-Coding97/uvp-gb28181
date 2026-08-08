package security

import (
	"errors"
	"sync"
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
	mu     sync.RWMutex
	policy Policy
	clock  Clock
	admit  *Admission
	scorer *Scorer
	events *AggregateStore
	bans   *BanStore
	agent  FirewallAgentClient
	subsMu sync.Mutex
	subs   map[chan RuntimeSnapshot]struct{}
}

func NewRuntime(policy Policy, clock Clock, agent FirewallAgentClient, nonceSecret []byte) *Runtime {
	if policy.Validate() != nil {
		policy = DefaultPolicy()
	}
	if clock == nil {
		clock = RealClock()
	}
	r := &Runtime{policy: policy, clock: clock, scorer: NewScorer(policy, clock, agent, nonceSecret), events: NewAggregateStore(policy.MaxEventKeys), bans: NewBanStore(), agent: agent, subs: make(map[chan RuntimeSnapshot]struct{})}
	r.admit = NewAdmission(policy, clock, nil, func(event Event) { r.events.Record(event) })
	return r
}

func (r *Runtime) Admission() *Admission { return r.admit }
func (r *Runtime) Record(event Event) error {
	event, decision, err := r.scorer.Observe(event)
	if err != nil {
		return err
	}
	if !r.events.Record(event) {
		return nil
	}
	if decision != nil {
		r.bans.Upsert(*decision, "auto")
		_ = r.admit.Ban(decision.SourceIP, decision.CreatedAt.Add(decision.TTL))
	}
	r.publish(r.Snapshot())
	return nil
}

func (r *Runtime) Snapshot() RuntimeSnapshot {
	r.mu.RLock()
	policy, clock := r.policy, r.clock
	r.mu.RUnlock()
	return RuntimeSnapshot{Mode: policy.Mode, Dropped: r.admit.Dropped(), Sampled: r.admit.Sampled(), Events: r.events.Snapshot(), Bans: r.bans.List(clock.Now()), Agent: agentStatus(r.agent), AsOf: clock.Now()}
}

func (r *Runtime) Events() []EventAggregate { return r.events.Snapshot() }
func (r *Runtime) Bans() []FirewallBan      { return r.bans.List(r.clock.Now()) }
func (r *Runtime) Policy() Policy           { r.mu.RLock(); defer r.mu.RUnlock(); return r.policy }
func (r *Runtime) UpdatePolicy(policy Policy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	r.policy = policy
	r.mu.Unlock()
	r.admit.SetPolicy(policy)
	r.scorer.SetPolicy(policy)
	return nil
}
func (r *Runtime) Unban(sourceIP, actor string) error {
	item, ok := r.bans.Unban(sourceIP, actor, r.clock.Now())
	if !ok {
		return errors.New("ban not found")
	}
	r.admit.Unban(item.Decision.SourceIP)
	if r.agent != nil {
		return r.agent.Unban(item.Decision.SourceIP)
	}
	return nil
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
