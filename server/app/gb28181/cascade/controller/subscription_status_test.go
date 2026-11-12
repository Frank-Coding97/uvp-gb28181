package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/service"
)

type subscriptionRuntimeFake struct{ publishGroupRuntimeFake }

func (r *subscriptionRuntimeFake) Subscriptions(p model.GbCascadePlatform) service.PlatformSubscriptions {
	result := service.NewPlatformSubscriptions("inactive")
	if p.ID == 1 {
		now := time.Date(2026, 10, 19, 2, 0, 0, 0, time.UTC)
		result.Catalog.State = "active"
		result.Catalog.Sessions = []service.SubscriptionSession{{Target: p.LocalDeviceID, Event: "Catalog;id=7", SubscribedAt: now, RenewedAt: now, ExpiresAt: now.Add(time.Hour), LastNotifyStatus: "pending"}}
	}
	return result
}

func TestManagementHTTPIncludesUpstreamSubscriptionsInListAndDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	defer sqlDB.Close()
	require.NoError(t, db.AutoMigrate(&model.GbCascadePlatform{}))
	repo := repository.NewGormRepository(db)
	for _, localID := range []string{"34020000002000000001", "34020000002000000002"} {
		require.NoError(t, repo.CreatePlatform(context.Background(), &model.GbCascadePlatform{Name: localID, LocalDeviceID: localID, Enabled: true, SecretCiphertext: []byte("secret-sentinel")}))
	}
	for _, runtime := range []service.ManagementRuntime{&subscriptionRuntimeFake{}, nil, &publishGroupRuntimeFake{}} {
		ctrl := NewManagementController(service.NewManagementService(repo, nil, runtime, nil), nil)
		router := gin.New()
		router.GET("/platforms", ctrl.List)
		router.GET("/platforms/:id", ctrl.Get)
		for _, path := range []string{"/platforms", "/platforms/1", "/platforms/2"} {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusOK, recorder.Code)
			require.NotContains(t, recorder.Body.String(), "secret-sentinel")
			var views []service.PlatformView
			if path == "/platforms" {
				var result struct{ List []service.PlatformView }
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
				views = result.List
				require.Len(t, views, 2)
			} else {
				var view service.PlatformView
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &view))
				views = []service.PlatformView{view}
			}
			for _, view := range views {
				status := view.Subscriptions
				require.Equal(t, "unsupported", status.MobilePosition.State)
				require.Equal(t, "unsupported", status.PTZPosition.State)
				require.Equal(t, "unsupported", status.Alarm.State)
				_, available := runtime.(*subscriptionRuntimeFake)
				if !available {
					require.Equal(t, "unavailable", status.Catalog.State)
				} else if view.ID == 1 {
					require.Equal(t, "active", status.Catalog.State)
					require.Len(t, status.Catalog.Sessions, 1)
					require.Equal(t, view.LocalDeviceID, status.Catalog.Sessions[0].Target)
				} else {
					require.Equal(t, "inactive", status.Catalog.State)
					require.Empty(t, status.Catalog.Sessions)
				}
			}
		}
	}
}
