package controllers_test

import (
	"context"
	"io"
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

	uplinkCalls int
	uplink      *talk.UplinkTarget
	uplinkErr   error
}

func (f *fakeTalkSessionService) Create(_ context.Context, request talk.CreateRequest) (*talk.CreateResult, error) {
	f.createCalls++
	f.request = request
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &talk.CreateResult{
		SessionID: "talk-session", Mode: request.Mode, State: "reserved",
		Uplink: talk.UplinkDescriptor{
			Protocol: "whip", Path: "/talk-sessions/talk-session/uplink",
			ContentType: "application/sdp", ICEServers: []talk.ICEServer{{URLs: []string{"stun:media.example.test:3478"}}},
		},
	}, nil
}

func (f *fakeTalkSessionService) PrepareUplink(context.Context, string) (*talk.UplinkTarget, error) {
	f.uplinkCalls++
	if f.uplinkErr != nil {
		return nil, f.uplinkErr
	}
	return f.uplink, nil
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
	router.POST("/talk-sessions/:sessionId/uplink", controller.Uplink)
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

func TestTalkControllerCreateReturnsPlatformUplinkEntry(t *testing.T) {
	service := &fakeTalkSessionService{}
	router, _, ownID, _ := newTalkControllerRouter(t, service)
	path := "/channel/" + strconv.FormatUint(uint64(ownID), 10) + "/talk-sessions"
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"mode":"talk"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	data, ok := unmarshal(t, recorder)["data"].(map[string]any)
	require.True(t, ok)
	uplink, ok := data["uplink"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "whip", uplink["protocol"])
	require.Equal(t, "application/sdp", uplink["contentType"])
	// 上行入口指向平台自己:host 取自请求、路径取自注册模式串,
	// 与网关 / 端口无关 —— 平台不得硬编码对外端口。
	require.Equal(t, "http://example.com/talk-sessions/talk-session/uplink", uplink["url"])

	// 信令契约里不得出现实现细节。ICE 服务器是例外:媒体面是浏览器直连媒体节点,
	// 这个地址必须给出去,否则跨网段根本不可用。
	body := recorder.Body.String()
	for _, leak := range []string{"18443", "token", "sourceStream", "recvStream", "ssrc", "nodeId", "nodeName"} {
		require.NotContains(t, body, leak)
	}
	require.Contains(t, body, "stun:media.example.test:3478")
}

func TestTalkControllerUplinkProxiesOfferAndHidesUpstreamLocation(t *testing.T) {
	var gotOffer, gotContentType string
	// 用 TLS 服务端:自签证书正好验证转发客户端确实跳过了证书校验,
	// 浏览器不再需要为媒体的自签证书做任何例外。
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		gotOffer = string(raw)
		w.Header().Set("Content-Type", "application/sdp")
		w.Header().Set("Location", "https://10.0.0.9:18443/index/api/whip/resource-1")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("v=0\r\nanswer"))
	}))
	defer upstream.Close()

	service := &fakeTalkSessionService{uplink: &talk.UplinkTarget{
		URL: upstream.URL + "/index/api/whip?app=talk&stream=s&token=secret", ContentType: "application/sdp",
	}}
	router, _, _, _ := newTalkControllerRouter(t, service)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/talk-sessions/talk-session/uplink", strings.NewReader("v=0\r\noffer"))
	request.Header.Set("Content-Type", "application/sdp")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusCreated, recorder.Code)
	require.Equal(t, "v=0\r\noffer", gotOffer)
	require.Equal(t, "application/sdp", gotContentType)
	require.Equal(t, "v=0\r\nanswer", recorder.Body.String())
	// 上游的媒体节点地址不得回到浏览器。
	require.Equal(t, "http://example.com/talk-sessions/talk-session", recorder.Header().Get("Location"))
	require.NotContains(t, recorder.Body.String(), "10.0.0.9")
}

// 对外地址必须跟着挂载位置走:挂在 /api/gb28181/device-mgmt 下,拼出来的地址
// 就带上这段前缀 —— 而不是在任何地方硬编码出来的。
func TestTalkControllerUplinkLocationFollowsMountedPrefix(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://10.0.0.9:18443/index/api/whip/resource-9")
		w.WriteHeader(http.StatusCreated)
	}))
	defer upstream.Close()

	service := &fakeTalkSessionService{uplink: &talk.UplinkTarget{URL: upstream.URL, ContentType: "application/sdp"}}
	controller := gbcontrollers.NewTalkController(service)
	controller.SetDB(func() *gorm.DB { return newScopedDeviceDB(t) })
	app.Response = response.NewResponseHandler()
	engine := gin.New()
	engine.Use(gin.Recovery(), withClaims(100))
	engine.Group("/api/gb28181/device-mgmt").POST(gbcontrollers.TalkUplinkRoute, controller.Uplink)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/gb28181/device-mgmt/talk-sessions/s-1/uplink", strings.NewReader("v=0"))
	request.Header.Set("Content-Type", "application/sdp")
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusCreated, recorder.Code)
	require.Equal(t, "http://example.com/api/gb28181/device-mgmt/talk-sessions/s-1", recorder.Header().Get("Location"))
}

func TestTalkControllerUplinkMapsPreparationFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"会话不存在", talk.ErrTalkSessionNotFound, http.StatusNotFound},
		{"会话已过期", talk.ErrTalkSessionExpired, http.StatusGone},
		{"已开始发布", talk.ErrUplinkNotReserved, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := &fakeTalkSessionService{uplinkErr: tc.err}
			router, _, _, _ := newTalkControllerRouter(t, service)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/talk-sessions/talk-session/uplink", strings.NewReader("v=0"))
			request.Header.Set("Content-Type", "application/sdp")
			router.ServeHTTP(recorder, request)
			require.Equal(t, tc.want, recorder.Code)
		})
	}
}
