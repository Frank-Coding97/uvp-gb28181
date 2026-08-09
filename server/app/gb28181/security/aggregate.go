package security

import (
	"sort"
	"sync"
	"time"
)

type EventAggregate struct {
	BucketAt    time.Time `json:"bucketAt"`
	SourceIP    string    `json:"sourceIp"`
	Transport   string    `json:"transport"`
	Method      string    `json:"method"`
	UserAgent   string    `json:"userAgent"`
	Reason      Reason    `json:"reason"`
	Action      Action    `json:"action"`
	Count       int64     `json:"count"`
	ScoreDelta  int64     `json:"scoreDelta"`
	FirstSeenAt time.Time `json:"firstSeenAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
}

type AggregateStore struct {
	mu      sync.Mutex
	maxKeys int
	items   map[string]EventAggregate
	dropped int64
}

func NewAggregateStore(maxKeys int) *AggregateStore {
	if maxKeys <= 0 {
		maxKeys = DefaultPolicy().MaxEventKeys
	}
	return &AggregateStore{maxKeys: maxKeys, items: make(map[string]EventAggregate)}
}

func (s *AggregateStore) Record(event Event) bool {
	ip, err := ValidateSource(event.SourceIP)
	if err != nil {
		return false
	}
	now := event.Occurred
	if now.IsZero() {
		now = time.Now()
	}
	bucket := now.Truncate(time.Minute)
	key := aggregateKey(bucket, ip.String(), event.Transport, event.Method, event.Reason, event.Action)
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[key]
	if !ok {
		if len(s.items) >= s.maxKeys {
			s.dropped++
			return false
		}
		item = EventAggregate{BucketAt: bucket, SourceIP: ip.String(), Transport: event.Transport, Method: event.Method, UserAgent: event.UserAgent, Reason: event.Reason, Action: event.Action, FirstSeenAt: now}
	}
	item.Count++
	item.ScoreDelta += int64(event.Score)
	item.LastSeenAt = now
	s.items[key] = item
	return true
}

func (s *AggregateStore) Snapshot() []EventAggregate {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]EventAggregate, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].LastSeenAt.Before(items[j].LastSeenAt) })
	return items
}

func (s *AggregateStore) Dropped() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

func (s *AggregateStore) Seed(items []EventAggregate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range items {
		if len(s.items) >= s.maxKeys {
			s.dropped++
			break
		}
		key := aggregateKey(item.BucketAt, item.SourceIP, item.Transport, item.Method, item.Reason, item.Action)
		s.items[key] = item
	}
}

func aggregateKey(bucket time.Time, source, transport, method string, reason Reason, action Action) string {
	return bucket.UTC().Format(time.RFC3339) + "|" + source + "|" + transport + "|" + method + "|" + string(reason) + "|" + string(action)
}

type BanStatus string

const (
	BanActive      BanStatus = "active"
	BanExpired     BanStatus = "expired"
	BanUnbanned    BanStatus = "unbanned"
	BanAgentFailed BanStatus = "agent_failed"
)

type FirewallBan struct {
	Decision             BanDecision `json:"decision"`
	Status               BanStatus   `json:"status"`
	RuleID               string      `json:"ruleId"`
	Origin               string      `json:"origin"`
	AgentState           string      `json:"agentState"`
	UnbannedAt           time.Time   `json:"unbannedAt,omitempty"`
	UnbannedBy           string      `json:"unbannedBy,omitempty"`
	LastError            string      `json:"lastError,omitempty"`
	FirewallAppliedAt    time.Time   `json:"firewallAppliedAt,omitempty"`
	BlockedCountAfterBan int64       `json:"blockedCountAfterBan"`
	LastBlockedAt        time.Time   `json:"lastBlockedAt,omitempty"`
}

type BanStore struct {
	mu    sync.Mutex
	items map[string]FirewallBan
}

func NewBanStore() *BanStore { return &BanStore{items: make(map[string]FirewallBan)} }

func (s *BanStore) Seed(items []FirewallBan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range items {
		current, ok := s.items[item.Decision.SourceIP]
		if !ok || current.Decision.CreatedAt.Before(item.Decision.CreatedAt) {
			s.items[item.Decision.SourceIP] = item
		}
	}
}

func (s *BanStore) Upsert(decision BanDecision, origin string) FirewallBan {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := FirewallBan{Decision: decision, Status: BanActive, RuleID: decision.DecisionID, Origin: origin, AgentState: "pending"}
	if old, ok := s.items[decision.SourceIP]; ok && old.Status == BanActive && old.Decision.CreatedAt.After(decision.CreatedAt) {
		return old
	}
	s.items[decision.SourceIP] = item
	return item
}

func (s *BanStore) MarkApplied(sourceIP string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[sourceIP]
	if !ok {
		return
	}
	item.Status = BanActive
	item.AgentState = "applied"
	item.LastError = ""
	item.FirewallAppliedAt = at
	s.items[sourceIP] = item
}

func (s *BanStore) RecordBlocked(sourceIP string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[sourceIP]
	if !ok || (item.Status != BanActive && item.Status != BanAgentFailed) {
		return
	}
	item.BlockedCountAfterBan++
	item.LastBlockedAt = at
	s.items[sourceIP] = item
}

func (s *BanStore) MarkAgentFailed(sourceIP, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[sourceIP]
	if !ok {
		return
	}
	item.Status = BanAgentFailed
	item.AgentState = "failed"
	item.LastError = message
	s.items[sourceIP] = item
}

func (s *BanStore) Get(sourceIP string, now time.Time) (FirewallBan, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[sourceIP]
	if !ok {
		return FirewallBan{}, false
	}
	if item.Status == BanActive && !item.Decision.ActiveAt(now) {
		item.Status = BanExpired
		s.items[sourceIP] = item
	}
	return item, true
}

func (s *BanStore) Find(identifier string, now time.Time) (FirewallBan, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sourceIP, item := range s.items {
		if sourceIP != identifier && item.Decision.DecisionID != identifier {
			continue
		}
	if item.Status == BanActive && !item.Decision.ActiveAt(now) {
			item.Status = BanExpired
			s.items[sourceIP] = item
		}
		return item, true
	}
	return FirewallBan{}, false
}

func (s *BanStore) Unban(sourceIP, actor string, now time.Time) (FirewallBan, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[sourceIP]
	if !ok {
		return FirewallBan{}, false
	}
	if item.Status != BanUnbanned {
		item.Status = BanUnbanned
		item.UnbannedAt = now
		item.UnbannedBy = actor
		s.items[sourceIP] = item
	}
	return item, true
}

func (s *BanStore) List(now time.Time) []FirewallBan {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]FirewallBan, 0, len(s.items))
	for source := range s.items {
		item := s.items[source]
		if item.Status == BanActive && !item.Decision.ActiveAt(now) {
			item.Status = BanExpired
			s.items[source] = item
		}
		items = append(items, item)
	}
	return items
}
