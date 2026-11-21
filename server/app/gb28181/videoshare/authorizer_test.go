package videoshare

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/authoritytx"
)

func TestDatabaseAuthorizerChecksCurrentPermissionsAndVisibility(t *testing.T) {
	db, channel := authorizerFixture(t, 10)
	var checked []string
	authorizer := NewDatabaseAuthorizer(func(_ context.Context, _ uint, path, method string) (bool, error) {
		checked = append(checked, method+" "+path)
		return true, nil
	})
	snapshot, err := authorizer(context.Background(), db, 7, channel.ID)
	require.NoError(t, err)
	require.Equal(t, uint64(9), snapshot.DeviceEpoch)
	require.Equal(t, []string{
		"PUT /api/gb28181/channel-shares/1",
		"POST /api/gb28181/play/34020000002000000001/34020000001320000001",
	}, checked)

	authorizer = NewDatabaseAuthorizer(func(_ context.Context, _ uint, _ string, method string) (bool, error) {
		return method != "POST", nil
	})
	_, err = authorizer(context.Background(), db, 7, channel.ID)
	require.ErrorIs(t, err, ErrExpired)

	authorizer = NewDatabaseAuthorizer(func(context.Context, uint, string, string) (bool, error) {
		return false, errors.New("permission database unavailable")
	})
	_, err = authorizer(context.Background(), db, 7, channel.ID)
	require.ErrorContains(t, err, "permission database unavailable")
}

func TestDatabaseAuthorizerRejectsInvisibleAndTransferredDevices(t *testing.T) {
	db, channel := authorizerFixture(t, 20)
	authorizer := NewDatabaseAuthorizer(func(context.Context, uint, string, string) (bool, error) { return true, nil })
	_, err := authorizer(context.Background(), db, 7, channel.ID)
	require.Error(t, err, "channel is owned by a department outside the creator's data scope")

	db, channel = authorizerFixture(t, 10)
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch = 10 WHERE id = ?", channel.ID).Error)
	_, err = authorizer(context.Background(), db, 7, channel.ID)
	require.NoError(t, err)
}

func TestDatabaseAuthorizerMapsUnavailableTargetsToExpired(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*testing.T, *gorm.DB, gbmodels.GbChannel)
	}{
		{"disabled creator", func(t *testing.T, db *gorm.DB, _ gbmodels.GbChannel) {
			require.NoError(t, db.Model(&basemodels.User{}).Where("id = ?", 7).Update("status", 0).Error)
		}},
		{"deleted creator", func(t *testing.T, db *gorm.DB, _ gbmodels.GbChannel) {
			require.NoError(t, db.Delete(&basemodels.User{}, 7).Error)
		}},
		{"deleted channel", func(t *testing.T, db *gorm.DB, channel gbmodels.GbChannel) {
			require.NoError(t, db.Delete(&gbmodels.GbChannel{}, channel.ID).Error)
		}},
		{"deleted device", func(t *testing.T, db *gorm.DB, channel gbmodels.GbChannel) {
			require.NoError(t, db.Where("device_id = ?", channel.DeviceID).Delete(&gbmodels.GbDevice{}).Error)
		}},
		{"invisible channel", func(t *testing.T, db *gorm.DB, channel gbmodels.GbChannel) {
			require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("id = ?", channel.ID).Update("owner_dept_id", 20).Error)
			require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", channel.DeviceID).Update("owner_dept_id", 20).Error)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, channel := authorizerFixture(t, 10)
			test.change(t, db, channel)
			authorizer := NewDatabaseAuthorizer(func(context.Context, uint, string, string) (bool, error) {
				t.Fatal("unavailable target must be rejected before permission evaluation")
				return false, nil
			})
			_, err := authorizer(context.Background(), db, 7, channel.ID)
			require.ErrorIs(t, err, ErrExpired)
		})
	}
}

func TestDatabaseAuthorizerPreservesDatabaseFailures(t *testing.T) {
	db, channel := authorizerFixture(t, 10)
	failure := errors.New("authority database unavailable")
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("video_share_authority_failure", func(query *gorm.DB) {
		query.AddError(failure)
	}))
	authorizer := NewDatabaseAuthorizer(func(context.Context, uint, string, string) (bool, error) { return true, nil })
	_, err := authorizer(context.Background(), db, 7, channel.ID)
	require.ErrorIs(t, err, failure)
	require.NotErrorIs(t, err, ErrExpired)
}

func TestDatabaseAuthorizerIgnoresDisabledFullAccessRole(t *testing.T) {
	db, channel := authorizerFixture(t, 20)
	role := basemodels.SysRole{Name: "disabled-full-access", Status: 0, DataScope: 1}
	require.NoError(t, db.Create(&role).Error)
	require.NoError(t, db.Create(&basemodels.SysUserRole{UserID: 7, RoleID: role.ID}).Error)
	authorizer := NewDatabaseAuthorizer(func(context.Context, uint, string, string) (bool, error) { return true, nil })
	_, err := authorizer(context.Background(), db, 7, channel.ID)
	require.ErrorIs(t, err, ErrExpired, "an active own-department role cannot borrow the disabled full-access role")
	activeFull := basemodels.SysRole{Name: "active-full-access", Status: 1, DataScope: 1}
	require.NoError(t, db.Create(&activeFull).Error)
	require.NoError(t, db.Create(&basemodels.SysUserRole{UserID: 7, RoleID: activeFull.ID}).Error)
	_, err = authorizer(context.Background(), db, 7, channel.ID)
	require.NoError(t, err, "an active full-access role still provides visibility")
}

func TestDatabaseAuthorizerWithoutRolesRequiresExplicitDeviceGrant(t *testing.T) {
	for _, targetType := range []string{gbmodels.GrantTargetTypeUser, gbmodels.GrantTargetTypeDept} {
		t.Run(targetType, func(t *testing.T) {
			db, channel := authorizerFixture(t, 10)
			require.NoError(t, db.Where("user_id = ?", 7).Delete(&basemodels.SysUserRole{}).Error)
			checks := 0
			authorizer := NewDatabaseAuthorizer(func(context.Context, uint, string, string) (bool, error) { checks++; return true, nil })
			_, err := authorizer(context.Background(), db, 7, channel.ID)
			require.ErrorIs(t, err, ErrExpired, "no role must not silently grant visibility to the user's department")
			require.Zero(t, checks)
			var device gbmodels.GbDevice
			require.NoError(t, db.Where("device_id = ?", channel.DeviceID).Take(&device).Error)
			require.NoError(t, db.Model(&device).Update("owner_dept_id", 20).Error)
			require.NoError(t, db.Model(&channel).Update("owner_dept_id", 20).Error)
			targetID := uint(7)
			if targetType == gbmodels.GrantTargetTypeDept {
				targetID = 10
			}
			require.NoError(t, db.Create(&gbmodels.GbDeviceGrant{DeviceID: device.ID, TargetType: targetType, TargetID: targetID}).Error)
			_, err = authorizer(context.Background(), db, 7, channel.ID)
			require.NoError(t, err)
			require.Equal(t, 2, checks, "explicit grant visibility still requires share and playback permissions")
		})
	}
}

func TestDatabaseAuthorizerRoleLookupFailureReturnsServiceUnavailable(t *testing.T) {
	db, channel := authorizerFixture(t, 10)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbChannelVideoShare{}, &authoritytx.Gate{}, &basemodels.SysOperationLog{}))
	require.NoError(t, db.Create(&authoritytx.Gate{ID: 1}).Error)
	signer, err := NewSigner(make([]byte, keyBytes))
	require.NoError(t, err)
	authorizer := NewDatabaseAuthorizer(func(context.Context, uint, string, string) (bool, error) { return true, nil })
	service, err := NewService(db, signer, func() time.Duration { return 5 * time.Minute }, authorizer)
	require.NoError(t, err)
	link, err := service.SetEnabled(context.Background(), channel.ID, 7, true, 0)
	require.NoError(t, err)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("FLV")) }))
	defer upstream.Close()
	service.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		return PlaybackSource{StreamID: "test", Generation: 1, HTTPFLVURL: upstream.URL}, nil
	}))
	viewer, err := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	defer service.CloseViewer(viewer.Token)
	failure := errors.New("role store unavailable")
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("share_role_failure", func(query *gorm.DB) {
		if query.Statement.Table == "sys_role" {
			query.AddError(failure)
		}
	}))
	_, err = authorizer(context.Background(), db, 7, channel.ID)
	require.ErrorIs(t, err, failure)
	require.NotErrorIs(t, err, ErrExpired)
	request := httptest.NewRequest(http.MethodGet, viewer.HTTPFLVPath, nil)
	recorder := httptest.NewRecorder()
	service.RelayHTTPFLV(request.Context(), recorder, viewer.Token)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.NotContains(t, recorder.Body.String(), failure.Error())
}

type shareVisibilityConfig struct{ app.YmlConfigInterf }

func (shareVisibilityConfig) GetUintSlice(key string) []uint {
	if key == "server.notcheckuser" {
		return []uint{7}
	}
	return nil
}

func TestDatabaseAuthorizerNotCheckUserStillRequiresSharePermission(t *testing.T) {
	db, channel := authorizerFixture(t, 20)
	previous := app.ConfigYml
	app.ConfigYml = shareVisibilityConfig{}
	defer func() { app.ConfigYml = previous }()
	authorizer := NewDatabaseAuthorizer(func(context.Context, uint, string, string) (bool, error) { return true, nil })
	_, err := authorizer(context.Background(), db, 7, channel.ID)
	require.NoError(t, err)
	authorizer = NewDatabaseAuthorizer(func(context.Context, uint, string, string) (bool, error) { return false, nil })
	_, err = authorizer(context.Background(), db, 7, channel.ID)
	require.ErrorIs(t, err, ErrExpired)
}

func authorizerFixture(t *testing.T, deviceDepartment uint) (*gorm.DB, gbmodels.GbChannel) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&basemodels.User{}, &basemodels.SysRole{}, &basemodels.SysUserRole{}, &basemodels.SysDepartment{}, &gbmodels.GbDevice{}, &gbmodels.GbDeviceGrant{}, &gbmodels.GbChannel{}))
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch INTEGER NOT NULL DEFAULT 1").Error)
	require.NoError(t, db.Create(&basemodels.User{BaseModel: basemodels.BaseModel{ID: 7}, Username: "creator", DeptID: 10, Status: 1}).Error)
	role := basemodels.SysRole{Name: "active-own-department", Status: 1, DataScope: 3}
	require.NoError(t, db.Create(&role).Error)
	require.NoError(t, db.Create(&basemodels.SysUserRole{UserID: 7, RoleID: role.ID}).Error)
	require.NoError(t, db.Create(&basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 10}, Name: "Creator department"}).Error)
	device := gbmodels.GbDevice{DeviceID: "34020000002000000001", OwnerDeptID: deviceDepartment}
	require.NoError(t, db.Create(&device).Error)
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch = 9 WHERE id = ?", device.ID).Error)
	channel := gbmodels.GbChannel{DeviceID: device.DeviceID, ChannelID: "34020000001320000001", OwnerDeptID: deviceDepartment}
	require.NoError(t, db.Create(&channel).Error)
	return db, channel
}
