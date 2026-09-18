package limit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestOpenAPIQuotaRejectsIncompleteDeviceCleanup(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
	}{
		{name: "pending", value: int64(2)},
		{name: "null", value: nil},
		{name: "nonpositive", value: int64(0)},
		{name: "ahead", value: int64(4)},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := quotaFixture(t, 1)
			require.NoError(t, db.Table("gb_device").Where("device_id = ?", "device-a").Update("cleanup_completed_epoch", test.value).Error)
			quota := NewQuota(db, func() time.Time { return quotaTestNow })
			_, err := quota.ReservePending(context.Background(), ReservationRequest{
				ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a",
			})
			require.ErrorIs(t, err, ErrQuotaUnavailable)

			var count int64
			require.NoError(t, db.Model(&models.PlayGrant{}).Count(&count).Error)
			require.Zero(t, count, "cleanup barrier must reject before creating a grant")
		})
	}
}

func TestOpenAPIQuotaAllowsDeviceOnlyAfterCleanupAcknowledgesCurrentEpoch(t *testing.T) {
	db := quotaFixture(t, 1)
	require.NoError(t, db.Table("gb_device").Where("device_id = ?", "device-a").Update("cleanup_completed_epoch", 2).Error)
	quota := NewQuota(db, func() time.Time { return quotaTestNow })
	request := ReservationRequest{ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"}
	_, err := quota.ReservePending(context.Background(), request)
	require.ErrorIs(t, err, ErrQuotaUnavailable)

	require.NoError(t, db.Table("gb_device").Where("device_id = ?", "device-a").Update("cleanup_completed_epoch", 3).Error)
	reservation, err := quota.ReservePending(context.Background(), request)
	require.NoError(t, err)
	require.NotEmpty(t, reservation.GrantID)
	require.Equal(t, int64(3), reservation.DeviceEpoch)
	var grant models.PlayGrant
	require.NoError(t, db.First(&grant, "grant_id = ?", reservation.GrantID).Error)
	require.Equal(t, int64(3), grant.DeviceEpoch)
}

func TestOpenAPIQuotaReservationKeepsAdmissionEpochAfterDeviceEpochBump(t *testing.T) {
	db := quotaFixture(t, 1)
	quota := NewQuota(db, func() time.Time { return quotaTestNow })
	request := ReservationRequest{ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"}

	reservation, err := quota.ReservePending(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, int64(3), reservation.DeviceEpoch)
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=4, cleanup_completed_epoch=4 WHERE device_id=?", request.DeviceID).Error)

	require.Equal(t, int64(3), reservation.DeviceEpoch, "a committed admission must not be upgraded from a later device epoch")
	var grant models.PlayGrant
	require.NoError(t, db.First(&grant, "grant_id = ?", reservation.GrantID).Error)
	require.Equal(t, int64(3), grant.DeviceEpoch)
}
