package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"sync"
	"time"
)

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

func (m *NonceManager) IssueForSource(sourceIP string) (string, error) {
	ip, err := ValidateSource(sourceIP)
	if err != nil {
		return "", err
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	buf := make([]byte, noncePayloadSize)
	binary.BigEndian.PutUint64(buf[:8], uint64(m.clock.Now().Unix()))
	copy(buf[8:], random)
	m.mu.Lock()
	defer m.mu.Unlock()
	sig := m.signForSource(buf, ip.String())[:nonceMACSize]
	return base64.RawURLEncoding.EncodeToString(append(buf, sig...)), nil
}

func (m *NonceManager) Validate(nonce, nonceCount string) error {
	return m.ValidateForTransaction(nonce, nonceCount, "")
}

func (m *NonceManager) ValidateForTransaction(nonce, nonceCount, transactionFingerprint string) error {
	raw, err := decodeNonce(nonce)
	if err != nil {
		return ErrNonceInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	issued, now, err := m.verifyNonce(raw, "", false)
	if err != nil {
		return err
	}
	return m.consumeNonce(nonce, nonceCount, transactionFingerprint, issued, now)
}

func (m *NonceManager) VerifySource(nonce, sourceIP string) error {
	ip, err := ValidateSource(sourceIP)
	if err != nil {
		return err
	}
	raw, err := decodeNonce(nonce)
	if err != nil {
		return ErrNonceInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, _, err = m.verifyNonce(raw, ip.String(), true)
	return err
}

func (m *NonceManager) ValidateForSourceTransaction(nonce, nonceCount, transactionFingerprint, sourceIP string) error {
	ip, err := ValidateSource(sourceIP)
	if err != nil {
		return err
	}
	raw, err := decodeNonce(nonce)
	if err != nil {
		return ErrNonceInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	issued, now, err := m.verifyNonce(raw, ip.String(), true)
	if err != nil {
		return err
	}
	return m.consumeNonce(nonce, nonceCount, transactionFingerprint, issued, now)
}

func decodeNonce(nonce string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(nonce)
	if err != nil || len(raw) != noncePayloadSize+nonceMACSize || base64.RawURLEncoding.EncodeToString(raw) != nonce {
		return nil, ErrNonceInvalid
	}
	return raw, nil
}

func (m *NonceManager) verifyNonce(raw []byte, sourceIP string, sourceBound bool) (time.Time, time.Time, error) {
	payload, sig := raw[:noncePayloadSize], raw[noncePayloadSize:]
	expected := m.sign(payload)
	if sourceBound {
		expected = m.signForSource(payload, sourceIP)
	}
	if !hmac.Equal(sig, expected[:nonceMACSize]) {
		return time.Time{}, time.Time{}, ErrNonceInvalid
	}
	issued := time.Unix(int64(binary.BigEndian.Uint64(payload[:8])), 0)
	now := m.clock.Now()
	if now.Before(issued) || now.Sub(issued) >= m.ttl {
		return time.Time{}, time.Time{}, ErrNonceExpired
	}
	return issued, now, nil
}

func (m *NonceManager) consumeNonce(nonce, nonceCount, transactionFingerprint string, issued, now time.Time) error {
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

func (m *NonceManager) signForSource(payload []byte, sourceIP string) []byte {
	h := hmac.New(sha256.New, m.secret)
	_, _ = h.Write([]byte("gb28181/nonce/source/v1"))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(sourceIP))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(payload)
	return h.Sum(nil)
}
