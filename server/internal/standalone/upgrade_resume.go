package standalone

import (
	"context"
	"errors"
)

func resumeUpgradeCompletion(ctx context.Context, paths Paths, operationID string) error {
	return resumeUpgradeCompletionWithTrust(ctx, paths, operationID, maintenanceBackendSHA256Allowlist)
}

// This continuation does not migrate or infer that a committing transaction
// succeeded. Only its already-persisted completion proof can authorize finish.
func resumeUpgradeCompletionWithTrust(ctx context.Context, paths Paths, operationID, trust string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	journal, err := ReadMaintenanceJournal(paths.InstallDir)
	if err != nil {
		return err
	}
	if journal.OperationID != operationID || journal.Phase != MaintenanceCompletionReady {
		return errors.New("upgrade continuation requires matching durable completion proof")
	}
	if _, _, err := loadMaintenanceReleasesWithTrust(paths.InstallDir, journal.CandidateVersion, trust); err != nil {
		return err
	}
	lock, err := AcquireInstanceLock(paths.InstallDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	locked, err := ReadMaintenanceJournal(paths.InstallDir)
	if err != nil {
		return err
	}
	if locked != journal {
		return errors.New("upgrade completion changed while acquiring ownership")
	}
	// Reconstruct only the preparation identity, never a general recovery owner.
	// finish rechecks the persisted proof, current, backup, release set and all
	// process/permit boundaries while holding the new lock and the config lock.
	prepared := journal
	prepared.Phase = MaintenanceUpgrading
	prepared.CompletedAt, prepared.CandidateCurrentSHA256 = "", ""
	owner := &upgradePreparation{
		journal: prepared, lock: lock, paths: paths, trust: trust,
		releaseIdentity: journal.ReleaseSetSHA256, committed: true,
	}
	return owner.finish(ctx)
}
