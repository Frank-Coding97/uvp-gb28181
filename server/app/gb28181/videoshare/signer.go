package videoshare

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	KeyID      = "v1"
	keyBytes   = 32
	keyFile    = "video-share.key"
	signDomain = "uvp-gb28181/video-share/v1"
)

var ErrInvalidCredential = errors.New("invalid video-share credential")

type Claims struct {
	PublicID   string
	Generation string
	ExpiresAt  int64
	KeyID      string
}

type Signer struct {
	key []byte
}

func NewSigner(key []byte) (*Signer, error) {
	if len(key) < keyBytes {
		return nil, errors.New("video-share signing key must be at least 32 bytes")
	}
	return &Signer{key: append([]byte(nil), key...)}, nil
}

func (s *Signer) Sign(claims Claims) (string, error) {
	if !validClaims(claims) {
		return "", ErrInvalidCredential
	}
	mac := hmac.New(sha256.New, s.key)
	_, _ = io.WriteString(mac, canonicalClaims(claims))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Signer) Verify(claims Claims, signature string) bool {
	want, err := s.Sign(claims)
	if err != nil || len(signature) != len(want) {
		return false
	}
	gotBytes, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || len(gotBytes) != sha256.Size {
		return false
	}
	wantBytes, _ := base64.RawURLEncoding.DecodeString(want)
	return subtle.ConstantTimeCompare(gotBytes, wantBytes) == 1
}

func validClaims(claims Claims) bool {
	return claims.PublicID != "" && claims.Generation != "" && claims.ExpiresAt > 0 && claims.KeyID == KeyID
}

func canonicalClaims(claims Claims) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s", signDomain, claims.PublicID, claims.Generation, claims.ExpiresAt, claims.KeyID)
}

// LoadOrCreateSigner uses an instance-persistent key file. Any malformed or
// unreadable existing file is an error; it never silently replaces key material.
func LoadOrCreateSigner(directory string) (*Signer, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(directory, keyFile)
	key, err := os.ReadFile(path)
	if err == nil {
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() || len(key) != keyBytes {
			return nil, errors.New("video-share signing key file is invalid")
		}
		return NewSigner(key)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key = make([]byte, keyBytes)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(directory, ".video-share-key-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(key)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if err = os.Link(tmp.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	key, err = os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return NewSigner(key)
}
