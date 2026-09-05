package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

type loggingAuditConfig struct{}

func (loggingAuditConfig) ConfigFileChangeListen(...func()) {}
func (loggingAuditConfig) Get(string) interface{}           { return nil }
func (loggingAuditConfig) GetString(key string) string {
	if key == "gormv2.usedbtype" {
		return "mysql"
	}
	return ""
}
func (loggingAuditConfig) GetBool(string) bool              { return false }
func (loggingAuditConfig) GetInt(string) int                { return 0 }
func (loggingAuditConfig) GetInt32(string) int32            { return 0 }
func (loggingAuditConfig) GetInt64(string) int64            { return 0 }
func (loggingAuditConfig) GetFloat64(string) float64        { return 0 }
func (loggingAuditConfig) GetDuration(string) time.Duration { return 0 }
func (loggingAuditConfig) GetStringSlice(string) []string   { return nil }
func (loggingAuditConfig) GetUintSlice(string) []uint       { return nil }
func (loggingAuditConfig) Set(string, interface{})          {}
func (loggingAuditConfig) SaveConfig() error                { return nil }

func TestLoggingAuditContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB, oldConfig, oldLog := app.GormDbMysql, app.ConfigYml, app.ZapLog
	t.Cleanup(func() { app.GormDbMysql, app.ConfigYml, app.ZapLog = oldDB, oldConfig, oldLog })
	core, observed := observer.New(zap.InfoLevel)
	root := zap.New(core)
	app.GormDbMysql, app.ConfigYml, app.ZapLog = db, loggingAuditConfig{}, root

	var callbackCtx context.Context
	var callbackOnce sync.Once
	callbackDone := make(chan struct{})
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:logging_audit_context", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*models.SysOperationLog); !ok {
			return
		}
		callbackCtx = tx.Statement.Context
		callbackOnce.Do(func() { close(callbackDone) })
		tx.AddError(errors.New("test audit database failure"))
	}))

	claims := &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 42, Username: "operator"}}
	parent, cancel := context.WithCancel(context.Background())
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		requestContext := context.WithValue(c.Request.Context(), consts.BindContextKeyName, claims)
		requestContext = logging.WithContext(requestContext, logging.WithIdentity(root, zap.String("request_id", "audit-rid")))
		c.Set(consts.BindContextKeyName, claims)
		c.Request = c.Request.WithContext(requestContext)
		c.Next()
	})
	engine.Use(OperationLogMiddleware())
	engine.POST("/audit", func(c *gin.Context) {
		cancel()
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPost, "/audit", nil).WithContext(parent)
	res := httptest.NewRecorder()
	engine.ServeHTTP(res, req)

	require.Equal(t, http.StatusNoContent, res.Code)
	select {
	case <-callbackDone:
	case <-time.After(time.Second):
		t.Fatal("async operation audit did not reach database")
	}
	require.NotNil(t, callbackCtx)
	require.NoError(t, callbackCtx.Err(), "audit must outlive request cancellation")
	detachedClaims, ok := callbackCtx.Value(consts.BindContextKeyName).(*app.Claims)
	require.True(t, ok)
	require.NotSame(t, claims, detachedClaims, "audit context must copy immutable Claims")
	require.Equal(t, claims.UserID, detachedClaims.UserID)
	require.Equal(t, claims.Username, detachedClaims.Username)

	require.Eventually(t, func() bool {
		for _, entry := range observed.All() {
			if entry.Message == "记录操作日志失败" {
				fields := entry.ContextMap()
				return fields["event"] == "audit.operation_log.persist_failed" && fields["request_id"] == "audit-rid"
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)
}
