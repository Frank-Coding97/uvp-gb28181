package client

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const (
	masterKeySize = 32
	secretSize    = 32
	akRandomSize  = 16
	secretVersion = int64(1)
)

var (
	ErrInvalidMasterKey = errors.New("invalid OpenAPI master key")
	ErrInvalidSecret    = errors.New("invalid OpenAPI secret")
)

// SecretManager protects SK material with one deployment-injected AES-256
// master key. The key is copied and never exposed after construction.
type SecretManager struct {
	masterKey []byte
	keyID     string
	random    io.Reader
}

func NewSecretManager(masterKey []byte, keyID string) (*SecretManager, error) {
	if len(masterKey) != masterKeySize || keyID == "" {
		return nil, ErrInvalidMasterKey
	}
	return &SecretManager{
		masterKey: append([]byte(nil), masterKey...),
		keyID:     keyID,
		random:    rand.Reader,
	}, nil
}

func (m *SecretManager) KeyID() string {
	if m == nil {
		return ""
	}
	return m.keyID
}

func (m *SecretManager) GenerateAccessKey() (string, error) {
	if m == nil || len(m.masterKey) != masterKeySize {
		return "", ErrInvalidMasterKey
	}
	raw := make([]byte, akRandomSize)
	if _, err := io.ReadFull(m.random, raw); err != nil {
		return "", fmt.Errorf("generate access key: %w", err)
	}
	return "uvp_" + fmt.Sprintf("%x", raw), nil
}

func (m *SecretManager) GenerateSecretKey() (string, error) {
	if m == nil || len(m.masterKey) != masterKeySize {
		return "", ErrInvalidMasterKey
	}
	raw := make([]byte, secretSize)
	if _, err := io.ReadFull(m.random, raw); err != nil {
		return "", fmt.Errorf("generate secret key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// SecretAAD is deliberately stable and binds all identity/version fields that
// must not be swapped between rows or rotations.
func SecretAAD(clientID int64, accessKey string, version int64) []byte {
	return []byte(fmt.Sprintf("uvp-openapi/secret/v1/client=%d&ak=%s&version=%d", clientID, accessKey, version))
}

func (m *SecretManager) Encrypt(clientID int64, accessKey string, version int64, secret string) ([]byte, []byte, error) {
	if m == nil || len(m.masterKey) != masterKeySize || clientID < 0 || version <= 0 || !validSecretText(secret) {
		return nil, nil, ErrInvalidSecret
	}
	cipherBlock, err := aes.NewCipher(m.masterKey)
	if err != nil {
		return nil, nil, ErrInvalidSecret
	}
	gcm, err := cipher.NewGCM(cipherBlock)
	if err != nil {
		return nil, nil, ErrInvalidSecret
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(m.random, iv); err != nil {
		return nil, nil, fmt.Errorf("generate secret IV: %w", err)
	}
	ciphertext := gcm.Seal(nil, iv, []byte(secret), SecretAAD(clientID, accessKey, version))
	return ciphertext, iv, nil
}

func (m *SecretManager) Decrypt(clientID int64, accessKey, keyID string, version int64, ciphertext, iv []byte) (string, error) {
	if m == nil || len(m.masterKey) != masterKeySize || keyID != m.keyID || clientID < 0 || version <= 0 || len(iv) != 12 || len(ciphertext) <= 16 {
		return "", ErrInvalidSecret
	}
	cipherBlock, err := aes.NewCipher(m.masterKey)
	if err != nil {
		return "", ErrInvalidSecret
	}
	gcm, err := cipher.NewGCM(cipherBlock)
	if err != nil {
		return "", ErrInvalidSecret
	}
	plaintext, err := gcm.Open(nil, iv, ciphertext, SecretAAD(clientID, accessKey, version))
	if err != nil || !validSecretText(string(plaintext)) {
		return "", ErrInvalidSecret
	}
	return string(plaintext), nil
}

func validSecretText(secret string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(secret)
	return err == nil && len(decoded) == secretSize
}
