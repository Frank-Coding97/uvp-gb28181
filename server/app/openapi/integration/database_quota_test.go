package integration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	openapiconfig "uvplatform.cn/uvp-gb28181/app/openapi/config"
	"uvplatform.cn/uvp-gb28181/app/openapi/limit"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

// Invoked only after the parent native gate has proved an empty test database
// and applied the actual migrations. All rows below belong to that fixture.
func checkNativeQuota(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(db.Statement.Context, 20*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	dateType := "DATETIME(6)"
	if db.Dialector.Name() == "postgres" {
		dateType = "TIMESTAMPTZ"
	}
	if db.Dialector.Name() == "sqlserver" {
		dateType = "DATETIME2(6)"
	}
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD device_id VARCHAR(20) NULL").Error)
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD deleted_at "+dateType+" NULL").Error)
	device, channel := "34020000002000000001", "34020000001320000001"
	require.NoError(t, db.Table("gb_device").Where("id = ?", 1).Update("device_id", device).Error)
	now := time.Now().UTC().Truncate(time.Second)
	clients := make([]models.Client, 2)
	for i := range clients {
		clients[i] = models.Client{AK: fmt.Sprintf("uvp_%032x", 990001+i), Name: "quota-fixture", OwnerDeptID: 10, Status: models.StatusActive, SecretCiphertext: []byte("fixture"), SecretIV: []byte("fixture"), SecretKeyID: "fixture", ViewerQuota: 10, CreatedAt: now, UpdatedAt: now}
		require.NoError(t, db.Create(&clients[i]).Error)
		require.NoError(t, db.Create(&models.ClientScope{ClientID: clients[i].ID, Scope: limit.PlayLiveApplyScope, Enabled: true, UpdatedAt: now}).Error)
	}
	quota := limit.NewQuota(db, func() time.Time { return now })
	request := limit.ReservationRequest{ClientID: clients[0].ID, Scope: limit.PlayLiveApplyScope, DeviceID: device, ChannelID: channel}
	var accepted, limited, failed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 11; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := quota.ReservePending(ctx, request)
			switch {
			case err == nil:
				accepted.Add(1)
			case errors.Is(err, limit.ErrQuotaExceeded):
				limited.Add(1)
			default:
				failed.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 10, accepted.Load(), "native row locks must serialize count+insert")
	require.EqualValues(t, 1, limited.Load())
	require.Zero(t, failed.Load(), "connection/SQL errors cannot masquerade as quota rejections")
	other := request
	other.ClientID = clients[1].ID
	_, err := quota.ReservePending(ctx, other)
	require.NoError(t, err)
	count, err := limit.NewQuota(db, func() time.Time { return now }).Occupied(ctx, request.ClientID)
	require.NoError(t, err)
	require.EqualValues(t, 10, count, "new service reads persisted occupancy")
	now = now.Add(30 * time.Second)
	released, err := quota.ReleaseExpired(ctx, request.ClientID)
	require.NoError(t, err)
	require.EqualValues(t, 10, released)
	var pendingFailed int64
	require.NoError(t, db.Model(&models.PlayGrant{}).Where("client_id = ? AND state = ? AND reason = ?", request.ClientID, "failed", "pending_timeout").Count(&pendingFailed).Error)
	require.EqualValues(t, 10, pendingFailed)
	_, err = quota.ReservePending(ctx, request)
	require.NoError(t, err)
	// Keep the rest of the destructive-down gate isolated and empty.
	for _, id := range []int64{clients[0].ID, clients[1].ID} {
		require.NoError(t, db.Where("client_id = ?", id).Delete(&models.PlayGrant{}).Error)
		require.NoError(t, db.Where("client_id = ?", id).Delete(&models.ClientScope{}).Error)
		require.NoError(t, db.Where("id = ?", id).Delete(&models.Client{}).Error)
	}
	t.Log("native quota: 11 requests => 10 reserved/1 limited; other client independent; reconstructed service preserves count; pending30s release passed")
}

func checkNativeMustAuthLatch(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(db.Statement.Context, 15*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	var failures atomic.Int32
	for i := 0; i < 11; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state, err := openapiconfig.NewMustAuthStore(db, time.Now).Latch(ctx)
			if err != nil || !state.MustAuthLocked || state.LockVersion != 1 {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Zero(t, failures.Load())
	state, err := openapiconfig.NewMustAuthStore(db, time.Now).Load(ctx)
	require.NoError(t, err)
	require.True(t, state.MustAuthLocked)
	require.EqualValues(t, 1, state.LockVersion)
	t.Log("native must-auth latch: concurrent requests preserve exactly one locked row/version1; new store restores state")
}
