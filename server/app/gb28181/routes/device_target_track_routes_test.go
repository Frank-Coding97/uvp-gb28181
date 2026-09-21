package routes

import (
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// ⛔ 字面量，**不**从 gbmodels 取值：本用例要证明"路由真的注册在这个路径上"。
// 从被测对象自己取值来断言自己，路径改歪时两侧一起歪、照样绿。
// 下面另有一条断言把常量与这个字面量对齐，那一条负责钉住"常量没被改错"。
const targetTrackAPIPath = "/api/gb28181/device-mgmt/channel/:id/target-track"

const targetTrackRoutePath = "/channel/:id/target-track"

func TestRegisterRoutesRegistersTargetTrackReadAndWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))

	registered := make(map[string]struct{})
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}

	_, getRegistered := registered["GET "+targetTrackAPIPath]
	_, postRegistered := registered["POST "+targetTrackAPIPath]
	require.True(t, getRegistered, "目标跟踪读接口必须注册（返回平台已下发的意图）")
	require.True(t, postRegistered, "目标跟踪写接口必须注册（A.2.3.1.14）")

	// 读写在**同一路径**上分方法，与 video-params 同形：面板是"一个资源两种动作"。
	// 正因为同路径，权限必须靠 method 区分 —— 见下面的用例。
	// 反过来说，两条路由必须**分两行注册**：gin 不允许同一路径同一方法注册两次，
	// 而把 POST 误写成 GET 的表现是"接口通、但下发永远只是读"，不报任何错。
	require.NotEqual(t, "GET "+targetTrackAPIPath, "POST "+targetTrackAPIPath)

	// 常量与字面量对账：路由用的是 RoutePath（组内相对），Casbin 看的是全路径。
	// ⛔ 这两个常量一旦不一致，表现是「接口通但恒 403」而且两侧都不报错。
	require.Equal(t, targetTrackRoutePath, gbmodels.TargetTrackRoutePath)
	require.Equal(t, targetTrackAPIPath, gbmodels.TargetTrackAPIPath)
}

// ⛔ 写权限不能由读权限顺带获得：两者同路径，所以"只有 method 不同"这件事
// 必须在 Casbin 层真的成立。这条用例在权限表被误改成 `*`（任意方法）时会红。
func TestTargetTrackWriteIsNotCoveredByReadPermission(t *testing.T) {
	m, err := model.NewModelFromString(videoParamCasbinModel)
	require.NoError(t, err)
	enforcer, err := casbin.NewEnforcer(m)
	require.NoError(t, err)

	_, err = enforcer.AddGroupingPolicy("user_1", "role_2", "*")
	require.NoError(t, err)
	// 只给读权限（迁移里读绑 gb28181:ptz:view 后落成的规则形态）。
	_, err = enforcer.AddPolicy("role_2", targetTrackAPIPath, "GET", "*")
	require.NoError(t, err)

	allowed, err := enforcer.Enforce("user_1", "/api/gb28181/device-mgmt/channel/7/target-track", "GET", "*")
	require.NoError(t, err)
	require.True(t, allowed, "读权限应能读到本通道的目标跟踪意图")

	allowed, err = enforcer.Enforce("user_1", "/api/gb28181/device-mgmt/channel/7/target-track", "POST", "*")
	require.NoError(t, err)
	require.False(t, allowed, "读权限绝不能被当成写权限（写会真的操作设备）")

	// 补上写权限（迁移里写绑 gb28181:ptz:control 后落成的规则形态）后才放行。
	_, err = enforcer.AddPolicy("role_2", targetTrackAPIPath, "POST", "*")
	require.NoError(t, err)
	allowed, err = enforcer.Enforce("user_1", "/api/gb28181/device-mgmt/channel/7/target-track", "POST", "*")
	require.NoError(t, err)
	require.True(t, allowed, "写权限应能下发目标跟踪")
}

// 相邻路径不得互相串权：`:id` 段是 keyMatch2 通配，但末段不同 ⇒
// 拿到别的通道子资源（这里是存储卡）的权限，不该顺带拿到目标跟踪的写权限。
func TestTargetTrackPathDoesNotInheritSiblingSubResource(t *testing.T) {
	m, err := model.NewModelFromString(videoParamCasbinModel)
	require.NoError(t, err)
	enforcer, err := casbin.NewEnforcer(m)
	require.NoError(t, err)

	_, err = enforcer.AddGroupingPolicy("user_1", "role_2", "*")
	require.NoError(t, err)
	_, err = enforcer.AddPolicy("role_2", "/api/gb28181/device-mgmt/channel/:id/storage-cards", "POST", "*")
	require.NoError(t, err)

	allowed, err := enforcer.Enforce("user_1", "/api/gb28181/device-mgmt/channel/7/target-track", "POST", "*")
	require.NoError(t, err)
	require.False(t, allowed, "存储卡格式化权限不应顺带覆盖目标跟踪下发")
}
