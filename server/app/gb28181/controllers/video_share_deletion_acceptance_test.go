package controllers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/videoshare"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
)

func TestVideoShareDeletionPermanentlyRetiresReusedSourceIdentity(t *testing.T) {
	for _, target := range []string{"device", "channel"} {
		t.Run(target, func(t *testing.T) {
			router, db, service, device, channel := videoShareDeletionFixture(t)
			ctx := context.Background()
			link, err := service.SetEnabled(ctx, channel.ID, 7, true, 0)
			require.NoError(t, err)
			_, err = service.Resolve(ctx, link.PublicID, link.Signature)
			require.NoError(t, err, "positive control: the original source is shareable")
			// A channel deletion must spare siblings on the same device; a device
			// deletion must spare channels belonging to another device.
			otherDevice := device
			if target == "device" {
				otherDevice = gbmodels.GbDevice{DeviceID: "34020000002000000002"}
				require.NoError(t, db.Create(&otherDevice).Error)
			}
			otherChannel := gbmodels.GbChannel{DeviceID: otherDevice.DeviceID, ChannelID: "34020000001320000002"}
			require.NoError(t, db.Create(&otherChannel).Error)
			otherLink, err := service.SetEnabled(ctx, otherChannel.ID, 7, true, 0)
			require.NoError(t, err)
			var beforeOther gbmodels.GbChannelVideoShare
			require.NoError(t, db.Where("channel_pk = ?", otherChannel.ID).Take(&beforeOther).Error)

			id := channel.ID
			if target == "device" {
				id = device.ID
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete,
				"/api/gb28181/device-mgmt/"+target+"/"+uintStr(id), nil))
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.EqualValues(t, 0, unmarshal(t, response)["code"], response.Body.String())
			var remaining int64
			require.NoError(t, db.Unscoped().Model(&gbmodels.GbChannel{}).Where("id = ?", channel.ID).Count(&remaining).Error)
			require.Zero(t, remaining, "the real deletion handler must physically remove the channel")
			var retired gbmodels.GbChannelVideoShare
			require.NoError(t, db.Where("channel_pk = ?", channel.ID).Take(&retired).Error)
			require.False(t, retired.Enabled)
			require.NotNil(t, retired.RevokedAt)
			require.Equal(t, link.Revision+1, retired.Revision)
			var afterOther gbmodels.GbChannelVideoShare
			require.NoError(t, db.Where("channel_pk = ?", otherChannel.ID).Take(&afterOther).Error)
			require.Equal(t, beforeOther, afterOther, "unrelated share lifecycle must remain untouched")
			_, err = service.Resolve(ctx, otherLink.PublicID, otherLink.Signature)
			require.NoError(t, err, "unrelated channel must remain shareable")
			_, err = service.Resolve(ctx, link.PublicID, link.Signature)
			require.Error(t, err, "deletion must immediately deny the link")

			// Restore both the numeric identity and GB identifier, as can happen
			// after importing/restoring a device. Deletion must retire the old
			// credential permanently, independently of future identity reuse.
			if target == "device" {
				require.NoError(t, db.Unscoped().Model(&gbmodels.GbDevice{}).Where("id = ?", device.ID).Count(&remaining).Error)
				require.Zero(t, remaining)
				replacement := gbmodels.GbDevice{DeviceID: device.DeviceID}
				replacement.ID = device.ID
				require.NoError(t, db.Create(&replacement).Error)
			}
			replacement := gbmodels.GbChannel{DeviceID: channel.DeviceID, ChannelID: channel.ChannelID}
			replacement.ID = channel.ID
			require.NoError(t, db.Create(&replacement).Error)
			_, err = service.Resolve(ctx, link.PublicID, link.Signature)
			require.Error(t, err, "the deleted source's old credential must never revive after identity reuse")
		})
	}
}

func TestVideoShareDeletionRetiresPendingWithoutStartingExpiry(t *testing.T) {
	for _, target := range []string{"device", "channel"} {
		t.Run(target, func(t *testing.T) {
			router, db, service, device, channel := videoShareDeletionFixture(t)
			ctx := context.Background()
			require.NoError(t, service.EnsurePending(ctx, channel.ID, device.ID, device.DeviceID, 1, nil, 1))
			var before gbmodels.GbChannelVideoShare
			require.NoError(t, db.Where("channel_pk = ?", channel.ID).Take(&before).Error)
			require.True(t, before.Enabled)
			require.Nil(t, before.ExpiresAt)
			id := channel.ID
			if target == "device" {
				id = device.ID
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete,
				"/api/gb28181/device-mgmt/"+target+"/"+uintStr(id), nil))
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var retired gbmodels.GbChannelVideoShare
			require.NoError(t, db.Where("channel_pk = ?", channel.ID).Take(&retired).Error)
			require.False(t, retired.Enabled, "unactivated auto shares must retire too")
			require.NotNil(t, retired.RevokedAt)
			require.Equal(t, before.Revision+1, retired.Revision)
			require.Nil(t, retired.ActivatedAt)
			require.Nil(t, retired.ExpiresAt)
			if target == "device" {
				replacement := gbmodels.GbDevice{DeviceID: device.DeviceID}
				replacement.ID = device.ID
				require.NoError(t, db.Create(&replacement).Error)
			}
			replacement := gbmodels.GbChannel{DeviceID: channel.DeviceID, ChannelID: channel.ChannelID}
			replacement.ID = channel.ID
			require.NoError(t, db.Create(&replacement).Error)
			require.NoError(t, service.EnsurePending(ctx, channel.ID, device.ID, device.DeviceID, 1, nil, 2))
			_, err := service.AcquireLink(ctx, channel.ID, 7, retired.Revision)
			require.ErrorIs(t, err, videoshare.ErrExpired, "catalog discovery and first click must not reopen the deleted lifecycle")
		})
	}
}

func TestVideoShareDeletionAuditFailureRollsBackSourceAndShare(t *testing.T) {
	for _, target := range []string{"device", "channel"} {
		t.Run(target, func(t *testing.T) {
			router, db, service, device, channel := videoShareDeletionFixture(t)
			ctx := context.Background()
			link, err := service.SetEnabled(ctx, channel.ID, 7, true, 0)
			require.NoError(t, err)
			var before gbmodels.GbChannelVideoShare
			require.NoError(t, db.Where("channel_pk = ?", channel.ID).Take(&before).Error)
			var auditBefore int64
			require.NoError(t, db.Model(&basemodels.SysOperationLog{}).Count(&auditBefore).Error)
			failure := errors.New("acceptance: deletion audit unavailable")
			auditAttempts := 0
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register("video_share_deletion_audit_failure", func(tx *gorm.DB) {
				if tx.Statement.Table == (basemodels.SysOperationLog{}).TableName() {
					auditAttempts++
					tx.AddError(failure)
				}
			}))
			t.Cleanup(func() { _ = db.Callback().Create().Remove("video_share_deletion_audit_failure") })
			id := channel.ID
			if target == "device" {
				id = device.ID
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete,
				"/api/gb28181/device-mgmt/"+target+"/"+uintStr(id), nil))
			require.Greater(t, auditAttempts, 0, "positive control: the audit failure must be injected")
			require.NotEqualValues(t, 0, unmarshal(t, response)["code"], "failed audit must fail the deletion")
			var afterDevice gbmodels.GbDevice
			require.NoError(t, db.Take(&afterDevice, device.ID).Error)
			require.Equal(t, device, afterDevice, "device deletion must roll back")
			var afterChannel gbmodels.GbChannel
			require.NoError(t, db.Take(&afterChannel, channel.ID).Error)
			require.Equal(t, channel, afterChannel, "channel deletion must roll back")
			var after gbmodels.GbChannelVideoShare
			require.NoError(t, db.Where("channel_pk = ?", channel.ID).Take(&after).Error)
			require.Equal(t, before, after, "failed audit must roll back the share retirement")
			var auditAfter int64
			require.NoError(t, db.Model(&basemodels.SysOperationLog{}).Count(&auditAfter).Error)
			require.Equal(t, auditBefore, auditAfter)
			_, err = service.Resolve(ctx, link.PublicID, link.Signature)
			require.NoError(t, err, "the rolled back source remains shareable")
		})
	}
}

func videoShareDeletionFixture(t *testing.T) (*gin.Engine, *gorm.DB, *videoshare.Service, gbmodels.GbDevice, gbmodels.GbChannel) {
	t.Helper()
	router, db := newDeviceMgmtRouter(t)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&basemodels.SysOperationLog{}))
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch INTEGER NOT NULL DEFAULT 1").Error)
	creator := basemodels.User{BaseModel: basemodels.BaseModel{ID: 7}, Username: "share-creator", Status: 1}
	role := basemodels.SysRole{Name: "share-full-access", Status: 1, DataScope: 1}
	require.NoError(t, db.Create(&creator).Error)
	require.NoError(t, db.Create(&role).Error)
	require.NoError(t, db.Create(&basemodels.SysUserRole{UserID: creator.ID, RoleID: role.ID}).Error)
	device := gbmodels.GbDevice{DeviceID: "34020000002000000001"}
	require.NoError(t, db.Create(&device).Error)
	channel := gbmodels.GbChannel{DeviceID: device.DeviceID, ChannelID: "34020000001320000001"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Take(&device, device.ID).Error)
	require.NoError(t, db.Take(&channel, channel.ID).Error)
	signer, err := videoshare.NewSigner(make([]byte, 32))
	require.NoError(t, err)
	service, err := videoshare.NewService(db, signer, func() time.Duration { return 5 * time.Minute },
		videoshare.NewDatabaseAuthorizer(func(context.Context, uint, string, string) (bool, error) { return true, nil }))
	require.NoError(t, err)
	t.Cleanup(service.DrainViewers)
	return router, db, service, device, channel
}
