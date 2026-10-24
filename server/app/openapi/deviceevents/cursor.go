package deviceevents

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"
)

type cursorBinding struct {
	ClientID       int64
	OwnerDeptID    uint
	DataScope      int8
	DeviceID       uint
	Kind           EventKind
	SubscriptionID string
}

type cursorPayload struct {
	Version        int       `json:"v"`
	ClientID       int64     `json:"c"`
	OwnerDeptID    uint      `json:"d"`
	DataScope      int8      `json:"s"`
	DeviceID       uint      `json:"r"`
	Kind           EventKind `json:"k"`
	SubscriptionID string    `json:"u,omitempty"`
	LastID         uint64    `json:"a"`
	HighWater      uint64    `json:"h"`
	IssuedAt       int64     `json:"i"`
}

type cursorCodec struct {
	key []byte
	ttl time.Duration
	now func() time.Time
}

func (codec cursorCodec) encode(payload cursorPayload) (string, error) {
	if len(codec.key) < 32 || codec.now == nil {
		return "", ErrUnavailable
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", ErrUnavailable
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	signature := hmacDigest(codec.key, encoded)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (codec cursorCodec) decode(token string, binding cursorBinding) (cursorPayload, error) {
	if len(codec.key) < 32 || codec.now == nil {
		return cursorPayload{}, ErrUnavailable
	}
	var payload cursorPayload
	parts := splitCursor(token)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return payload, ErrInvalidCursor
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, hmacDigest(codec.key, parts[0])) {
		return payload, ErrInvalidCursor
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(body, &payload) != nil {
		return cursorPayload{}, ErrInvalidCursor
	}
	if payload.Version != 1 || payload.ClientID != binding.ClientID || payload.OwnerDeptID != binding.OwnerDeptID || payload.DataScope != binding.DataScope || payload.DeviceID != binding.DeviceID || payload.Kind != binding.Kind || payload.SubscriptionID != binding.SubscriptionID || payload.IssuedAt <= 0 || payload.HighWater > 0 && payload.LastID > payload.HighWater {
		return cursorPayload{}, ErrInvalidCursor
	}
	now := codec.now()
	issuedAt := time.Unix(payload.IssuedAt, 0)
	if issuedAt.After(now.Add(time.Minute)) {
		return cursorPayload{}, ErrInvalidCursor
	}
	if now.Sub(issuedAt) > codec.ttl {
		return cursorPayload{}, ErrExpiredCursor
	}
	return payload, nil
}

func hmacDigest(key []byte, content string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(content))
	return mac.Sum(nil)
}

func splitCursor(token string) []string {
	for index := range token {
		if token[index] == '.' {
			if index != len(token)-1 && !containsDot(token[index+1:]) {
				return []string{token[:index], token[index+1:]}
			}
			return nil
		}
	}
	return nil
}

func containsDot(value string) bool {
	for index := range value {
		if value[index] == '.' {
			return true
		}
	}
	return false
}
