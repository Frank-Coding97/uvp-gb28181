package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/tokenhelper"
)

type fakeSessionValidator struct{ err error }

func (f fakeSessionValidator) ValidateSession(context.Context, string, uint) error { return f.err }
func (f fakeSessionValidator) TouchSession(context.Context, string) error          { return nil }

func TestJWTAuthMiddlewareChecksPersistentSessionBeforeHandler(t *testing.T) {
	oldToken, oldValidator, oldLog := app.TokenService, app.SessionValidator, app.ZapLog
	t.Cleanup(func() { app.TokenService, app.SessionValidator, app.ZapLog = oldToken, oldValidator, oldLog })
	app.ZapLog = zap.NewNop()
	app.TokenService = &tokenhelper.TokenService{JWTSecret: "test_secret", TokenExpire: 3600}
	token, err := app.TokenService.GenerateTokenForSession(&app.ClaimsUser{UserID: 7, Username: "admin"}, "sid-a", "jti-a")
	require.NoError(t, err)

	for _, tc := range []struct {
		name string
		err  error
		code int
	}{
		{name: "valid", code: http.StatusOK},
		{name: "revoked", err: service.ErrSessionUnavailable, code: http.StatusUnauthorized},
		{name: "database failure", err: service.ErrSessionStore, code: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app.SessionValidator = fakeSessionValidator{err: tc.err}
			engine := gin.New()
			engine.Use(JWTAuthMiddleware())
			engine.GET("/protected", func(c *gin.Context) { c.Status(http.StatusOK) })
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			res := httptest.NewRecorder()
			engine.ServeHTTP(res, req)
			require.Equal(t, tc.code, res.Code)
		})
	}
}

func TestJWTAuthMiddlewareRejectsMissingSessionValidator(t *testing.T) {
	oldToken, oldValidator, oldLog := app.TokenService, app.SessionValidator, app.ZapLog
	t.Cleanup(func() { app.TokenService, app.SessionValidator, app.ZapLog = oldToken, oldValidator, oldLog })
	app.ZapLog = zap.NewNop()
	app.TokenService = &tokenhelper.TokenService{JWTSecret: "test_secret", TokenExpire: 3600}
	token, err := app.TokenService.GenerateTokenForSession(&app.ClaimsUser{UserID: 7}, "sid-a", "jti-a")
	require.NoError(t, err)
	app.SessionValidator = nil

	engine := gin.New()
	engine.Use(JWTAuthMiddleware())
	engine.GET("/protected", func(c *gin.Context) { c.Status(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()
	engine.ServeHTTP(res, req)
	require.Equal(t, http.StatusServiceUnavailable, res.Code)
}

func TestJWTAuthMiddlewareRejectsRevokedSessionWhenNotFoundErrorMasked(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysUserSession{}))
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:disable_raise_record_not_found", func(g *gorm.DB) {
		g.Statement.RaiseErrorOnNotFound = false
	}))

	now := time.Now()
	sessions := service.NewAuthSessionService(db)
	sessions.SetClock(func() time.Time { return now })
	require.NoError(t, sessions.Create(context.Background(), &models.SysUserSession{
		SID: "sid-revoked", UserID: 7, ClientIP: "10.0.0.1", LoginLocation: "内网",
		Browser: "test", OS: "test", LoginAt: now, LastActiveAt: now, SessionExpiresAt: now.Add(time.Hour),
	}))
	revokedBy := uint(1)
	_, err = sessions.Revoke(context.Background(), "sid-revoked", "forced", &revokedBy)
	require.NoError(t, err)

	oldToken, oldValidator, oldLog := app.TokenService, app.SessionValidator, app.ZapLog
	t.Cleanup(func() { app.TokenService, app.SessionValidator, app.ZapLog = oldToken, oldValidator, oldLog })
	app.ZapLog = zap.NewNop()
	app.TokenService = &tokenhelper.TokenService{JWTSecret: "test_secret", TokenExpire: 3600}
	app.SessionValidator = sessions
	token, err := app.TokenService.GenerateTokenForSession(&app.ClaimsUser{UserID: 7, Username: "admin"}, "sid-revoked", "jti-revoked")
	require.NoError(t, err)

	handlerReached := false
	engine := gin.New()
	engine.Use(JWTAuthMiddleware())
	engine.GET("/protected", func(c *gin.Context) {
		handlerReached = true
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()
	engine.ServeHTTP(res, req)

	require.Equal(t, http.StatusUnauthorized, res.Code)
	require.False(t, handlerReached)
}

func TestConfigureTrustedProxiesRequiresExplicitAddresses(t *testing.T) {
	engine := gin.New()
	require.NoError(t, ConfigureTrustedProxies(engine, []string{"10.0.0.0/8", "192.168.1.10"}))
	require.NoError(t, ConfigureTrustedProxies(gin.New(), nil))
	require.Error(t, ConfigureTrustedProxies(gin.New(), []string{"not-an-ip"}))
	require.False(t, errors.Is(ConfigureTrustedProxies(gin.New(), []string{"not-an-ip"}), nil))
}

func TestConfigureTrustedProxiesUsesForwardedClientIPFromTrustedPeer(t *testing.T) {
	engine := gin.New()
	require.NoError(t, ConfigureTrustedProxies(engine, []string{"127.0.0.1", "::1"}))
	engine.GET("/client-ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	req := httptest.NewRequest(http.MethodGet, "/client-ip", nil)
	req.RemoteAddr = "127.0.0.1:5188"
	req.Header.Set("X-Forwarded-For", "203.0.113.42")
	res := httptest.NewRecorder()
	engine.ServeHTTP(res, req)

	require.Equal(t, http.StatusOK, res.Code)
	require.Equal(t, "203.0.113.42", res.Body.String())
}

func TestConfigureTrustedProxiesDoesNotTrustForwardedClientIPFromUntrustedPeer(t *testing.T) {
	engine := gin.New()
	require.NoError(t, ConfigureTrustedProxies(engine, []string{"127.0.0.1", "::1"}))
	engine.GET("/client-ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	req := httptest.NewRequest(http.MethodGet, "/client-ip", nil)
	req.RemoteAddr = "198.51.100.7:4000"
	req.Header.Set("X-Forwarded-For", "203.0.113.42")
	res := httptest.NewRecorder()
	engine.ServeHTTP(res, req)

	require.Equal(t, http.StatusOK, res.Code)
	require.Equal(t, "198.51.100.7", res.Body.String())
}
