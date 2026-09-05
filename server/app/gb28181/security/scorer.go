package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
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

	mu        sync.Mutex
	buckets   map[string]scoreBucket
	endpoints map[string]Endpoint
	decisions map[string]BanDecision
	invites   map[string][]inviteObservation
}

func NewScorer(policy Policy, clock Clock, agent FirewallAgentClient, nonceSecret []byte) *Scorer {
	if clock == nil {
		clock = RealClock()
	}
	if policy.Validate() != nil {
		policy = DefaultPolicy()
	}
	return &Scorer{
		policy:    policy,
		clock:     clock,
		agent:     agent,
		nonce:     NewNonceManager(nonceSecret, policy.NonceTTL, clock),
		buckets:   make(map[string]scoreBucket),
		endpoints: make(map[string]Endpoint),
		decisions: make(map[string]BanDecision),
		invites:   make(map[string][]inviteObservation),
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
	if policy.IsAllowlisted(event.SourceIP) {
		event.Action = ActionAllow
		return event, nil, nil
	}
	event.Action = ActionDrop
	if policy.Mode == ModeObserve {
		event.Action = ActionAllow
	}
	s.prune(event.Occurred, event.SourceIP, riskBucketKey(event))
	persistent := false
	if event.Reason == ReasonInviteRate && event.RiskScope == ScopeSource {
		observations := s.invites[event.SourceIP]
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
		s.invites[event.SourceIP] = observations
		persistent = len(observations) >= persistentInviteThreshold
	}
	delta := reasonScore(event.Reason)
	event.Score = delta
	key := riskBucketKey(event)
	b := s.buckets[key]
	if b.started.IsZero() || event.Occurred.Sub(b.started) >= policy.Window {
		b = scoreBucket{started: event.Occurred}
	}
	b.score += delta
	b.count++
	s.buckets[key] = b
	total := b.score
	if persistent && total < policy.BanScore {
		total = policy.BanScore
	}
	if total < policy.BanScore || policy.Mode == ModeObserve || event.RiskScope != ScopeSource {
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
		decision.TriggerCount = len(s.invites[event.SourceIP])
		decision.TriggerThreshold = persistentInviteThreshold
		decision.WindowSeconds = int(persistentInviteWindow / time.Second)
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
}

func (s *Scorer) HasDecision(decision BanDecision) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.decisions[decision.SourceIP].DecisionID == decision.DecisionID
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
	case ReasonNonceStale, ReasonManualBlacklist, ReasonActiveBan:
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
		delete(s.endpoints, deviceID)
		return Endpoint{}, false
	}
	return e, ok
}

// NonceManager issues signed, short-lived and single-use nonces for SIP Digest.
type NonceManager struct {
	secret []byte
	ttl    time.Duration
	clock  Clock
	mu     sync.Mutex
	used   map[string]nonceUse
}

type nonceUse struct {
	transactionFingerprint string
	expiresAt              time.Time
}

const (
	noncePayloadSize = 24
	nonceMACSize     = 16
)

func NewNonceManager(secret []byte, ttl time.Duration, clock Clock) *NonceManager {
	if clock == nil {
		clock = RealClock()
	}
	if len(secret) == 0 {
		secret = make([]byte, 32)
		_, _ = rand.Read(secret)
	}
	return &NonceManager{secret: append([]byte(nil), secret...), ttl: ttl, clock: clock, used: make(map[string]nonceUse)}
}

func (m *NonceManager) SetTTL(ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ttl != ttl {
		// Retire outstanding challenges on a TTL change. Otherwise increasing
		// TTL can revive consumed nonces whose replay entries were pruned.
		m.secret = m.sign([]byte("nonce-policy-change"))
		clear(m.used)
		m.ttl = ttl
	}
}

func (m *NonceManager) Issue() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	buf := make([]byte, noncePayloadSize)
	binary.BigEndian.PutUint64(buf[:8], uint64(m.clock.Now().Unix()))
	copy(buf[8:], random)
	m.mu.Lock()
	defer m.mu.Unlock()
	// A 128-bit HMAC tag keeps the nonce below legacy devices' 64-byte limit.
	sig := m.sign(buf)[:nonceMACSize]
	return base64.RawURLEncoding.EncodeToString(append(buf, sig...)), nil
}

func (m *NonceManager) Validate(nonce, nonceCount string) error {
	return m.ValidateForTransaction(nonce, nonceCount, "")
}

func (m *NonceManager) ValidateForTransaction(nonce, nonceCount, transactionFingerprint string) error {
	raw, err := base64.RawURLEncoding.DecodeString(nonce)
	if err != nil || len(raw) != noncePayloadSize+nonceMACSize || base64.RawURLEncoding.EncodeToString(raw) != nonce {
		return ErrNonceInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	payload, sig := raw[:noncePayloadSize], raw[noncePayloadSize:]
	if !hmac.Equal(sig, m.sign(payload)[:nonceMACSize]) {
		return ErrNonceInvalid
	}
	issued := time.Unix(int64(binary.BigEndian.Uint64(payload[:8])), 0)
	now := m.clock.Now()
	if now.Before(issued) || now.Sub(issued) >= m.ttl {
		return ErrNonceExpired
	}
	key := nonce + "|" + strings.TrimSpace(nonceCount)
	for usedKey, use := range m.used {
		if !use.expiresAt.After(now) {
			delete(m.used, usedKey)
		}
	}
	if use, exists := m.used[key]; exists {
		if transactionFingerprint != "" && hmac.Equal([]byte(use.transactionFingerprint), []byte(transactionFingerprint)) {
			return nil
		}
		return ErrNonceReplay
	}
	m.used[key] = nonceUse{transactionFingerprint: transactionFingerprint, expiresAt: issued.Add(m.ttl)}
	return nil
}

func (m *NonceManager) sign(payload []byte) []byte {
	h := hmac.New(sha256.New, m.secret)
	_, _ = h.Write(payload)
	return h.Sum(nil)
}

func endpointIP(address string) string {
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return address
}
