package standalone

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	runMarkerName   = ".uvp-running.json"
	runMarkerSchema = 1
)

var errRunMarkerInvalid = errors.New("standalone: invalid run marker")

type runMarkerPayload struct {
	Schema    int    `json:"schema"`
	Nonce     string `json:"nonce"`
	StartedAt string `json:"started_at"`
}

// RunMarker identifies one standalone process lifetime. PreviousUnclean is
// true when a valid marker from an earlier lifetime was still present.
type RunMarker struct {
	PreviousUnclean bool

	path    string
	lockDir string
	nonce   string
	once    sync.Once
	err     error
}

// BeginRun records a new process lifetime under the protected data directory.
// The caller must acquire the instance lock and validate/initialize Paths
// before calling this function.
func BeginRun(paths Paths) (*RunMarker, error) {
	if !paths.Explicit {
		return nil, errors.New("standalone: run marker requires explicit paths")
	}
	installDir, err := cleanAbsolute(paths.InstallDir)
	if err != nil {
		return nil, fmt.Errorf("run marker install directory: %w", err)
	}
	dataDir, err := cleanAbsolute(paths.DataDir)
	if err != nil {
		return nil, fmt.Errorf("run marker data directory: %w", err)
	}
	if !isWithin(installDir, dataDir) {
		return nil, fmt.Errorf("run marker data directory: %w", ErrPathOutsideInstall)
	}

	markerPath := filepath.Join(dataDir, runMarkerName)
	var previousUnclean bool
	var nonce string
	err = withConfigLock(installDir, func() error {
		raw, readErr := readSecureConfigFile(markerPath)
		switch {
		case readErr == nil:
			if _, err := decodeRunMarker(raw); err != nil {
				return err
			}
			previousUnclean = true
		case errors.Is(readErr, os.ErrNotExist):
			// No marker is the clean first-run state.
		default:
			return readErr
		}

		nonce, err = newRunNonce()
		if err != nil {
			return err
		}
		payload := runMarkerPayload{
			Schema:    runMarkerSchema,
			Nonce:     nonce,
			StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode run marker: %w", err)
		}
		return writeSecureConfigFile(markerPath, encoded, previousUnclean, nil)
	})
	if err != nil {
		return nil, err
	}
	return &RunMarker{PreviousUnclean: previousUnclean, path: markerPath, lockDir: installDir, nonce: nonce}, nil
}

// Finish removes this lifetime's marker only when the current file still
// carries this lifetime's nonce. It is safe to call repeatedly or together
// from multiple cleanup paths.
func (m *RunMarker) Finish() error {
	if m == nil {
		return nil
	}
	m.once.Do(func() { m.err = m.finish() })
	return m.err
}

func (m *RunMarker) finish() error {
	if m.path == "" || m.lockDir == "" || m.nonce == "" {
		return errors.New("standalone: run marker is not initialized")
	}
	return withConfigLock(m.lockDir, func() error {
		raw, err := readSecureConfigFile(m.path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		payload, err := decodeRunMarker(raw)
		if err != nil {
			return err
		}
		if payload.Nonce != m.nonce {
			return nil
		}
		if err := os.Remove(m.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove run marker %q: %w", m.path, err)
		}
		return nil
	})
}

func newRunNonce() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", errors.New("generate run marker nonce")
	}
	return hex.EncodeToString(raw[:]), nil
}

func decodeRunMarker(raw []byte) (runMarkerPayload, error) {
	var payload runMarkerPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return runMarkerPayload{}, fmt.Errorf("%w: %v", errRunMarkerInvalid, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return runMarkerPayload{}, fmt.Errorf("%w: multiple JSON values", errRunMarkerInvalid)
		}
		return runMarkerPayload{}, fmt.Errorf("%w: trailing data", errRunMarkerInvalid)
	}
	if payload.Schema != runMarkerSchema {
		return runMarkerPayload{}, fmt.Errorf("%w: unsupported schema", errRunMarkerInvalid)
	}
	if len(payload.Nonce) != hex.EncodedLen(32) {
		return runMarkerPayload{}, fmt.Errorf("%w: invalid nonce", errRunMarkerInvalid)
	}
	var nonce [32]byte
	if _, err := hex.Decode(nonce[:], []byte(payload.Nonce)); err != nil {
		return runMarkerPayload{}, fmt.Errorf("%w: invalid nonce", errRunMarkerInvalid)
	}
	if !strings.HasSuffix(payload.StartedAt, "Z") {
		return runMarkerPayload{}, fmt.Errorf("%w: timestamp is not UTC", errRunMarkerInvalid)
	}
	startedAt, err := time.Parse(time.RFC3339Nano, payload.StartedAt)
	if err != nil || startedAt.Location() != time.UTC {
		return runMarkerPayload{}, fmt.Errorf("%w: invalid timestamp", errRunMarkerInvalid)
	}
	return payload, nil
}
