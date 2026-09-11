package securestore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const AlgorithmAES256GCM = "AES-256-GCM"

var (
	ErrKeyUnavailable = errors.New("cascade encryption key unavailable")
	ErrInvalidKey     = errors.New("invalid cascade encryption key")
	ErrInvalidPurpose = errors.New("invalid cascade encryption purpose")
	ErrAuthentication = errors.New("cascade ciphertext authentication failed")
)

type Envelope struct {
	Ciphertext []byte `json:"ciphertext"`
	Nonce      []byte `json:"nonce"`
	Algorithm  string `json:"algorithm"`
	KeyVersion string `json:"keyVersion"`
}

type Cipher struct {
	aead       cipher.AEAD
	keyVersion string
}

func NewCipher(key []byte, keyVersion string) (*Cipher, error) {
	if len(key) != 32 || strings.TrimSpace(keyVersion) == "" {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrInvalidKey
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize cascade encryption: %w", err)
	}
	return &Cipher{aead: aead, keyVersion: keyVersion}, nil
}

func LoadCipherFromEnv(envName, keyVersion string) (*Cipher, error) {
	if strings.TrimSpace(envName) == "" {
		return nil, ErrKeyUnavailable
	}
	value := os.Getenv(envName)
	if value == "" {
		return nil, ErrKeyUnavailable
	}
	key, err := decodeKey(value)
	if err != nil {
		return nil, err
	}
	return NewCipher(key, keyVersion)
}

func (c *Cipher) Encrypt(purpose string, plaintext []byte) (Envelope, error) {
	if strings.TrimSpace(purpose) == "" {
		return Envelope{}, ErrInvalidPurpose
	}
	if c == nil || c.aead == nil {
		return Envelope{}, ErrKeyUnavailable
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return Envelope{}, fmt.Errorf("generate cascade nonce: %w", err)
	}
	return Envelope{
		Ciphertext: c.aead.Seal(nil, nonce, plaintext, additionalData(AlgorithmAES256GCM, c.keyVersion, purpose)),
		Nonce:      nonce, Algorithm: AlgorithmAES256GCM, KeyVersion: c.keyVersion,
	}, nil
}

func (c *Cipher) Decrypt(purpose string, envelope Envelope) ([]byte, error) {
	if strings.TrimSpace(purpose) == "" {
		return nil, ErrInvalidPurpose
	}
	if c == nil || c.aead == nil {
		return nil, ErrKeyUnavailable
	}
	if envelope.Algorithm != AlgorithmAES256GCM || envelope.KeyVersion != c.keyVersion {
		return nil, ErrAuthentication
	}
	plaintext, err := c.aead.Open(nil, envelope.Nonce, envelope.Ciphertext, additionalData(envelope.Algorithm, envelope.KeyVersion, purpose))
	if err != nil {
		return nil, ErrAuthentication
	}
	return plaintext, nil
}

func decodeKey(value string) ([]byte, error) {
	if len(value) == 32 {
		return []byte(value), nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	return nil, ErrInvalidKey
}

func additionalData(algorithm, keyVersion, purpose string) []byte {
	return []byte("uvp-gb28181-cascade\x00" + algorithm + "\x00" + keyVersion + "\x00" + purpose)
}
