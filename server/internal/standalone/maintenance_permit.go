package standalone

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	maintenancePermitFileName      = "permit.json"
	maintenancePermitFramePrefix   = "UVP-MAINTENANCE-1 "
	maintenancePermitTokenByteSize = 32
	maintenancePermitTokenTextSize = 43
	maintenancePermitFrameSize     = len(maintenancePermitFramePrefix) + maintenancePermitTokenTextSize + 1
	maintenancePermitMaxFileSize   = 4096
	maintenancePermitTTL           = time.Minute
)

var errInvalidMaintenancePermit = errors.New("standalone: invalid maintenance permit")
var errMaintenancePermitOwner = errors.New("standalone: maintenance permit requires parent instance ownership")

// MaintenancePermitClaims identifies the one maintenance action authorized by
// a successfully consumed permit. The frame secret is never returned.
type MaintenancePermitClaims struct {
	OperationID string `json:"operation_id"`
	Purpose     string `json:"purpose"`
	Version     string `json:"version"`
}

type maintenancePermitEnvelope struct {
	Digest        string    `json:"digest"`
	OperationID   string    `json:"operation_id"`
	Purpose       string    `json:"purpose"`
	Version       string    `json:"version"`
	BackendSHA256 string    `json:"backend_sha256"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// IssueMaintenancePermit creates one short-lived permit for a child process.
// The caller must retain InstanceLock through the maintenance transaction and
// child lifetime.
func IssueMaintenancePermit(installDir, operationID, purpose, version string) ([]byte, error) {
	root, err := cleanAbsolute(installDir)
	if err != nil {
		return nil, err
	}
	var frame []byte
	err = withMaintenancePermitLock(root, func() error {
		journal, err := ReadMaintenanceJournal(root)
		if err != nil {
			return err
		}
		if journal.OperationID != operationID {
			return errInvalidMaintenancePermit
		}
		if err := validateMaintenancePermitSelection(journal, purpose, version); err != nil {
			return err
		}
		release, err := LoadReleaseVersion(root, version)
		if err != nil {
			return fmt.Errorf("validate maintenance permit release: %w", err)
		}
		backendSHA, err := releaseFileSHA256(release.BackendExe)
		if err != nil {
			return fmt.Errorf("hash maintenance permit backend: %w", err)
		}
		secret := make([]byte, maintenancePermitTokenByteSize)
		if _, err := rand.Read(secret); err != nil {
			clear(secret)
			return errors.New("generate maintenance permit")
		}
		digest := sha256.Sum256(secret)
		frame = make([]byte, 0, maintenancePermitFrameSize)
		frame = append(frame, maintenancePermitFramePrefix...)
		frame = append(frame, base64.RawURLEncoding.EncodeToString(secret)...)
		frame = append(frame, '\n')
		clear(secret)
		envelope := maintenancePermitEnvelope{
			Digest:        hex.EncodeToString(digest[:]),
			OperationID:   journal.OperationID,
			Purpose:       purpose,
			Version:       version,
			BackendSHA256: backendSHA,
			ExpiresAt:     time.Now().UTC().Add(maintenancePermitTTL),
		}
		raw, err := json.Marshal(envelope)
		clear(digest[:])
		if err != nil {
			return err
		}
		if err := writeSecureConfigFile(maintenancePermitPath(root), raw, false, nil); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		clear(frame)
		return nil, err
	}
	return frame, nil
}

// ConsumeMaintenancePermit verifies and atomically consumes one permit. The
// caller owns any outer deadline on reader; this function reads one fixed frame
// and does not wait for EOF.
func ConsumeMaintenancePermit(installDir, purpose string, reader io.Reader) (MaintenancePermitClaims, error) {
	executable, err := os.Executable()
	if err != nil {
		return MaintenancePermitClaims{}, errors.New("locate maintenance executable")
	}
	return consumeMaintenancePermitAt(installDir, purpose, reader, executable, time.Now)
}

func consumeMaintenancePermitAt(installDir, purpose string, reader io.Reader, executable string, now func() time.Time) (MaintenancePermitClaims, error) {
	var claims MaintenancePermitClaims
	root, err := cleanAbsolute(installDir)
	if err != nil {
		return claims, err
	}
	if now == nil {
		now = time.Now
	}
	err = withMaintenancePermitLock(root, func() error {
		envelope, err := readMaintenancePermitEnvelope(root)
		if err != nil {
			return err
		}
		journal, err := ReadMaintenanceJournal(root)
		if err != nil {
			return err
		}
		if envelope.OperationID != journal.OperationID {
			return errInvalidMaintenancePermit
		}
		if envelope.Purpose != purpose {
			return errInvalidMaintenancePermit
		}
		if err := validateMaintenancePermitSelection(journal, envelope.Purpose, envelope.Version); err != nil {
			return err
		}
		secret, err := readMaintenancePermitFrame(reader)
		if err != nil {
			return err
		}
		defer clear(secret)
		if !maintenancePermitDigestMatches(secret, envelope.Digest) {
			return errInvalidMaintenancePermit
		}
		nowUTC := now().UTC()
		if !envelope.ExpiresAt.After(nowUTC) || envelope.ExpiresAt.Sub(nowUTC) > maintenancePermitTTL {
			return errInvalidMaintenancePermit
		}
		release, err := LoadReleaseVersion(root, envelope.Version)
		if err != nil {
			return fmt.Errorf("validate maintenance permit release: %w", err)
		}
		actualPath, err := cleanAbsolute(executable)
		if err != nil || !sameMaintenanceExecutablePath(actualPath, release.BackendExe) {
			return errInvalidMaintenancePermit
		}
		candidateSHA, err := releaseFileSHA256(release.BackendExe)
		if err != nil {
			return fmt.Errorf("hash maintenance permit release: %w", err)
		}
		actualSHA, err := releaseFileSHA256(actualPath)
		if err != nil || !strings.EqualFold(candidateSHA, envelope.BackendSHA256) || !strings.EqualFold(actualSHA, candidateSHA) {
			return errInvalidMaintenancePermit
		}
		if err := requireMaintenanceInstanceLock(root); err != nil {
			return err
		}
		if err := os.Remove(maintenancePermitPath(root)); err != nil {
			return fmt.Errorf("consume maintenance permit: %w", err)
		}
		claims = MaintenancePermitClaims{OperationID: envelope.OperationID, Purpose: envelope.Purpose, Version: envelope.Version}
		return nil
	})
	if err != nil {
		return MaintenancePermitClaims{}, err
	}
	return claims, nil
}

func withMaintenancePermitLock(root string, fn func() error) error {
	return withConfigLock(root, func() error {
		if err := requireMaintenanceInstanceLock(root); err != nil {
			return err
		}
		return fn()
	})
}

func requireMaintenanceInstanceLock(installDir string) error {
	lock, err := AcquireInstanceLock(installDir)
	if err == nil {
		_ = lock.Close()
		return errMaintenancePermitOwner
	}
	if errors.Is(err, ErrInstanceRunning) {
		return nil
	}
	return fmt.Errorf("check maintenance instance ownership: %w", err)
}

func validateMaintenancePermitSelection(journal MaintenanceJournal, purpose, version string) error {
	if strings.TrimSpace(purpose) == "" || !validReleaseVersion(version) {
		return errInvalidMaintenancePermit
	}
	candidatePhase := journal.Phase == MaintenanceUpgrading || journal.Phase == MaintenanceCommitting
	restorePhase := journal.Phase == MaintenanceRestoring
	candidatePurpose := purpose == "bootstrap_db" || purpose == "migrate_up" || purpose == "db_check" || purpose == "candidate_health"
	restorePurpose := purpose == "revoke_sessions" || purpose == "db_check"
	if (candidatePhase && candidatePurpose && version == journal.CandidateVersion) || (restorePhase && restorePurpose && version == journal.OldVersion) {
		return nil
	}
	return errInvalidMaintenancePermit
}

func readMaintenancePermitEnvelope(installDir string) (maintenancePermitEnvelope, error) {
	var envelope maintenancePermitEnvelope
	path := maintenancePermitPath(installDir)
	info, err := os.Lstat(path)
	if err != nil {
		return envelope, err
	}
	if info.Size() > maintenancePermitMaxFileSize {
		return envelope, errInvalidMaintenancePermit
	}
	raw, err := readSecureConfigFile(path)
	if err != nil {
		return envelope, err
	}
	fields, err := decodeStrictJSONObject(raw)
	if err != nil || requireExactJSONKeys(fields, "digest", "operation_id", "purpose", "version", "backend_sha256", "expires_at") != nil {
		return envelope, errInvalidMaintenancePermit
	}
	if err := decodeReleaseJSON(raw, &envelope); err != nil || !validMaintenancePermitEnvelope(envelope) {
		return envelope, errInvalidMaintenancePermit
	}
	return envelope, nil
}

func validMaintenancePermitEnvelope(envelope maintenancePermitEnvelope) bool {
	return validMaintenancePermitHex(envelope.Digest) && validMaintenancePermitHex(envelope.OperationID) && validMaintenancePermitHex(envelope.BackendSHA256) && validReleaseVersion(envelope.Version) && strings.TrimSpace(envelope.Purpose) != "" && !envelope.ExpiresAt.IsZero()
}

func validMaintenancePermitHex(value string) bool {
	raw, err := hex.DecodeString(value)
	clear(raw)
	return err == nil && len(raw) == sha256.Size
}

func maintenancePermitDigestMatches(secret []byte, encoded string) bool {
	digest := sha256.Sum256(secret)
	stored, err := hex.DecodeString(encoded)
	if err != nil || len(stored) != sha256.Size {
		clear(digest[:])
		clear(stored)
		return false
	}
	match := subtle.ConstantTimeCompare(digest[:], stored) == 1
	clear(digest[:])
	clear(stored)
	return match
}

func maintenancePermitPath(installDir string) string {
	return filepath.Join(installDir, maintenanceDirName, maintenancePermitFileName)
}

func readMaintenancePermitFrame(reader io.Reader) ([]byte, error) {
	if reader == nil {
		return nil, errInvalidMaintenancePermit
	}
	frame := make([]byte, maintenancePermitFrameSize)
	if _, err := io.ReadFull(reader, frame); err != nil || !bytes.Equal(frame[:len(maintenancePermitFramePrefix)], []byte(maintenancePermitFramePrefix)) || frame[len(frame)-1] != '\n' {
		clear(frame)
		return nil, errInvalidMaintenancePermit
	}
	token := frame[len(maintenancePermitFramePrefix) : len(frame)-1]
	secret, err := base64.RawURLEncoding.DecodeString(string(token))
	if err != nil || len(secret) != maintenancePermitTokenByteSize || base64.RawURLEncoding.EncodeToString(secret) != string(token) {
		clear(frame)
		clear(secret)
		return nil, errInvalidMaintenancePermit
	}
	clear(frame)
	return secret, nil
}

func sameMaintenanceExecutablePath(actual, candidate string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(actual, candidate)
	}
	return actual == candidate
}
