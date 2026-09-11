package gb28181

import "context"

// Completion channels are captured when each worker starts. Cancellation alone
// cannot prove that a final metrics flush or a database prune has returned.
var (
	metricsPersistDone, metricsCleanupDone, dashboardRetentionDone    <-chan struct{}
	startupProbeDone, positionHistoryPruneDone, schedulerLogPruneDone <-chan struct{}
	heartbeatDone, threadPollerDone, nodeConvergenceDone              <-chan struct{}
	trafficSamplerDone, trafficPrunerDone                             <-chan struct{}
	startupProbeCancel                                                context.CancelFunc
)

func runBackground(run func()) <-chan struct{} {
	done := make(chan struct{})
	go func() { defer close(done); run() }()
	return done
}

func waitBackground(ctx context.Context, completions ...<-chan struct{}) error {
	for _, done := range completions {
		if done == nil {
			continue
		}
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func waitControlPlaneBackground(ctx context.Context) error {
	return waitBackground(ctx, metricsPersistDone, metricsCleanupDone, dashboardRetentionDone,
		startupProbeDone, positionHistoryPruneDone, schedulerLogPruneDone,
		heartbeatDone, threadPollerDone, nodeConvergenceDone, trafficSamplerDone, trafficPrunerDone)
}

func quiesceZLMBackground(ctx context.Context) error {
	if startupProbeCancel != nil {
		startupProbeCancel()
		startupProbeCancel = nil
	}
	if heartbeatCancel != nil {
		heartbeatCancel()
		heartbeatCancel = nil
	}
	if err := waitBackground(ctx, startupProbeDone, heartbeatDone, threadPollerDone, nodeConvergenceDone); err != nil {
		return err
	}
	return drainZLMManagementCore(ctx)
}
