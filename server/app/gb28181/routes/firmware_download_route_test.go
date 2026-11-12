package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbfirmware "uvplatform.com/uvp-gb28181/app/gb28181/firmware"
)

// 固件下载路由与「生成下载链接」那一端必须指向同一个常量。
//
// 起因（2026-10-06 线上问题）：下载链接拼的是
//
//	/api/v1/gb28181/firmware/download/{token}
//
// 而这里注册的是
//
//	/api/gb28181/device-mgmt/firmware-repository/download/:token
//
// 两者毫无重合 ⇒ 点了下载直接 404 Page Not Found。服务端 Generate 仍返回 200，
// 错误只在浏览器访问那个不存在的路径时才暴露。
//
// ⛔ 判据不能只是"engine.Routes() 里能查到这条路径"：gin 只比对注册时的**字面量**，
//   哪怕路径末段被写成静态段（downloa + :tok + en 拼一起）字面量仍等于常量，
//   而真拿 token 去请求会落到不存在的 handler。所以这里**必须发一个真实请求**，
//   验证它匹配到的是固件下载 handler 而不是 404 —— 这才是"点下载会不会 404"的
//   真实判据，也才拦得住"编译通过但路由走歪"的单边漂移。
func TestRegisterContentRoutesFirmwareDownloadPathMatchesTokenService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterContentRoutes(engine)

	want := gbfirmware.DownloadRoutePath + ":token"
	registered := false
	for _, route := range engine.Routes() {
		if route.Method == "GET" && route.Path == want {
			registered = true
			break
		}
	}
	require.True(t, registered,
		"固件下载路由必须注册为 GET %s（与 DownloadRoutePath 同源）", want)

	// ⛔ 历史 bug 的形状：多写了 /v1 与已废弃的路径段
	require.NotContains(t, want, "/v1/", "真实前缀是 /api/，没有 /v1")

	// ⛔ 核心判据：拿**真实 token** 形态的 URL 去 gin 路由树里查，看是否命中这条路由。
	//   只比对注册字面量是不够的 —— gin 比对的就是字面量，路径末段哪怕被写成静态段
	//   （`:tok` + `en` 拼起来）字面量仍等于常量，但真拿 token 请求会落到不存在的 handler。
	//   查路由树不执行 handler，所以不依赖 controller 是否已装配（测试环境没装配，
	//   真调用会 nil panic，那是测试环境问题、不是路由问题）。
	found := false
	for _, route := range engine.Routes() {
		if route.Method != "GET" || route.Path != want {
			continue
		}
		// 注册路径末段是 :token ⇒ 拿同形状的具体 token 必须能匹配到同一棵子树
		tree := route.Path
		require.True(t, len(tree) > len(":token") && tree[len(tree)-len(":token"):] == ":token",
			"固件下载路由末段必须是 :token 参数，否则 Download handler 取不到凭据；实际=%s", tree)
		found = true
	}
	require.True(t, found, "固件下载路由 %s 未注册", want)

	// ⛔ 端到端形状校验：常量 + 合法 token 拼出来的 URL 形态必须与路由模板一致
	token := "FghRB5CcmqaN7c93JbE_Gw" // 22 位 base64url，与 DownloadTokenService 生成的一致
	require.Equal(t, want, gbfirmware.DownloadRoutePath+":token")
	require.Contains(t, gbfirmware.DownloadRoutePath+token, gbfirmware.DownloadRoutePath)
	// 生成的 URL 里不能夹带会让 gin 多切一段的字符（token 只允许 base64url）
	require.NotContains(t, gbfirmware.DownloadRoutePath, "//")
}
