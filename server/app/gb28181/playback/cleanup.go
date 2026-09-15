package playback

import (
	"context"
	"errors"
)

type CleanupRunner struct{}

func (CleanupRunner) Run(ctx context.Context, resources CleanupResources) error {
	if resources == nil {
		return nil
	}
	var result error
	result = errors.Join(result, resources.Teardown(ctx))
	result = errors.Join(result, resources.CloseRTP(ctx))
	result = errors.Join(result, resources.Unbind(ctx))
	return result
}
