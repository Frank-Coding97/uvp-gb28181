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
	gbcontrollers "uvplatform.com/uvp-gb28181/app/gb28181/controllers"
	"uvplatform.com/uvp-gb28181/app/gb28181/videoshare"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
)

func videoShareListFixture(t *testing.T, actorID uint) (*gin.Engine, *gbcontrollers.DeviceMgmtController, uint, uint) {
	t.Helper()
	_, db := newDeviceMgmtRouter(t)
	_, online, offline := seedDevicesAndChannels(t, db)
	if actorID != 0 {
		require.NoError(t, db.Create(&basemodels.User{BaseModel: basemodels.BaseModel{ID: actorID}, Username: "list-operator", Status: 1, DeptID: 10}).Error)
		role := basemodels.SysRole{Name: "list-scope", Status: 1, DataScope: 1}
		require.NoError(t, db.Create(&role).Error)
		require.NoError(t, db.Create(&basemodels.SysUserRole{UserID: actorID, RoleID: role.ID}).Error)
	}
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	router := gin.New()
	if actorID != 0 {
		router.Use(withClaims(actorID))
	}
	router.GET("/channels", controller.ListChannels)
	return router, controller, online, offline
}

func getShareChannelList(t *testing.T, router *gin.Engine) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/channels?pageSize=2", nil))
	return recorder
}

func TestDeviceMgmtListChannelsAddsOnlyAuthorizedBatchShareState(t *testing.T) {
	router, controller, online, offline := videoShareListFixture(t, 7)
	now := time.Now().UTC().Truncate(time.Second)
	expiry := now.Add(time.Minute)
	calls := 0
	controller.SetVideoShareBatchStatus(func(ctx context.Context, ids []uint, actorID uint) (map[uint]videoshare.StatusSummary, error) {
		calls++
		require.Equal(t, uint(7), actorID)
		require.ElementsMatch(t, []uint{online, offline}, ids)
		deadline, ok := ctx.Deadline()
		require.True(t, ok, "optional status enrichment must have a short timeout")
		require.LessOrEqual(t, time.Until(deadline), 2*time.Second)
		return map[uint]videoshare.StatusSummary{online: {Enabled: true, Pending: false, Revision: 4, ExpiresAt: &expiry, ServerTime: now}}, nil
	})
	recorder := getShareChannelList(t, router)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, calls)
	data := unmarshal(t, recorder)["data"].(map[string]any)
	require.EqualValues(t, 2, data["total"])
	require.EqualValues(t, 1, data["page"])
	require.EqualValues(t, 2, data["pageSize"])
	rows := data["list"].([]any)
	require.Len(t, rows, 2)
	for _, raw := range rows {
		row := raw.(map[string]any)
		require.NotEmpty(t, row["channelId"])
		require.NotEmpty(t, row["name"])
		require.Contains(t, row, "status")
		if uint(row["id"].(float64)) == online {
			state := row["videoShare"].(map[string]any)
			require.Equal(t, true, state["enabled"])
			require.EqualValues(t, 4, state["revision"])
			require.Len(t, state, 5)
		} else {
			require.NotContains(t, row, "videoShare")
		}
	}
	for _, credential := range []string{"signature", "publicId", "generation"} {
		require.NotContains(t, recorder.Body.String(), credential)
	}
}

func TestDeviceMgmtListChannelsWithoutShareProviderPreservesResponse(t *testing.T) {
	router, _, _, _ := videoShareListFixture(t, 7)
	recorder := getShareChannelList(t, router)
	require.Equal(t, http.StatusOK, recorder.Code)
	rows := unmarshal(t, recorder)["data"].(map[string]any)["list"].([]any)
	for _, row := range rows {
		require.NotContains(t, row.(map[string]any), "videoShare")
	}
}

func TestDeviceMgmtListChannelsWithoutActorOmitsShareState(t *testing.T) {
	router, controller, _, _ := videoShareListFixture(t, 0)
	controller.SetVideoShareBatchStatus(func(context.Context, []uint, uint) (map[uint]videoshare.StatusSummary, error) {
		t.Fatal("missing identity must not query share status")
		return nil, nil
	})
	recorder := getShareChannelList(t, router)
	require.Equal(t, http.StatusOK, recorder.Code)
	rows := unmarshal(t, recorder)["data"].(map[string]any)["list"].([]any)
	for _, row := range rows {
		require.NotContains(t, row.(map[string]any), "videoShare")
	}
}

func TestDeviceMgmtListChannelsShareDependencyFailureIsUnavailable(t *testing.T) {
	router, controller, _, _ := videoShareListFixture(t, 7)
	controller.SetVideoShareBatchStatus(func(context.Context, []uint, uint) (map[uint]videoshare.StatusSummary, error) {
		return nil, errors.New("secret database connection detail")
	})
	recorder := getShareChannelList(t, router)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "secret database")
	require.NotContains(t, recorder.Body.String(), `"enabled":false`)
}
