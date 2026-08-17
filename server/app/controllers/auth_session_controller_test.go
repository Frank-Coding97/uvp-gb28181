package controllers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
	"uvplatform.cn/uvp-gb28181/app/utils/tokenhelper"
)

type fakeAuthSessionLifecycle struct {
	rotatePair *service.SessionTokenPair
	rotateSID  string
	revokeSID  string
}

func (f *fakeAuthSessionLifecycle) CreateLogin(context.Context, *models.User, service.LoginMetadata, app.TokenServiceInterface, time.Duration) (*service.SessionTokenPair, error) {
	return nil, nil
}
func (f *fakeAuthSessionLifecycle) RotateRefresh(_ context.Context, _ string, _ app.TokenServiceInterface) (*service.SessionTokenPair, error) {
	return f.rotatePair, nil
}
func (f *fakeAuthSessionLifecycle) Revoke(_ context.Context, sid, _ string, _ *uint) (bool, error) {
	f.revokeSID = sid
	return true, nil
}

func TestAuthControllerRefreshUsesSessionCASService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokens := &tokenhelper.TokenService{JWTSecret: "test_secret", TokenExpire: 3600, RefreshExpire: 86400}
	access, err := tokens.GenerateTokenForSession(&app.ClaimsUser{UserID: 7, Username: "admin"}, "sid-a", "access-jti")
	require.NoError(t, err)
	refresh, err := tokens.GenerateRefreshTokenForSessionUntil(7, "sid-a", "refresh-jti", time.Now().Add(time.Hour))
	require.NoError(t, err)
	fake := &fakeAuthSessionLifecycle{rotatePair: &service.SessionTokenPair{AccessToken: access, RefreshToken: refresh, SID: "sid-a"}}
	controller := newAuthControllerWithDependencies(fake, tokens, time.Hour)
	app.Response = response.NewResponseHandler()

	req := httptest.NewRequest("POST", "/api/refreshToken", nil)
	req.Header.Set("RefreshToken", "old-refresh")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = req
	controller.RefreshToken(c)
	require.Equal(t, 200, recorder.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	data := body["data"].(map[string]any)
	require.Equal(t, access, data["accessToken"])
}

func TestAuthControllerLogoutRevokesOnlyCurrentSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokens := &tokenhelper.TokenService{JWTSecret: "test_secret", TokenExpire: 3600, RefreshExpire: 86400}
	fake := &fakeAuthSessionLifecycle{}
	controller := newAuthControllerWithDependencies(fake, tokens, time.Hour)
	app.Response = response.NewResponseHandler()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/api/users/logout", nil)
	c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}, SessionClaims: app.SessionClaims{SID: "sid-a"}})
	controller.Logout(c)
	require.Equal(t, 200, recorder.Code)
	require.Equal(t, "sid-a", fake.revokeSID)
}
