package playauth

import (
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	DefaultAuthorizationRegistryCapacity = 4096
	MinimumAuthorizationStartLifetime    = 30 * time.Second
)

var (
	ErrAuthorizationRegistryUnavailable = errors.New("play authorization registry unavailable")
	ErrAuthorizationRegistryFull        = errors.New("play authorization registry full")
	ErrAuthorizationNotFound            = errors.New("play authorization not found")
	ErrAuthorizationExpired             = errors.New("play authorization expired")
	ErrAuthorizationTerminal            = errors.New("play authorization terminal")
	ErrAuthorizationAlreadyBound        = errors.New("play authorization already bound")
	ErrAuthorizationClaimsMismatch      = errors.New("play authorization claims mismatch")
	ErrAuthorizationLifetimeTooShort    = errors.New("play authorization lifetime too short")
)

type AuthorizationState uint8

const (
	AuthorizationUnbound AuthorizationState = iota + 1
	AuthorizationBound
	AuthorizationTerminal
)

// AutoStartVerifier separates the pre-media check from the regular playback
// verifier. Only an unbound authorization can enter automatic start.
type AutoStartVerifier interface {
	VerifyForAutoStart(string, Binding) (Claims, error)
}

type AuthorizationRegistryOption func(*AuthorizationRegistry)

func WithAuthorizationRegistryCapacity(capacity int) AuthorizationRegistryOption {
	return func(registry *AuthorizationRegistry) {
		if capacity >= 0 && capacity <= DefaultAuthorizationRegistryCapacity {
			registry.capacity = capacity
		}
	}
}

func WithAuthorizationRegistryNow(now func() time.Time) AuthorizationRegistryOption {
	return func(registry *AuthorizationRegistry) {
		if now != nil {
			registry.now = now
		}
	}
}

type authorizationBinding struct {
	deviceID        string
	channelID       string
	app             string
	stream          string
	mediaServerID   string
	mediaGeneration uint64
}

type authorizationRecord struct {
	binding         authorizationBinding
	issuedAt        time.Time
	expiresAt       time.Time
	nonce           string
	state           AuthorizationState
	mediaGeneration uint64
}

// AuthorizationRegistry is intentionally in-memory. A restart therefore
// invalidates all outstanding tokens rather than accepting untracked grants.
type AuthorizationRegistry struct {
	mu               sync.Mutex
	capacity         int
	now              func() time.Time
	records          map[string]authorizationRecord
	mediaGenerations map[uint64]map[string]struct{}
}

func NewAuthorizationRegistry(opts ...AuthorizationRegistryOption) *AuthorizationRegistry {
	registry := &AuthorizationRegistry{
		capacity:         DefaultAuthorizationRegistryCapacity,
		now:              time.Now,
		records:          make(map[string]authorizationRecord),
		mediaGenerations: make(map[uint64]map[string]struct{}),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(registry)
		}
	}
	return registry
}

// Register records a token before it is exposed to a caller. A zero media
// generation represents a cold-stream authorization; non-zero is already
// bound to the current live generation.
func (r *AuthorizationRegistry) Register(prepared Prepared, binding Binding) error {
	if r == nil {
		return ErrAuthorizationRegistryUnavailable
	}
	now := r.currentTime()
	if !validBinding(binding) || strings.TrimSpace(prepared.Nonce) == "" ||
		strings.TrimSpace(prepared.AuthorizationGeneration) == "" || prepared.IssuedAt.IsZero() ||
		!prepared.ExpiresAt.After(prepared.IssuedAt) || prepared.ExpiresAt.Sub(now) < MinimumAuthorizationStartLifetime {
		return ErrAuthorizationLifetimeTooShort
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked(now)
	if _, exists := r.records[prepared.AuthorizationGeneration]; exists {
		return ErrAuthorizationClaimsMismatch
	}
	if r.capacity <= 0 || len(r.records) >= r.capacity {
		return ErrAuthorizationRegistryFull
	}
	record := authorizationRecord{
		binding:         resourceBinding(binding),
		issuedAt:        prepared.IssuedAt.UTC(),
		expiresAt:       prepared.ExpiresAt.UTC(),
		nonce:           prepared.Nonce,
		state:           AuthorizationUnbound,
		mediaGeneration: 0,
	}
	if binding.MediaGeneration != 0 {
		record.state = AuthorizationBound
		record.mediaGeneration = binding.MediaGeneration
		r.addGenerationLocked(binding.MediaGeneration, prepared.AuthorizationGeneration)
	}
	r.records[prepared.AuthorizationGeneration] = record
	return nil
}

// BindAuthorization atomically claims an unbound authorization for one media
// generation. Repeating the same binding is harmless; changing generation is
// rejected so a token cannot be replayed to start another stream.
func (r *AuthorizationRegistry) BindAuthorization(authorizationGeneration string, mediaGeneration uint64) error {
	if r == nil {
		return ErrAuthorizationRegistryUnavailable
	}
	if strings.TrimSpace(authorizationGeneration) == "" || mediaGeneration == 0 {
		return ErrAuthorizationClaimsMismatch
	}
	now := r.currentTime()
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[authorizationGeneration]
	if !ok {
		return ErrAuthorizationNotFound
	}
	if !now.Before(record.expiresAt) {
		return ErrAuthorizationExpired
	}
	switch record.state {
	case AuthorizationUnbound:
		record.state = AuthorizationBound
		record.mediaGeneration = mediaGeneration
		r.records[authorizationGeneration] = record
		r.addGenerationLocked(mediaGeneration, authorizationGeneration)
		return nil
	case AuthorizationBound:
		if record.mediaGeneration == mediaGeneration {
			return nil
		}
		return ErrAuthorizationAlreadyBound
	case AuthorizationTerminal:
		return ErrAuthorizationTerminal
	default:
		return ErrAuthorizationClaimsMismatch
	}
}

// TerminateMediaGeneration prevents every authorization attached to the
// finished generation from being used again. Terminal records remain until the
// next bounded cleanup so callers can distinguish terminal from unknown.
func (r *AuthorizationRegistry) TerminateMediaGeneration(mediaGeneration uint64) int {
	if r == nil || mediaGeneration == 0 {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := r.mediaGenerations[mediaGeneration]
	terminated := 0
	for key := range keys {
		record, ok := r.records[key]
		if !ok || record.state != AuthorizationBound || record.mediaGeneration != mediaGeneration {
			continue
		}
		record.state = AuthorizationTerminal
		r.records[key] = record
		terminated++
	}
	delete(r.mediaGenerations, mediaGeneration)
	return terminated
}

// Expire removes expired and terminal entries. Work is bounded by the hard
// registry capacity, including when callers configured a smaller capacity.
func (r *AuthorizationRegistry) Expire() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.expireLocked(r.currentTime())
}

func (r *AuthorizationRegistry) Size() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.records)
}

func (r *AuthorizationRegistry) verify(claims Claims, expected Binding, requireUnbound bool) error {
	return r.verifyWithLifetime(claims, expected, requireUnbound, requireUnbound)
}

func (r *AuthorizationRegistry) verifyUnbound(claims Claims, expected Binding) error {
	return r.verifyWithLifetime(claims, expected, true, false)
}

func (r *AuthorizationRegistry) verifyWithLifetime(claims Claims, expected Binding, requireUnbound, requireStartLifetime bool) error {
	if r == nil {
		return ErrAuthorizationRegistryUnavailable
	}
	if !validBinding(expected) || strings.TrimSpace(claims.AuthorizationGeneration) == "" {
		return ErrAuthorizationClaimsMismatch
	}
	now := r.currentTime()
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[claims.AuthorizationGeneration]
	if !ok {
		return ErrAuthorizationNotFound
	}
	if record.state == AuthorizationTerminal {
		return ErrAuthorizationTerminal
	}
	if !now.Before(record.expiresAt) {
		return ErrAuthorizationExpired
	}
	if !record.matchesClaims(claims) || !record.binding.sameResource(expected) {
		return ErrAuthorizationClaimsMismatch
	}
	if requireUnbound {
		if record.state != AuthorizationUnbound || expected.MediaGeneration != 0 {
			return ErrAuthorizationAlreadyBound
		}
		if requireStartLifetime && record.expiresAt.Sub(now) < MinimumAuthorizationStartLifetime {
			return ErrAuthorizationLifetimeTooShort
		}
		return nil
	}
	if record.state == AuthorizationUnbound {
		if expected.MediaGeneration != 0 {
			return ErrAuthorizationClaimsMismatch
		}
		return nil
	}
	if record.state != AuthorizationBound || expected.MediaGeneration != record.mediaGeneration {
		return ErrAuthorizationClaimsMismatch
	}
	return nil
}

func (r *AuthorizationRegistry) currentTime() time.Time {
	if r.now == nil {
		return time.Now().UTC()
	}
	return r.now().UTC()
}

func (r *AuthorizationRegistry) expireLocked(now time.Time) int {
	removed := 0
	for key, record := range r.records {
		if record.state != AuthorizationTerminal && now.Before(record.expiresAt) {
			continue
		}
		r.removeLocked(key, record)
		removed++
	}
	return removed
}

func (r *AuthorizationRegistry) addGenerationLocked(mediaGeneration uint64, key string) {
	keys := r.mediaGenerations[mediaGeneration]
	if keys == nil {
		keys = make(map[string]struct{})
		r.mediaGenerations[mediaGeneration] = keys
	}
	keys[key] = struct{}{}
}

func (r *AuthorizationRegistry) removeLocked(key string, record authorizationRecord) {
	delete(r.records, key)
	if record.state != AuthorizationBound || record.mediaGeneration == 0 {
		return
	}
	keys := r.mediaGenerations[record.mediaGeneration]
	delete(keys, key)
	if len(keys) == 0 {
		delete(r.mediaGenerations, record.mediaGeneration)
	}
}

func resourceBinding(binding Binding) authorizationBinding {
	return authorizationBinding{
		deviceID: binding.DeviceID, channelID: binding.ChannelID, app: binding.App,
		stream: binding.Stream, mediaServerID: binding.MediaServerID,
		mediaGeneration: binding.MediaGeneration,
	}
}

func (b authorizationBinding) sameResource(binding Binding) bool {
	return b.deviceID == binding.DeviceID && b.channelID == binding.ChannelID && b.app == binding.App &&
		b.stream == binding.Stream && b.mediaServerID == binding.MediaServerID
}

func (r authorizationRecord) matchesClaims(claims Claims) bool {
	return r.nonce == claims.Nonce && r.issuedAt.Unix() == claims.IssuedAt && r.expiresAt.Unix() == claims.ExpiresAt &&
		r.binding.deviceID == claims.DeviceID && r.binding.channelID == claims.ChannelID && r.binding.app == claims.App &&
		r.binding.stream == claims.Stream && r.binding.mediaServerID == claims.MediaServerID &&
		r.binding.mediaGeneration == claims.MediaGeneration
}

// AuthorizationService combines cryptographic token validation with the
// process-local lifecycle registry. A valid HMAC alone is deliberately not a
// valid authorization after a restart.
type AuthorizationService struct {
	signer   *Signer
	registry *AuthorizationRegistry
}

func NewAuthorizationService(signer *Signer, registry *AuthorizationRegistry) *AuthorizationService {
	return &AuthorizationService{signer: signer, registry: registry}
}

func (s *AuthorizationService) Prepare() (Prepared, error) {
	if s == nil || s.signer == nil {
		return Prepared{}, ErrAuthorizationRegistryUnavailable
	}
	return s.signer.Prepare()
}

func (s *AuthorizationService) Bind(prepared Prepared, binding Binding) (Grant, error) {
	if s == nil || s.signer == nil || s.registry == nil {
		return Grant{}, ErrAuthorizationRegistryUnavailable
	}
	grant, err := s.signer.Bind(prepared, binding)
	if err != nil {
		return Grant{}, err
	}
	if err := s.registry.Register(prepared, binding); err != nil {
		return Grant{}, err
	}
	return grant, nil
}

func (s *AuthorizationService) IssueDirect(binding Binding) (Grant, error) {
	prepared, err := s.Prepare()
	if err != nil {
		return Grant{}, err
	}
	return s.Bind(prepared, binding)
}

func (s *AuthorizationService) Verify(token string, expected Binding) (Claims, error) {
	if s == nil || s.signer == nil || s.registry == nil {
		return Claims{}, ErrAuthorizationRegistryUnavailable
	}
	claims, err := s.signer.Verify(token, expected)
	if err == nil {
		if verifyErr := s.registry.verify(claims, expected, false); verifyErr != nil {
			return Claims{}, verifyErr
		}
		return claims, nil
	}
	if expected.MediaGeneration == 0 {
		return Claims{}, err
	}
	preauthorized := expected
	preauthorized.MediaGeneration = 0
	claims, err = s.signer.Verify(token, preauthorized)
	if err != nil {
		return Claims{}, err
	}
	if err := s.registry.verifyUnbound(claims, preauthorized); err != nil {
		return Claims{}, err
	}
	if err := s.registry.BindAuthorization(claims.AuthorizationGeneration, expected.MediaGeneration); err != nil {
		return Claims{}, err
	}
	if err := s.registry.verify(claims, expected, false); err != nil {
		return Claims{}, err
	}
	return claims, nil
}

func (s *AuthorizationService) VerifyForAutoStart(token string, expected Binding) (Claims, error) {
	if s == nil || s.signer == nil || s.registry == nil {
		return Claims{}, ErrAuthorizationRegistryUnavailable
	}
	if expected.MediaGeneration != 0 {
		return Claims{}, ErrAuthorizationClaimsMismatch
	}
	claims, err := s.signer.Verify(token, expected)
	if err != nil {
		return Claims{}, err
	}
	if err := s.registry.verify(claims, expected, true); err != nil {
		return Claims{}, err
	}
	return claims, nil
}

func (s *AuthorizationService) BindAuthorization(authorizationGeneration string, mediaGeneration uint64) error {
	if s == nil || s.registry == nil {
		return ErrAuthorizationRegistryUnavailable
	}
	return s.registry.BindAuthorization(authorizationGeneration, mediaGeneration)
}

func (s *AuthorizationService) TerminateMediaGeneration(mediaGeneration uint64) int {
	if s == nil || s.registry == nil {
		return 0
	}
	return s.registry.TerminateMediaGeneration(mediaGeneration)
}
