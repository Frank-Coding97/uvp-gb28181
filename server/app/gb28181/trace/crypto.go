package trace

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

const EncryptionAES256GCM = "AES-256-GCM"

var ErrInvalidEncryptionKey = errors.New("invalid SIP trace encryption key")

type EncryptedPayload struct {
	Ciphertext   []byte
	Nonce        []byte
	Algorithm    string
	KeyVersion   string
	DigestSHA256 string
}

type PayloadCipher interface {
	Encrypt([]byte) (EncryptedPayload, error)
}

type Cipher struct {
	aead       cipher.AEAD
	keyVersion string
}

func NewCipher(key []byte, keyVersion string) (*Cipher, error) {
	if len(key) != 32 {
		return nil, ErrInvalidEncryptionKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrInvalidEncryptionKey
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize SIP trace encryption: %w", err)
	}
	return &Cipher{aead: aead, keyVersion: keyVersion}, nil
}

func LoadCipherFromEnv(envName, keyVersion string) (*Cipher, error) {
	if envName == "" {
		return nil, ErrInvalidEncryptionKey
	}
	key, err := decodeEncryptionKey(os.Getenv(envName))
	if err != nil {
		return nil, err
	}
	return NewCipher(key, keyVersion)
}

func (c *Cipher) Encrypt(plaintext []byte) (EncryptedPayload, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return EncryptedPayload{}, fmt.Errorf("generate SIP trace nonce: %w", err)
	}
	aad := payloadAAD(EncryptionAES256GCM, c.keyVersion)
	ciphertext := c.aead.Seal(nil, nonce, plaintext, aad)
	digest := sha256.Sum256(plaintext)
	return EncryptedPayload{
		Ciphertext:   ciphertext,
		Nonce:        nonce,
		Algorithm:    EncryptionAES256GCM,
		KeyVersion:   c.keyVersion,
		DigestSHA256: hex.EncodeToString(digest[:]),
	}, nil
}

func (c *Cipher) Decrypt(payload EncryptedPayload) ([]byte, error) {
	if payload.Algorithm != EncryptionAES256GCM || payload.KeyVersion != c.keyVersion {
		return nil, fmt.Errorf("unsupported SIP trace encryption metadata")
	}
	plaintext, err := c.aead.Open(nil, payload.Nonce, payload.Ciphertext, payloadAAD(payload.Algorithm, payload.KeyVersion))
	if err != nil {
		return nil, fmt.Errorf("decrypt SIP trace payload: authentication failed")
	}
	digest := sha256.Sum256(plaintext)
	if hex.EncodeToString(digest[:]) != payload.DigestSHA256 {
		return nil, fmt.Errorf("decrypt SIP trace payload: digest mismatch")
	}
	return plaintext, nil
}

func decodeEncryptionKey(value string) ([]byte, error) {
	if len(value) == 32 {
		return []byte(value), nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	return nil, ErrInvalidEncryptionKey
}

func payloadAAD(algorithm, keyVersion string) []byte {
	return []byte(algorithm + "\x00" + keyVersion)
}

type failingCipher struct {
	err error
}

func (c failingCipher) Encrypt([]byte) (EncryptedPayload, error) {
	return EncryptedPayload{}, c.err
}
