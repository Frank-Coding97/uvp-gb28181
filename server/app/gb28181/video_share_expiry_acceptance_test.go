package gb28181

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/videoshare"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/authoritytx"
	"uvplatform.com/uvp-gb28181/app/utils/logging"
)

type expiryAcceptanceScanner struct {
	service *videoshare.Service
	calls   atomic.Int32
}

func (s *expiryAcceptanceScanner) ScanExpired(ctx context.Context, limit int) (int, error) {
	s.calls.Add(1)
	return s.service.ScanExpired(logging.WithContext(ctx, zap.NewNop()), limit)
}

func TestVideoShareExpiryRuntimeRecoversMissedAuditAcrossReloads(t *testing.T) {
	// Go's synctest controls the actual time.Now/timers used by both production
	// Service and runtime. This is a controlled-clock stop/restart test, not a
	// claim that a real application stayed offline for six wall-clock minutes.
	synctest.Test(t, func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
		require.NoError(t, err)
		raw, err := db.DB()
		require.NoError(t, err)
		raw.SetMaxOpenConns(1)
		defer func() { require.NoError(t, raw.Close()) }()
		require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{},
			&gbmodels.GbChannelVideoShare{}, &basemodels.SysOperationLog{}, &authoritytx.Gate{}))
		require.NoError(t, db.Create(&authoritytx.Gate{ID: 1}).Error)
		require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch INTEGER NOT NULL DEFAULT 1").Error)
		device := gbmodels.GbDevice{DeviceID: "34020000002000000061"}
		require.NoError(t, db.Create(&device).Error)
		channel := gbmodels.GbChannel{DeviceID: device.DeviceID, ChannelID: "34020000001320000061"}
		require.NoError(t, db.Create(&channel).Error)
		signer, err := videoshare.NewSigner(make([]byte, 32))
		require.NoError(t, err)
		newService := func() *videoshare.Service {
			service, err := videoshare.NewService(db, signer, func() time.Duration { return 5 * time.Minute },
				func(context.Context, *gorm.DB, uint, uint) (videoshare.AuthorizationSnapshot, error) {
					return videoshare.AuthorizationSnapshot{DevicePK: device.ID, DeviceID: device.DeviceID, DeviceEpoch: 1}, nil
				})
			require.NoError(t, err)
			return service
		}
		service := newService()
		issued, err := service.SetEnabled(context.Background(), channel.ID, 7, true, 0)
		require.NoError(t, err)
		require.Equal(t, 5*time.Minute, issued.ExpiresAt.Sub(time.Now()))
		readShare := func() gbmodels.GbChannelVideoShare {
			var share gbmodels.GbChannelVideoShare
			require.NoError(t, db.Where("channel_pk = ?", channel.ID).Take(&share).Error)
			return share
		}
		expiryAudits := func() []basemodels.SysOperationLog {
			var logs []basemodels.SysOperationLog
			require.NoError(t, db.Where("operation = ?", "video_share.expired").Find(&logs).Error)
			return logs
		}
		start := func(service *videoshare.Service) (*expiryAcceptanceScanner, controlPlaneShutdownSnapshot) {
			scanner := &expiryAcceptanceScanner{service: service}
			cancel, done := startVideoShareExpiryRuntime(scanner, 5*time.Second)
			// Ensure failure assertions also cancel the production worker before
			// the bubble exits and the real SQL connection pool is closed.
			t.Cleanup(func() { cancel() })
			synctest.Wait()
			require.EqualValues(t, 1, scanner.calls.Load(), "exactly one immediate scan per started runtime")
			return scanner, controlPlaneShutdownSnapshot{videoShareExpiryCancel: cancel, videoShareExpiryDone: done}
		}
		stop := func(scanner *expiryAcceptanceScanner, snapshot controlPlaneShutdownSnapshot) {
			require.NoError(t, stopControlPlaneSnapshot(context.Background(), snapshot))
			require.NoError(t, stopControlPlaneSnapshot(context.Background(), snapshot), "repeated stop must be idempotent")
			synctest.Wait()
			select {
			case <-snapshot.videoShareExpiryDone:
			default:
				t.Fatal("shutdown returned before the real expiry runtime exited")
			}
			calls := scanner.calls.Load()
			time.Sleep(10 * time.Second)
			synctest.Wait()
			require.Equal(t, calls, scanner.calls.Load(), "stopped runtime must not leave a ticking worker")
		}

		scanner, snapshot := start(service)
		time.Sleep(4 * time.Minute)
		synctest.Wait()
		require.True(t, readShare().Enabled)
		require.Empty(t, expiryAudits())
		stop(scanner, snapshot)
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		require.True(t, time.Now().After(*issued.ExpiresAt))
		require.True(t, readShare().Enabled, "no worker ran while stopped")
		require.Empty(t, expiryAudits(), "missed expiry must still await the next runtime's startup scan")
		_, err = service.Resolve(context.Background(), issued.PublicID, issued.Signature)
		require.ErrorIs(t, err, videoshare.ErrExpired, "media authorization must reject expiry even before audit recovery")

		restartedAt := time.Now().UTC()
		scanner, snapshot = start(newService())
		recovered := readShare()
		require.False(t, recovered.Enabled)
		require.Equal(t, "expired", recovered.RevokeReason)
		require.Equal(t, restartedAt, *recovered.RevokedAt)
		require.Equal(t, *issued.ExpiresAt, *recovered.ExpiresAt)
		require.Equal(t, issued.Revision+1, recovered.Revision, "recovery adds exactly one expiry revocation")
		require.Equal(t, issued.Generation, recovered.Generation)
		logs := expiryAudits()
		require.Len(t, logs, 1)
		var metadata map[string]any
		require.NoError(t, json.Unmarshal([]byte(logs[0].RequestData), &metadata))
		require.Equal(t, "video_share.expired", metadata["event"])
		require.Equal(t, issued.ExpiresAt.Format(time.RFC3339Nano), metadata["expires_at"])
		require.Equal(t, restartedAt, logs[0].CreatedAt.UTC(), "audit records recovery time separately from original expiry")
		stop(scanner, snapshot)
		for i := 0; i < 3; i++ {
			scanner, snapshot = start(newService())
			time.Sleep(5 * time.Second)
			synctest.Wait()
			require.EqualValues(t, 2, scanner.calls.Load(), "reload owns one startup scan and one periodic scan")
			stop(scanner, snapshot)
			require.Equal(t, recovered, readShare(), "later reloads must not mutate the expired lifecycle again")
			require.Len(t, expiryAudits(), 1, "later scans/reloads must not duplicate the missed-expiry audit")
		}
	})
}
