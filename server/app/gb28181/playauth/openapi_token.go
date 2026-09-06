package playauth

import (
	"bytes"
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	openAPIPlayKeyContext = "uvp-gb28181/openapi-play-authorization/v3"
	openAPIPlayAudience   = "gb28181-openapi-play"
	OpenAPIPlayTTL        = 120 * time.Second
	maxOpenAPITokenBytes  = 8192
)

// OpenAPIBinding is populated from an authorized, persisted grant, never from
// the caller's HTTP body. It does not grant permission by itself. The runtime
// must also qualify the node/protocol and check current persistent authority.
type OpenAPIBinding struct {
	GrantID         string `json:"grant"`
	ClientID        int64  `json:"client"`
	ClientEpoch     int64  `json:"cepoch"`
	Scope           string `json:"scope"`
	ScopeEpoch      int64  `json:"sepoch"`
	DeviceEpoch     int64  `json:"depoch"`
	DeviceID        string `json:"did"`
	ChannelID       string `json:"cid"`
	NodeUUID        string `json:"node"`
	BootNonce       string `json:"boot"`
	Schema          string `json:"schema"`
	VHost           string `json:"vhost"`
	App             string `json:"app"`
	Stream          string `json:"stream"`
	MediaGeneration uint64 `json:"mgen"`
	Protocol        string `json:"protocol"`
}

type OpenAPIClaims struct {
	Version  int    `json:"v"`
	Audience string `json:"aud"`
	KeyID    string `json:"kid"`
	OpenAPIBinding
	IssuedAt  int64 `json:"iat"`
	ExpiresAt int64 `json:"exp"`
}

// IssueOpenAPI signs only the supplied, fully prepared binding. T10's grant
// transaction must persist matching claims before this token reaches a caller.
// The returned authorization generation is the durable random grant UUID.
func (s *Signer) IssueOpenAPI(binding OpenAPIBinding) (Grant, error) {
	if s == nil || s.now == nil || !validOpenAPIBinding(binding) {
		return Grant{}, ErrTokenInvalid
	}
	key, ok := s.keys[s.activeID]
	if !ok || len(key.openapi) != 32 {
		return Grant{}, ErrKeyInvalid
	}
	now := s.now().UTC().Truncate(time.Second)
	if now.Unix() <= 0 {
		return Grant{}, ErrTokenInvalid
	}
	claims := OpenAPIClaims{Version: 3, Audience: openAPIPlayAudience, KeyID: s.activeID, OpenAPIBinding: binding, IssuedAt: now.Unix(), ExpiresAt: now.Add(OpenAPIPlayTTL).Unix()}
	payload, err := json.Marshal(claims)
	if err != nil {
		return Grant{}, ErrTokenInvalid
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	token := encoded + "." + base64.RawURLEncoding.EncodeToString(signature(key.openapi, []byte(encoded)))
	if len(token) > maxOpenAPITokenBytes {
		return Grant{}, ErrTokenInvalid
	}
	return Grant{Token: token, ExpiresAt: time.Unix(claims.ExpiresAt, 0).UTC(), AuthorizationGeneration: binding.GrantID}, nil
}

// AuthenticateOpenAPI proves integrity and format ONLY. It does not authorize
// playback, check current epochs, bind a viewer, or reject ordinary expiration.
// The durable service must check expiration for first binding; an identical
// already-bound connection may be rechecked without treating TTL as revocation.
// Never return an allowing Hook response from this method alone.
func (s *Signer) AuthenticateOpenAPI(token string) (OpenAPIClaims, error) {
	if s == nil || s.now == nil || len(token) == 0 || len(token) > maxOpenAPITokenBytes {
		return OpenAPIClaims{}, ErrTokenInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return OpenAPIClaims{}, ErrTokenInvalid
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil || base64.RawURLEncoding.EncodeToString(payload) != parts[0] || !utf8.Valid(payload) {
		return OpenAPIClaims{}, ErrTokenInvalid
	}
	var claims OpenAPIClaims
	if json.Unmarshal(payload, &claims) != nil {
		return OpenAPIClaims{}, ErrTokenInvalid
	}
	key, ok := s.keys[claims.KeyID]
	mac, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if !ok || len(key.openapi) != 32 || err != nil || len(mac) != 32 || base64.RawURLEncoding.EncodeToString(mac) != parts[1] || !hmac.Equal(mac, signature(key.openapi, []byte(parts[0]))) {
		return OpenAPIClaims{}, ErrTokenInvalid
	}
	// Tokens are produced by this exact canonical encoder. Requiring its byte
	// representation rejects duplicate, case-shadowed, unknown and null fields.
	canonical, err := json.Marshal(claims)
	if err != nil || !bytes.Equal(payload, canonical) || claims.Version != 3 || claims.Audience != openAPIPlayAudience || claims.KeyID == "" || !validOpenAPIBinding(claims.OpenAPIBinding) || claims.IssuedAt <= 0 || claims.ExpiresAt <= claims.IssuedAt || claims.ExpiresAt-claims.IssuedAt != int64(OpenAPIPlayTTL/time.Second) || time.Unix(claims.IssuedAt, 0).After(s.now().UTC().Add(maxClockSkew)) {
		return OpenAPIClaims{}, ErrTokenInvalid
	}
	return claims, nil
}

// VerifyOpenAPI adds expected-binding and new-connection TTL checks, but still
// cannot replace the mandatory durable grant/current-authority transaction.
func (s *Signer) VerifyOpenAPI(token string, expected OpenAPIBinding) (OpenAPIClaims, error) {
	claims, err := s.AuthenticateOpenAPI(token)
	if err != nil {
		return OpenAPIClaims{}, err
	}
	if claims.OpenAPIBinding != expected {
		return OpenAPIClaims{}, ErrTokenBindingMismatch
	}
	if !s.now().UTC().Before(time.Unix(claims.ExpiresAt, 0)) {
		return OpenAPIClaims{}, ErrTokenExpired
	}
	return claims, nil
}

func validOpenAPIBinding(b OpenAPIBinding) bool {
	id, err := uuid.Parse(b.GrantID)
	if err != nil || id == uuid.Nil || id.String() != b.GrantID || b.ClientID <= 0 || b.ClientEpoch <= 0 || b.ScopeEpoch <= 0 || b.DeviceEpoch <= 0 || b.Scope != "play:live:apply" || !validGBID(b.DeviceID) || !validGBID(b.ChannelID) || b.MediaGeneration == 0 || len(b.BootNonce) != 32 || (b.Protocol != "https-flv" && b.Protocol != "wss-flv") {
		return false
	}
	for _, c := range b.BootNonce {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	for _, field := range []struct {
		value string
		max   int
	}{{b.NodeUUID, 64}, {b.Schema, 32}, {b.VHost, 128}, {b.App, 64}, {b.Stream, 255}} {
		if !validOpenAPIField(field.value, field.max) {
			return false
		}
	}
	return true
}

func validOpenAPIField(value string, max int) bool {
	if value == "" || len(value) > max || strings.TrimSpace(value) != value || !utf8.ValidString(value) {
		return false
	}
	for _, c := range value {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}
