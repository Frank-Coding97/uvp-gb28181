package playauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playurl"
)

const (
	ModeDirect     = "direct"
	QueryParameter = "play_token"
	DefaultTTL     = 120 * time.Second

	tokenVersion             = 1
	tokenAudience            = "gb28181-play"
	playKeyContext           = "uvp-gb28181/play-authorization/v1"
	callbackCapabilityDomain = "uvp-gb28181/on-stream-not-found-callback/v1"
	maxClockSkew             = 30 * time.Second
	minimumRootKeyBytes      = 32
)

var (
	ErrKeyInvalid        = errors.New("invalid play authorization key")
	ErrTokenInvalid      = errors.New("invalid play authorization token")
	ErrTokenExpired      = errors.New("expired play authorization token")
	ErrCapabilityInvalid = errors.New("invalid callback capability")
	ErrURLInvalid        = errors.New("invalid playback URL")
)

type Binding struct {
	DeviceID      string
	ChannelID     string
	App           string
	Stream        string
	MediaServerID string
}

type Claims struct {
	Version       int    `json:"v"`
	Audience      string `json:"aud"`
	Mode          string `json:"mode"`
	DeviceID      string `json:"did"`
	ChannelID     string `json:"cid"`
	App           string `json:"app"`
	Stream        string `json:"stream"`
	MediaServerID string `json:"node"`
	IssuedAt      int64  `json:"iat"`
	ExpiresAt     int64  `json:"exp"`
	Nonce         string `json:"nonce"`
}

type Grant struct {
	Token     string
	ExpiresAt time.Time
}

type DirectIssuer interface {
	IssueDirect(Binding) (Grant, error)
}

type Verifier interface {
	Verify(string, Binding) (Claims, error)
}

type Option func(*Signer) error

type Signer struct {
	key []byte
	ttl time.Duration
	now func() time.Time
}

func NewSigner(root []byte, opts ...Option) (*Signer, error) {
	if len(root) < minimumRootKeyBytes {
		return nil, ErrKeyInvalid
	}
	key, err := deriveKey(root, playKeyContext)
	if err != nil {
		return nil, err
	}
	signer := &Signer{key: key, ttl: DefaultTTL, now: time.Now}
	for _, opt := range opts {
		if err := opt(signer); err != nil {
			return nil, err
		}
	}
	return signer, nil
}

func WithTTL(ttl time.Duration) Option {
	return func(s *Signer) error {
		if ttl <= 0 {
			return ErrTokenInvalid
		}
		s.ttl = ttl
		return nil
	}
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

func (s *Signer) IssueDirect(binding Binding) (Grant, error) {
	if s == nil || !validBinding(binding) {
		return Grant{}, ErrTokenInvalid
	}
	now := s.now().UTC()
	expiresAt := now.Add(s.ttl)
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return Grant{}, ErrTokenInvalid
	}
	claims := Claims{
		Version: tokenVersion, Audience: tokenAudience, Mode: ModeDirect,
		DeviceID: binding.DeviceID, ChannelID: binding.ChannelID,
		App: binding.App, Stream: binding.Stream, MediaServerID: binding.MediaServerID,
		IssuedAt: now.Unix(), ExpiresAt: expiresAt.Unix(),
		Nonce: base64.RawURLEncoding.EncodeToString(nonce),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return Grant{}, ErrTokenInvalid
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signature := s.signature([]byte(encoded))
	return Grant{
		Token:     encoded + "." + base64.RawURLEncoding.EncodeToString(signature),
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Signer) Verify(token string, expected Binding) (Claims, error) {
	if s == nil || !validBinding(expected) {
		return Claims{}, ErrTokenInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Claims{}, ErrTokenInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, s.signature([]byte(parts[0]))) {
		return Claims{}, ErrTokenInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrTokenInvalid
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, ErrTokenInvalid
	}
	if claims.Version != tokenVersion || claims.Audience != tokenAudience || claims.Mode != ModeDirect ||
		claims.Nonce == "" || claims.DeviceID != expected.DeviceID || claims.ChannelID != expected.ChannelID ||
		claims.App != expected.App || claims.Stream != expected.Stream || claims.MediaServerID != expected.MediaServerID {
		return Claims{}, ErrTokenInvalid
	}
	now := s.now().UTC()
	if claims.IssuedAt <= 0 || claims.ExpiresAt <= claims.IssuedAt || time.Unix(claims.IssuedAt, 0).After(now.Add(maxClockSkew)) {
		return Claims{}, ErrTokenInvalid
	}
	if !now.Before(time.Unix(claims.ExpiresAt, 0)) {
		return Claims{}, ErrTokenExpired
	}
	return claims, nil
}

func (s *Signer) signature(payload []byte) []byte {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func CallbackCapability(apiSecret, mediaServerID string) (string, error) {
	if strings.TrimSpace(apiSecret) == "" || strings.TrimSpace(mediaServerID) == "" {
		return "", ErrCapabilityInvalid
	}
	key, err := deriveKey([]byte(apiSecret), callbackCapabilityDomain)
	if err != nil {
		return "", ErrCapabilityInvalid
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(mediaServerID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func VerifyCallbackCapability(apiSecret, mediaServerID, capability string) bool {
	want, err := CallbackCapability(apiSecret, mediaServerID)
	if err != nil || capability == "" {
		return false
	}
	return hmac.Equal([]byte(want), []byte(capability))
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
	mac := hmac.New(sha256.New, root)
	_, _ = mac.Write([]byte(context))
	return mac.Sum(nil), nil
}

func validBinding(binding Binding) bool {
	return validGBID(binding.DeviceID) && validGBID(binding.ChannelID) &&
		strings.TrimSpace(binding.App) != "" && strings.TrimSpace(binding.Stream) != "" &&
		strings.TrimSpace(binding.MediaServerID) != ""
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
