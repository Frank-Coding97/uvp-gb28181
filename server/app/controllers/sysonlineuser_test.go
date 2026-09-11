package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type fakeOnlineUserService struct {
	filter service.OnlineSessionFilter
	list   []service.OnlineSessionView
	total  int64
	result *service.ForceLogoutResult
	err    error
}

func (f *fakeOnlineUserService) ListOnline(_ context.Context, filter service.OnlineSessionFilter) ([]service.OnlineSessionView, int64, error) {
	f.filter = filter
	return f.list, f.total, f.err
}

func (f *fakeOnlineUserService) ForceLogout(_ context.Context, _, _ string, _ uint) (*service.ForceLogoutResult, error) {
	return f.result, f.err
}

func onlineUserTestRouter(controller *SysOnlineUserController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	app.Response = response.NewResponseHandler()
	app.ZapLog = zap.NewNop()
	router := gin.New()
	router.Use(gin.Recovery(), func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{
			ClaimsUser:    app.ClaimsUser{UserID: 9, Username: "operator"},
			SessionClaims: app.SessionClaims{SID: "current-session"},
		})
	})
	router.GET("/api/sysOnlineUser/list", controller.List)
	router.POST("/api/sysOnlineUser/forceLogout", controller.ForceLogout)
	router.POST("/api/users/session/heartbeat", controller.Heartbeat)
	return router
}

func TestOnlineUserListContractDoesNotExposeCredentials(t *testing.T) {
	fake := &fakeOnlineUserService{
		list:  []service.OnlineSessionView{{SID: "sid-a", Username: "admin", Status: "active"}},
		total: 1,
	}
	router := onlineUserTestRouter(newSysOnlineUserControllerWithService(fake))
	req := httptest.NewRequest(http.MethodGet, "/api/sysOnlineUser/list?pageNum=2&pageSize=30&username=adm&departmentId=3&clientIp=10.0.0.1&status=active", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code)
	require.Equal(t, service.OnlineSessionFilter{PageNum: 2, PageSize: 30, Username: "adm", DepartmentID: 3, ClientIP: "10.0.0.1", Status: "active"}, fake.filter)
	body := strings.ToLower(res.Body.String())
	require.NotContains(t, body, "token")
	require.NotContains(t, body, "hash")
	require.NotContains(t, body, "jti")
	var responseBody struct {
		Data struct {
			CurrentSID string `json:"currentSid"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &responseBody))
	require.Equal(t, "current-session", responseBody.Data.CurrentSID)
}

func TestOnlineUserForceLogoutResponses(t *testing.T) {
	tests := []struct {
		name    string
		result  *service.ForceLogoutResult
		err     error
		status  int
		message string
	}{
		{name: "revoked", result: &service.ForceLogoutResult{Revoked: true, SID: "target-session"}, status: http.StatusOK, message: "强制下线成功"},
		{name: "already offline", result: &service.ForceLogoutResult{SID: "target-session"}, status: http.StatusOK, message: "会话已离线"},
		{name: "current session rejected", err: service.ErrCurrentSessionForceLogout, status: http.StatusBadRequest, message: "不能强退当前会话"},
		{name: "unknown session", err: service.ErrSessionUnavailable, status: http.StatusNotFound, message: "会话已离线"},
		{name: "store failure", err: service.ErrSessionStore, status: http.StatusServiceUnavailable, message: "强制下线失败"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeOnlineUserService{result: tc.result, err: tc.err}
			router := onlineUserTestRouter(newSysOnlineUserControllerWithService(fake))
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/api/sysOnlineUser/forceLogout", strings.NewReader(`{"sid":"target-session"}`)))
			require.Equal(t, tc.status, res.Code)
			var body map[string]any
			require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
			require.Equal(t, tc.message, body["message"])
		})
	}
}

func TestSessionHeartbeatReturnsMinimalResponse(t *testing.T) {
	router := onlineUserTestRouter(newSysOnlineUserControllerWithService(&fakeOnlineUserService{}))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/api/users/session/heartbeat", nil))
	require.Equal(t, http.StatusOK, res.Code)
	require.JSONEq(t, `{"code":0,"message":"","data":{"online":true}}`, res.Body.String())
}

func TestMaskSessionIDNeverReturnsCompleteValue(t *testing.T) {
	require.Equal(t, "***", maskSessionID("short"))
	require.Equal(t, "12345678...", maskSessionID("1234567890abcdef"))
	require.False(t, strings.Contains(maskSessionID("1234567890abcdef"), "90abcdef"))
}
