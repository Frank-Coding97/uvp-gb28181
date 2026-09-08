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

const maintenanceKindUnclean = "unclean_recovery"

type MaintenancePhase string

const (
	MaintenancePreparing            MaintenancePhase = "preparing"
	MaintenanceUpgrading            MaintenancePhase = "upgrading"
	MaintenanceCommitting           MaintenancePhase = "committing"
	MaintenanceCompletionReady      MaintenancePhase = "completion_ready"
	MaintenanceRestoreRequired      MaintenancePhase = "restore_required"
	MaintenanceRestoring            MaintenancePhase = "restoring"
	MaintenanceAwaitingConfirmation MaintenancePhase = "awaiting_local_confirmation"
)

var ErrMaintenanceRequired = errors.New("standalone: unfinished maintenance requires local recovery")

// Maintenance state lives outside config/data so restoring a historical
// snapshot cannot replace the gate that protects the current operation.
type MaintenanceJournal struct {
	Schema                 int              `json:"schema"`
	OperationID            string           `json:"operation_id"`
	OldVersion             string           `json:"old_version"`
	CandidateVersion       string           `json:"candidate_version,omitempty"`
	Kind                   string           `json:"kind,omitempty"`
	RunMarkerSHA256        string           `json:"run_marker_sha256,omitempty"`
	SourceConfigSHA256     string           `json:"source_config_sha256,omitempty"`
	SourceDataSHA256       string           `json:"source_data_sha256,omitempty"`
	OldCurrentSHA256       string           `json:"old_current_sha256"`
	BackupRoot             string           `json:"backup_root"`
	BackupManifestSHA256   string           `json:"backup_manifest_sha256"`
	Phase                  MaintenancePhase `json:"phase"`
	CreatedAt              time.Time        `json:"created_at"`
	ReleaseSetSHA256       string           `json:"release_set_sha256,omitempty"`
	CandidateCurrentSHA256 string           `json:"candidate_current_sha256,omitempty"`
	CompletedAt            string           `json:"completed_at,omitempty"`
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
	root, err := cleanAbsolute(installDir)
	if err != nil {
		return MaintenanceJournal{}, err
	}
	return readMaintenanceJournalFile(filepath.Join(root, maintenanceDirName, "journal.json"))
}

func readMaintenanceJournalFile(path string) (MaintenanceJournal, error) {
	var journal MaintenanceJournal
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
	fields, err := decodeStrictJSONObject(raw)
	if err != nil {
		return journal, err
	}
	if err := decodeReleaseJSON(raw, &journal); err != nil {
		return journal, err
	}
	if err := validateMaintenanceJournalJSON(fields, journal.Schema); err != nil {
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

// The unclean-recovery journal is created only while the parent owns the
// installation instance. The gate is intentionally created before the final
// write so every error after publication leaves startup fail-closed.
func createUncleanPreparingJournal(installDir string, journal MaintenanceJournal) error {
	if journal.Schema != 2 || journal.Phase != MaintenancePreparing {
		return errors.New("invalid initial unclean maintenance phase")
	}
	if err := validateMaintenanceJournal(journal); err != nil {
		return err
	}
	root, err := cleanAbsolute(installDir)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	return withConfigLock(root, func() error {
		if err := requireMaintenanceInstanceLock(root); err != nil {
			return err
		}
		dir := filepath.Join(root, maintenanceDirName)
		if err := os.Mkdir(dir, 0700); err != nil {
			return err
		}
		if err := protectConfigDir(dir, true); err != nil {
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
		if journal.Schema == 2 {
			switch expected {
			case MaintenanceRestoreRequired:
				allowed = next == MaintenanceRestoring
			case MaintenanceRestoring:
				allowed = next == MaintenanceAwaitingConfirmation
			}
		} else {
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
		if err := validateMaintenanceJournal(journal); err != nil {
			return err
		}
		raw, err := json.Marshal(journal)
		if err != nil {
			return err
		}
		return writeSecureConfigFile(filepath.Join(installDir, maintenanceDirName, "journal.json"), raw, true, nil)
	})
}

func validateMaintenanceJournal(journal MaintenanceJournal) error {
	switch journal.Schema {
	case 1:
		return validateSchema1MaintenanceJournal(journal)
	case 2:
		return validateSchema2MaintenanceJournal(journal)
	default:
		return errors.New("invalid maintenance metadata")
	}
}

func validateSchema1MaintenanceJournal(journal MaintenanceJournal) error {
	if journal.CreatedAt.IsZero() || !validReleaseVersion(journal.OldVersion) || !validReleaseVersion(journal.CandidateVersion) || journal.Kind != "" || journal.RunMarkerSHA256 != "" || journal.SourceConfigSHA256 != "" || journal.SourceDataSHA256 != "" {
		return errors.New("invalid maintenance metadata")
	}
	checksums := []string{journal.OperationID, journal.OldCurrentSHA256}
	// Older in-progress journals remain readable for recovery, but cannot be
	// granted completion without a persisted preparation release-set identity.
	if journal.ReleaseSetSHA256 != "" {
		checksums = append(checksums, journal.ReleaseSetSHA256)
	}
	if journal.Phase == MaintenanceCompletionReady {
		completed, err := time.Parse(time.RFC3339Nano, journal.CompletedAt)
		if err != nil || completed.Before(journal.CreatedAt) {
			return errors.New("invalid maintenance completion time")
		}
		checksums = append(checksums, journal.ReleaseSetSHA256, journal.CandidateCurrentSHA256)
	} else if journal.CompletedAt != "" || journal.CandidateCurrentSHA256 != "" {
		return errors.New("unfinished maintenance cannot claim completion")
	}
	if journal.Phase == MaintenancePreparing {
		if journal.BackupManifestSHA256 != "" {
			return errors.New("preparing maintenance cannot claim a completed backup")
		}
	} else {
		checksums = append(checksums, journal.BackupManifestSHA256)
	}
	for _, value := range checksums {
		raw, err := hex.DecodeString(value)
		if err != nil || len(raw) != 32 {
			return errors.New("invalid maintenance identity or checksum")
		}
	}
	if _, err := cleanAbsolute(journal.BackupRoot); err != nil {
		return errors.New("invalid maintenance backup root")
	}
	switch journal.Phase {
	case MaintenancePreparing, MaintenanceUpgrading, MaintenanceCommitting, MaintenanceCompletionReady, MaintenanceRestoreRequired, MaintenanceRestoring, MaintenanceAwaitingConfirmation:
		return nil
	default:
		return errors.New("invalid maintenance phase")
	}
}

func validateSchema2MaintenanceJournal(journal MaintenanceJournal) error {
	if journal.CreatedAt.IsZero() || !validReleaseVersion(journal.OldVersion) || journal.CandidateVersion != "" || journal.Kind != maintenanceKindUnclean || journal.CandidateCurrentSHA256 != "" || journal.CompletedAt != "" {
		return errors.New("invalid unclean maintenance metadata")
	}
	for _, value := range []string{journal.OperationID, journal.OldCurrentSHA256, journal.ReleaseSetSHA256, journal.RunMarkerSHA256, journal.SourceConfigSHA256, journal.SourceDataSHA256} {
		raw, err := hex.DecodeString(value)
		if err != nil || len(raw) != 32 {
			return errors.New("invalid unclean maintenance identity or checksum")
		}
	}
	if journal.Phase == MaintenancePreparing {
		if journal.BackupManifestSHA256 != "" {
			return errors.New("preparing unclean maintenance cannot claim a completed backup")
		}
	} else {
		raw, err := hex.DecodeString(journal.BackupManifestSHA256)
		if err != nil || len(raw) != 32 {
			return errors.New("invalid unclean maintenance backup checksum")
		}
	}
	if _, err := cleanAbsolute(journal.BackupRoot); err != nil {
		return errors.New("invalid maintenance backup root")
	}
	switch journal.Phase {
	case MaintenancePreparing, MaintenanceRestoreRequired, MaintenanceRestoring, MaintenanceAwaitingConfirmation:
		return nil
	default:
		return errors.New("invalid unclean maintenance phase")
	}
}

func validateMaintenanceJournalJSON(fields map[string]json.RawMessage, schema int) error {
	if schema == 1 {
		return requireExactJSONKeys(fields,
			"schema", "operation_id", "old_version", "candidate_version", "old_current_sha256",
			"backup_root", "backup_manifest_sha256", "phase", "created_at", "release_set_sha256",
			"candidate_current_sha256", "completed_at")
	}
	if schema != 2 {
		return errors.New("invalid maintenance metadata schema")
	}
	keys := []string{
		"schema", "operation_id", "old_version", "kind", "run_marker_sha256",
		"source_config_sha256", "source_data_sha256", "old_current_sha256", "backup_root",
		"backup_manifest_sha256", "phase", "created_at", "release_set_sha256",
	}
	if err := requireExactJSONKeys(fields, keys...); err != nil {
		return err
	}
	if len(fields) != len(keys) {
		return errors.New("invalid unclean maintenance journal fields")
	}
	return nil
}

// The caller must have validated the journal before selecting its source.
func maintenanceSourceSelection(journal MaintenanceJournal) string {
	if journal.Schema == 2 {
		return journal.OldVersion
	}
	return journal.CandidateVersion
}
