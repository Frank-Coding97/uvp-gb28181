package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/utils/ginhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
	"uvplatform.cn/uvp-gb28181/app/utils/tokenhelper"
)

type loggingClaimsSessionValidator struct{}

func (loggingClaimsSessionValidator) ValidateSession(context.Context, string, uint) error { return nil }
func (loggingClaimsSessionValidator) TouchSession(context.Context, string) error          { return nil }

type loggingDemoConfig struct{}

func (loggingDemoConfig) ConfigFileChangeListen(...func()) {}
func (loggingDemoConfig) Get(string) interface{}           { return nil }
func (loggingDemoConfig) GetString(string) string          { return "" }
func (loggingDemoConfig) GetBool(key string) bool          { return key == "server.demoaccount.enabled" }
func (loggingDemoConfig) GetInt(string) int                { return 0 }
func (loggingDemoConfig) GetInt32(string) int32            { return 0 }
func (loggingDemoConfig) GetInt64(string) int64            { return 0 }
func (loggingDemoConfig) GetFloat64(string) float64        { return 0 }
func (loggingDemoConfig) GetDuration(string) time.Duration { return 0 }
func (loggingDemoConfig) GetStringSlice(string) []string   { return nil }
func (loggingDemoConfig) GetUintSlice(key string) []uint {
	if key == "server.demoaccount.userids" {
		return []uint{7}
	}
	return nil
}
func (loggingDemoConfig) Set(string, interface{}) {}
func (loggingDemoConfig) SaveConfig() error       { return nil }

func TestLoggingClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldToken, oldValidator, oldLog := app.TokenService, app.SessionValidator, app.ZapLog
	t.Cleanup(func() { app.TokenService, app.SessionValidator, app.ZapLog = oldToken, oldValidator, oldLog })

	core, observed := observer.New(zap.InfoLevel)
	root := zap.New(core)
	app.ZapLog = root
	app.TokenService = &tokenhelper.TokenService{JWTSecret: "logging-test-secret", TokenExpire: 3600}
	app.SessionValidator = loggingClaimsSessionValidator{}
	token, err := app.TokenService.GenerateTokenForSession(&app.ClaimsUser{UserID: 7, Username: "admin"}, "sid-claims", "jti-claims")
	require.NoError(t, err)

	var ginClaims, contextClaims *app.Claims
	engine := gin.New()
	engine.Use(ginhelper.RequestLogging(root), JWTAuthMiddleware())
	engine.GET("/protected", func(c *gin.Context) {
		value, ok := c.Get(consts.BindContextKeyName)
		require.True(t, ok)
		ginClaims, ok = value.(*app.Claims)
		require.True(t, ok)
		contextClaims, ok = c.Request.Context().Value(consts.BindContextKeyName).(*app.Claims)
		require.True(t, ok)
		app.Log(c.Request.Context()).Info("authenticated request", zap.String("event", "test.logging_claims"))
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()
	engine.ServeHTTP(res, req)

	require.Equal(t, http.StatusNoContent, res.Code)
	require.NotNil(t, ginClaims)
	require.Same(t, ginClaims, contextClaims)
	require.Equal(t, uint(7), contextClaims.UserID)
	require.Equal(t, "admin", contextClaims.Username)
	var claimEntry *observer.LoggedEntry
	for _, entry := range observed.All() {
		if entry.ContextMap()["event"] == "test.logging_claims" {
			copy := entry
			claimEntry = &copy
			break
		}
	}
	require.NotNil(t, claimEntry)
	require.NotEmpty(t, claimEntry.ContextMap()["request_id"], "JWT must retain RequestLogging scope")
}

func TestLoggingClaimsJWTFailureUsesScopedSafeEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldToken, oldValidator, oldLog := app.TokenService, app.SessionValidator, app.ZapLog
	t.Cleanup(func() { app.TokenService, app.SessionValidator, app.ZapLog = oldToken, oldValidator, oldLog })
	core, observed := observer.New(zap.InfoLevel)
	root := zap.New(core)
	app.ZapLog = root
	app.TokenService = &tokenhelper.TokenService{JWTSecret: "logging-test-secret", TokenExpire: 3600}
	app.SessionValidator = loggingClaimsSessionValidator{}

	engine := gin.New()
	engine.Use(ginhelper.RequestLogging(root), JWTAuthMiddleware())
	engine.GET("/protected", func(c *gin.Context) { t.Fatal("handler must not run") })
	res := httptest.NewRecorder()
	engine.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/protected", nil))

	require.Equal(t, http.StatusUnauthorized, res.Code)
	var failure *observer.LoggedEntry
	for _, entry := range observed.All() {
		if entry.Message == "Get access token failed" {
			copy := entry
			failure = &copy
			break
		}
	}
	require.NotNil(t, failure)
	require.Equal(t, "auth.access_token.read_failed", failure.ContextMap()["event"])
	require.NotEmpty(t, failure.ContextMap()["request_id"])
}

func TestLoggingClaimsDemoAccountUsesRequestScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldConfig, oldLog := app.ConfigYml, app.ZapLog
	t.Cleanup(func() { app.ConfigYml, app.ZapLog = oldConfig, oldLog })
	core, observed := observer.New(zap.InfoLevel)
	root := zap.New(core)
	app.ConfigYml = loggingDemoConfig{}
	app.ZapLog = root

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		claims := &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7, Username: "demo"}}
		c.Set(consts.BindContextKeyName, claims)
		c.Request = c.Request.WithContext(logging.WithContext(c.Request.Context(), logging.WithIdentity(root, zap.String("request_id", "demo-rid"))))
		c.Next()
	})
	engine.Use(DemoAccountMiddleware())
	engine.POST("/write", func(c *gin.Context) { t.Fatal("demo write must be rejected") })
	res := httptest.NewRecorder()
	engine.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/write", nil))

	require.Equal(t, http.StatusForbidden, res.Code)
	var denied *observer.LoggedEntry
	for _, entry := range observed.All() {
		if entry.Message == "演示账号尝试执行非GET操作" {
			copy := entry
			denied = &copy
			break
		}
	}
	require.NotNil(t, denied)
	require.Equal(t, "auth.demo_account.denied", denied.ContextMap()["event"])
	require.Equal(t, "demo-rid", denied.ContextMap()["request_id"])
}
