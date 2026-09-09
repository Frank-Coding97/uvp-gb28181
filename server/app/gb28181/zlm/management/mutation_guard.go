package management

import (
	"context"
	"errors"
)

// MutationGuard is the management-side seam for the process-wide MP4/source
// mutation gate. The gate owns persistence checks and serialization; this
// package only supplies the exact target and executes the external operation
// in the context returned by the outer gate callback.
type MutationGuard func(context.Context, OwnershipTarget, func(context.Context) error) error

// These aliases make the two mutation boundaries explicit at configuration
// sites while keeping one callback contract for the bootstrap adapter.
type MP4MutationGuard = MutationGuard
type SourceCloseGuard = MutationGuard

var errMutationGuardCallback = errors.New("mutation guard callback is not configured")

func runMutationGuard(ctx context.Context, guard MutationGuard, target OwnershipTarget, operation func(context.Context) error) error {
	if operation == nil {
		return errMutationGuardCallback
	}
	ctx = nonNilContext(ctx)
	if guard == nil {
		return operation(ctx)
	}
	return guard(ctx, target, func(operationCtx context.Context) error {
		return operation(nonNilContext(operationCtx))
	})
}
