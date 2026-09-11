package standalone

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type releaseCurrentPointer struct {
	Version string `json:"version"`
}

// commitMaintenanceCurrent requires the caller to hold InstanceLock for the
// whole maintenance operation. It retains the maintenance journal and gate.
func commitMaintenanceCurrent(installDir, operationID string) error {
	root, err := cleanAbsolute(installDir)
	if err != nil {
		return err
	}
	return withConfigLock(root, func() error {
		journal, err := ReadMaintenanceJournal(root)
		if err != nil {
			return err
		}
		if journal.OperationID != operationID {
			return errors.New("maintenance operation mismatch")
		}
		if journal.Phase != MaintenanceCommitting {
			return errors.New("maintenance phase is not committing")
		}
		if _, err := LoadReleaseVersion(root, journal.CandidateVersion); err != nil {
			return fmt.Errorf("validate candidate release: %w", err)
		}
		candidateRaw, err := json.Marshal(releaseCurrentPointer{Version: journal.CandidateVersion})
		if err != nil {
			return fmt.Errorf("encode current pointer: %w", err)
		}
		currentPath := filepath.Join(root, "current.json")
		if _, err := ensureReleaseChild(root, currentPath, false); err != nil {
			return fmt.Errorf("release current pointer: %w", err)
		}
		currentRaw, err := os.ReadFile(currentPath)
		if err != nil {
			return fmt.Errorf("read release current pointer: %w", err)
		}
		currentHash := currentPointerSHA256(currentRaw)
		candidateHash := currentPointerSHA256(candidateRaw)
		if bytes.Equal(currentRaw, candidateRaw) && strings.EqualFold(currentHash, candidateHash) {
			return nil
		}
		if !strings.EqualFold(currentHash, journal.OldCurrentSHA256) {
			return errors.New("release current pointer changed during maintenance")
		}
		return writeCurrentPointerAtomically(currentPath, candidateRaw)
	})
}

func currentPointerSHA256(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func writeCurrentPointerAtomically(target string, data []byte) error {
	tempFile, err := os.CreateTemp(filepath.Dir(target), ".uvp-current-*")
	if err != nil {
		return fmt.Errorf("create current pointer temporary file: %w", err)
	}
	tempPath := tempFile.Name()
	removeTemp := true
	defer func() {
		_ = tempFile.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := tempFile.Chmod(0o600); err != nil {
		return fmt.Errorf("protect current pointer temporary file: %w", err)
	}
	n, err := tempFile.Write(data)
	if err != nil {
		return fmt.Errorf("write current pointer: %w", err)
	}
	if n != len(data) {
		return fmt.Errorf("write current pointer: %w", io.ErrShortWrite)
	}
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("flush current pointer: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close current pointer: %w", err)
	}
	if err := replaceCurrentPointerAtomically(tempPath, target); err != nil {
		return err
	}
	removeTemp = false
	return nil
}
