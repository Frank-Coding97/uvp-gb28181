package standalone

import (
	"context"
	"errors"
	"os"
)

// recoveryMoveTree is the single-directory step used under the recovery
// coordinator's instance/config locks and persistent gate. It never removes
// either name and never infers success from the rename return value alone.
func recoveryMoveTree(ctx context.Context, source, target, identity string, rename func(string, string) error) error {
	if !validMaintenancePermitHex(identity) || rename == nil {
		return errors.New("invalid recovery move identity")
	}
	state := func() (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		_, sourceErr := os.Lstat(source)
		_, targetErr := os.Lstat(target)
		for _, err := range []error{sourceErr, targetErr} {
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, err
			}
		}
		sourceExists, targetExists := sourceErr == nil, targetErr == nil
		if sourceExists == targetExists {
			return false, errors.New("ambiguous recovery directory names")
		}
		path := source
		if targetExists {
			path = target
		}
		actual, err := recoveryTreeIdentity(ctx, path)
		if err != nil {
			return false, err
		}
		if actual != identity {
			return false, errors.New("recovery directory identity changed")
		}
		return targetExists, nil
	}
	complete, err := state()
	if err != nil || complete {
		return err
	}
	renameErr := rename(source, target)
	complete, err = state()
	if err != nil {
		return errors.Join(renameErr, err)
	}
	if complete {
		return nil
	}
	return errors.Join(renameErr, errors.New("recovery directory was not moved"))
}
