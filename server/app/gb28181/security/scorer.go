package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
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
	seq       uint64
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
	}
}

func (s *Scorer) Nonce() *NonceManager { return s.nonce }
func (s *Scorer) SetPolicy(policy Policy) {
	if policy.Validate() == nil {
		s.mu.Lock()
		s.policy = policy
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
	policy := s.policy
	s.mu.Unlock()
	if policy.IsAllowlisted(event.SourceIP) {
		event.Action = ActionAllow
		return event, nil, nil
	}
	delta := reasonScore(event.Reason)
	event.Score = delta
	event.Action = ActionAllow
	if policy.Mode != ModeObserve {
		event.Action = ActionDrop
	}

	// Aggregate a source across reasons within the same window so a mixed
	// sequence of mismatch, digest and replay signals reaches a threshold.
	key := riskBucketKey(event)
	s.mu.Lock()
	b := s.buckets[key]
	if b.started.IsZero() || event.Occurred.Sub(b.started) >= policy.Window {
		b = scoreBucket{started: event.Occurred}
	}
	b.score += delta
	b.count++
	s.buckets[key] = b
	total := b.score
	s.mu.Unlock()

	if total < policy.BanScore || policy.Mode == ModeObserve || event.RiskScope != ScopeSource {
		return event, nil, nil
	}

	ttl, shouldBan := policy.BanForScore(total)
	if !shouldBan {
		return event, nil, nil
	}
	decision := BanDecision{
		DecisionID:       s.nextDecisionID(event.SourceIP, event.Occurred),
		SourceIP:         event.SourceIP,
		DeviceID:         event.DeviceID,
		RiskScope:        event.RiskScope,
		Reason:           event.Reason,
		Score:            total,
		TTL:              ttl,
		Permanent:        ttl == 0,
		CreatedAt:        event.Occurred,
		TriggerMethod:    strings.ToUpper(event.Method),
		TriggerCount:     b.count,
		TriggerThreshold: triggerThreshold(policy, event.Reason),
		WindowSeconds:    int(policy.Window / time.Second),
		PolicyMode:       policy.Mode,
	}
	event.Action = ActionBan
	s.mu.Lock()
	if previous, exists := s.decisions[event.SourceIP]; exists {
		if previous.ActiveAt(event.Occurred) {
			s.mu.Unlock()
			return event, nil, nil
		}
		delete(s.decisions, event.SourceIP)
	}
	s.decisions[event.SourceIP] = decision
	s.mu.Unlock()
	return event, &decision, nil
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

func (s *Scorer) nextDecisionID(source string, now time.Time) string {
	s.mu.Lock()
	s.seq++
	seq := s.seq
	s.mu.Unlock()
	return fmt.Sprintf("ban-%x-%d", sha256.Sum256([]byte(source)), seq)[:28]
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

func (m *NonceManager) Issue() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	buf := make([]byte, 24)
	binary.BigEndian.PutUint64(buf[:8], uint64(m.clock.Now().Unix()))
	copy(buf[8:], random)
	sig := m.sign(buf)
	return base64.RawURLEncoding.EncodeToString(append(buf, sig...)), nil
}

func (m *NonceManager) Validate(nonce, nonceCount string) error {
	return m.ValidateForTransaction(nonce, nonceCount, "")
}

func (m *NonceManager) ValidateForTransaction(nonce, nonceCount, transactionFingerprint string) error {
	raw, err := base64.RawURLEncoding.DecodeString(nonce)
	if err != nil || len(raw) != 56 {
		return ErrNonceInvalid
	}
	payload, sig := raw[:24], raw[24:]
	if !hmac.Equal(sig, m.sign(payload)) {
		return ErrNonceInvalid
	}
	issued := time.Unix(int64(binary.BigEndian.Uint64(payload[:8])), 0)
	now := m.clock.Now()
	if now.Before(issued) || now.Sub(issued) > m.ttl {
		return ErrNonceExpired
	}
	key := nonce + "|" + strings.TrimSpace(nonceCount)
	m.mu.Lock()
	defer m.mu.Unlock()
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
