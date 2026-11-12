package routes

import (
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 与 stream-probe 的用例共用同一条 Casbin 模型（keyMatch2 匹配 :id 这类路径段）。
const videoParamCasbinModel = `
[request_definition]
r = sub, obj, act, dom

[policy_definition]
p = sub, obj, act, dom

[role_definition]
g = _, _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub, r.dom) && keyMatch2(r.obj, p.obj) && regexMatch(r.act, p.act) && (r.dom == p.dom || p.dom == "*")
`

const videoParamAPIPath = "/api/gb28181/device-mgmt/channel/:id/video-params"

func TestRegisterRoutesRegistersVideoParamReadAndWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))

	routes := make(map[string]struct{})
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}

	_, getRegistered := routes["GET "+videoParamAPIPath]
	_, postRegistered := routes["POST "+videoParamAPIPath]
	require.True(t, getRegistered, "视频参数读接口必须注册（ConfigDownload）")
	require.True(t, postRegistered, "视频参数写接口必须注册（DeviceConfig）")

	// 读写在**同一路径**上分方法：这是刻意的 —— 面板是"一个资源两种动作"，
	// 路径分裂只会让权限表里多出一条与语义无关的记录。
	// 反过来说，正因为同路径，权限必须靠 method 区分，见下面的用例。
}

// ⛔ 写权限不能由读权限顺带获得：两者同路径，所以"只有 method 不同"这件事
// 必须在 Casbin 层真的成立。这条用例在权限表被误改成 `*`（任意方法）时会红。
func TestVideoParamWriteIsNotCoveredByReadPermission(t *testing.T) {
	m, err := model.NewModelFromString(videoParamCasbinModel)
	require.NoError(t, err)
	enforcer, err := casbin.NewEnforcer(m)
	require.NoError(t, err)

	_, err = enforcer.AddGroupingPolicy("user_1", "role_2", "*")
	require.NoError(t, err)
	// 只给读权限（迁移里读绑 gb28181:ptz:view 后落成的规则形态）。
	_, err = enforcer.AddPolicy("role_2", videoParamAPIPath, "GET", "*")
	require.NoError(t, err)

	allowed, err := enforcer.Enforce("user_1", "/api/gb28181/device-mgmt/channel/7/video-params", "GET", "*")
	require.NoError(t, err)
	require.True(t, allowed, "读权限应能读到本通道的视频参数")

	allowed, err = enforcer.Enforce("user_1", "/api/gb28181/device-mgmt/channel/7/video-params", "POST", "*")
	require.NoError(t, err)
	require.False(t, allowed, "读权限绝不能被当成写权限（写入有副作用，会真的改设备配置）")

	// 补上写权限（迁移里写绑 gb28181:ptz:control 后落成的规则形态）后才放行。
	_, err = enforcer.AddPolicy("role_2", videoParamAPIPath, "POST", "*")
	require.NoError(t, err)
	allowed, err = enforcer.Enforce("user_1", "/api/gb28181/device-mgmt/channel/7/video-params", "POST", "*")
	require.NoError(t, err)
	require.True(t, allowed, "写权限应能下发视频参数")
}

// 同方法的相邻路径不得互相串权：:id 段是 keyMatch2 通配，
// 但路径末段不同，说明"不会因为前缀相同就顺手拿到别的通道子资源"。
func TestVideoParamPathDoesNotInheritSiblingSubResource(t *testing.T) {
	m, err := model.NewModelFromString(videoParamCasbinModel)
	require.NoError(t, err)
	enforcer, err := casbin.NewEnforcer(m)
	require.NoError(t, err)

	_, err = enforcer.AddGroupingPolicy("user_1", "role_2", "*")
	require.NoError(t, err)
	_, err = enforcer.AddPolicy("role_2", "/api/gb28181/device-mgmt/channel/:id/storage-cards", "GET", "*")
	require.NoError(t, err)

	allowed, err := enforcer.Enforce("user_1", "/api/gb28181/device-mgmt/channel/7/video-params", "GET", "*")
	require.NoError(t, err)
	require.False(t, allowed, "存储卡读取权限不应顺带覆盖视频参数读取")
}
