package videoshare

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/authoritytx"
)

func authorizationAcceptanceFixture(t *testing.T, check PermissionChecker) (*Service, gbmodels.GbChannel) {
	t.Helper()
	db, channel := authorizerFixture(t, 10)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbChannelVideoShare{}, &authoritytx.Gate{}, &basemodels.SysOperationLog{}))
	require.NoError(t, db.Create(&authoritytx.Gate{ID: 1}).Error)
	admin := basemodels.User{BaseModel: basemodels.BaseModel{ID: 10}, Username: "current-admin", DeptID: 10, Status: 1}
	role := basemodels.SysRole{Name: "current-admin-full-access", Status: 1, DataScope: 1}
	require.NoError(t, db.Create(&admin).Error)
	require.NoError(t, db.Create(&role).Error)
	require.NoError(t, db.Create(&basemodels.SysUserRole{UserID: admin.ID, RoleID: role.ID}).Error)
	signer, err := NewSigner(make([]byte, keyBytes))
	require.NoError(t, err)
	service, err := NewService(db, signer, func() time.Duration { return 5 * time.Minute }, NewDatabaseAuthorizer(check))
	require.NoError(t, err)
	t.Cleanup(service.DrainViewers)
	return service, channel
}

func TestAuthorizationAcceptanceCurrentAdminCannotReviveLostCreatorLink(t *testing.T) {
	var creatorAllowed atomic.Bool
	creatorAllowed.Store(true)
	service, channel := authorizationAcceptanceFixture(t, func(_ context.Context, actor uint, _, _ string) (bool, error) {
		return actor == 10 || creatorAllowed.Load(), nil
	})
	ctx := context.Background()
	original, err := service.SetEnabled(ctx, channel.ID, 7, true, 0)
	require.NoError(t, err)
	creatorAllowed.Store(false)
	for _, action := range []string{"acquire", "enable"} {
		var offered Link
		if action == "acquire" {
			offered, err = service.AcquireLink(ctx, channel.ID, 10, original.Revision)
		} else {
			offered, err = service.SetEnabled(ctx, channel.ID, 10, true, original.Revision)
		}
		require.NoError(t, err, "an authorized management action must not replace creator identity implicitly")
		require.Equal(t, original.Generation, offered.Generation)
		_, err = service.Resolve(ctx, original.PublicID, original.Signature)
		require.ErrorIs(t, err, ErrExpired)
		_, err = service.Resolve(ctx, offered.PublicID, offered.Signature)
		require.ErrorIs(t, err, ErrExpired, "current admin permission cannot authorize the old creator's link")
		var row gbmodels.GbChannelVideoShare
		require.NoError(t, service.db.Where("channel_pk = ?", channel.ID).Take(&row).Error)
		require.NotNil(t, row.CreatedBy)
		require.EqualValues(t, 7, *row.CreatedBy)
	}
	fresh, err := service.Regenerate(ctx, channel.ID, 10, original.Revision)
	require.NoError(t, err)
	require.NotEqual(t, original.Generation, fresh.Generation)
	current, err := service.Resolve(ctx, fresh.PublicID, fresh.Signature)
	require.NoError(t, err)
	require.EqualValues(t, 10, *current.CreatedBy)
	_, err = service.Resolve(ctx, original.PublicID, original.Signature)
	require.Error(t, err, "explicit regeneration must retire the old credential")
}

func TestAuthorizationAcceptanceRecreatedGBIdentifiersDoNotInheritShare(t *testing.T) {
	for _, target := range []string{"device-recreated-same-gbid", "channel-recreated-same-gbid", "channel-moved-to-other-device"} {
		t.Run(target, func(t *testing.T) {
			service, channel := authorizationAcceptanceFixture(t, func(context.Context, uint, string, string) (bool, error) { return true, nil })
			ctx := context.Background()
			link, err := service.SetEnabled(ctx, channel.ID, 7, true, 0)
			require.NoError(t, err)
			var oldDevice gbmodels.GbDevice
			require.NoError(t, service.db.Where("device_id = ?", channel.DeviceID).Take(&oldDevice).Error)
			switch target {
			case "device-recreated-same-gbid":
				require.NoError(t, service.db.Unscoped().Delete(&oldDevice).Error)
				replacement := gbmodels.GbDevice{DeviceID: oldDevice.DeviceID, OwnerDeptID: 10}
				require.NoError(t, service.db.Create(&replacement).Error)
				require.NotEqual(t, oldDevice.ID, replacement.ID)
				// Even an equal epoch and GB identifier cannot substitute for the persisted device row.
				require.NoError(t, service.db.Model(&replacement).Update("access_epoch", 9).Error)
			case "channel-recreated-same-gbid":
				require.NoError(t, service.db.Unscoped().Delete(&channel).Error)
				replacement := gbmodels.GbChannel{DeviceID: channel.DeviceID, ChannelID: channel.ChannelID, OwnerDeptID: 10}
				require.NoError(t, service.db.Create(&replacement).Error)
				require.NotEqual(t, channel.ID, replacement.ID)
				status, err := service.Status(ctx, replacement.ID, 10)
				require.NoError(t, err)
				require.False(t, status.Enabled)
				require.Zero(t, status.Revision, "same GB code must not transfer the old numeric channel's lifecycle")
			case "channel-moved-to-other-device":
				replacement := gbmodels.GbDevice{DeviceID: "34020000002000000002", OwnerDeptID: 10}
				require.NoError(t, service.db.Create(&replacement).Error)
				require.NoError(t, service.db.Model(&replacement).Update("access_epoch", 9).Error)
				require.NoError(t, service.db.Model(&channel).Update("device_id", replacement.DeviceID).Error)
			}
			_, err = service.Resolve(ctx, link.PublicID, link.Signature)
			require.ErrorIs(t, err, ErrExpired)
		})
	}
}

func TestAuthorizationAcceptancePendingUsesActualActivatingCreator(t *testing.T) {
	service, channel := authorizationAcceptanceFixture(t, func(context.Context, uint, string, string) (bool, error) { return true, nil })
	ctx := context.Background()
	policyActor := uint(7)
	var device gbmodels.GbDevice
	require.NoError(t, service.db.Where("device_id = ?", channel.DeviceID).Take(&device).Error)
	require.NoError(t, service.EnsurePending(ctx, channel.ID, device.ID, device.DeviceID, 9, &policyActor, 3))
	var pending gbmodels.GbChannelVideoShare
	require.NoError(t, service.db.Where("channel_pk = ?", channel.ID).Take(&pending).Error)
	require.Nil(t, pending.CreatedBy, "global policy actor is not a link creator")
	require.Nil(t, pending.ActivatedAt)
	require.Nil(t, pending.ExpiresAt)
	require.EqualValues(t, 7, *pending.PolicyActorID)
	link, err := service.AcquireLink(ctx, channel.ID, 10, pending.Revision)
	require.NoError(t, err)
	active, err := service.Resolve(ctx, link.PublicID, link.Signature)
	require.NoError(t, err)
	require.EqualValues(t, 10, *active.CreatedBy)
	require.Equal(t, "auto", active.Source)
	require.NoError(t, service.db.Transaction(func(tx *gorm.DB) error {
		return RevokeCreatorTx(ctx, tx, policyActor, "creator_access_changed", time.Now())
	}))
	_, err = service.Resolve(ctx, link.PublicID, link.Signature)
	require.NoError(t, err, "revoking the policy administrator must not revoke another user's activated share")
	require.NoError(t, service.db.Transaction(func(tx *gorm.DB) error {
		return RevokeCreatorTx(ctx, tx, 10, "creator_access_changed", time.Now())
	}))
	_, err = service.Resolve(ctx, link.PublicID, link.Signature)
	require.ErrorIs(t, err, ErrExpired)
}

func TestAuthorizationAcceptanceRejectedRequestsNeverPrepareMedia(t *testing.T) {
	for _, scenario := range []string{"disabled", "expired", "creator-disabled", "creator-permission-lost", "share-store-failed", "role-store-failed"} {
		t.Run(scenario, func(t *testing.T) {
			var creatorAllowed atomic.Bool
			creatorAllowed.Store(true)
			service, channel := authorizationAcceptanceFixture(t, func(context.Context, uint, string, string) (bool, error) {
				return creatorAllowed.Load(), nil
			})
			ctx := context.Background()
			link, err := service.SetEnabled(ctx, channel.ID, 7, true, 0)
			require.NoError(t, err)
			var prepares atomic.Int32
			service.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
				prepares.Add(1)
				return PlaybackSource{StreamID: "authorized-source", Generation: 1, HTTPFLVURL: "http://media.invalid/live.flv", HLSURL: "http://media.invalid/hls.m3u8"}, nil
			}))
			// Missing/tampered credentials fail before any media preparation, while
			// a valid request provides a positive control for the counter.
			_, err = service.CreateViewer(ctx, "unknown-public-id", link.Signature)
			require.ErrorIs(t, err, ErrNotFound)
			_, err = service.CreateViewer(ctx, link.PublicID, link.Signature+"tampered")
			require.ErrorIs(t, err, ErrInvalidCredential)
			require.Zero(t, prepares.Load())
			viewer, err := service.CreateViewer(ctx, link.PublicID, link.Signature)
			require.NoError(t, err)
			require.EqualValues(t, 1, prepares.Load())
			expectedStatus := http.StatusGone
			switch scenario {
			case "disabled":
				_, err = service.SetEnabled(ctx, channel.ID, 7, false, link.Revision)
				require.NoError(t, err)
			case "expired":
				require.NoError(t, service.db.Model(&gbmodels.GbChannelVideoShare{}).Where("channel_pk = ?", channel.ID).
					Update("expires_at", time.Now().Add(-time.Second)).Error)
			case "creator-disabled":
				require.NoError(t, service.db.Model(&basemodels.User{}).Where("id = ?", 7).Update("status", 0).Error)
			case "creator-permission-lost":
				creatorAllowed.Store(false)
			case "share-store-failed":
				require.NoError(t, service.db.Migrator().DropTable(&gbmodels.GbChannelVideoShare{}))
				expectedStatus = http.StatusServiceUnavailable
			case "role-store-failed":
				require.NoError(t, service.db.Migrator().DropTable(&basemodels.SysRole{}))
				expectedStatus = http.StatusServiceUnavailable
			}
			_, err = service.CreateViewer(ctx, link.PublicID, link.Signature)
			require.Error(t, err)
			for _, relay := range []func(context.Context, http.ResponseWriter){
				func(ctx context.Context, w http.ResponseWriter) { service.RelayHTTPFLV(ctx, w, viewer.Token) },
				func(ctx context.Context, w http.ResponseWriter) { service.RelayHLS(ctx, w, viewer.Token, "") },
			} {
				response := httptest.NewRecorder()
				relay(ctx, response)
				require.Equal(t, expectedStatus, response.Code)
				require.NotContains(t, response.Body.String(), "media.invalid")
				require.NotContains(t, response.Body.String(), "no such table")
			}
			require.EqualValues(t, 1, prepares.Load(), "rejected create and relay requests must not reacquire source leases or mint media authorization")
		})
	}
}
