package gb28181

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"uvplatform.com/uvp-gb28181/app/gb28181/devicecleanup"
	"uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/play"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

// deviceCleanupReconciler is the device-level aggregation owner for the durable
// media cleanup barrier. playauth.DeviceCleanupStore refuses every media
// admission while access_epoch is ahead of cleanup_completed_epoch, and an
// ownership transfer is what puts them out of step; this loop is the only thing
// that puts them back, so without it a transferred device never plays again.
var deviceCleanupReconciler *devicecleanup.Reconciler

// liveCleanupService is the playback half of the evidence: it stops this
// process's sessions for one device and reports how far that got.
type liveCleanupService interface {
	ClearDeviceBefore(ctx context.Context, deviceID string, targetEpoch int64) (play.DeviceCleanupReport, error)
}

// playLiveMedia adapts the playback service's process-local, known-only live
// cleanup onto the reconciler's evidence contract. Every non-terminal status is
// converted into an error on purpose: the reconciler must never read a partial
// drain as proof that no old-epoch session survives.
type playLiveMedia struct{ service liveCleanupService }

func (m playLiveMedia) SettleDeviceBefore(ctx context.Context, deviceID string, targetEpoch int64) error {
	if m.service == nil {
		return devicecleanup.ErrReconcilerUnavailable
	}
	report, err := m.service.ClearDeviceBefore(ctx, deviceID, targetEpoch)
	if err != nil {
		return err
	}
	switch report.Status {
	case play.DeviceCleanupSettled, play.DeviceCleanupNoTrackedEvidence:
		return nil
	default:
		return fmt.Errorf("%w: 进程内直播未达终态 status=%s settled=%d pending=%d unknown=%d newer=%d",
			playauth.ErrDeviceCleanupPending, report.Status, report.Settled, report.Pending, report.Unknown, report.Newer)
	}
}

// platformStreamProjection reads the platform's own durable statement about
// which channels currently hold a media stream. A process-local drain cannot
// speak for a session another instance still owns, so the reconciler needs this
// independent, cross-process half of the live evidence.
type platformStreamProjection struct{}

func (platformStreamProjection) ListPlayingChannels(ctx context.Context) (models.GbChannelList, error) {
	return models.ListPlayingChannels(ctx)
}

// startDeviceCleanupReconciler wires and starts the aggregation owner. It
// requires the playback service: without a live media authority the barrier can
// only stay closed, so a missing dependency is reported and left fail-closed
// rather than silently guessed.
func startDeviceCleanupReconciler(service *play.Service, devices *playauth.DeviceCleanupStore, intents *playauth.DeviceOperationIntentStore) error {
	if service == nil || devices == nil || intents == nil {
		app.ZapLog.Warn("GB28181 设备清理协调器未装配: 缺少播放/清理依赖，已转移设备的清理水位保持关闭",
			zap.String("event", "gb28181.lifecycle.device_cleanup_reconciler_skipped"))
		return nil
	}
	reconciler, err := devicecleanup.New(devices, intents, playLiveMedia{service: service}, platformStreamProjection{},
		devicecleanup.WithReport(func(stats devicecleanup.Stats, err error) {
			if err != nil {
				app.ZapLog.Error("设备清理水位对账失败",
					zap.String("event", "gb28181.lifecycle.device_cleanup_reconcile_failed"),
					zap.Int("examined", stats.Examined), zap.Int("completed", stats.Completed),
					zap.Int("pending", stats.Pending), zap.Int("failed", stats.Failed), zap.Error(err))
				return
			}
			if stats.Completed > 0 {
				app.ZapLog.Info("设备清理水位已追平，媒体闸门重新打开",
					zap.String("event", "gb28181.lifecycle.device_cleanup_reconciled"),
					zap.Int("examined", stats.Examined), zap.Int("completed", stats.Completed))
			}
		}),
	)
	if err != nil {
		return fmt.Errorf("装配设备清理协调器失败: %w", err)
	}
	if err := reconciler.Start(context.Background()); err != nil {
		return fmt.Errorf("启动设备清理协调器失败: %w", err)
	}
	deviceCleanupReconciler = reconciler
	app.ZapLog.Info("GB28181 设备清理协调器已启动",
		zap.String("event", "gb28181.lifecycle.device_cleanup_reconciler_started"),
		zap.Duration("interval", devicecleanup.DefaultInterval))
	return nil
}
