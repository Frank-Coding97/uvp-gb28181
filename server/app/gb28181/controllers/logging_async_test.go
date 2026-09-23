package controllers_test

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func TestLoggingAsyncContextPTZReconcile(t *testing.T) {
	controller, db, channel, _ := newPTZResourceController(t)
	core, entries := observer.New(zap.InfoLevel)
	ctx, cancel := context.WithCancel(logging.WithContext(context.Background(), logging.WithIdentity(zap.New(core), zap.String("request_id", "ptz-request"))))
	defer cancel()
	scopes := make(chan context.Context, 1)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("logging:test_reconcile", func(tx *gorm.DB) {
		action := ""
		switch value := tx.Statement.Dest.(type) {
		case *gbmodels.GbPTZOperation:
			action = value.Action
		case map[string]interface{}:
			action, _ = value["action"].(string)
		}
		if action != "refresh_presets" {
			return
		}
		// Client disappears while the already-admitted background action is persisted.
		cancel()
		logging.FromContext(tx.Statement.Context, nil).Info("reconcile scope")
		scopes <- tx.Statement.Context
	}))
	router := gin.New()
	router.POST("/channel/:id/ptz/presets", controller.CreatePTZPreset)
	req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/presets", strings.NewReader(`{"name":"entry","presetId":2}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)
	select {
	case scope := <-scopes:
		require.NoError(t, scope.Err())
		require.Len(t, entries.All(), 1)
		require.Equal(t, "ptz-request", entries.All()[0].ContextMap()["request_id"])
	case <-time.After(time.Second):
		t.Fatal("background reconciliation did not reach persistence")
	}
	// Query shares the only DB connection and therefore waits for the insert to complete.
	var operation gbmodels.GbPTZOperation
	require.NoError(t, db.Where("action = ?", "refresh_presets").First(&operation).Error)
	require.Equal(t, gbmodels.PTZOperationQueued, operation.Status)
}
