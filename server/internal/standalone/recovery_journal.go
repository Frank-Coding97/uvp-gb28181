package standalone

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Launcher-only progress avoids changing the maintenance backend protocol.
// The sealed identities include the failed directories, so recovery cannot
// silently accept a different failure scene after interruption.
type recoveryJournal struct {
	Schema               int    `json:"schema"`
	ContextSHA256        string `json:"context_sha256,omitempty"`
	OperationID          string `json:"operation_id"`
	BackupManifestSHA256 string `json:"backup_manifest_sha256"`
	OldVersion           string `json:"old_version"`
	OldCurrentSHA256     string `json:"old_current_sha256"`
	ReleaseSetSHA256     string `json:"release_set_sha256"`
	Phase                string `json:"phase"`
	StagedConfigSHA256   string `json:"staged_config_sha256"`
	StagedDataSHA256     string `json:"staged_data_sha256"`
	FailedConfigSHA256   string `json:"failed_config_sha256"`
	FailedDataSHA256     string `json:"failed_data_sha256"`
}

func recoveryPhaseIndex(phase string) int {
	if phase == "pointer_unchanged_verified" {
		return 6
	}
	for i, name := range []string{"staging", "staged", "config_isolated", "config_published", "data_isolated", "data_published", "pointer_restored"} {
		if phase == name {
			return i
		}
	}
	return -1
}

func validateRecoveryJournal(j recoveryJournal) error {
	if (j.Schema != 1 && j.Schema != 2) || !validReleaseVersion(j.OldVersion) || recoveryPhaseIndex(j.Phase) < 0 {
		return errors.New("invalid recovery metadata")
	}
	if (j.Schema == 1 && (j.ContextSHA256 != "" || j.Phase == "pointer_unchanged_verified")) || (j.Schema == 2 && (!validMaintenancePermitHex(j.ContextSHA256) || j.Phase == "pointer_restored")) {
		return errors.New("invalid recovery operation context")
	}
	for _, value := range []string{j.OperationID, j.BackupManifestSHA256, j.OldCurrentSHA256, j.ReleaseSetSHA256} {
		if !validMaintenancePermitHex(value) {
			return errors.New("invalid recovery identity")
		}
	}
	for _, value := range []string{j.StagedConfigSHA256, j.StagedDataSHA256, j.FailedConfigSHA256, j.FailedDataSHA256} {
		if j.Phase == "staging" {
			if value != "" {
				return errors.New("unsealed recovery cannot claim directory identities")
			}
		} else if !validMaintenancePermitHex(value) {
			return errors.New("sealed recovery identity missing")
		}
	}
	return nil
}

func readRecoveryJournal(root string) (recoveryJournal, error) {
	var j recoveryJournal
	path := filepath.Join(root, maintenanceDirName, "recovery.json")
	info, err := os.Lstat(path)
	if err != nil {
		return j, err
	}
	if info.Size() > 8192 {
		return j, errors.New("recovery journal exceeds limit")
	}
	raw, err := readSecureConfigFile(path)
	if err != nil {
		return j, err
	}
	var header struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return j, err
	}
	keys := []string{"schema", "operation_id", "backup_manifest_sha256", "old_version", "old_current_sha256", "release_set_sha256", "phase", "staged_config_sha256", "staged_data_sha256", "failed_config_sha256", "failed_data_sha256"}
	if header.Schema == 2 {
		keys = append(keys, "context_sha256")
	}
	if err := decodeBackupObject(raw, &j, keys...); err != nil {
		return j, err
	}
	return j, validateRecoveryJournal(j)
}

// The coordinator retains InstanceLock across this CAS and filesystem work.
// This metadata operation does not itself prove a directory move or backup.
func persistRecoveryJournal(root string, expected *recoveryJournal, next recoveryJournal) error {
	if err := validateRecoveryJournal(next); err != nil {
		return err
	}
	return withConfigLock(root, func() error {
		if err := requireMaintenanceInstanceLock(root); err != nil {
			return err
		}
		outer, err := ReadMaintenanceJournal(root)
		if err != nil {
			return err
		}
		if !recoveryContextMatches(next, outer) || outer.OperationID != next.OperationID || outer.BackupManifestSHA256 != next.BackupManifestSHA256 || outer.OldVersion != next.OldVersion || outer.OldCurrentSHA256 != next.OldCurrentSHA256 || outer.ReleaseSetSHA256 != next.ReleaseSetSHA256 {
			return errors.New("recovery does not match maintenance operation")
		}
		if expected == nil {
			if outer.Phase != MaintenanceRestoreRequired || next.Phase != "staging" {
				return errors.New("invalid recovery entry phase")
			}
		} else {
			if outer.Phase != MaintenanceRestoring {
				return errors.New("recovery is not restoring")
			}
			current, err := readRecoveryJournal(root)
			if err != nil {
				return err
			}
			if current != *expected {
				return errors.New("recovery progress changed")
			}
			if recoveryPhaseIndex(next.Phase) != recoveryPhaseIndex(current.Phase)+1 {
				return errors.New("invalid recovery transition")
			}
			comparison := next
			comparison.Phase = current.Phase
			if current.Phase == "staging" {
				comparison.StagedConfigSHA256, comparison.StagedDataSHA256 = "", ""
				comparison.FailedConfigSHA256, comparison.FailedDataSHA256 = "", ""
			}
			if comparison != current {
				return errors.New("sealed recovery identity changed")
			}
		}
		raw, err := json.Marshal(next)
		if err != nil {
			return err
		}
		return writeSecureConfigFile(filepath.Join(root, maintenanceDirName, "recovery.json"), raw, expected != nil, nil)
	})
}
