package gb28181

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/gb28181/devicecleanup"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type fakeLiveCleanup struct {
	report play.DeviceCleanupReport
	err    error
}

func (f fakeLiveCleanup) ClearDeviceBefore(context.Context, string, int64) (play.DeviceCleanupReport, error) {
	return f.report, f.err
}

// A partial drain must never be forwarded as proof: the reconciler only opens
// the media gate on nil, so anything short of terminal has to become an error.
func TestPlayLiveMediaOnlyProvesTerminalDrains(t *testing.T) {
	ctx := context.Background()
	for _, status := range []play.DeviceCleanupStatus{play.DeviceCleanupSettled, play.DeviceCleanupNoTrackedEvidence} {
		t.Run(string(status), func(t *testing.T) {
			require.NoError(t, playLiveMedia{service: fakeLiveCleanup{report: play.DeviceCleanupReport{Status: status}}}.
				SettleDeviceBefore(ctx, "34020000001320000051", 2))
		})
	}
	for _, status := range []play.DeviceCleanupStatus{play.DeviceCleanupPending, play.DeviceCleanupUnknown, ""} {
		t.Run("unproven:"+string(status), func(t *testing.T) {
			err := playLiveMedia{service: fakeLiveCleanup{report: play.DeviceCleanupReport{Status: status, Pending: 1}}}.
				SettleDeviceBefore(ctx, "34020000001320000051", 2)
			require.ErrorIs(t, err, playauth.ErrDeviceCleanupPending)
		})
	}
}

func TestPlayLiveMediaPropagatesDrainFailure(t *testing.T) {
	failure := errors.New("fixture: live cleanup refused")
	err := playLiveMedia{service: fakeLiveCleanup{report: play.DeviceCleanupReport{Status: play.DeviceCleanupSettled}, err: failure}}.
		SettleDeviceBefore(context.Background(), "34020000001320000051", 2)
	require.ErrorIs(t, err, failure)
}

func TestPlayLiveMediaRejectsMissingPlaybackAuthority(t *testing.T) {
	err := playLiveMedia{}.SettleDeviceBefore(context.Background(), "34020000001320000051", 2)
	require.ErrorIs(t, err, devicecleanup.ErrReconcilerUnavailable)
}

func TestStartDeviceCleanupReconcilerRequiresEveryDependency(t *testing.T) {
	previous := app.ZapLog
	app.ZapLog = zap.NewNop()
	t.Cleanup(func() { app.ZapLog = previous })
	require.NoError(t, startDeviceCleanupReconciler(nil, nil, nil), "a missing dependency is reported, not fatal")
	require.Nil(t, deviceCleanupReconciler)
}

// The composition root must actually wire the watermark owner: a dangling
// DeviceCleanupStore is precisely the defect this wiring exists to fix, and it
// is invisible to every unit test that injects its own store.
func TestBootstrapStartsAndStopsTheDeviceCleanupOwner(t *testing.T) {
	bootstrap, err := os.ReadFile("bootstrap.go")
	require.NoError(t, err)
	source := string(bootstrap)
	begin, end := strings.Index(source, "func startSIPDependenciesWithFactory("), strings.Index(source, "func buildPlaySigner(")
	require.Greater(t, end, begin)
	start := source[begin:end]
	wired := strings.Index(start, "startDeviceCleanupReconciler(playSvc,")
	require.GreaterOrEqual(t, wired, 0, "bootstrap must start the device cleanup watermark owner")
	require.Less(t, strings.Index(start, "gbroutes.SetPlayService(playSvc)"), wired,
		"the owner drains the playback service, so it must start after that service is published")

	shutdown, err := os.ReadFile("bootstrap_shutdown.go")
	require.NoError(t, err)
	shutdownSource := string(shutdown)
	require.Contains(t, shutdownSource, "deviceCleanupReconciler: deviceCleanupReconciler",
		"the owner must be captured by the shutdown snapshot")
	require.Contains(t, shutdownSource, "r.deviceCleanupReconciler.Stop(ctx)",
		"the owner must be joined during shutdown")
	require.Contains(t, shutdownSource, "deviceCleanupReconciler = nil",
		"the owner global must be cleared so a restart can wire a fresh one")
}
