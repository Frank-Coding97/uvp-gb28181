package routes

import (
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const streamProbeCasbinModel = `
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

func TestRegisterRoutesUsesDedicatedStreamProbePath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))

	routes := make(map[string]struct{})
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}

	_, newPathRegistered := routes["POST /api/gb28181/stream-probes/:streamId"]
	_, oldPathRegistered := routes["POST /api/gb28181/play/:deviceId/probe"]
	require.True(t, newPathRegistered, "探针必须注册在独立路径")
	require.False(t, oldPathRegistered, "旧探针路径必须移除，避免被播放路径权限误匹配")
}

func TestStreamProbePathDoesNotReusePlaybackPermission(t *testing.T) {
	m, err := model.NewModelFromString(streamProbeCasbinModel)
	require.NoError(t, err)
	enforcer, err := casbin.NewEnforcer(m)
	require.NoError(t, err)

	_, err = enforcer.AddGroupingPolicy("user_1", "role_2", "*")
	require.NoError(t, err)
	_, err = enforcer.AddPolicy("role_2", "/api/gb28181/play/:deviceId/:channelId", "POST", "*")
	require.NoError(t, err)

	allowed, err := enforcer.Enforce("user_1", "/api/gb28181/play/device-1/probe", "POST", "*")
	require.NoError(t, err)
	require.True(t, allowed, "旧探针路径会被播放权限匹配，必须保留这个回归证据")

	allowed, err = enforcer.Enforce("user_1", "/api/gb28181/stream-probes/stream-1", "POST", "*")
	require.NoError(t, err)
	require.False(t, allowed, "新探针路径不应被播放权限匹配")

	_, err = enforcer.AddPolicy("role_2", "/api/gb28181/stream-probes/:streamId", "POST", "*")
	require.NoError(t, err)
	allowed, err = enforcer.Enforce("user_1", "/api/gb28181/stream-probes/stream-1", "POST", "*")
	require.NoError(t, err)
	require.True(t, allowed, "独立探针权限应能授权新路径")
}
