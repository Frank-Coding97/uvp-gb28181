// Package bootstrapcredential carries a one-time bootstrap credential over a
// dedicated stdin frame without persisting the credential.
package bootstrapcredential

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"sync"
)

const (
	protocolPrefix = "UVP-BOOTSTRAP-1 "
	tokenByteSize  = 32
	tokenTextSize  = 43
	frameSize      = len(protocolPrefix) + tokenTextSize + 1
)

var (
	// ErrInvalidCredential is returned for every malformed, unknown, consumed,
	// or invalidated credential. It intentionally contains no input data.
	ErrInvalidCredential = errors.New("invalid bootstrap credential")

	errCredentialWrite = errors.New("bootstrap credential write failed")
)

// Verifier owns the digest of one bootstrap credential. The clear flag is
// represented by valid so the digest can be wiped on both successful use and
// invalidation.
type Verifier struct {
	mu    sync.Mutex
	hash  [sha256.Size]byte
	valid bool
}

// NewToken creates a fresh 32-byte URL-safe, unpadded credential. The token is
// returned to the caller only and is never persisted by this package.
func NewToken() (string, error) {
	var raw [tokenByteSize]byte
	if _, err := rand.Read(raw[:]); err != nil {
		clear(raw[:])
		return "", errors.New("generate bootstrap credential")
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	clear(raw[:])
	return token, nil
}

// Write writes one complete bootstrap frame. Ownership of w remains with the
// caller; a caller using a pipe must close it after Write returns.
func Write(w io.Writer, token string) error {
	if w == nil || !validToken(token) {
		return ErrInvalidCredential
	}
	frame := make([]byte, 0, frameSize)
	frame = append(frame, protocolPrefix...)
	frame = append(frame, token...)
	frame = append(frame, '\n')
	defer clear(frame)
	for len(frame) > 0 {
		written, err := w.Write(frame)
		if written < 0 || written > len(frame) {
			return errCredentialWrite
		}
		if written > 0 {
			frame = frame[written:]
		}
		if err != nil || written == 0 {
			return errCredentialWrite
		}
	}
	return nil
}

// Read consumes at most one frame plus one byte. The extra byte distinguishes
// an exact frame from a frame with trailing data; the caller owns the reader
// and is responsible for applying an outer startup deadline.
func Read(r io.Reader) (string, error) {
	if r == nil {
		return "", ErrInvalidCredential
	}
	raw, err := io.ReadAll(io.LimitReader(r, int64(frameSize+1)))
	defer clear(raw)
	if err != nil || len(raw) != frameSize {
		return "", ErrInvalidCredential
	}
	if !bytes.Equal(raw[:len(protocolPrefix)], []byte(protocolPrefix)) || raw[frameSize-1] != '\n' {
		return "", ErrInvalidCredential
	}
	token := string(raw[len(protocolPrefix) : len(protocolPrefix)+tokenTextSize])
	if !validToken(token) {
		return "", ErrInvalidCredential
	}
	return token, nil
}

// NewVerifier prepares a one-time verifier without retaining the plaintext
// credential.
func NewVerifier(token string) (*Verifier, error) {
	digest, ok := digestToken(token)
	if !ok {
		return nil, ErrInvalidCredential
	}
	return &Verifier{hash: digest, valid: true}, nil
}

// Use verifies token and runs callback while holding the verifier lock. A
// successful callback consumes the credential; a failed callback leaves it
// available for a retry.
func (v *Verifier) Use(token string, callback func() error) error {
	if v == nil || callback == nil {
		return ErrInvalidCredential
	}
	digest, ok := digestToken(token)
	if !ok {
		return ErrInvalidCredential
	}
	defer clear(digest[:])

	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.valid || subtle.ConstantTimeCompare(v.hash[:], digest[:]) != 1 {
		return ErrInvalidCredential
	}
	if err := callback(); err != nil {
		return err
	}
	clear(v.hash[:])
	v.valid = false
	return nil
}

// WithCredential is the descriptive name for Use and has identical locking
// and consume semantics.
func (v *Verifier) WithCredential(token string, callback func() error) error {
	return v.Use(token, callback)
}

// Consume verifies and consumes token without running an external operation.
// WithCredential is preferred when the consume decision depends on a
// transaction that may need to be retried.
func (v *Verifier) Consume(token string) error {
	return v.Use(token, func() error { return nil })
}

// Invalidate permanently disables this verifier and clears its stored digest.
func (v *Verifier) Invalidate() {
	if v == nil {
		return
	}
	v.mu.Lock()
	clear(v.hash[:])
	v.valid = false
	v.mu.Unlock()
}

func validToken(token string) bool {
	if len(token) != tokenTextSize {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != tokenByteSize || base64.RawURLEncoding.EncodeToString(raw) != token {
		clear(raw)
		return false
	}
	clear(raw)
	return true
}

func digestToken(token string) ([sha256.Size]byte, bool) {
	var digest [sha256.Size]byte
	if !validToken(token) {
		return digest, false
	}
	digest = sha256.Sum256([]byte(token))
	return digest, true
}
