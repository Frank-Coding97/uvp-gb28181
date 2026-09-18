package management

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// PreviewAudience is intentionally different from the application JWT and
	// the GB28181 playauth audience. A token signed for one boundary must never
	// be accepted by another boundary.
	PreviewAudience = "zlm-media-preview"

	// PreviewQueryParameter is the only query parameter accepted for a
	// non-GB28181 management preview.
	PreviewQueryParameter = "media_access_token"

	// The short lifetime is a security boundary, not a cache duration. The
	// bounds also make an accidentally long-lived token impossible to issue.
	DefaultPreviewTTL = 90 * time.Second
	MinPreviewTTL     = 60 * time.Second
	MaxPreviewTTL     = 120 * time.Second

	previewTokenVersion = 1
	previewJTIBytes     = 16 // 128 bits of entropy
	previewMinimumKey   = 32
	previewMaxTokenSize = 4096
	previewMaxClockSkew = 30 * time.Second
)

var (
	ErrPreviewKeyInvalid       = errors.New("invalid media preview key")
	ErrPreviewKeyReused        = fmt.Errorf("%w: media preview key is already used by another boundary", ErrPreviewKeyInvalid)
	ErrPreviewTTLInvalid       = errors.New("invalid media preview ttl")
	ErrPreviewTokenInvalid     = errors.New("invalid media preview token")
	ErrPreviewTokenExpired     = errors.New("expired media preview token")
	ErrPreviewBindingMismatch  = fmt.Errorf("%w: media preview binding mismatch", ErrPreviewTokenInvalid)
	ErrPreviewIPMismatch       = fmt.Errorf("%w: media preview client ip mismatch", ErrPreviewTokenInvalid)
	ErrPreviewUnsupported      = errors.New("media preview is unsupported")
	ErrSnapshotInvalidResponse = errors.New("snapshot proxy received an invalid response")
	ErrSnapshotTooLarge        = errors.New("snapshot proxy response is too large")
	ErrSnapshotTimeout         = errors.New("snapshot proxy timed out")
	ErrSnapshotUpstream        = errors.New("snapshot proxy upstream request failed")
)

// PreviewResourceClass is the explicit resource boundary used by the Hook.
// In particular, app=rtp is not a resource classification.
type PreviewResourceClass string

const (
	PreviewResourceGB                PreviewResourceClass = "gb"
	PreviewResourceNonGBPreviewable  PreviewResourceClass = "non_gb_previewable"
	PreviewResourceLegacyPublic      PreviewResourceClass = "legacy_public"
	PreviewResourceUnknownConflicted PreviewResourceClass = "unknown_conflicted"
)

// PreviewResource is the complete resource input to a classifier. The vhost
// is part of the identity and must not be dropped while deciding authorization.
type PreviewResource struct {
	NodeUUID string `json:"nodeUuid"`
	VHost    string `json:"vhost"`
	Schema   string `json:"schema"`
	App      string `json:"app"`
	Stream   string `json:"stream"`
}

func (r PreviewResource) MediaIdentity() MediaIdentity {
	return MediaIdentity{Schema: r.Schema, Vhost: r.VHost, App: r.App, Stream: r.Stream}
}

func (r PreviewResource) Validate() error {
	if strings.TrimSpace(r.NodeUUID) == "" {
		return newValidationError(map[string]string{"nodeUuid": "must not be empty"})
	}
	if err := (r.MediaIdentity()).Validate(); err != nil {
		return err
	}
	return nil
}

// PreviewClassifier is intentionally a narrow interface. T14 supplies the
// business/resource adapter; the management package never guesses from app or
// stream names on its own.
type PreviewClassifier interface {
	Classify(context.Context, PreviewResource) (PreviewResourceClass, error)
}

type PreviewClassifierFunc func(context.Context, PreviewResource) (PreviewResourceClass, error)

func (f PreviewClassifierFunc) Classify(ctx context.Context, resource PreviewResource) (PreviewResourceClass, error) {
	if f == nil {
		return PreviewResourceUnknownConflicted, ErrPreviewTokenInvalid
	}
	return f(ctx, resource)
}

// PreviewBinding is used by both issuance and verification. UserID is trusted
// only at the IssueForUser boundary; Hook verification deliberately leaves it
// zero because a ZLM callback cannot prove which logged-in user owns the URL.
type PreviewBinding struct {
	UserID       uint64 `json:"-"`
	NodeUUID     string `json:"nodeUuid"`
	VHost        string `json:"vhost"`
	Schema       string `json:"schema"`
	App          string `json:"app"`
	Stream       string `json:"stream"`
	ClientIP     string `json:"clientIp,omitempty"`
	BindClientIP bool   `json:"bindClientIp,omitempty"`
}

func (b PreviewBinding) MediaIdentity() MediaIdentity {
	return MediaIdentity{Schema: b.Schema, Vhost: b.VHost, App: b.App, Stream: b.Stream}
}

func (b PreviewBinding) Resource() PreviewResource {
	return PreviewResource{
		NodeUUID: b.NodeUUID, VHost: b.VHost, Schema: b.Schema, App: b.App, Stream: b.Stream,
	}
}

func (b PreviewBinding) Validate(requireUser bool) error {
	fields := make(map[string]string)
	if requireUser && b.UserID == 0 {
		fields["userId"] = "must be positive"
	}
	if strings.TrimSpace(b.NodeUUID) == "" {
		fields["nodeUuid"] = "must not be empty"
	}
	if err := b.MediaIdentity().Validate(); err != nil {
		var validationErr *ValidationError
		if errors.As(err, &validationErr) && validationErr != nil {
			for field, message := range validationErr.Fields {
				fields[field] = message
			}
		} else {
			fields["media"] = "is invalid"
		}
	}
	if b.BindClientIP || strings.TrimSpace(b.ClientIP) != "" {
		if _, err := normalizePreviewIP(b.ClientIP); err != nil {
			fields["clientIp"] = "must be a valid IP address"
		}
	}
	if len(fields) > 0 {
		return newValidationError(fields)
	}
	return nil
}

// PreviewTokenVerifier is the Hook-facing verifier boundary. It does not
// expose signing or key material to the handler.
type PreviewTokenVerifier interface {
	Verify(string, PreviewBinding) (PreviewClaims, error)
}

// PreviewKeyPolicy names the application secrets that a management preview key
// must never reuse. Values are compared but never retained in error strings.
type PreviewKeyPolicy struct {
	JWTRootSecret    string
	GBPlayAuthSecret string
	GlobalZLMSecret  string
	NodeAPISecrets   []string
}

// PreviewSigner is an independent stateless HMAC signer. It has no registry:
// repeated requests in the same schema/TTL are valid and JTI is only an audit
// correlation value.
type PreviewSigner struct {
	key       []byte
	ttl       atomic.Int64
	now       func() time.Time
	random    io.Reader
	forbidden []string
}

type PreviewSignerOption func(*PreviewSigner) error

func NewPreviewSigner(key []byte, options ...PreviewSignerOption) (*PreviewSigner, error) {
	if err := ValidatePreviewKey(key); err != nil {
		return nil, err
	}
	signer := &PreviewSigner{
		key:    append([]byte(nil), key...),
		now:    time.Now,
		random: rand.Reader,
	}
	signer.ttl.Store(int64(DefaultPreviewTTL))
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(signer); err != nil {
			return nil, err
		}
	}
	if err := validatePreviewKeyReuse(signer.key, signer.forbidden); err != nil {
		return nil, err
	}
	return signer, nil
}

func NewPreviewSignerWithPolicy(key []byte, policy PreviewKeyPolicy, options ...PreviewSignerOption) (*PreviewSigner, error) {
	return NewPreviewSigner(key, append([]PreviewSignerOption{WithPreviewKeyPolicy(policy)}, options...)...)
}

// ValidatePreviewKey checks only length and exact reuse. It deliberately does
// not include the compared value in any returned error.
func ValidatePreviewKey(key []byte, forbidden ...string) error {
	if len(key) < previewMinimumKey {
		return ErrPreviewKeyInvalid
	}
	return validatePreviewKeyReuse(key, forbidden)
}

func validatePreviewKeyReuse(key []byte, forbidden []string) error {
	for _, candidate := range forbidden {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" && hmac.Equal(key, []byte(candidate)) {
			return ErrPreviewKeyReused
		}
	}
	return nil
}

func WithPreviewRejectedSecrets(secrets ...string) PreviewSignerOption {
	return func(signer *PreviewSigner) error {
		for _, secret := range secrets {
			if strings.TrimSpace(secret) != "" {
				signer.forbidden = append(signer.forbidden, secret)
			}
		}
		return validatePreviewKeyReuse(signer.key, signer.forbidden)
	}
}

func WithPreviewKeyPolicy(policy PreviewKeyPolicy) PreviewSignerOption {
	return WithPreviewRejectedSecrets(append([]string{
		policy.JWTRootSecret, policy.GBPlayAuthSecret, policy.GlobalZLMSecret,
	}, policy.NodeAPISecrets...)...)
}

func WithPreviewTTL(ttl time.Duration) PreviewSignerOption {
	return func(signer *PreviewSigner) error {
		if ttl <= 0 || ttl < MinPreviewTTL || ttl > MaxPreviewTTL {
			return ErrPreviewTTLInvalid
		}
		signer.ttl.Store(int64(ttl))
		return nil
	}
}

func (s *PreviewSigner) SetTTL(ttl time.Duration) error {
	if s == nil || ttl < MinPreviewTTL || ttl > MaxPreviewTTL {
		return ErrPreviewTTLInvalid
	}
	s.ttl.Store(int64(ttl))
	return nil
}

func WithPreviewNow(now func() time.Time) PreviewSignerOption {
	return func(signer *PreviewSigner) error {
		if now == nil {
			return ErrPreviewTokenInvalid
		}
		signer.now = now
		return nil
	}
}

func WithPreviewRandomReader(reader io.Reader) PreviewSignerOption {
	return func(signer *PreviewSigner) error {
		if reader == nil {
			return ErrPreviewTokenInvalid
		}
		signer.random = reader
		return nil
	}
}

// PreviewClaims are the signed, non-secret claims carried by a management
// preview token. JTI is returned for correlation; callers should persist only
// JTIHash, never the raw bearer token or JTI.
type PreviewClaims struct {
	Version        int    `json:"v"`
	Audience       string `json:"aud"`
	UserID         uint64 `json:"userId"`
	NodeUUID       string `json:"nodeUuid"`
	VHost          string `json:"vhost"`
	Schema         string `json:"schema"`
	App            string `json:"app"`
	Stream         string `json:"stream"`
	IssuedAt       int64  `json:"iat"`
	ExpiresAt      int64  `json:"exp"`
	JTI            string `json:"jti"`
	ClientIPDigest string `json:"iph,omitempty"`
}

type PreviewGrant struct {
	Token     string        `json:"token"`
	Claims    PreviewClaims `json:"-"`
	ExpiresAt time.Time     `json:"expiresAt"`
	JTIHash   string        `json:"jtiHash"`
}

// IssueForUser is the trusted issuance boundary. The actor user ID is a
// separate argument and always overwrites any value copied into binding by a
// client-facing request DTO.
func (s *PreviewSigner) IssueForUser(userID uint64, binding PreviewBinding) (PreviewGrant, error) {
	binding.UserID = userID
	return s.issue(binding, userID)
}

func (s *PreviewSigner) issue(binding PreviewBinding, userID uint64) (PreviewGrant, error) {
	if s == nil || s.now == nil || s.random == nil || userID == 0 || binding.Validate(true) != nil {
		return PreviewGrant{}, ErrPreviewTokenInvalid
	}
	binding.UserID = userID
	now := s.now().UTC().Truncate(time.Second)
	ttl := time.Duration(s.ttl.Load())
	if ttl < MinPreviewTTL || ttl > MaxPreviewTTL {
		return PreviewGrant{}, ErrPreviewTTLInvalid
	}
	jtiBytes := make([]byte, previewJTIBytes)
	if _, err := io.ReadFull(s.random, jtiBytes); err != nil {
		return PreviewGrant{}, ErrPreviewTokenInvalid
	}
	claims := PreviewClaims{
		Version: previewTokenVersion, Audience: PreviewAudience, UserID: userID,
		NodeUUID: binding.NodeUUID, VHost: binding.VHost, Schema: binding.Schema,
		App: binding.App, Stream: binding.Stream, IssuedAt: now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(), JTI: base64.RawURLEncoding.EncodeToString(jtiBytes),
	}
	if binding.BindClientIP || strings.TrimSpace(binding.ClientIP) != "" {
		ip, err := normalizePreviewIP(binding.ClientIP)
		if err != nil {
			return PreviewGrant{}, ErrPreviewTokenInvalid
		}
		claims.ClientIPDigest = s.clientIPDigest(ip)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return PreviewGrant{}, ErrPreviewTokenInvalid
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := s.sign([]byte(encodedPayload))
	token := encodedPayload + "." + base64.RawURLEncoding.EncodeToString(signature)
	return PreviewGrant{
		Token: token, Claims: claims, ExpiresAt: time.Unix(claims.ExpiresAt, 0).UTC(),
		JTIHash: JTIHash(claims.JTI),
	}, nil
}

func (s *PreviewSigner) Verify(token string, expected PreviewBinding) (PreviewClaims, error) {
	if s == nil || s.now == nil || len(s.key) < previewMinimumKey || expected.Validate(false) != nil || len(token) > previewMaxTokenSize {
		return PreviewClaims{}, ErrPreviewTokenInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return PreviewClaims{}, ErrPreviewTokenInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return PreviewClaims{}, ErrPreviewTokenInvalid
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(providedSignature) != sha256.Size || !hmac.Equal(providedSignature, s.sign([]byte(parts[0]))) {
		return PreviewClaims{}, ErrPreviewTokenInvalid
	}
	var claims PreviewClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return PreviewClaims{}, ErrPreviewTokenInvalid
	}
	if claims.Version != previewTokenVersion || claims.Audience != PreviewAudience || claims.UserID == 0 ||
		strings.TrimSpace(claims.NodeUUID) == "" || claims.VHost == "" || claims.Schema == "" ||
		claims.App == "" || claims.Stream == "" || claims.IssuedAt <= 0 || claims.ExpiresAt <= claims.IssuedAt ||
		claims.ExpiresAt-claims.IssuedAt < int64(MinPreviewTTL/time.Second) ||
		claims.ExpiresAt-claims.IssuedAt > int64(MaxPreviewTTL/time.Second) || !validPreviewJTI(claims.JTI) {
		return PreviewClaims{}, ErrPreviewTokenInvalid
	}
	if claims.NodeUUID != expected.NodeUUID || claims.VHost != expected.VHost || claims.Schema != expected.Schema ||
		claims.App != expected.App || claims.Stream != expected.Stream {
		return PreviewClaims{}, ErrPreviewBindingMismatch
	}
	if expected.UserID != 0 && claims.UserID != expected.UserID {
		return PreviewClaims{}, ErrPreviewBindingMismatch
	}
	if expected.BindClientIP && claims.ClientIPDigest == "" {
		return PreviewClaims{}, ErrPreviewIPMismatch
	}
	if claims.ClientIPDigest != "" {
		ip, err := normalizePreviewIP(expected.ClientIP)
		if err != nil || !hmac.Equal([]byte(claims.ClientIPDigest), []byte(s.clientIPDigest(ip))) {
			return PreviewClaims{}, ErrPreviewIPMismatch
		}
	}
	now := s.now().UTC()
	issuedAt := time.Unix(claims.IssuedAt, 0)
	if issuedAt.After(now.Add(previewMaxClockSkew)) {
		return PreviewClaims{}, ErrPreviewTokenInvalid
	}
	if !now.Before(time.Unix(claims.ExpiresAt, 0)) {
		return PreviewClaims{}, ErrPreviewTokenExpired
	}
	return claims, nil
}

func (s *PreviewSigner) SetKeyPolicy(policy PreviewKeyPolicy) error {
	if s == nil {
		return ErrPreviewKeyInvalid
	}
	secrets := append([]string{policy.JWTRootSecret, policy.GBPlayAuthSecret, policy.GlobalZLMSecret}, policy.NodeAPISecrets...)
	if err := validatePreviewKeyReuse(s.key, secrets); err != nil {
		return err
	}
	s.forbidden = append([]string(nil), secrets...)
	return nil
}

func (s *PreviewSigner) sign(value []byte) []byte {
	h := hmac.New(sha256.New, s.key)
	_, _ = h.Write(value)
	return h.Sum(nil)
}

func (s *PreviewSigner) clientIPDigest(ip string) string {
	h := hmac.New(sha256.New, s.key)
	_, _ = h.Write([]byte("uvp-gb28181/media-preview/ip/v1\x00" + ip))
	return hex.EncodeToString(h.Sum(nil))
}

func normalizePreviewIP(value string) (string, error) {
	address, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	return address.Unmap().String(), nil
}

func validPreviewJTI(jti string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(jti)
	return err == nil && len(decoded) >= previewJTIBytes
}

// JTIHash is the only value suitable for an audit correlation field. It is a
// one-way digest and does not make the bearer token or raw JTI reusable.
func JTIHash(jti string) string {
	sum := sha256.Sum256([]byte("uvp-gb28181/media-preview/jti/v1\x00" + jti))
	return hex.EncodeToString(sum[:])
}

func (c PreviewClaims) JTIHash() string { return JTIHash(c.JTI) }
