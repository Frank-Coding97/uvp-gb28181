package security

import (
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNonceInvalid = errors.New("nonce invalid")
	ErrNonceExpired = errors.New("nonce expired")
	ErrNonceReplay  = errors.New("nonce replay")
	ErrNonceStale   = errors.New("nonce stale")
)

type scoreBucket struct {
	started time.Time
	score   int
	count   int
}

type Endpoint struct {
	DeviceID  string
	Transport string
	Address   string
	ExpiresAt time.Time
	UpdatedAt time.Time
}

// Scorer converts security observations into explainable enforcement decisions.
type Scorer struct {
	policy Policy
	clock  Clock
	agent  FirewallAgentClient
	nonce  *NonceManager

	mu              sync.Mutex
	buckets         map[string]scoreBucket
	endpoints       map[string]Endpoint
	decisions       map[string]BanDecision
	invites         map[string][]inviteObservation
	registrations   map[string][]registerObservation
	autoBanDisabled bool
}

func NewScorer(policy Policy, clock Clock, agent FirewallAgentClient, nonceSecret []byte) *Scorer {
	if clock == nil {
		clock = RealClock()
	}
	if policy.Validate() != nil {
		policy = DefaultPolicy()
	}
	return &Scorer{
		policy:        policy,
		clock:         clock,
		agent:         agent,
		nonce:         NewNonceManager(nonceSecret, policy.NonceTTL, clock),
		buckets:       make(map[string]scoreBucket),
		endpoints:     make(map[string]Endpoint),
		decisions:     make(map[string]BanDecision),
		invites:       make(map[string][]inviteObservation),
		registrations: make(map[string][]registerObservation),
	}
}

func (s *Scorer) Nonce() *NonceManager { return s.nonce }
func (s *Scorer) SetPolicy(policy Policy) {
	if policy.Validate() == nil {
		s.mu.Lock()
		s.policy = policy
		s.nonce.SetTTL(policy.NonceTTL)
		s.mu.Unlock()
	}
}

func (s *Scorer) Observe(event Event) (Event, *BanDecision, error) {
	ip, err := ValidateSource(event.SourceIP)
	if err != nil {
		return event, nil, err
	}
	event.SourceIP = ip.String()
	event.RiskScope = riskScopeForEvent(event)
	if event.Occurred.IsZero() {
		event.Occurred = s.clock.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	policy := s.policy
	// Authentication rejection is an observed fact even in observe mode or
	// for allowlisted sources; neither setting bypasses REGISTER Digest.
	alreadyRejected := event.Action == ActionDrop
	if policy.IsAllowlisted(event.SourceIP) {
		if !alreadyRejected {
			event.Action = ActionAllow
		}
		return event, nil, nil
	}
	event.Action = ActionDrop
	if policy.Mode == ModeObserve && !alreadyRejected {
		event.Action = ActionAllow
	}
	sourceKey := scoringBucketKey(Event{SourceIP: event.SourceIP, RiskScope: ScopeSource, Transport: event.Transport, SourceVerified: event.SourceVerified})
	s.prune(event.Occurred, sourceKey, scoringBucketKey(event))
	persistent := false
	if event.Reason == ReasonInviteRate && event.RiskScope == ScopeSource {
		observations := s.invites[sourceKey]
		// SIP retransmissions within the transaction lifetime carry no new risk.
		// An identical probe sent after that lifetime is counted again.
		for _, previous := range observations {
			if event.TransactionID != "" && previous.transaction == event.TransactionID && event.Occurred.Sub(previous.at) < 32*time.Second {
				event.Score = 0
				return event, nil, nil
			}
		}
		observations = append(observations, inviteObservation{at: event.Occurred, transaction: event.TransactionID})
		if len(observations) > persistentInviteThreshold {
			observations = observations[len(observations)-persistentInviteThreshold:]
		}
		s.invites[sourceKey] = observations
		persistent = len(observations) >= persistentInviteThreshold
	}
	registerScan := false
	verifiedRegisterScan := false
	if event.Reason == ReasonRegisterIDInvalid {
		registerScan, verifiedRegisterScan = s.observeInvalidRegister(event)
	}
	delta := reasonScore(event.Reason)
	event.Score = delta
	key := scoringBucketKey(event)
	b := s.buckets[key]
	if b.started.IsZero() || event.Occurred.Sub(b.started) >= policy.Window {
		b = scoreBucket{started: event.Occurred}
	}
	b.score += delta
	b.count++
	s.buckets[key] = b
	total := b.score
	if (persistent || verifiedRegisterScan) && total < policy.BanScore {
		total = policy.BanScore
	}
	if persistent {
		event.Reason = ReasonInvitePersistent
	}
	if registerScan {
		event.Reason = ReasonRegisterEnumeration
	}
	if total < policy.BanScore || policy.Mode == ModeObserve || event.RiskScope != ScopeSource ||
		s.autoBanDisabled || !verifiedEventSource(event) || s.protectedSource(event.SourceIP) {
		return event, nil, nil
	}
	ttl, shouldBan := policy.BanForScore(total)
	if !shouldBan {
		return event, nil, nil
	}
	event.Action = ActionBan
	if previous, exists := s.decisions[event.SourceIP]; exists && previous.ActiveAt(event.Occurred) {
		return event, nil, nil
	}
	decision := BanDecision{
		DecisionID: "ban-" + uuid.NewString(), SourceIP: event.SourceIP, DeviceID: event.DeviceID,
		RiskScope: event.RiskScope, Reason: event.Reason, Score: total, TTL: ttl, Permanent: ttl == 0, CreatedAt: event.Occurred,
		TriggerMethod: strings.ToUpper(event.Method), TriggerCount: b.count,
		TriggerThreshold: triggerThreshold(policy, event.Reason), WindowSeconds: int(policy.Window / time.Second), PolicyMode: policy.Mode,
	}
	if persistent {
		decision.Reason = ReasonInvitePersistent
		event.Reason = ReasonInvitePersistent
		decision.TriggerCount = len(s.invites[sourceKey])
		decision.TriggerThreshold = persistentInviteThreshold
		decision.WindowSeconds = int(persistentInviteWindow / time.Second)
	}
	if verifiedRegisterScan {
		decision.Reason = ReasonRegisterEnumeration
		decision.TriggerCount = len(s.registrations["verified|"+event.SourceIP])
		decision.TriggerThreshold = registerScanThreshold
		decision.WindowSeconds = int(registerScanWindow / time.Second)
	}
	s.decisions[event.SourceIP] = decision
	return event, &decision, nil
}

const persistentInviteWindow = 10 * time.Minute
const persistentInviteThreshold = 10

type inviteObservation struct {
	at          time.Time
	transaction string
}

// prune bounds transient attacker-controlled state; active bans are retained.
func (s *Scorer) prune(now time.Time, source, bucketKey string) {
	for key, b := range s.buckets {
		if now.Sub(b.started) >= s.policy.Window {
			delete(s.buckets, key)
		}
	}
	for source, entries := range s.invites {
		i := 0
		for i < len(entries) && now.Sub(entries[i].at) >= persistentInviteWindow {
			i++
		}
		if i == len(entries) {
			delete(s.invites, source)
		} else {
			s.invites[source] = entries[i:]
		}
	}
	for source, d := range s.decisions {
		if !d.ActiveAt(now) {
			delete(s.decisions, source)
		}
	}
	if _, exists := s.buckets[bucketKey]; !exists && len(s.buckets) >= s.policy.MaxEventKeys {
		var oldest string
		var at time.Time
		for key, b := range s.buckets {
			if at.IsZero() || b.started.Before(at) {
				oldest, at = key, b.started
			}
		}
		delete(s.buckets, oldest)
	}
	if _, exists := s.invites[source]; !exists && len(s.invites) >= s.policy.MaxEventKeys {
		var oldest string
		var at time.Time
		for key, b := range s.invites {
			if at.IsZero() || b[len(b)-1].at.Before(at) {
				oldest, at = key, b[len(b)-1].at
			}
		}
		delete(s.invites, oldest)
	}
}

func (s *Scorer) Unban(source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.decisions, source)
	delete(s.buckets, riskBucketKey(Event{SourceIP: source, RiskScope: ScopeSource}))
	delete(s.invites, source)
	delete(s.invites, "source|"+source)
	delete(s.invites, "verified|source|"+source)
	delete(s.buckets, "verified|source|"+source)
	delete(s.registrations, source)
	delete(s.registrations, "verified|"+source)
}

func (s *Scorer) HasDecision(decision BanDecision) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.autoBanDisabled && !s.protectedSource(decision.SourceIP) && s.decisions[decision.SourceIP].DecisionID == decision.DecisionID
}

func triggerThreshold(policy Policy, reason Reason) int {
	delta := reasonScore(reason)
	if delta <= 0 {
		return policy.BanScore
	}
	return (policy.BanScore + delta - 1) / delta
}

func reasonScore(reason Reason) int {
	switch reason {
	case ReasonNonceReplay:
		return 20
	case ReasonInviteRate:
		return 20
	case ReasonNonceInvalid, ReasonNonceExpired:
		return 30
	case ReasonServerMismatch:
		return 25
	case ReasonDigestFailure:
		return 10
	case ReasonUnregisteredMsg:
		return 5
	case ReasonPacketTooLarge, ReasonConnectionRate, ReasonUnknownMethod:
		return 10
	case ReasonNonceStale, ReasonManualBlacklist, ReasonActiveBan, ReasonRegisterIDInvalid, ReasonRegisterEnumeration:
		return 0
	default:
		return 1
	}
}

func (s *Scorer) UpdateTrustedEndpoint(deviceID, transport, address string, expiresAt time.Time) error {
	if strings.TrimSpace(deviceID) == "" {
		return errors.New("device id required")
	}
	ip, err := ValidateSource(address)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.endpoints[deviceID] = Endpoint{DeviceID: deviceID, Transport: strings.ToUpper(transport), Address: ip.String(), ExpiresAt: expiresAt, UpdatedAt: s.clock.Now()}
	delete(s.buckets, riskBucketKey(Event{SourceIP: ip.String(), DeviceID: deviceID, RiskScope: ScopeDevice}))
	s.mu.Unlock()
	return nil
}

func riskScopeForEvent(event Event) RiskScope {
	if event.Reason == ReasonRegisterIDInvalid || event.Reason == ReasonRegisterEnumeration {
		return ScopeSource
	}
	if strings.TrimSpace(string(event.RiskScope)) == string(ScopeSource) || event.RiskScope == ScopeSource {
		return ScopeSource
	}
	if strings.TrimSpace(event.DeviceID) != "" {
		return ScopeDevice
	}
	return ScopeSource
}

func riskBucketKey(event Event) string {
	scope := riskScopeForEvent(event)
	if scope == ScopeDevice {
		return string(scope) + "|" + strings.TrimSpace(event.DeviceID)
	}
	return string(ScopeSource) + "|" + event.SourceIP
}

func (s *Scorer) TrustedEndpoint(deviceID string) (Endpoint, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.endpoints[deviceID]
	if ok && !e.ExpiresAt.IsZero() && !s.clock.Now().Before(e.ExpiresAt) {
		return Endpoint{}, false
	}
	return e, ok
}

func endpointIP(address string) string {
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return address
}
