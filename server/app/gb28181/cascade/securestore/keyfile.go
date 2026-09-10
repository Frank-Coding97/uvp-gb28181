package securestore

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// LoadOrCreateCipher preserves explicit deployment keys and otherwise persists a local key.
// A complete temporary file is linked atomically, so concurrent startups never replace a key.
func LoadOrCreateCipher(envName, path, version string) (*Cipher, error) {
	if os.Getenv(envName) != "" {
		return LoadCipherFromEnv(envName, version)
	}
	read := func() (*Cipher, error) {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, ErrInvalidKey
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		key, err := decodeKey(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, err
		}
		return NewCipher(key, version)
	}
	c, err := read()
	if err == nil {
		return c, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cascade-key-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(base64.StdEncoding.EncodeToString(key)); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Link(f.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	return read()
}
