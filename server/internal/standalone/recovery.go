package standalone

import (
	"context"
	"errors"
	"os"
)

// Recovery deliberately stops before reopening any business capability.
// Local administrator confirmation is a separate admission step.
func restoreStoppedWithRunners(ctx context.Context, paths Paths, operation, trust string, run MaintenanceRunner, redis recoveryRedisRunner) (result MaintenanceJournal, failure error) {
	if ctx == nil || run == nil || redis == nil {
		return result, errors.New("recovery requires offline runners")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	lock, err := AcquireInstanceLock(paths.InstallDir)
	if err != nil {
		return result, err
	}
	defer func() { failure = errors.Join(failure, lock.Close()) }()
	outer, err := ReadMaintenanceJournal(paths.InstallDir)
	if err != nil {
		return result, err
	}
	if outer.OperationID != operation || (outer.Phase != MaintenanceRestoreRequired && outer.Phase != MaintenanceRestoring) {
		return result, errors.New("recovery operation is not awaiting restoration")
	}
	j, err := readRecoveryJournal(paths.InstallDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if errors.Is(err, os.ErrNotExist) || j.Phase == "staging" || j.Phase == "staged" {
		if _, err := prepareRecoveryStage(ctx, paths, operation, trust, run, redis); err != nil {
			return result, err
		}
		j, err = readRecoveryJournal(paths.InstallDir)
		if err != nil {
			return result, err
		}
	}
	if j.Phase != "pointer_restored" {
		if err := publishRecoveryDirectories(ctx, paths.InstallDir, operation, trust, nil); err != nil {
			return result, err
		}
	}
	if err := restoreRecoveryCurrent(ctx, paths.InstallDir, operation, trust, nil); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := advanceMaintenance(paths.InstallDir, operation, MaintenanceRestoring, MaintenanceAwaitingConfirmation); err != nil {
		return result, err
	}
	return ReadMaintenanceJournal(paths.InstallDir)
}
