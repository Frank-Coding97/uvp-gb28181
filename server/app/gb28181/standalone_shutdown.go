package gb28181

import (
	"context"
	"errors"

	gbroutes "uvplatform.cn/uvp-gb28181/app/gb28181/routes"
)

// Quiesce stops business producers and media sessions while preserving the
// recording Hook indexer, node resolver and database. The launcher must stop
// ZLM and drain HTTP callbacks before calling StopContext to dismantle these.
func Quiesce(ctx context.Context) error {
	sipLifecycleMu.Lock()
	defer sipLifecycleMu.Unlock()
	if playSvc != nil {
		playSvc.BeginShutdown()
	}
	gbroutes.SetPlayService(nil) // also cancels and joins auto-start workers
	if playReconciler != nil {
		playReconciler.Stop()
		playReconciler = nil
	}
	if recordingReconciler != nil {
		recordingReconciler.Stop()
		recordingReconciler = nil
	}
	if sipServer != nil {
		drainer, ok := sipServer.(interface{ DrainRequests(context.Context) error })
		if !ok {
			return errors.New("SIP request draining unavailable")
		}
		if err := drainer.DrainRequests(ctx); err != nil {
			// Do not clear processors while an admitted handler can still use them.
			return err
		}
	}
	// Stop configuration/restart producers before the launcher shuts down media.
	// Keep the registry and recording Hook dependencies installed through finalize.
	if err := quiesceZLMBackground(ctx); err != nil {
		return err
	}
	var result error
	if recordingSvc != nil {
		result = errors.Join(result, recordingSvc.Shutdown(ctx))
	}
	if playSvc != nil {
		result = errors.Join(result, playSvc.Shutdown(ctx))
	}
	result = errors.Join(result, stopPlaybackRuntime(ctx))
	result = errors.Join(result, stopTalkRuntime(ctx))
	result = errors.Join(result, stopCascadeRuntime(ctx))
	stopRecordQueryRuntime()
	stopFirmwareUpgradeRuntime()
	stopPTZRuntime()
	if subscriptionScheduler != nil {
		subscriptionScheduler.Stop()
		subscriptionScheduler = nil
	}
	if offlineScanner != nil {
		offlineScanner.Stop()
		offlineScanner = nil
	}
	if sipServer != nil {
		result = errors.Join(result, sipServer.Shutdown(ctx))
		sipServer = nil
	}
	return errors.Join(result, ctx.Err())
}
