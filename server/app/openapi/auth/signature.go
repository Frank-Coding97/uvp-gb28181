package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// Sign computes the lowercase hexadecimal HMAC-SHA256 signature. secretKey
// is the unpadded base64url representation of exactly 32 decoded key bytes.
func Sign(input SignatureInput, secretKey string) (string, error) {
	key, err := decodeSecretKey(secretKey)
	if err != nil {
		return "", errInvalidInput
	}
	canonical, err := CanonicalString(input)
	if err != nil {
		return "", errInvalidInput
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// Verify checks a lowercase hexadecimal signature using constant-time HMAC
// comparison. It returns the same generic error for malformed or mismatched
// signatures and never includes expected signature material.
func Verify(input SignatureInput, secretKey, signature string) error {
	if !isLowerHex(signature, 32) {
		return errInvalidInput
	}
	key, err := decodeSecretKey(secretKey)
	if err != nil {
		return errInvalidInput
	}
	canonical, err := CanonicalString(input)
	if err != nil {
		return errInvalidInput
	}
	provided, err := hex.DecodeString(signature)
	if err != nil {
		return errInvalidInput
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(canonical))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return errInvalidInput
	}
	return nil
}

func decodeSecretKey(secretKey string) ([]byte, error) {
	key, err := base64.RawURLEncoding.DecodeString(secretKey)
	if err != nil || len(key) != 32 {
		return nil, errInvalidInput
	}
	return key, nil
}
