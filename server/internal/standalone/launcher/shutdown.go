package launcher

import (
	"context"
	"errors"
)

// ShutdownSteps contains the ordered operations used to stop the standalone
// runtime. The callbacks own their concrete process and protocol operations;
// Shutdown only controls ordering and cancellation boundaries.
type ShutdownSteps struct {
	Quiesce  func(context.Context) error
	Media    func(context.Context) error
	Finalize func(context.Context) error
	Backend  func(context.Context) error
	Redis    func(context.Context) error
}

type shutdownStageError struct {
	stage   string
	cause   error
	missing bool
}

func (e shutdownStageError) Error() string {
	if e.missing {
		return "shutdown " + e.stage + " step is missing"
	}
	if e.stage == "" {
		return shutdownCauseMessage("shutdown", e.cause)
	}
	return shutdownCauseMessage("shutdown "+e.stage, e.cause)
}

func (e shutdownStageError) Unwrap() error {
	return e.cause
}

func shutdownCauseMessage(prefix string, cause error) string {
	switch {
	case errors.Is(cause, context.Canceled):
		return prefix + ": context canceled"
	case errors.Is(cause, context.DeadlineExceeded):
		return prefix + ": context deadline exceeded"
	default:
		return prefix + " failed"
	}
}

// Shutdown runs the injected shutdown operations in their required order.
// Each callback runs synchronously in the caller's goroutine. A callback that
// ignores ctx therefore remains the caller's responsibility and cannot be
// detached as an untracked goroutine.
func Shutdown(ctx context.Context, steps ShutdownSteps) error {
	if ctx == nil {
		return errors.New("shutdown: nil context")
	}
	if err := ctx.Err(); err != nil {
		return shutdownStageError{cause: err}
	}

	ordered := [...]struct {
		name string
		call func(context.Context) error
	}{
		{name: "quiesce", call: steps.Quiesce},
		{name: "media", call: steps.Media},
		{name: "finalize", call: steps.Finalize},
		{name: "backend", call: steps.Backend},
		{name: "redis", call: steps.Redis},
	}
	for _, step := range ordered {
		if step.call == nil {
			return shutdownStageError{stage: step.name, missing: true}
		}
	}
	for _, step := range ordered {
		if err := ctx.Err(); err != nil {
			return shutdownStageError{stage: step.name, cause: err}
		}
		if err := step.call(ctx); err != nil {
			return shutdownStageError{stage: step.name, cause: err}
		}
		if err := ctx.Err(); err != nil {
			return shutdownStageError{stage: step.name, cause: err}
		}
	}
	return nil
}
