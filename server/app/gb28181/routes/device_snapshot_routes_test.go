package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestRegisterRoutesIncludesDeviceSnapshotEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterContentRoutes(engine)
	RegisterRoutes(engine.Group("/api"))
	routes := make(map[string]bool)
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	require.True(t, routes["POST /api/gb28181/device-mgmt/channel/:id/snapshot-sessions"])
	require.True(t, routes["GET /api/gb28181/device-mgmt/channel/:id/snapshot-sessions/:sessionId"])
	// ⛔ 上传路由必须两种形态都在：设备真机发 `POST`（路径无文件名、带尾斜杠），
	// 靠 gin 的 307 尾斜杠重定向兜不住（设备不跟随）⇒ 只有 catch-all 才匹配得上。
	require.True(t, routes["POST /api/gb28181/device-snapshots/uploads/*token"])
	require.True(t, routes["PUT /api/gb28181/device-snapshots/uploads/:token/:filename"])
	require.True(t, routes["GET /api/gb28181/device-snapshots/uploads/:token/:filename"])
	// ⭐ 图像库稳定读接口：按库行 id 取图（JWT + gb28181:device:snapshot）。
	// ⛔ 用**全路径常量**断言，钉的是"路由真的注册在这个路径上"（鉴权中间件按 path+method
	// 查 casbin，路径写歪的表现是"接口通但恒 403"，而且不报任何错）。
	// ⚠️ 但它**不能**证明"这条路径 == 迁移里 sys_api 登记的那条"：常量与路由一起改歪时
	// 两侧同时变、照样绿。那一层由 models 包的
	// `TestSnapshotLibraryPathConstantsMatchTheMigrationLiterals` 用字面量钉住。
	require.Truef(t, routes["GET "+gbmodels.SnapshotLibraryAPIPath],
		"图像库读图路由缺失: %s", gbmodels.SnapshotLibraryAPIPath)
	// ⭐ 图像库**列表**接口：与上面那条读图接口是两条不同层的路径（`/snapshots` 与
	// `/snapshots/:id/content`），两条都必须注册。
	// ⛔ 少注册这一条的表现是"图像库页面打得开、列表永远空"（404 在前端被当成空数组），
	// 所以这里必须有一条独立的断言，不能靠"读图那条在"来代表。
	require.Truef(t, routes["GET "+gbmodels.SnapshotListAPIPath],
		"图像库列表路由缺失: %s", gbmodels.SnapshotListAPIPath)
	require.NotEqual(t, gbmodels.SnapshotLibraryAPIPath, gbmodels.SnapshotListAPIPath,
		"两条路径常量撞在一起了：读图接口会被列表接口顶掉")
}
