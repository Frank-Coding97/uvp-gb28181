package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestOperationModuleGB28181Routes 覆盖国标模块全部路由分组,
// 确保不再落入"其他"。前缀按具体度降序匹配(见 getOperationModule)。
func TestOperationModuleGB28181Routes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		path   string
		module string
	}{
		// 设备管理(含目录树/地图/异常/PTZ/对讲/回放,以及旧设备接口)
		{"/api/gb28181/device-mgmt/devices", "GB28181设备管理"},
		{"/api/gb28181/device-mgmt/catalog/tree", "GB28181设备管理"},
		{"/api/gb28181/device-mgmt/channel/566/ptz/presets", "GB28181设备管理"},
		{"/api/gb28181/device/list", "GB28181设备管理"},
		// 实时点播
		{"/api/gb28181/play/0621055469/monitor", "GB28181实时点播"},
		// 多屏播放(必须在 play 之前匹配,见顺序用例)
		{"/api/gb28181/playback-schemes", "GB28181多屏播放"},
		// 云端录像
		{"/api/gb28181/cloud-recordings/files", "GB28181云端录像"},
		// 告警管理(既有映射)
		{"/api/gb28181/alarms/batch-delete", "GB28181告警管理"},
		// 级联管理
		{"/api/gb28181/cascade/platforms", "GB28181级联管理"},
		// 流媒体管理
		{"/api/gb28181/zlm/nodes", "GB28181流媒体管理"},
		// 接入安全
		{"/api/gb28181/security/access-rules", "GB28181接入安全"},
		// SIP 服务配置 / 初始化 / 看板 / 接入信息 / 扫码接入
		{"/api/gb28181/sip/service-config/ptz-default-speed", "GB28181服务配置"},
		{"/api/gb28181/sip/setup/status", "GB28181初始化配置"},
		{"/api/gb28181/sip/dashboard/snapshot", "GB28181信令看板"},
		{"/api/gb28181/sip/platform", "GB28181SIP接入信息"},
		{"/api/gb28181/sip/qr/token", "GB28181扫码接入"},
		// SIP 日志
		{"/api/gb28181/sip-traces/sessions", "GB28181SIP日志"},
	}

	for _, tc := range cases {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodGet, tc.path, nil)
		require.Equal(t, tc.module, getOperationModule(ctx), "path: %s", tc.path)
	}
}

// TestOperationModulePrefixOrder 验证前缀重叠时更具体的路径优先命中。
func TestOperationModulePrefixOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// /device 是 /device-mgmt 的子串,必须命中设备管理而非兜底
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/map/clusters", nil)
	require.Equal(t, "GB28181设备管理", getOperationModule(ctx))

	// /play 是 /playback-schemes 的子串,必须命中多屏播放而非点播
	ctx, _ = gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/gb28181/playback-schemes/1", nil)
	require.Equal(t, "GB28181多屏播放", getOperationModule(ctx))
}

// TestOperationModuleNonGB28181 验证非国标模块映射不回退。
func TestOperationModuleNonGB28181(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		path   string
		module string
	}{
		{"/api/users/list", "用户管理"},
		{"/api/sysMenu/getRouters", "菜单管理"},
		{"/api/sysOperationLog/list", "操作日志管理"},
		{"/api/login", "认证管理"},
		// 未匹配路径仍兜底"其他"
		{"/api/unknown/path", "其他"},
	}

	for _, tc := range cases {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodGet, tc.path, nil)
		require.Equal(t, tc.module, getOperationModule(ctx), "path: %s", tc.path)
	}
}
