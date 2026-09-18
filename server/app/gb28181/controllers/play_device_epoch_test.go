package controllers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

func TestOpenAPIBackendPermissionQueryCapturesDeviceEpoch(t *testing.T) {
	for _, suffix := range []string{"", "/authorization"} {
		t.Run(suffix, func(t *testing.T) {
			service := &fixedAuthorizationControllerService{}
			router := newFixedAuthorizationControllerRouter(t, 100, service)
			db := app.DB()
			service.authorize = func(req play.AuthorizedRequest) error {
				require.EqualValues(t, 1, req.DeviceEpoch)
				// Transfer after the permission query but before token issuance.
				require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
					if err := tx.Table("gb_device").Where("device_id = ?", req.DeviceID).
						Updates(map[string]any{"owner_dept_id": 20, "access_epoch": 2}).Error; err != nil {
						return err
					}
					return tx.Table("gb_channel").Where("device_id = ?", req.DeviceID).Update("owner_dept_id", 20).Error
				}))
				return playauth.NewDeviceSecurityStore(db).AuthorizeEpoch(context.Background(), req.DeviceID, req.DeviceEpoch)
			}
			path := "/api/gb28181/play/34020000002000000010/37011200001310000010" + suffix
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
			require.EqualValues(t, 1, unmarshal(t, w)["code"], w.Body.String())
			require.EqualValues(t, 1, service.request.DeviceEpoch, "must not upgrade the authorized snapshot")
			service.request = play.AuthorizedRequest{}
			w = httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
			require.EqualValues(t, 1, unmarshal(t, w)["code"])
			require.Zero(t, service.request.DeviceEpoch, "old owner must not reach playback after transfer")
		})
	}
}

func TestOpenAPIBackendPermissionEpochPreservesPersonnelSharing(t *testing.T) {
	service := &fixedAuthorizationControllerService{}
	router := newFixedAuthorizationControllerRouter(t, 100, service)
	db := app.DB()
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch = 7").Error)
	var other gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", "34020000002000000020").First(&other).Error)
	require.NoError(t, db.Create(&gbmodels.GbDeviceGrant{DeviceID: other.ID, TargetType: gbmodels.GrantTargetTypeUser, TargetID: 100}).Error)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/gb28181/play/34020000002000000020/37011200001310000020/authorization", nil))
	require.EqualValues(t, 0, unmarshal(t, w)["code"], w.Body.String())
	require.EqualValues(t, 7, service.request.DeviceEpoch)
}

func TestOpenAPIBackendPermissionEpochFailsClosed(t *testing.T) {
	for _, state := range []string{"missing-column", "null", "zero", "mismatched-owner", "deleted-root", "deleted-channel"} {
		t.Run(state, func(t *testing.T) {
			service := &fixedAuthorizationControllerService{}
			router := newFixedAuthorizationControllerRouter(t, 100, service)
			db := app.DB()
			query := db.Table("gb_device").Where("device_id = ?", "34020000002000000010")
			switch state {
			case "missing-column":
				require.NoError(t, db.Exec("ALTER TABLE gb_device DROP COLUMN access_epoch").Error)
			case "null":
				require.NoError(t, query.Update("access_epoch", nil).Error)
			case "zero":
				require.NoError(t, query.Update("access_epoch", 0).Error)
			case "mismatched-owner":
				require.NoError(t, query.Update("owner_dept_id", 20).Error)
			case "deleted-root":
				require.NoError(t, query.Update("deleted_at", time.Now()).Error)
			case "deleted-channel":
				require.NoError(t, db.Table("gb_channel").Where("device_id = ?", "34020000002000000010").Update("deleted_at", time.Now()).Error)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/gb28181/play/34020000002000000010/37011200001310000010/authorization", nil))
			require.EqualValues(t, 1, unmarshal(t, w)["code"], w.Body.String())
			require.Zero(t, service.calls.Load())
		})
	}
}
