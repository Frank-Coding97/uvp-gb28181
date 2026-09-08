package standalone

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

// execute is single-owner: Close must not run concurrently. The launcher runner
// must wait for every owned process to exit before returning, including failures.
// candidate_health owns its temporary Redis/media lifecycle and backend probe.
// This stage keeps the gate even after committing; completion/recovery is separate.
func (p *upgradePreparation) execute(ctx context.Context, run func(context.Context, Paths, string, string, string) error) (result error) {
	if p == nil || p.lock == nil || run == nil {
		return errors.New("upgrade execution requires its preparation owner and runner")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.checkExecutionIdentity(); err != nil {
		return err
	}
	// Recheck the entire snapshot before any migration can modify live data.
	if err := promotePreparedMaintenance(ctx, p.paths.InstallDir, p.journal.OperationID, p.journal.BackupRoot); err != nil {
		return err
	}
	phase := MaintenanceUpgrading
	defer func() {
		if result != nil {
			// Use no cancelled context: publishing the failure gate must still run.
			result = errors.Join(result, advanceMaintenance(p.paths.InstallDir, p.journal.OperationID, phase, MaintenanceRestoreRequired))
		}
	}()
	for _, purpose := range []string{"migrate_up", "db_check", "candidate_health"} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.checkExecutionIdentity(); err != nil {
			return err
		}
		if err := run(ctx, p.paths, p.journal.OperationID, purpose, p.journal.CandidateVersion); err != nil {
			// Child output/configuration may contain secrets; return only the stage.
			return fmt.Errorf("upgrade %s failed", purpose)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.checkExecutionIdentity(); err != nil {
		return err
	}
	if err := advanceMaintenance(p.paths.InstallDir, p.journal.OperationID, MaintenanceUpgrading, MaintenanceCommitting); err != nil {
		return err
	}
	phase = MaintenanceCommitting
	return commitMaintenanceCurrent(p.paths.InstallDir, p.journal.OperationID)
}

func (p *upgradePreparation) checkExecutionIdentity() error {
	if p.lock == nil {
		return errors.New("upgrade preparation is closed")
	}
	if err := requireMaintenanceInstanceLock(p.paths.InstallDir); err != nil {
		return err
	}
	journal, err := ReadMaintenanceJournal(p.paths.InstallDir)
	if err != nil {
		return err
	}
	if journal != p.journal || journal.Phase != MaintenanceUpgrading {
		return errors.New("upgrade preparation identity changed")
	}
	if _, _, err := loadMaintenanceReleasesWithTrust(p.paths.InstallDir, journal.CandidateVersion, p.trust); err != nil {
		return err
	}
	releases, identity, err := installedMaintenanceReleaseSnapshot(p.paths.InstallDir)
	if err != nil {
		return err
	}
	if identity != p.releaseIdentity {
		return errors.New("installed releases changed after upgrade preparation")
	}
	currentHash, err := releaseFileSHA256(filepath.Join(p.paths.InstallDir, "current.json"))
	if err != nil || currentHash != journal.OldCurrentSHA256 {
		return errors.New("current pointer changed after upgrade preparation")
	}
	return backupComponentsStopped(releases...)
}
