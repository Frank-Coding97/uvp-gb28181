package standalone

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
)

func recoveryContextSHA256(outer MaintenanceJournal) string {
	if outer.Schema == 1 {
		return ""
	}
	// Phase changes do not alter the authority being recovered.
	identity := []string{outer.Kind, outer.OperationID, outer.OldVersion, outer.RunMarkerSHA256, outer.BackupManifestSHA256, outer.OldCurrentSHA256, outer.ReleaseSetSHA256, outer.BackupRoot, outer.SourceConfigSHA256, outer.SourceDataSHA256}
	raw, _ := json.Marshal(identity)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func recoveryContextMatches(progress recoveryJournal, outer MaintenanceJournal) bool {
	return progress.Schema == outer.Schema && progress.ContextSHA256 == recoveryContextSHA256(outer)
}

func recoveryFinalPhase(outer MaintenanceJournal) string {
	if outer.Schema == 2 && outer.Kind == maintenanceKindUnclean {
		return "pointer_unchanged_verified"
	}
	return "pointer_restored"
}

func verifyRecoveryBackup(ctx context.Context, outer MaintenanceJournal) (BackupManifest, error) {
	manifest, err := VerifyBackup(ctx, outer.BackupRoot)
	if err != nil {
		return manifest, err
	}
	if manifest.Version != outer.OldVersion {
		return manifest, errors.New("recovery source version changed")
	}
	if outer.Schema == 1 {
		if manifest.FormatVersion != 1 {
			return manifest, errors.New("upgrade recovery requires a clean historical backup")
		}
	} else if outer.Schema == 2 && outer.Kind == maintenanceKindUnclean {
		if manifest.FormatVersion != 2 || manifest.Kind != "unclean_snapshot" || manifest.OperationID != outer.OperationID || manifest.RunMarkerSHA256 != outer.RunMarkerSHA256 || manifest.SourceCurrentSHA256 != outer.OldCurrentSHA256 {
			return manifest, errors.New("unclean recovery snapshot context mismatch")
		}
	} else {
		return manifest, errors.New("unknown recovery operation kind")
	}
	digest, err := releaseFileSHA256(filepath.Join(outer.BackupRoot, backupManifestFile))
	if err != nil {
		return manifest, err
	}
	if digest != outer.BackupManifestSHA256 {
		return manifest, errors.New("recovery snapshot identity changed")
	}
	return manifest, nil
}
