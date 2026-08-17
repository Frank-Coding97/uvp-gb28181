package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"uvplatform.cn/uvp-gb28181/app/global/app"
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

func TestConfigureTrustedProxiesRequiresExplicitAddresses(t *testing.T) {
	engine := gin.New()
	require.NoError(t, ConfigureTrustedProxies(engine, []string{"10.0.0.0/8", "192.168.1.10"}))
	require.NoError(t, ConfigureTrustedProxies(gin.New(), nil))
	require.Error(t, ConfigureTrustedProxies(gin.New(), []string{"not-an-ip"}))
	require.False(t, errors.Is(ConfigureTrustedProxies(gin.New(), []string{"not-an-ip"}), nil))
}
