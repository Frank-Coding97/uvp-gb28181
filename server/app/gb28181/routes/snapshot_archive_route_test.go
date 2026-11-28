package routes

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// TestSnapshotArchiveRouteCoexistsWithContentRoute 钉住 gin 路由树里
// 「静态段 `/snapshots/archive`」与「参数段 `/snapshots/:id/content`」的共存。
//
// ⛔ 这两个写法在 httprouter 系的老版本里是**注册即 panic** 的
// （`archive` 与 `:id` 在同一层争同一段）。gin v1.11 允许，但这是**版本行为**：
// 升级 gin 后如果又变回去，表现会是**整个服务起不来**（panic 在路由注册阶段），
// 而不是这条接口单独坏掉 —— 所以单独钉一条用例，比等现场起不来再查便宜得多。
//
// ⛔ 除了"能注册"，还必须证明**匹配是对的**：`/snapshots/archive` 不能被
// `:id/content` 吃掉（否则 `archive` 会被当成 id，解析失败 → 恒 404），
// 而 `/snapshots/12/content` 也不能被静态段抢走。
func TestSnapshotArchiveRouteCoexistsWithContentRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET(gbmodels.SnapshotArchiveRoutePath, func(c *gin.Context) {
		c.String(http.StatusOK, "archive")
	})
	router.GET(gbmodels.SnapshotLibraryRoutePath, func(c *gin.Context) {
		c.String(http.StatusOK, "content:"+c.Param("id"))
	})

	cases := []struct{ target, want string }{
		{"/snapshots/archive", "archive"},
		{"/snapshots/12/content", "content:12"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tc.target, nil)
		router.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusOK, recorder.Code, tc.target)
		require.Equal(t, tc.want, recorder.Body.String(), tc.target)
	}
}

// TestSnapshotArchiveRouteIsRegisteredInDeviceMgmtGroup 路由必须真的挂进鉴权组。
//
// ⛔ 只断言"常量存在"是不够的：漏注册的表现是 404，而前端会把它当成
// "这张图不存在"静默吞掉（批量下载按钮点了没反应）。这里直接读源码找那一行，
// 比启动整个 server 便宜，且能在 CI 里跑。
func TestSnapshotArchiveRouteIsRegisteredInDeviceMgmtGroup(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("routes.go"))
	require.NoError(t, err)
	require.Contains(t, string(raw),
		"dmgmt.GET(gbmodels.SnapshotArchiveRoutePath, deviceMgmtController.ArchiveSnapshots)",
		"批量下载路由必须挂在 dmgmt 组下（否则绕过鉴权或直接 404）")
}
