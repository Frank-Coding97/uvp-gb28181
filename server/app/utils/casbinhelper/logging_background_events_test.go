package casbinhelper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

type scriptedPolicyAdapter struct {
	calls atomic.Int32
	err   error
}

func (a *scriptedPolicyAdapter) LoadPolicy(model.Model) error {
	if a.calls.Add(1) > 1 {
		return a.err
	}
	return nil
}

func (a *scriptedPolicyAdapter) SavePolicy(model.Model) error { return nil }
func (a *scriptedPolicyAdapter) AddPolicy(string, string, []string) error {
	return nil
}
func (a *scriptedPolicyAdapter) RemovePolicy(string, string, []string) error {
	return nil
}
func (a *scriptedPolicyAdapter) RemoveFilteredPolicy(string, string, int, ...string) error {
	return nil
}

func TestLoggingCasbinReloadFailureUsesStaticEventAndSafeError(t *testing.T) {
	observed := installCasbinObserver(t)
	const rawError = "password=casbin-secret policy backend credentials"
	adapter := &scriptedPolicyAdapter{err: errors.New(rawError)}
	helper := &CasbinHelper{enforcer: newLoggingPolicyEnforcer(t, adapter)}
	startAutoLoadForTest(t, helper, time.Millisecond)

	entry := waitForCasbinEvent(t, observed, "casbin.policy_reload.failed")
	require.Equal(t, "casbin", entry.LoggerName)
	require.Equal(t, zap.ErrorLevel, entry.Level)
	fields := entry.ContextMap()
	require.Equal(t, "casbin.policy_reload.failed", fields["event"])
	require.NotContains(t, fmt.Sprint(fields), rawError)
	errorFields, ok := fields["error"].(map[string]interface{})
	require.True(t, ok)
	require.NotEmpty(t, errorFields["class"])
	require.NotEmpty(t, errorFields["type"])
	require.NoError(t, closeCasbinContext(t, helper, context.Background()))
}

func TestLoggingCasbinReloadSuccessUsesDebugEvent(t *testing.T) {
	observed := installCasbinObserver(t)
	adapter := &scriptedPolicyAdapter{}
	helper := &CasbinHelper{enforcer: newLoggingPolicyEnforcer(t, adapter)}
	startAutoLoadForTest(t, helper, time.Millisecond)

	entry := waitForCasbinEvent(t, observed, "casbin.policy_reload.succeeded")
	require.Equal(t, "casbin", entry.LoggerName)
	require.Equal(t, zap.DebugLevel, entry.Level)
	require.NoError(t, closeCasbinContext(t, helper, context.Background()))
}

func TestLoggingCasbinPermissionEventsUseRequestLogger(t *testing.T) {
	previousConfig, previousLog := app.ConfigYml, app.ZapLog
	t.Cleanup(func() { app.ConfigYml, app.ZapLog = previousConfig, previousLog })

	fallbackCore, fallbackObserved := observer.New(zap.DebugLevel)
	requestCore, requestObserved := observer.New(zap.DebugLevel)
	app.ConfigYml = staticCasbinConfig{}
	app.ZapLog = zap.New(fallbackCore)
	helper := setupTestCasbin(t)

	recorder := httptest.NewRecorder()
	requestLogger := zap.New(requestCore).With(zap.String("request_id", "casbin-request-1"))
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req = req.WithContext(logging.WithContext(req.Context(), requestLogger))
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = req
	ginContext.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}})

	helper.CasbinMiddleware()(ginContext)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	entry := waitForCasbinEvent(t, requestObserved, "casbin.permission.denied")
	require.Equal(t, "casbin", entry.LoggerName)
	require.Equal(t, zap.WarnLevel, entry.Level)
	require.Equal(t, "casbin-request-1", entry.ContextMap()["request_id"])
	require.NotContains(t, entry.ContextMap(), "path")
	require.NotContains(t, fmt.Sprint(entry.ContextMap()), "/private")
	require.Empty(t, fallbackObserved.All(), "permission logs must use the request logger")
}

type staticCasbinConfig struct{}

func (staticCasbinConfig) ConfigFileChangeListen(...func()) {}
func (staticCasbinConfig) Get(string) interface{}           { return nil }
func (staticCasbinConfig) GetString(string) string          { return "" }
func (staticCasbinConfig) GetBool(string) bool              { return false }
func (staticCasbinConfig) GetInt(string) int                { return 0 }
func (staticCasbinConfig) GetInt32(string) int32            { return 0 }
func (staticCasbinConfig) GetInt64(string) int64            { return 0 }
func (staticCasbinConfig) GetFloat64(string) float64        { return 0 }
func (staticCasbinConfig) GetDuration(string) time.Duration { return 0 }
func (staticCasbinConfig) GetStringSlice(string) []string   { return nil }
func (staticCasbinConfig) GetUintSlice(string) []uint       { return nil }
func (staticCasbinConfig) Set(string, interface{})          {}
func (staticCasbinConfig) SaveConfig() error                { return nil }

var _ persist.Adapter = (*scriptedPolicyAdapter)(nil)
