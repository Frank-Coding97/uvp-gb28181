package middleware

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type captchaLoginConfig struct{ open bool }

func (c captchaLoginConfig) ConfigFileChangeListen(...func()) {}
func (c captchaLoginConfig) Get(string) interface{}           { return nil }
func (c captchaLoginConfig) GetString(string) string          { return "" }
func (c captchaLoginConfig) GetBool(key string) bool          { return key == "captcha.open" && c.open }
func (c captchaLoginConfig) GetInt(string) int                { return 0 }
func (c captchaLoginConfig) GetInt32(string) int32            { return 0 }
func (c captchaLoginConfig) GetInt64(string) int64            { return 0 }
func (c captchaLoginConfig) GetFloat64(string) float64        { return 0 }
func (c captchaLoginConfig) GetDuration(string) time.Duration { return 0 }
func (c captchaLoginConfig) GetStringSlice(string) []string   { return nil }
func (c captchaLoginConfig) GetUintSlice(string) []uint       { return nil }
func (c captchaLoginConfig) Set(string, interface{})          {}
func (c captchaLoginConfig) SaveConfig() error                { return nil }

type captchaLoginRecorder struct {
	events []app.LoginLogEvent
	err    error
}

func (r *captchaLoginRecorder) RecordLogin(_ context.Context, event app.LoginLogEvent) error {
	r.events = append(r.events, event)
	return r.err
}

func TestCaptchaMiddlewareRecordsFailureWithoutChangingResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldConfig, oldResponse, oldRecorder := app.ConfigYml, app.Response, app.LoginLogRecorder
	t.Cleanup(func() { app.ConfigYml, app.Response, app.LoginLogRecorder = oldConfig, oldResponse, oldRecorder })
	app.ConfigYml = captchaLoginConfig{open: true}
	app.Response = response.NewResponseHandler()
	recorder := &captchaLoginRecorder{err: errors.New("audit unavailable")}
	app.LoginLogRecorder = recorder

	router := gin.New()
	nextCalled := false
	router.POST("/api/login", CaptchaMiddleware(), func(c *gin.Context) {
		nextCalled = true
		c.Status(http.StatusNoContent)
	})
	body := []byte(`{"username":"alice","password":"secret-password"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.False(t, nextCalled)
	require.Len(t, recorder.events, 1)
	event := recorder.events[0]
	require.Equal(t, "alice", event.Username)
	require.Equal(t, service.LoginResultFailure, event.Result)
	require.Equal(t, service.LoginFailureCaptchaInvalid, event.FailureReason)
	require.NotContains(t, event.UserAgent, "secret-password")
}
