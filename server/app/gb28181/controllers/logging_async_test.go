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
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/utils/logging"
)

func TestLoggingAsyncContextPTZReconcile(t *testing.T) {
	controller, db, channel, _ := newPTZResourceController(t)
	core, entries := observer.New(zap.InfoLevel)
	ctx, cancel := context.WithCancel(logging.WithContext(context.Background(), logging.WithIdentity(zap.New(core), zap.String("request_id", "ptz-request"))))
	defer cancel()
	scopes := make(chan error, 1)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("logging:test_reconcile", func(tx *gorm.DB) {
		action := ""
		switch value := tx.Statement.Dest.(type) {
		case *gbmodels.GbPTZOperation:
			action = value.Action
		case map[string]interface{}:
			action, _ = value["action"].(string)
		// ⛔ GORM 走 clause.OnConflict / 表驱动写入时，Statement.Dest 是
		//   **`*map[string]interface{}`（指向 map 的指针）**，不是 map 本身。
		//   原来只匹配了后两支，于是 refresh_presets 这条最关键的 Create 永远 return：
		//   回调不 cancel、不送 channel，用例在 1 秒后报
		//   "background reconciliation did not reach persistence" ——
		//   看着像后台没落库，实际是桩没照真实类型建模（DB 里记录是有的）。
		case *map[string]interface{}:
			if value != nil {
				action, _ = (*value)["action"].(string)
			}
		}
		if action != "refresh_presets" {
			return
		}
		// Client disappears while the already-admitted background action is persisted.
		cancel()
		logging.FromContext(tx.Statement.Context, nil).Info("reconcile scope")
		// ⛔ 必须在**这一刻**取 Err()，不能把 ctx 送到 channel 之后再查：
		//   后台那次 Refresh 结束时 `defer cancel()` 会取消它自己的超时 ctx（正常的清理动作），
		//   于是在接收侧再查 Err() 就变成 canceled —— 整包跑时调度稍慢一点就必现，
		//   单跑却常绿（赛跑）。断言的点是"落库时上下文还没被客户端断开带崩"。
		scopes <- tx.Statement.Context.Err()
	}))
	router := gin.New()
	router.POST("/channel/:id/ptz/presets", controller.CreatePTZPreset)
	req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/presets", strings.NewReader(`{"name":"entry","presetId":2}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)
	select {
	case errAtPersist := <-scopes:
		require.NoError(t, errAtPersist, "客户端断开不能穿透到后台对账的上下文")
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
