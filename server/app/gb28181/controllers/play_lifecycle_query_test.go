package controllers_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbdashboard "uvplatform.cn/uvp-gb28181/app/gb28181/dashboard"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
)

func TestPlayLifecycleQueryDefaultsToTenAndEnforcesDeviceScope(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	seedScopedDeviceRows(t, db)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPlayAttempt{}, &gbmodels.GbPlayLifecycleEvent{}))
	base := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 12; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		require.NoError(t, db.Create(&gbmodels.GbPlayAttempt{
			CorrelationID: fmt.Sprintf("visible-%02d", i), DeviceCode: "34020000002000000010",
			ChannelCode: "37011200001310000010", Outcome: "success", CurrentStage: string(play.StageMedia),
			MediaState: string(play.MediaStateReady), ClientState: string(play.ClientStateUnknown),
			LifecycleState: string(play.LifecycleStateInProgress), StartedAt: at, LastEventAt: &at,
		}).Error)
	}
	hiddenAt := base.Add(time.Hour)
	require.NoError(t, db.Create(&gbmodels.GbPlayAttempt{
		CorrelationID: "hidden-life", DeviceCode: "34020000002000000020", ChannelCode: "37011200001310000020",
		Outcome: "success", LifecycleState: string(play.LifecycleStateInProgress), StartedAt: hiddenAt, LastEventAt: &hiddenAt,
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbPlayLifecycleEvent{
		EventID: "hidden-event", LifecycleID: "hidden-life", Sequence: 1, EventAt: hiddenAt,
		Stage: string(play.StageMedia), EventName: play.EventMediaReady, FactState: string(play.FactConfirmed),
		Source: string(play.SourcePlayService), DeviceCode: "34020000002000000020", ChannelCode: "37011200001310000020",
		MetadataJSON: []byte(`{"internal":"secret"}`),
	}).Error)

	controller := gbcontrollers.NewPlayController(nil, gbcontrollers.WithPlayLifecycleQueryStore(gbdashboard.NewPlayLifecycleStore(db)))
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(100))
	router.GET("/api/gb28181/play/lifecycles", controller.LifecycleList)
	router.GET("/api/gb28181/play/lifecycles/:lifecycleId", controller.LifecycleDetail)

	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/gb28181/play/lifecycles", nil))
	data := unmarshal(t, listResponse)["data"].(map[string]any)
	require.EqualValues(t, 12, data["total"])
	require.EqualValues(t, 10, data["pageSize"])
	require.Len(t, data["list"], 10)

	detailResponse := httptest.NewRecorder()
	router.ServeHTTP(detailResponse, httptest.NewRequest(http.MethodGet, "/api/gb28181/play/lifecycles/hidden-life", nil))
	require.EqualValues(t, 1, unmarshal(t, detailResponse)["code"])
	require.False(t, strings.Contains(detailResponse.Body.String(), "internal"))
	require.False(t, strings.Contains(detailResponse.Body.String(), "metadata"))
}
