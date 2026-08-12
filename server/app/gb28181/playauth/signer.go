package playauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playurl"
)

const (
	ModeDirect     = "direct"
	QueryParameter = "play_token"
	DefaultTTL     = 120 * time.Second

	tokenVersion             = 2
	tokenAudience            = "gb28181-play"
	playKeyContext           = "uvp-gb28181/play-authorization/v2"
	ipKeyContext             = "uvp-gb28181/play-ip-binding/v1"
	callbackCapabilityDomain = "uvp-gb28181/on-stream-not-found-callback/v1"
	maxClockSkew             = 30 * time.Second
	minimumRootKeyBytes      = 32
)

var (
	ErrKeyInvalid                   = errors.New("invalid play authorization key")
	ErrTokenInvalid                 = errors.New("invalid play authorization token")
	ErrTokenExpired                 = errors.New("expired play authorization token")
	ErrTokenTampered                = fmt.Errorf("%w: tampered", ErrTokenInvalid)
	ErrTokenBindingMismatch         = fmt.Errorf("%w: resource binding mismatch", ErrTokenInvalid)
	ErrTokenMediaGenerationMismatch = fmt.Errorf("%w: media generation mismatch", ErrTokenInvalid)
	ErrTokenIPMismatch              = fmt.Errorf("%w: client ip mismatch", ErrTokenInvalid)
	ErrCapabilityInvalid            = errors.New("invalid callback capability")
	ErrURLInvalid                   = errors.New("invalid playback URL")
)

type KeyMaterial struct {
	ID     string
	Secret []byte
}

type Binding struct {
	DeviceID        string
	ChannelID       string
	App             string
	Stream          string
	MediaServerID   string
	MediaGeneration uint64
	BindClientIP    bool
	ClientIP        string
}

type Claims struct {
	Version                 int    `json:"v"`
	Audience                string `json:"aud"`
	Mode                    string `json:"mode"`
	KeyID                   string `json:"kid"`
	DeviceID                string `json:"did"`
	ChannelID               string `json:"cid"`
	App                     string `json:"app"`
	Stream                  string `json:"stream"`
	MediaServerID           string `json:"node"`
	MediaGeneration         uint64 `json:"mgen"`
	IssuedAt                int64  `json:"iat"`
	ExpiresAt               int64  `json:"exp"`
	Nonce                   string `json:"nonce"`
	AuthorizationGeneration string `json:"agen"`
	ClientIPDigest          string `json:"iph,omitempty"`
}

type Prepared struct {
	IssuedAt                time.Time
	ExpiresAt               time.Time
	Nonce                   string
	AuthorizationGeneration string
}

type Grant struct {
	Token                   string
	ExpiresAt               time.Time
	AuthorizationGeneration string
}

type DirectIssuer interface {
	IssueDirect(Binding) (Grant, error)
}

type Verifier interface {
	Verify(string, Binding) (Claims, error)
}

type Option func(*Signer) error

type derivedKey struct {
	sign []byte
	ip   []byte
}

type Signer struct {
	activeID string
	keys     map[string]derivedKey
	ttl      atomic.Int64
	now      func() time.Time
	random   io.Reader
}

func NewSigner(root []byte, opts ...Option) (*Signer, error) {
	return NewKeyring(KeyMaterial{Secret: root}, nil, opts...)
}

func NewKeyring(active KeyMaterial, previous *KeyMaterial, opts ...Option) (*Signer, error) {
	activeID, activeKey, err := buildKey(active)
	if err != nil {
		return nil, err
	}
	signer := &Signer{
		activeID: activeID,
		keys:     map[string]derivedKey{activeID: activeKey},
		now:      time.Now,
		random:   rand.Reader,
	}
	signer.ttl.Store(int64(DefaultTTL))
	if previous != nil && len(previous.Secret) > 0 {
		previousID, previousKey, keyErr := buildKey(*previous)
		if keyErr != nil || previousID == activeID {
			return nil, ErrKeyInvalid
		}
		signer.keys[previousID] = previousKey
	}
	for _, opt := range opts {
		if err := opt(signer); err != nil {
			return nil, err
		}
	}
	return signer, nil
}

func KeyID(secret []byte) string {
	sum := sha256.Sum256(secret)
	return base64.RawURLEncoding.EncodeToString(sum[:9])
}

func buildKey(material KeyMaterial) (string, derivedKey, error) {
	if len(material.Secret) < minimumRootKeyBytes {
		return "", derivedKey{}, ErrKeyInvalid
	}
	id := strings.TrimSpace(material.ID)
	if id == "" {
		id = KeyID(material.Secret)
	}
	if strings.ContainsAny(id, ". 	\r\n") {
		return "", derivedKey{}, ErrKeyInvalid
	}
	signKey, err := deriveKey(material.Secret, playKeyContext)
	if err != nil {
		return "", derivedKey{}, err
	}
	ipKey, err := deriveKey(material.Secret, ipKeyContext)
	if err != nil {
		return "", derivedKey{}, err
	}
	return id, derivedKey{sign: signKey, ip: ipKey}, nil
}

func WithTTL(ttl time.Duration) Option {
	return func(s *Signer) error {
		if ttl <= 0 {
			return ErrTokenInvalid
		}
		s.ttl.Store(int64(ttl))
		return nil
	}
}

func (s *Signer) SetTTL(ttl time.Duration) error {
	if s == nil || ttl <= 0 {
		return ErrTokenInvalid
	}
	s.ttl.Store(int64(ttl))
	return nil
}

func WithNow(now func() time.Time) Option {
	return func(s *Signer) error {
		if now == nil {
			return ErrTokenInvalid
		}
		s.now = now
		return nil
	}
}

func WithRandomReader(reader io.Reader) Option {
	return func(s *Signer) error {
		if reader == nil {
			return ErrTokenInvalid
		}
		s.random = reader
		return nil
	}
}

func (s *Signer) Prepare() (Prepared, error) {
	if s == nil || s.random == nil || s.now == nil || s.ttl.Load() <= 0 {
		return Prepared{}, ErrTokenInvalid
	}
	randomBytes := make([]byte, 32)
	if _, err := io.ReadFull(s.random, randomBytes); err != nil {
		return Prepared{}, ErrTokenInvalid
	}
	now := s.now().UTC()
	return Prepared{
		IssuedAt:                now,
		ExpiresAt:               now.Add(time.Duration(s.ttl.Load())),
		Nonce:                   base64.RawURLEncoding.EncodeToString(randomBytes[:16]),
		AuthorizationGeneration: base64.RawURLEncoding.EncodeToString(randomBytes[16:]),
	}, nil
}

func (s *Signer) Bind(prepared Prepared, binding Binding) (Grant, error) {
	if s == nil || !validBinding(binding) || prepared.Nonce == "" || prepared.AuthorizationGeneration == "" ||
		prepared.IssuedAt.IsZero() || !prepared.ExpiresAt.After(prepared.IssuedAt) {
		return Grant{}, ErrTokenInvalid
	}
	key, ok := s.keys[s.activeID]
	if !ok {
		return Grant{}, ErrKeyInvalid
	}
	claims := Claims{
		Version: tokenVersion, Audience: tokenAudience, Mode: ModeDirect, KeyID: s.activeID,
		DeviceID: binding.DeviceID, ChannelID: binding.ChannelID, App: binding.App,
		Stream: binding.Stream, MediaServerID: binding.MediaServerID, MediaGeneration: binding.MediaGeneration,
		IssuedAt: prepared.IssuedAt.Unix(), ExpiresAt: prepared.ExpiresAt.Unix(), Nonce: prepared.Nonce,
		AuthorizationGeneration: prepared.AuthorizationGeneration,
	}
	if binding.BindClientIP {
		digest, err := clientIPDigest(key.ip, binding.ClientIP)
		if err != nil {
			return Grant{}, ErrTokenInvalid
		}
		claims.ClientIPDigest = digest
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return Grant{}, ErrTokenInvalid
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return Grant{
		Token:                   encoded + "." + base64.RawURLEncoding.EncodeToString(signature(key.sign, []byte(encoded))),
		ExpiresAt:               prepared.ExpiresAt,
		AuthorizationGeneration: prepared.AuthorizationGeneration,
	}, nil
}

func (s *Signer) IssueDirect(binding Binding) (Grant, error) {
	prepared, err := s.Prepare()
	if err != nil {
		return Grant{}, err
	}
	return s.Bind(prepared, binding)
}

func (s *Signer) Verify(token string, expected Binding) (Claims, error) {
	if s == nil || !validBinding(expected) {
		return Claims{}, ErrTokenInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Claims{}, ErrTokenTampered
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrTokenTampered
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, ErrTokenTampered
	}
	key, ok := s.keys[claims.KeyID]
	if !ok || claims.KeyID == "" {
		return Claims{}, ErrTokenTampered
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(providedSignature, signature(key.sign, []byte(parts[0]))) {
		return Claims{}, ErrTokenTampered
	}
	if claims.Version != tokenVersion || claims.Audience != tokenAudience || claims.Mode != ModeDirect ||
		claims.Nonce == "" || claims.AuthorizationGeneration == "" || claims.DeviceID != expected.DeviceID ||
		claims.ChannelID != expected.ChannelID || claims.App != expected.App || claims.Stream != expected.Stream ||
		claims.MediaServerID != expected.MediaServerID {
		return Claims{}, ErrTokenBindingMismatch
	}
	if claims.MediaGeneration != expected.MediaGeneration {
		return Claims{}, ErrTokenMediaGenerationMismatch
	}
	if expected.BindClientIP && claims.ClientIPDigest == "" {
		return Claims{}, ErrTokenIPMismatch
	}
	if claims.ClientIPDigest != "" {
		digest, digestErr := clientIPDigest(key.ip, expected.ClientIP)
		if digestErr != nil || !hmac.Equal([]byte(claims.ClientIPDigest), []byte(digest)) {
			return Claims{}, ErrTokenIPMismatch
		}
	}
	now := s.now().UTC()
	if claims.IssuedAt <= 0 || claims.ExpiresAt <= claims.IssuedAt || time.Unix(claims.IssuedAt, 0).After(now.Add(maxClockSkew)) {
		return Claims{}, ErrTokenTampered
	}
	if !now.Before(time.Unix(claims.ExpiresAt, 0)) {
		return Claims{}, ErrTokenExpired
	}
	return claims, nil
}

// CorrelationID is safe for logs and audit records. It is not a bearer
// credential and never exposes the random authorization generation itself.
func CorrelationID(authorizationGeneration string) string {
	if authorizationGeneration == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("uvp-gb28181/play-audit/v1:" + authorizationGeneration))
	return base64.RawURLEncoding.EncodeToString(sum[:9])
}

func signature(key, payload []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func clientIPDigest(key []byte, rawIP string) (string, error) {
	normalized, err := NormalizeClientIP(rawIP)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(signature(key, []byte(normalized))), nil
}

func NormalizeClientIP(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.Trim(value, "[]")
	address, err := netip.ParseAddr(value)
	if err != nil {
		return "", ErrTokenInvalid
	}
	return address.Unmap().String(), nil
}

func CallbackCapability(apiSecret, mediaServerID string) (string, error) {
	if strings.TrimSpace(apiSecret) == "" || strings.TrimSpace(mediaServerID) == "" {
		return "", ErrCapabilityInvalid
	}
	key, err := deriveKey([]byte(apiSecret), callbackCapabilityDomain)
	if err != nil {
		return "", ErrCapabilityInvalid
	}
	return base64.RawURLEncoding.EncodeToString(signature(key, []byte(mediaServerID))), nil
}

func VerifyCallbackCapability(apiSecret, mediaServerID, capability string) bool {
	want, err := CallbackCapability(apiSecret, mediaServerID)
	return err == nil && capability != "" && hmac.Equal([]byte(want), []byte(capability))
}

func DecorateURLs(values playurl.URLs, token string) (playurl.URLs, error) {
	if token == "" {
		return playurl.URLs{}, ErrTokenInvalid
	}
	decorated := values.AsMap()
	if len(decorated) == 0 {
		return playurl.URLs{}, ErrURLInvalid
	}
	for protocol, raw := range decorated {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return playurl.URLs{}, fmt.Errorf("%w: %s", ErrURLInvalid, protocol)
		}
		query := parsed.Query()
		query.Set(QueryParameter, token)
		parsed.RawQuery = query.Encode()
		decorated[protocol] = parsed.String()
	}
	return playurl.FromMap(decorated), nil
}

func deriveKey(root []byte, context string) ([]byte, error) {
	if len(root) == 0 {
		return nil, ErrKeyInvalid
	}
	return signature(root, []byte(context)), nil
}

func validBinding(binding Binding) bool {
	if !validGBID(binding.DeviceID) || !validGBID(binding.ChannelID) || strings.TrimSpace(binding.App) == "" ||
		strings.TrimSpace(binding.Stream) == "" || strings.TrimSpace(binding.MediaServerID) == "" {
		return false
	}
	if binding.BindClientIP {
		_, err := NormalizeClientIP(binding.ClientIP)
		return err == nil
	}
	return true
}

func validGBID(value string) bool {
	if len(value) != 20 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
