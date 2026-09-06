package playauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"sync"
	"time"
)

const (
	DefaultAuthorizationRegistryCapacity = 4096
	MinimumAuthorizationStartLifetime    = 30 * time.Second
	VerifiedClientSourceLifetime         = 30 * time.Second
	authorizationContextTimeout          = 5 * time.Second
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
	ErrAuthorizationClientNotVerified   = errors.New("play authorization client source not verified")
	ErrAuthorizationDeviceEpoch         = errors.New("play authorization device epoch unavailable")
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

type ContextDirectIssuer interface {
	IssueDirectContext(context.Context, Binding) (Grant, error)
}

type ContextPreparedIssuer interface {
	Prepare() (Prepared, error)
	BindContext(context.Context, Prepared, Binding) (Grant, error)
}

type ContextAutoStartVerifier interface {
	VerifyForAutoStartContext(context.Context, string, Binding) (Claims, error)
}

// VerifiedClientAutoStartVerifier permits a cold, IP-bound authorization to
// continue from OnPlay to on_stream_not_found only after OnPlay has verified
// the real client address.
type VerifiedClientAutoStartVerifier interface {
	AutoStartVerifier
	MarkVerifiedClientSource(string, Claims, Binding) error
	VerifyForVerifiedClientAutoStart(string, Binding) (Claims, error)
}

type ContextVerifiedClientAutoStartVerifier interface {
	ContextAutoStartVerifier
	MarkVerifiedClientSourceContext(context.Context, string, Claims, Binding) error
	VerifyForVerifiedClientAutoStartContext(context.Context, string, Binding) (Claims, error)
}

type DeviceSecurityAuthority interface {
	Load(context.Context, string) (DeviceSecurityState, error)
	AuthorizeLegacy(context.Context, string, int64) error
	AuthorizeEpoch(context.Context, string, int64) error
}

type AuthorizationRegistryOption func(*AuthorizationRegistry)

func WithDeviceSecurityAuthority(authority DeviceSecurityAuthority) AuthorizationServiceOption {
	return func(service *AuthorizationService) { service.authority = authority }
}

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
	deviceEpoch     int64
	version         int
	app             string
	stream          string
	mediaServerID   string
	mediaGeneration uint64
}

type authorizationRecord struct {
	binding                   authorizationBinding
	issuedAt                  time.Time
	expiresAt                 time.Time
	nonce                     string
	state                     AuthorizationState
	mediaGeneration           uint64
	verifiedClientTokenDigest [sha256.Size]byte
	verifiedClientUntil       time.Time
}

// AuthorizationRegistry is intentionally in-memory. A restart invalidates
// cold-stream preauthorizations while explicit live-media tokens stay
// independently verifiable by HMAC for their remaining lifetime.
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
	switch record.state {
	case AuthorizationUnbound:
		if !now.Before(record.expiresAt) {
			return ErrAuthorizationExpired
		}
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

func (r *AuthorizationRegistry) discard(authorizationGeneration string) {
	if r == nil || strings.TrimSpace(authorizationGeneration) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if record, ok := r.records[authorizationGeneration]; ok {
		r.removeLocked(authorizationGeneration, record)
	}
}

func (r *AuthorizationRegistry) verify(claims Claims, expected Binding, requireUnbound bool) error {
	return r.verifyWithLifetime(claims, expected, requireUnbound, requireUnbound)
}

func (r *AuthorizationRegistry) verifyUnbound(claims Claims, expected Binding) error {
	return r.verifyWithLifetime(claims, expected, true, false)
}

// MarkVerifiedClientSource records a brief proof that OnPlay validated an
// IP-bound cold authorization with ZLM's real client address. The token is
// retained only as a digest and is never written to logs or metrics.
func (r *AuthorizationRegistry) MarkVerifiedClientSource(token string, claims Claims, expected Binding) error {
	if r == nil {
		return ErrAuthorizationRegistryUnavailable
	}
	if strings.TrimSpace(token) == "" || !validBinding(expected) || expected.MediaGeneration != 0 ||
		!expected.BindClientIP || claims.ClientIPDigest == "" || strings.TrimSpace(claims.AuthorizationGeneration) == "" {
		return ErrAuthorizationClaimsMismatch
	}
	now := r.currentTime()
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[claims.AuthorizationGeneration]
	if !ok {
		return ErrAuthorizationNotFound
	}
	if record.state != AuthorizationUnbound || !now.Before(record.expiresAt) {
		return ErrAuthorizationClientNotVerified
	}
	if !record.matchesClaims(claims) || !record.binding.sameResource(expected) {
		return ErrAuthorizationClaimsMismatch
	}
	record.verifiedClientTokenDigest = sha256.Sum256([]byte(token))
	record.verifiedClientUntil = now.Add(VerifiedClientSourceLifetime)
	if record.verifiedClientUntil.After(record.expiresAt) {
		record.verifiedClientUntil = record.expiresAt
	}
	r.records[claims.AuthorizationGeneration] = record
	return nil
}

// VerifyClientProof rechecks the exact HMAC-authenticated claims against the
// short-lived OnPlay source proof. The fallback caller has no raw client IP;
// the token digest is therefore the proof handle, never a substitute token.
func (r *AuthorizationRegistry) VerifyClientProof(token string, claims Claims, expected Binding) error {
	if r == nil {
		return ErrAuthorizationRegistryUnavailable
	}
	if strings.TrimSpace(token) == "" || !validBinding(expected) || expected.MediaGeneration != 0 ||
		strings.TrimSpace(claims.AuthorizationGeneration) == "" || claims.ClientIPDigest == "" ||
		!claimsEpochMatchesExpected(claims, expected, false) {
		return ErrAuthorizationClaimsMismatch
	}
	now := r.currentTime()
	digest := sha256.Sum256([]byte(token))
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[claims.AuthorizationGeneration]
	if !ok {
		return ErrAuthorizationNotFound
	}
	if record.state != AuthorizationUnbound || !now.Before(record.expiresAt) ||
		!now.Before(record.verifiedClientUntil) ||
		subtle.ConstantTimeCompare(record.verifiedClientTokenDigest[:], digest[:]) != 1 {
		return ErrAuthorizationClientNotVerified
	}
	if record.expiresAt.Sub(now) < MinimumAuthorizationStartLifetime ||
		!record.matchesClaims(claims) || !record.binding.sameResource(expected) {
		return ErrAuthorizationClaimsMismatch
	}
	return nil
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
	version := tokenVersionV2
	if binding.DeviceEpoch > 0 {
		version = tokenVersionV4
	}
	return authorizationBinding{
		deviceID: binding.DeviceID, channelID: binding.ChannelID, deviceEpoch: binding.DeviceEpoch, version: version, app: binding.App,
		stream: binding.Stream, mediaServerID: binding.MediaServerID,
		mediaGeneration: binding.MediaGeneration,
	}
}

func (b authorizationBinding) sameResource(binding Binding) bool {
	return b.deviceID == binding.DeviceID && b.channelID == binding.ChannelID && b.deviceEpoch == binding.DeviceEpoch && b.app == binding.App &&
		b.stream == binding.Stream && b.mediaServerID == binding.MediaServerID
}

func (r authorizationRecord) matchesClaims(claims Claims) bool {
	return r.nonce == claims.Nonce && r.issuedAt.Unix() == claims.IssuedAt && r.expiresAt.Unix() == claims.ExpiresAt &&
		r.binding.version == claims.Version && r.binding.deviceID == claims.DeviceID && r.binding.channelID == claims.ChannelID && r.binding.deviceEpoch == claims.DeviceEpoch && r.binding.app == claims.App &&
		r.binding.stream == claims.Stream && r.binding.mediaServerID == claims.MediaServerID &&
		r.binding.mediaGeneration == claims.MediaGeneration
}

// AuthorizationService combines stateless HMAC validation for live media with
// the process-local lifecycle registry required by cold-stream preauthorization.
type AuthorizationService struct {
	signer    *Signer
	registry  *AuthorizationRegistry
	metrics   *Metrics
	authority DeviceSecurityAuthority
}

type AuthorizationServiceOption func(*AuthorizationService)

func WithAuthorizationMetrics(metrics *Metrics) AuthorizationServiceOption {
	return func(service *AuthorizationService) { service.metrics = metrics }
}

func NewAuthorizationService(signer *Signer, registry *AuthorizationRegistry, opts ...AuthorizationServiceOption) *AuthorizationService {
	service := &AuthorizationService{signer: signer, registry: registry}
	for _, opt := range opts {
		if opt != nil {
			opt(service)
		}
	}
	return service
}

func (s *AuthorizationService) SetTTL(ttl time.Duration) error {
	if s == nil || s.signer == nil {
		return ErrAuthorizationRegistryUnavailable
	}
	return s.signer.SetTTL(ttl)
}

func (s *AuthorizationService) Prepare() (prepared Prepared, err error) {
	defer func() {
		if err != nil {
			s.recordMetric(MetricOutcomeForError("prepare", err))
		}
	}()
	if s == nil || s.signer == nil {
		return Prepared{}, ErrAuthorizationRegistryUnavailable
	}
	return s.signer.Prepare()
}

func (s *AuthorizationService) Bind(prepared Prepared, binding Binding) (grant Grant, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), authorizationContextTimeout)
	defer cancel()
	return s.BindContext(ctx, prepared, binding)
}

func (s *AuthorizationService) BindContext(ctx context.Context, prepared Prepared, binding Binding) (grant Grant, err error) {
	defer func() {
		if err != nil {
			s.recordMetric(MetricOutcomeForError("bind", err))
			return
		}
		s.recordMetric(MetricOutcomeIssued)
	}()
	if err := s.requireAuthorityContext(ctx); err != nil {
		return Grant{}, err
	}
	if !validResourceBinding(binding) {
		return Grant{}, ErrTokenInvalid
	}
	if binding.DeviceEpoch <= 0 {
		return Grant{}, ErrAuthorizationDeviceEpoch
	}
	state, err := s.authority.Load(ctx, binding.DeviceID)
	if err != nil {
		return Grant{}, err
	}
	if state.AccessEpoch <= 0 {
		return Grant{}, ErrAuthorizationDeviceEpoch
	}
	if binding.DeviceEpoch != state.AccessEpoch {
		return Grant{}, ErrTokenRevoked
	}
	grant, err = s.signer.Bind(prepared, binding)
	if err != nil {
		return Grant{}, err
	}
	if binding.MediaGeneration == 0 {
		if s.registry == nil {
			return Grant{}, ErrAuthorizationRegistryUnavailable
		}
		if err := s.registry.Register(prepared, binding); err != nil {
			return Grant{}, err
		}
	}
	if err := s.authority.AuthorizeEpoch(ctx, binding.DeviceID, binding.DeviceEpoch); err != nil {
		if binding.MediaGeneration == 0 {
			s.registry.discard(prepared.AuthorizationGeneration)
		}
		return Grant{}, err
	}
	return grant, nil
}

func (s *AuthorizationService) IssueDirect(binding Binding) (Grant, error) {
	ctx, cancel := context.WithTimeout(context.Background(), authorizationContextTimeout)
	defer cancel()
	return s.IssueDirectContext(ctx, binding)
}

func (s *AuthorizationService) IssueDirectContext(ctx context.Context, binding Binding) (Grant, error) {
	if err := requireAuthorizationContext(ctx); err != nil {
		return Grant{}, err
	}
	prepared, err := s.Prepare()
	if err != nil {
		return Grant{}, err
	}
	return s.BindContext(ctx, prepared, binding)
}

func (s *AuthorizationService) Verify(token string, expected Binding) (claims Claims, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), authorizationContextTimeout)
	defer cancel()
	return s.VerifyContext(ctx, token, expected)
}

func (s *AuthorizationService) VerifyContext(ctx context.Context, token string, expected Binding) (claims Claims, err error) {
	defer func() {
		if err != nil {
			s.recordMetric(MetricOutcomeForError(token, err))
			return
		}
		s.recordMetric(MetricOutcomeVerified)
	}()
	if err := s.requireAuthorityContext(ctx); err != nil {
		return Claims{}, err
	}
	claims, resolved, err := s.authenticateContextToken(token, expected)
	if err == nil {
		claims, err = s.signer.Verify(token, resolved)
		if err != nil {
			return Claims{}, err
		}
		if err := s.authorizeClaims(ctx, claims); err != nil {
			return Claims{}, err
		}
		if claims.MediaGeneration != 0 {
			return claims, nil
		}
		if s.registry == nil {
			return Claims{}, ErrAuthorizationRegistryUnavailable
		}
		if verifyErr := s.registry.verify(claims, resolved, false); verifyErr != nil {
			return Claims{}, verifyErr
		}
		return claims, nil
	}
	if !errors.Is(err, ErrTokenMediaGenerationMismatch) || expected.MediaGeneration == 0 {
		return Claims{}, err
	}
	if s.registry == nil {
		return Claims{}, ErrAuthorizationRegistryUnavailable
	}
	preauthorized := expected
	preauthorized.MediaGeneration = 0
	claims, resolved, err = s.authenticateContextToken(token, preauthorized)
	if err != nil {
		return Claims{}, err
	}
	claims, err = s.signer.Verify(token, resolved)
	if err != nil {
		return Claims{}, err
	}
	if err := s.authorizeClaims(ctx, claims); err != nil {
		return Claims{}, err
	}
	boundExpected := resolved
	boundExpected.MediaGeneration = expected.MediaGeneration
	if err := s.registry.verify(claims, boundExpected, false); err == nil {
		return claims, nil
	}
	if err := s.registry.verifyUnbound(claims, resolved); err != nil {
		return Claims{}, err
	}
	if err := s.registry.BindAuthorization(claims.AuthorizationGeneration, expected.MediaGeneration); err != nil {
		return Claims{}, err
	}
	if err := s.registry.verify(claims, boundExpected, false); err != nil {
		return Claims{}, err
	}
	return claims, nil
}

func (s *AuthorizationService) VerifyForAutoStart(token string, expected Binding) (claims Claims, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), authorizationContextTimeout)
	defer cancel()
	return s.VerifyForAutoStartContext(ctx, token, expected)
}

func (s *AuthorizationService) VerifyForAutoStartContext(ctx context.Context, token string, expected Binding) (claims Claims, err error) {
	defer func() {
		if err != nil {
			s.recordMetric(MetricOutcomeForError(token, err))
			return
		}
		s.recordMetric(MetricOutcomeVerified)
	}()
	if err := s.requireAuthorityContext(ctx); err != nil || s.registry == nil {
		if err != nil {
			return Claims{}, err
		}
		return Claims{}, ErrAuthorizationRegistryUnavailable
	}
	if expected.MediaGeneration != 0 {
		return Claims{}, ErrAuthorizationClaimsMismatch
	}
	claims, resolved, err := s.authenticateContextToken(token, expected)
	if err != nil {
		return Claims{}, err
	}
	claims, err = s.signer.Verify(token, resolved)
	if err != nil {
		return Claims{}, err
	}
	if err := s.authorizeClaims(ctx, claims); err != nil {
		return Claims{}, err
	}
	if err := s.registry.verify(claims, resolved, true); err != nil {
		return Claims{}, err
	}
	return claims, nil
}

func (s *AuthorizationService) MarkVerifiedClientSource(token string, claims Claims, expected Binding) error {
	ctx, cancel := context.WithTimeout(context.Background(), authorizationContextTimeout)
	defer cancel()
	return s.MarkVerifiedClientSourceContext(ctx, token, claims, expected)
}

func (s *AuthorizationService) MarkVerifiedClientSourceContext(ctx context.Context, token string, claims Claims, expected Binding) error {
	if err := s.requireAuthorityContext(ctx); err != nil || s.registry == nil {
		if err != nil {
			return err
		}
		return ErrAuthorizationRegistryUnavailable
	}
	verifiedClaims, resolved, err := s.authenticateContextToken(token, expected)
	if err != nil {
		return err
	}
	verifiedClaims, err = s.signer.Verify(token, resolved)
	if err != nil {
		return err
	}
	if verifiedClaims != claims {
		return ErrAuthorizationClaimsMismatch
	}
	if err := s.authorizeClaims(ctx, verifiedClaims); err != nil {
		return err
	}
	return s.registry.MarkVerifiedClientSource(token, claims, resolved)
}

func (s *AuthorizationService) VerifyForVerifiedClientAutoStart(token string, expected Binding) (claims Claims, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), authorizationContextTimeout)
	defer cancel()
	return s.VerifyForVerifiedClientAutoStartContext(ctx, token, expected)
}

func (s *AuthorizationService) VerifyForVerifiedClientAutoStartContext(ctx context.Context, token string, expected Binding) (claims Claims, err error) {
	defer func() {
		if err != nil {
			s.recordMetric(MetricOutcomeForError(token, err))
			return
		}
		s.recordMetric(MetricOutcomeVerified)
	}()
	if err := s.requireAuthorityContext(ctx); err != nil || s.registry == nil {
		if err != nil {
			return Claims{}, err
		}
		return Claims{}, ErrAuthorizationRegistryUnavailable
	}
	if expected.MediaGeneration != 0 {
		return Claims{}, ErrAuthorizationClaimsMismatch
	}
	claims, resolved, err := s.authenticateContextToken(token, expected)
	if err != nil {
		return Claims{}, verifiedFallbackReject(err)
	}
	if claims.ClientIPDigest == "" {
		return Claims{}, ErrAuthorizationClientNotVerified
	}
	digest, err := base64.RawURLEncoding.Strict().DecodeString(claims.ClientIPDigest)
	if err != nil || len(digest) != sha256.Size {
		return Claims{}, ErrAuthorizationClientNotVerified
	}
	if err := s.validateClaimsLifetime(claims); err != nil {
		return Claims{}, verifiedFallbackReject(err)
	}
	if err := s.registry.VerifyClientProof(token, claims, resolved); err != nil {
		return Claims{}, err
	}
	if err := s.authorizeClaims(ctx, claims); err != nil {
		return Claims{}, err
	}
	return claims, nil
}

func verifiedFallbackReject(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return ErrAuthorizationClientNotVerified
}

func (s *AuthorizationService) authenticateContextToken(token string, expected Binding) (Claims, Binding, error) {
	claims, err := s.signer.authenticateBoundToken(token, expected)
	if err != nil {
		return Claims{}, Binding{}, err
	}
	resolved := expected
	if claims.Version == tokenVersionV4 {
		if claims.DeviceEpoch <= 0 {
			return Claims{}, Binding{}, ErrAuthorizationDeviceEpoch
		}
		resolved.DeviceEpoch = claims.DeviceEpoch
	} else if claims.Version == tokenVersionV2 {
		if expected.DeviceEpoch != 0 {
			return Claims{}, Binding{}, ErrTokenBindingMismatch
		}
		resolved.DeviceEpoch = 0
	} else {
		return Claims{}, Binding{}, ErrTokenTampered
	}
	return claims, resolved, nil
}

func (s *AuthorizationService) authorizeClaims(ctx context.Context, claims Claims) error {
	if err := requireAuthorizationContext(ctx); err != nil {
		return err
	}
	if !validDeviceSecurityAuthority(s.authority) {
		return ErrAuthorizationRegistryUnavailable
	}
	switch claims.Version {
	case tokenVersionV2:
		if claims.DeviceEpoch != 0 {
			return ErrTokenBindingMismatch
		}
		return s.authority.AuthorizeLegacy(ctx, claims.DeviceID, claims.IssuedAt)
	case tokenVersionV4:
		if claims.DeviceEpoch <= 0 {
			return ErrAuthorizationDeviceEpoch
		}
		return s.authority.AuthorizeEpoch(ctx, claims.DeviceID, claims.DeviceEpoch)
	default:
		return ErrTokenTampered
	}
}

func (s *AuthorizationService) validateClaimsLifetime(claims Claims) error {
	if s == nil || s.signer == nil {
		return ErrAuthorizationRegistryUnavailable
	}
	return s.signer.validateLifetimeAndRevocation(claims)
}

func (s *AuthorizationService) requireAuthorityContext(ctx context.Context) error {
	if s == nil || s.signer == nil || !validDeviceSecurityAuthority(s.authority) {
		return ErrAuthorizationRegistryUnavailable
	}
	return requireAuthorizationContext(ctx)
}

func requireAuthorizationContext(ctx context.Context) error {
	if isNilInterface(ctx) {
		return ErrAuthorizationRegistryUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func validDeviceSecurityAuthority(authority DeviceSecurityAuthority) bool {
	return !isNilInterface(authority)
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func (s *AuthorizationService) recordMetric(outcome MetricOutcome) {
	if s != nil && s.metrics != nil {
		s.metrics.Record(outcome)
	}
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
