package standalone

import (
	"context"
	"errors"
)

// RecoveryRedisRunner starts only the disposable Redis processes used by the
// offline recovery stage. The coordinator keeps the instance lock and
// restoring maintenance gate while it invokes this callback.
type RecoveryRedisRunner func(context.Context, string, string, string, string, int) error

// RestoreStopped runs the offline recovery transaction and stops at
// MaintenanceAwaitingConfirmation. It never confirms the operation or removes
// the maintenance gate. The trust set is the launcher build's private,
// compile-time backend allowlist; callers cannot provide or override it.
//
// The operation ID is read from the protected maintenance journal immediately
// before entering the core coordinator. The coordinator rechecks it while
// holding InstanceLock, so a concurrent journal change fails closed.
func RestoreStopped(ctx context.Context, paths Paths, run MaintenanceRunner, redis RecoveryRedisRunner) (MaintenanceJournal, error) {
	if ctx == nil || run == nil || redis == nil {
		return MaintenanceJournal{}, errors.New("recovery requires offline runners")
	}
	if err := ctx.Err(); err != nil {
		return MaintenanceJournal{}, err
	}
	journal, err := ReadMaintenanceJournal(paths.InstallDir)
	if err != nil {
		return MaintenanceJournal{}, err
	}
	result, err := restoreStoppedWithRunners(ctx, paths, journal.OperationID, maintenanceBackendSHA256Allowlist, run, recoveryRedisRunner(redis))
	if err != nil {
		return MaintenanceJournal{}, err
	}
	if result.Phase != MaintenanceAwaitingConfirmation {
		return MaintenanceJournal{}, errors.New("recovery did not reach local confirmation")
	}
	return result, nil
}
