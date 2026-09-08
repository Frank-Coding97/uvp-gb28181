package standalone

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Qualified independently from schema-1 upgrade support; only backends tested
// with the unclean-recovery protocol may be selected for this operation.
var uncleanRecoveryBackendSHA256Allowlist string

type UncleanRecoveryResult struct {
	OperationID               string
	AwaitingLocalConfirmation bool
}

func RecoverUncleanStopped(ctx context.Context, paths Paths, destination string, run MaintenanceRunner, redis RecoveryRedisRunner) (UncleanRecoveryResult, error) {
	return recoverUncleanStoppedWithTrust(ctx, paths, destination, uncleanRecoveryBackendSHA256Allowlist, run, recoveryRedisRunner(redis))
}

func recoverUncleanStoppedWithTrust(ctx context.Context, paths Paths, destination, trust string, run MaintenanceRunner, redis recoveryRedisRunner) (result UncleanRecoveryResult, failure error) {
	if ctx == nil || run == nil || redis == nil {
		return result, errors.New("unclean recovery requires offline runners")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	lock, err := AcquireInstanceLock(paths.InstallDir)
	if err != nil {
		return result, err
	}
	defer func() { failure = errors.Join(failure, lock.Close()) }()
	journal, err := prepareUncleanRecoveryOwned(ctx, paths, destination, trust)
	if err != nil {
		return result, err
	}
	if journal.Phase == MaintenancePreparing {
		if _, err := os.Lstat(journal.BackupRoot); errors.Is(err, os.ErrNotExist) {
			if _, err := backupStoppedAdmitted(ctx, paths, journal.BackupRoot, journal.OperationID, trust, journal.ReleaseSetSHA256); err != nil {
				return result, err
			}
		} else if err != nil {
			return result, err
		}
		if err := promoteUncleanSnapshot(ctx, paths, journal, trust); err != nil {
			return result, err
		}
		journal, err = ReadMaintenanceJournal(paths.InstallDir)
		if err != nil {
			return result, err
		}
	}
	if journal.Phase != MaintenanceAwaitingConfirmation {
		journal, err = restoreStoppedOwnedWithRunners(ctx, paths, journal.OperationID, trust, run, redis)
		if err != nil {
			return result, err
		}
	}
	result.OperationID = journal.OperationID
	result.AwaitingLocalConfirmation = true
	if _, _, err := verifyRecoveryConfirmation(ctx, paths.InstallDir, journal.OperationID, trust); err != nil {
		return result, err
	}
	pristine, err := recoveryPristinePendingAdmin(ctx, paths.DatabasePath)
	if err != nil {
		return result, err
	}
	if pristine {
		if err := archivePristineUncleanRecovery(ctx, paths.InstallDir, journal.OperationID, trust); err != nil {
			return result, err
		}
		result.AwaitingLocalConfirmation = false
	}
	return result, nil
}

func prepareUncleanRecoveryOwned(ctx context.Context, paths Paths, destination, trust string) (MaintenanceJournal, error) {
	var empty MaintenanceJournal
	if err := paths.Validate(); err != nil {
		return empty, err
	}
	current, err := LoadRelease(paths.InstallDir)
	if err != nil {
		return empty, err
	}
	if _, _, err := loadMaintenanceReleasesWithTrust(paths.InstallDir, current.Version, trust); err != nil {
		return empty, err
	}
	if err := CheckMaintenanceGate(paths.InstallDir); err != nil {
		journal, err := ReadMaintenanceJournal(paths.InstallDir)
		if err != nil {
			return empty, err
		}
		if journal.Schema != 2 || journal.Kind != maintenanceKindUnclean {
			return empty, errors.New("existing maintenance is not an unclean recovery")
		}
		if destination != "" {
			requested, err := cleanAbsolute(destination)
			if err != nil {
				return empty, err
			}
			parent, err := filepath.EvalSymlinks(filepath.Dir(requested))
			if err != nil || filepath.Join(parent, filepath.Base(requested)) != journal.BackupRoot {
				return empty, errors.New("unclean recovery snapshot destination changed")
			}
		}
		return journal, nil
	}
	observation, err := InspectRunMarker(paths)
	if !errors.Is(err, ErrUncleanRecoveryRequired) || !observation.PreviousUnclean {
		return empty, errors.New("unclean recovery requires an intact previous-run marker")
	}
	destination, err = backupDestination(paths, destination)
	if err != nil {
		return empty, err
	}
	releases, identity, err := installedMaintenanceReleaseSnapshot(paths.InstallDir)
	if err != nil {
		return empty, err
	}
	if err := backupComponentsStopped(releases...); err != nil {
		return empty, err
	}
	currentSHA, err := releaseFileSHA256(filepath.Join(paths.InstallDir, "current.json"))
	if err != nil {
		return empty, err
	}
	configSHA, err := recoveryTreeIdentity(ctx, paths.ConfigDir)
	if err != nil {
		return empty, err
	}
	dataSHA, err := recoveryTreeIdentity(ctx, paths.DataDir)
	if err != nil {
		return empty, err
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return empty, err
	}
	journal := MaintenanceJournal{Schema: 2, Kind: maintenanceKindUnclean, OperationID: hex.EncodeToString(nonce[:]), OldVersion: current.Version, OldCurrentSHA256: currentSHA, BackupRoot: destination, Phase: MaintenancePreparing, CreatedAt: time.Now().UTC(), ReleaseSetSHA256: identity, RunMarkerSHA256: observation.SHA256, SourceConfigSHA256: configSHA, SourceDataSHA256: dataSHA}
	if err := createUncleanPreparingJournal(paths.InstallDir, journal); err != nil {
		return empty, err
	}
	return journal, nil
}

func promoteUncleanSnapshot(ctx context.Context, paths Paths, expected MaintenanceJournal, trust string) error {
	return withConfigLock(paths.InstallDir, func() error {
		current, err := ReadMaintenanceJournal(paths.InstallDir)
		if err != nil {
			return err
		}
		if current != expected || current.Schema != 2 || current.Phase != MaintenancePreparing {
			return errors.New("unclean snapshot preparation changed")
		}
		identity, err := checkPreparingBackupAdmission(paths, current.BackupRoot, current.OperationID, trust)
		if err != nil {
			return err
		}
		if identity != current.ReleaseSetSHA256 {
			return errors.New("unclean snapshot release set changed")
		}
		digest, err := releaseFileSHA256(filepath.Join(current.BackupRoot, backupManifestFile))
		if err != nil {
			return err
		}
		current.BackupManifestSHA256 = digest
		if _, err := verifyRecoveryBackup(ctx, current); err != nil {
			return err
		}
		current.Phase = MaintenanceRestoreRequired
		if err := validateMaintenanceJournal(current); err != nil {
			return err
		}
		raw, err := json.Marshal(current)
		if err != nil {
			return err
		}
		return writeSecureConfigFile(filepath.Join(paths.InstallDir, maintenanceDirName, "journal.json"), raw, true, nil)
	})
}
