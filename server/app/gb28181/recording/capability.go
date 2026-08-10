package recording

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

const (
	CapabilityModePlay     = "play"
	CapabilityModeDownload = "download"

	capabilityAudience = "cloud-recording-content"
	capabilityVersion  = 1
)

var (
	ErrCapabilityKey     = errors.New("invalid capability key")
	ErrCapabilityInvalid = errors.New("invalid capability")
	ErrCapabilityExpired = errors.New("expired capability")
)

type CapabilityClaims struct {
	Version  int    `json:"v"`
	KeyID    string `json:"kid"`
	Audience string `json:"aud"`
	FileID   string `json:"fid"`
	Mode     string `json:"mode"`
	UserID   uint   `json:"uid"`
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
	JTI      string `json:"jti"`
}

type CapabilityGrant struct {
	Token     string
	ExpiresAt time.Time
}

type CapabilitySigner struct {
	key   []byte
	keyID string
	now   func() time.Time
}

func NewCapabilitySigner(key []byte, keyID string) (*CapabilitySigner, error) {
	if len(key) < 32 || keyID == "" {
		return nil, ErrCapabilityKey
	}
	return &CapabilitySigner{key: append([]byte(nil), key...), keyID: keyID, now: time.Now}, nil
}

func (s *CapabilitySigner) Issue(fileID string, userID uint, mode string, durationSeconds *float64) (CapabilityGrant, error) {
	if fileID == "" || userID == 0 || !validCapabilityMode(mode) {
		return CapabilityGrant{}, ErrCapabilityInvalid
	}
	now := s.now().UTC()
	expiresAt, err := capabilityExpiry(now, mode, durationSeconds)
	if err != nil {
		return CapabilityGrant{}, err
	}
	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil {
		return CapabilityGrant{}, ErrCapabilityInvalid
	}
	claims := CapabilityClaims{
		Version: capabilityVersion, KeyID: s.keyID, Audience: capabilityAudience,
		FileID: fileID, Mode: mode, UserID: userID,
		IssuedAt: now.Unix(), Expires: expiresAt.Unix(),
		JTI: base64.RawURLEncoding.EncodeToString(jtiBytes),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return CapabilityGrant{}, ErrCapabilityInvalid
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	token := encodedPayload + "." + base64.RawURLEncoding.EncodeToString(s.signature([]byte(encodedPayload)))
	return CapabilityGrant{Token: token, ExpiresAt: expiresAt}, nil
}

func (s *CapabilitySigner) Verify(token, fileID, mode string) (CapabilityClaims, error) {
	payloadPart, signaturePart, ok := splitCapability(token)
	if !ok {
		return CapabilityClaims{}, ErrCapabilityInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(signaturePart)
	if err != nil || !hmac.Equal(signature, s.signature([]byte(payloadPart))) {
		return CapabilityClaims{}, ErrCapabilityInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return CapabilityClaims{}, ErrCapabilityInvalid
	}
	var claims CapabilityClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return CapabilityClaims{}, ErrCapabilityInvalid
	}
	if claims.Version != capabilityVersion || claims.KeyID != s.keyID || claims.Audience != capabilityAudience ||
		claims.FileID != fileID || claims.Mode != mode || claims.UserID == 0 || claims.JTI == "" || !validCapabilityMode(claims.Mode) {
		return CapabilityClaims{}, ErrCapabilityInvalid
	}
	now := s.now().UTC().Unix()
	if claims.Expires <= now {
		return CapabilityClaims{}, ErrCapabilityExpired
	}
	if claims.IssuedAt > now || claims.Expires <= claims.IssuedAt {
		return CapabilityClaims{}, ErrCapabilityInvalid
	}
	return claims, nil
}

func capabilityExpiry(now time.Time, mode string, durationSeconds *float64) (time.Time, error) {
	switch mode {
	case CapabilityModeDownload:
		return now.Add(15 * time.Minute), nil
	case CapabilityModePlay:
		ttl := 90 * time.Minute
		if durationSeconds != nil {
			if *durationSeconds < 0 {
				return time.Time{}, ErrCapabilityInvalid
			}
			ttl = time.Duration(*durationSeconds*float64(time.Second)) + 30*time.Minute
		}
		if ttl > 2*time.Hour {
			ttl = 2 * time.Hour
		}
		return now.Add(ttl), nil
	default:
		return time.Time{}, ErrCapabilityInvalid
	}
}

func (s *CapabilitySigner) signature(payload []byte) []byte {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func validCapabilityMode(mode string) bool {
	return mode == CapabilityModePlay || mode == CapabilityModeDownload
}

func splitCapability(token string) (string, string, bool) {
	for i := 0; i < len(token); i++ {
		if token[i] != '.' {
			continue
		}
		if i == 0 || i == len(token)-1 {
			return "", "", false
		}
		for j := i + 1; j < len(token); j++ {
			if token[j] == '.' {
				return "", "", false
			}
		}
		return token[:i], token[i+1:], true
	}
	return "", "", false
}
