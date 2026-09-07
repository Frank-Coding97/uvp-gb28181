package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/cachehelper"
	"uvplatform.cn/uvp-gb28181/app/utils/passwordhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
	"uvplatform.cn/uvp-gb28181/app/utils/tokenhelper"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"
)

type fakeAuthSessionLifecycle struct {
	createPair *service.SessionTokenPair
	createErr  error
	rotatePair *service.SessionTokenPair
	rotateSID  string
	revokeSID  string
}

func (f *fakeAuthSessionLifecycle) CreateLogin(context.Context, *models.User, service.LoginMetadata, app.TokenServiceInterface, time.Duration) (*service.SessionTokenPair, error) {
	return f.createPair, f.createErr
}

type loginTestConfig struct{ values map[string]any }

func (c *loginTestConfig) ConfigFileChangeListen(...func())     {}
func (c *loginTestConfig) Get(key string) any                   { return c.values[key] }
func (c *loginTestConfig) GetString(key string) string          { v, _ := c.values[key].(string); return v }
func (c *loginTestConfig) GetBool(key string) bool              { v, _ := c.values[key].(bool); return v }
func (c *loginTestConfig) GetInt(key string) int                { v, _ := c.values[key].(int); return v }
func (c *loginTestConfig) GetInt32(key string) int32            { return int32(c.GetInt(key)) }
func (c *loginTestConfig) GetInt64(key string) int64            { return int64(c.GetInt(key)) }
func (c *loginTestConfig) GetFloat64(string) float64            { return 0 }
func (c *loginTestConfig) GetDuration(key string) time.Duration { return time.Duration(c.GetInt(key)) }
func (c *loginTestConfig) GetStringSlice(string) []string       { return nil }
func (c *loginTestConfig) GetUintSlice(string) []uint           { return nil }
func (c *loginTestConfig) Set(key string, value interface{})    { c.values[key] = value }
func (c *loginTestConfig) SaveConfig() error                    { return nil }

type captureLoginRecorder struct {
	events []app.LoginLogEvent
	err    error
}

func (r *captureLoginRecorder) RecordLogin(_ context.Context, event app.LoginLogEvent) error {
	r.events = append(r.events, event)
	return r.err
}

func setupLoginControllerTest(t *testing.T, lockThreshold int) (*gorm.DB, *captureLoginRecorder) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}))
	oldDB, oldConfig, oldCache := app.GormDbMysql, app.ConfigYml, app.Cache
	oldResponse, oldLog, oldRecorder := app.Response, app.ZapLog, app.LoginLogRecorder
	t.Cleanup(func() {
		app.GormDbMysql, app.ConfigYml, app.Cache = oldDB, oldConfig, oldCache
		app.Response, app.ZapLog, app.LoginLogRecorder = oldResponse, oldLog, oldRecorder
	})
	app.GormDbMysql = db
	app.ConfigYml = &loginTestConfig{values: map[string]any{
		"gormv2.usedbtype": "mysql", "safe.loginlockthreshold": lockThreshold,
		"safe.loginlockexpire": 60, "safe.loginlockduration": 600,
	}}
	app.Cache = cachehelper.NewMemoryHelper()
	app.Response = response.NewResponseHandler()
	app.ZapLog = zap.NewNop()
	recorder := &captureLoginRecorder{}
	app.LoginLogRecorder = recorder
	return db, recorder
}

func invokeLogin(t *testing.T, controller *AuthController, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/api/login", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	func() {
		defer func() { _ = recover() }()
		controller.Login(c)
	}()
	return recorder
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

func TestAuthControllerLoginRecordsEveryResultWithoutChangingResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokens := &tokenhelper.TokenService{JWTSecret: "test_secret", TokenExpire: 3600, RefreshExpire: 86400}
	access, err := tokens.GenerateTokenForSession(&app.ClaimsUser{UserID: 7, Username: "alice"}, "sid-a", "access-jti")
	require.NoError(t, err)
	refresh, err := tokens.GenerateRefreshTokenForSessionUntil(7, "sid-a", "refresh-jti", time.Now().Add(time.Hour))
	require.NoError(t, err)
	hash, err := passwordhelper.HashPassword("correct-password")
	require.NoError(t, err)

	tests := []struct {
		name        string
		user        *models.User
		password    string
		createPair  *service.SessionTokenPair
		createErr   error
		recorderErr error
		wantHTTP    int
		wantResult  string
		wantReason  string
		wantUserID  bool
	}{
		{name: "unknown user", password: "wrong-password", wantHTTP: 400, wantResult: service.LoginResultFailure, wantReason: service.LoginFailureUserNotFound},
		{name: "disabled user", user: &models.User{BaseModel: models.BaseModel{ID: 7}, Username: "alice", Password: hash, Status: 0}, password: "correct-password", wantHTTP: 400, wantResult: service.LoginResultFailure, wantReason: service.LoginFailureUserDisabled, wantUserID: true},
		{name: "wrong password", user: &models.User{BaseModel: models.BaseModel{ID: 7}, Username: "alice", Password: hash, Status: 1}, password: "wrong-password", wantHTTP: 400, wantResult: service.LoginResultFailure, wantReason: service.LoginFailurePasswordIncorrect, wantUserID: true},
		{name: "session create failure", user: &models.User{BaseModel: models.BaseModel{ID: 7}, Username: "alice", Password: hash, Status: 1}, password: "correct-password", createErr: service.ErrSessionStore, wantHTTP: 503, wantResult: service.LoginResultFailure, wantReason: service.LoginFailureSessionCreate, wantUserID: true},
		{name: "success survives audit failure", user: &models.User{BaseModel: models.BaseModel{ID: 7}, Username: "alice", Password: hash, Status: 1}, password: "correct-password", createPair: &service.SessionTokenPair{AccessToken: access, RefreshToken: refresh, SID: "sid-a"}, recorderErr: errors.New("audit unavailable"), wantHTTP: 200, wantResult: service.LoginResultSuccess, wantUserID: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, recorder := setupLoginControllerTest(t, 0)
			if tc.user != nil {
				status := tc.user.Status
				require.NoError(t, db.Create(tc.user).Error)
				if status == 0 {
					require.NoError(t, db.Model(&models.User{}).Where("id = ?", tc.user.ID).Update("status", 0).Error)
				}
			}
			recorder.err = tc.recorderErr
			fake := &fakeAuthSessionLifecycle{createPair: tc.createPair, createErr: tc.createErr}
			controller := newAuthControllerWithDependencies(fake, tokens, time.Hour)
			got := invokeLogin(t, controller, "alice", tc.password)
			require.Equal(t, tc.wantHTTP, got.Code)
			require.Len(t, recorder.events, 1)
			event := recorder.events[0]
			require.Equal(t, tc.wantResult, event.Result)
			require.Equal(t, tc.wantReason, event.FailureReason)
			require.Equal(t, tc.wantUserID, event.UserID != nil)
			require.Equal(t, "alice", event.Username)
			require.NotContains(t, event.UserAgent, tc.password)
		})
	}
}

func TestAuthControllerLoginRecordsAccountLock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, recorder := setupLoginControllerTest(t, 1)
	hash, err := passwordhelper.HashPassword("correct-password")
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.User{BaseModel: models.BaseModel{ID: 7}, Username: "alice", Password: hash, Status: 1}).Error)
	tokens := &tokenhelper.TokenService{JWTSecret: "test_secret", TokenExpire: 3600, RefreshExpire: 86400}
	got := invokeLogin(t, newAuthControllerWithDependencies(&fakeAuthSessionLifecycle{}, tokens, time.Hour), "alice", "wrong-password")
	require.Equal(t, 400, got.Code)
	require.Len(t, recorder.events, 1)
	require.Equal(t, service.LoginFailureAccountLocked, recorder.events[0].FailureReason)
}
