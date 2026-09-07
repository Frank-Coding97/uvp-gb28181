package ginhelper

import (
	"context"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/scheduler"
)

// ShutdownScheduler leaves the consumer running while accepted jobs finish.
// Canceling it first can lose the last result or block a full producer queue.
func ShutdownScheduler(ctx context.Context) error {
	if app.JobScheduler != nil {
		if drainer, ok := app.JobScheduler.(interface{ Shutdown(context.Context) error }); ok {
			if err := drainer.Shutdown(ctx); err != nil {
				return err
			}
		} else {
			app.JobScheduler.Stop()
		}
	}
	return scheduler.WaitResultHandler(ctx)
}
