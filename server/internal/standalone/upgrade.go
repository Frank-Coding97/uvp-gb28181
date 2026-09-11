package standalone

import (
	"context"
	"errors"
)

// MaintenanceRunner runs one bounded, offline step and waits for every process
// it owns to exit. The launcher supplies its Job-backed implementation.
type MaintenanceRunner func(context.Context, Paths, string, string, string) error

// UpgradeStopped retains ownership through backup, offline checks, pointer
// commit and durable completion. Any incomplete operation keeps its gate for
// explicit recovery; a successful return means the gate has been archived.
func UpgradeStopped(ctx context.Context, paths Paths, candidateVersion, destination string, run MaintenanceRunner) (result error) {
	if ctx == nil || run == nil {
		return errors.New("upgrade requires a context and maintenance runner")
	}
	owner, err := prepareUpgradeStopped(ctx, paths, candidateVersion, destination)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, owner.Close()) }()
	if err := owner.execute(ctx, run); err != nil {
		return err
	}
	return owner.finish(ctx)
}
