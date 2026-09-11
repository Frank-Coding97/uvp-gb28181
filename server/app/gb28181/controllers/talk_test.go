package controllers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/talk"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type fakeTalkSessionService struct {
	createCalls int
	request     talk.CreateRequest
	createErr   error
	session     *gbmodels.GbTalkSession
}

func (f *fakeTalkSessionService) Create(_ context.Context, request talk.CreateRequest) (*talk.CreateResult, error) {
	f.createCalls++
	f.request = request
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &talk.CreateResult{SessionID: "talk-session", Mode: request.Mode, State: "reserved"}, nil
}

func (f *fakeTalkSessionService) Get(context.Context, string) (*gbmodels.GbTalkSession, error) {
	return f.session, nil
}

func (f *fakeTalkSessionService) Renew(context.Context, string) (*gbmodels.GbTalkSession, error) {
	return f.session, nil
}

func (f *fakeTalkSessionService) Cleanup(context.Context, string, gbmodels.TalkSessionState, string) error {
	return nil
}

func newTalkControllerRouter(t *testing.T, service gbcontrollers.TalkSessionService) (*gin.Engine, *gorm.DB, uint, uint) {
	t.Helper()
	db := newScopedDeviceDB(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbTalkSession{}))
	seedDeptScopedUser(t, db, 100, 10)
	deviceOwn := &gbmodels.GbDevice{DeviceID: "device-own", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 10}
	deviceHidden := &gbmodels.GbDevice{DeviceID: "device-hidden", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 20}
	require.NoError(t, db.Create(deviceOwn).Error)
	require.NoError(t, db.Create(deviceHidden).Error)
	caps := `{"talk":true}`
	channelOwn := &gbmodels.GbChannel{DeviceID: deviceOwn.DeviceID, ChannelID: "channel-own", Status: gbmodels.ChannelStatusOnline, OwnerDeptID: 10, Capabilities: &caps}
	channelHidden := &gbmodels.GbChannel{DeviceID: deviceHidden.DeviceID, ChannelID: "channel-hidden", Status: gbmodels.ChannelStatusOnline, OwnerDeptID: 20, Capabilities: &caps}
	require.NoError(t, db.Create(channelOwn).Error)
	require.NoError(t, db.Create(channelHidden).Error)
	controller := gbcontrollers.NewTalkController(service)
	controller.SetDB(func() *gorm.DB { return db })
	app.Response = response.NewResponseHandler()
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(100))
	router.POST("/channel/:id/talk-sessions", controller.Create)
	router.GET("/channel/:id/talk-sessions/:sessionId", controller.Get)
	router.DELETE("/channel/:id/talk-sessions/:sessionId", controller.Delete)
	return router, db, channelOwn.ID, channelHidden.ID
}

func TestTalkControllerCreateScopesChannelAndStoresActor(t *testing.T) {
	service := &fakeTalkSessionService{}
	router, _, ownID, hiddenID := newTalkControllerRouter(t, service)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/channel/"+strconv.FormatUint(uint64(ownID), 10)+"/talk-sessions", strings.NewReader(`{"mode":"talk"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, service.createCalls)
	require.EqualValues(t, 100, service.request.ActorID)
	require.EqualValues(t, 10, service.request.ActorDeptID)
	require.Equal(t, gbmodels.TalkSessionModeTalk, service.request.Mode)

	hidden := httptest.NewRecorder()
	hiddenRequest := httptest.NewRequest(http.MethodPost, "/channel/"+strconv.FormatUint(uint64(hiddenID), 10)+"/talk-sessions", strings.NewReader(`{"mode":"talk"}`))
	hiddenRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(hidden, hiddenRequest)
	require.Equal(t, 1, service.createCalls)
}

func TestTalkControllerForwardsTalkModeAndReturnsIt(t *testing.T) {
	service := &fakeTalkSessionService{}
	router, _, ownID, _ := newTalkControllerRouter(t, service)
	path := "/channel/" + strconv.FormatUint(uint64(ownID), 10) + "/talk-sessions"
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"mode":"talk"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, gbmodels.TalkSessionModeTalk, service.request.Mode)
	data, ok := unmarshal(t, response)["data"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "talk", data["mode"])
}

func TestTalkControllerCreatesBroadcastSession(t *testing.T) {
	service := &fakeTalkSessionService{}
	router, _, ownID, _ := newTalkControllerRouter(t, service)
	path := "/channel/" + strconv.FormatUint(uint64(ownID), 10) + "/talk-sessions"
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"mode":"broadcast"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, service.createCalls)
	require.Equal(t, gbmodels.TalkSessionModeBroadcast, service.request.Mode)
	require.Contains(t, response.Body.String(), `"mode":"broadcast"`)
}

func TestTalkControllerRejectsMissingOrInvalidMode(t *testing.T) {
	service := &fakeTalkSessionService{}
	router, _, ownID, _ := newTalkControllerRouter(t, service)
	path := "/channel/" + strconv.FormatUint(uint64(ownID), 10) + "/talk-sessions"

	for _, body := range []string{"{}", `{"mode":"unsupported"}`} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusBadRequest, response.Code)
	}
	require.Zero(t, service.createCalls)
}

func TestTalkControllerRejectsSessionFromAnotherChannel(t *testing.T) {
	service := &fakeTalkSessionService{session: &gbmodels.GbTalkSession{SessionID: "other", ChannelID: 999, State: gbmodels.TalkSessionActive, ExpiresAt: time.Now()}}
	router, _, ownID, _ := newTalkControllerRouter(t, service)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/channel/"+strconv.FormatUint(uint64(ownID), 10)+"/talk-sessions/other", nil))
	require.NotEqual(t, http.StatusInternalServerError, response.Code)
	require.EqualValues(t, 1, unmarshal(t, response)["code"])
}

func TestTalkControllerGetReturnsPersistedMode(t *testing.T) {
	service := &fakeTalkSessionService{}
	router, _, ownID, _ := newTalkControllerRouter(t, service)
	service.session = &gbmodels.GbTalkSession{
		SessionID: "broadcast-session", ChannelID: ownID, Mode: gbmodels.TalkSessionModeBroadcast,
		State: gbmodels.TalkSessionReserved, ExpiresAt: time.Now(),
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/channel/"+strconv.FormatUint(uint64(ownID), 10)+"/talk-sessions/broadcast-session", nil))

	require.Equal(t, http.StatusOK, response.Code)
	data, ok := unmarshal(t, response)["data"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "broadcast", data["mode"])
}

func TestTalkControllerReturns503WhenUnconfigured(t *testing.T) {
	controller := gbcontrollers.NewTalkController(nil)
	router := gin.New()
	router.POST("/channel/:id/talk-sessions", controller.Create)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/channel/1/talk-sessions", nil))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
}
