package standalone

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const maintenanceDirName = ".uvp-maintenance"

type MaintenancePhase string

const (
	MaintenanceUpgrading            MaintenancePhase = "upgrading"
	MaintenanceCommitting           MaintenancePhase = "committing"
	MaintenanceRestoreRequired      MaintenancePhase = "restore_required"
	MaintenanceRestoring            MaintenancePhase = "restoring"
	MaintenanceAwaitingConfirmation MaintenancePhase = "awaiting_local_confirmation"
)

var ErrMaintenanceRequired = errors.New("standalone: unfinished maintenance requires local recovery")

// Maintenance state lives outside config/data so restoring a historical
// snapshot cannot replace the gate that protects the current operation.
type MaintenanceJournal struct {
	Schema               int              `json:"schema"`
	OperationID          string           `json:"operation_id"`
	OldVersion           string           `json:"old_version"`
	CandidateVersion     string           `json:"candidate_version"`
	OldCurrentSHA256     string           `json:"old_current_sha256"`
	BackupRoot           string           `json:"backup_root"`
	BackupManifestSHA256 string           `json:"backup_manifest_sha256"`
	Phase                MaintenancePhase `json:"phase"`
	CreatedAt            time.Time        `json:"created_at"`
}

// CheckMaintenanceGate must run with instance ownership before creating any
// component. An empty, corrupt or unreadable gate also blocks normal startup.
func CheckMaintenanceGate(installDir string) error {
	root, err := cleanAbsolute(installDir)
	if err != nil {
		return err
	}
	_, err = os.Lstat(filepath.Join(root, maintenanceDirName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: cannot inspect maintenance state", ErrMaintenanceRequired)
	}
	return ErrMaintenanceRequired
}

func ReadMaintenanceJournal(installDir string) (MaintenanceJournal, error) {
	var journal MaintenanceJournal
	root, err := cleanAbsolute(installDir)
	if err != nil {
		return journal, err
	}
	path := filepath.Join(root, maintenanceDirName, "journal.json")
	info, err := os.Lstat(path)
	if err != nil {
		return journal, err
	}
	if info.Size() > 8192 {
		return journal, errors.New("maintenance journal exceeds limit")
	}
	raw, err := readSecureConfigFile(path)
	if err != nil {
		return journal, err
	}
	if _, err := decodeStrictJSONObject(raw); err != nil {
		return journal, err
	}
	if err := decodeReleaseJSON(raw, &journal); err != nil {
		return journal, err
	}
	if err := validateMaintenanceJournal(journal); err != nil {
		return journal, err
	}
	return journal, nil
}

// The maintenance coordinator must hold InstanceLock across these mutations
// and all subsequent backup, migration and recovery work. Failed creation
// deliberately retains the gate directory; startup must not infer success.
func createMaintenanceJournal(installDir string, journal MaintenanceJournal) error {
	if err := validateMaintenanceJournal(journal); err != nil {
		return err
	}
	if journal.Phase != MaintenanceUpgrading {
		return errors.New("invalid initial maintenance phase")
	}
	root, err := cleanAbsolute(installDir)
	if err != nil {
		return err
	}
	return withConfigLock(root, func() error {
		dir := filepath.Join(root, maintenanceDirName)
		if err := os.Mkdir(dir, 0700); err != nil {
			return err
		}
		if err := protectConfigDir(dir, true); err != nil {
			return err
		}
		raw, err := json.Marshal(journal)
		if err != nil {
			return err
		}
		return writeSecureConfigFile(filepath.Join(dir, "journal.json"), raw, false, nil)
	})
}

func advanceMaintenance(installDir, operationID string, expected, next MaintenancePhase) error {
	return withConfigLock(installDir, func() error {
		journal, err := ReadMaintenanceJournal(installDir)
		if err != nil {
			return err
		}
		if journal.OperationID != operationID {
			return errors.New("maintenance operation mismatch")
		}
		allowed := false
		switch expected {
		case MaintenanceUpgrading:
			allowed = next == MaintenanceCommitting || next == MaintenanceRestoreRequired
		case MaintenanceCommitting:
			allowed = next == MaintenanceRestoreRequired
		case MaintenanceRestoreRequired:
			allowed = next == MaintenanceRestoring
		case MaintenanceRestoring:
			allowed = next == MaintenanceAwaitingConfirmation
		}
		if !allowed {
			return errors.New("invalid maintenance transition")
		}
		if journal.Phase == next {
			return nil
		}
		if journal.Phase != expected {
			return errors.New("maintenance phase changed")
		}
		journal.Phase = next
		raw, err := json.Marshal(journal)
		if err != nil {
			return err
		}
		return writeSecureConfigFile(filepath.Join(installDir, maintenanceDirName, "journal.json"), raw, true, nil)
	})
}

func validateMaintenanceJournal(journal MaintenanceJournal) error {
	if journal.Schema != 1 || journal.CreatedAt.IsZero() || !validReleaseVersion(journal.OldVersion) || !validReleaseVersion(journal.CandidateVersion) {
		return errors.New("invalid maintenance metadata")
	}
	for _, value := range []string{journal.OperationID, journal.OldCurrentSHA256, journal.BackupManifestSHA256} {
		raw, err := hex.DecodeString(value)
		if err != nil || len(raw) != 32 {
			return errors.New("invalid maintenance identity or checksum")
		}
	}
	if _, err := cleanAbsolute(journal.BackupRoot); err != nil {
		return errors.New("invalid maintenance backup root")
	}
	switch journal.Phase {
	case MaintenanceUpgrading, MaintenanceCommitting, MaintenanceRestoreRequired, MaintenanceRestoring, MaintenanceAwaitingConfirmation:
		return nil
	default:
		return errors.New("invalid maintenance phase")
	}
}
