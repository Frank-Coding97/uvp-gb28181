package routes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// deviceConfigAPIPath 是本组接口在权限表里登记的 path —— 只在常量里写一次，
// 下面的用例同时用它跟 gin 真实注册的路由、以及迁移 SQL 里的字符串对账。
const deviceConfigAPIPath = "/api/gb28181/device-mgmt/channel/:id/device-configs"

func TestRegisterRoutesRegistersDeviceConfigReadAndWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))

	routes := make(map[string]struct{})
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}

	_, getRegistered := routes["GET "+deviceConfigAPIPath]
	_, postRegistered := routes["POST "+deviceConfigAPIPath]
	require.True(t, getRegistered, "设备配置读接口必须注册（A.2.4.7 ConfigDownload）")
	require.True(t, postRegistered, "设备配置写接口必须注册（A.2.3.2.5 DeviceConfig）")

	// ⛔ 与 video-params 是**两条独立路径**而不是同路径不同 method：
	// 两者落到两张表（gb_device_config / gb_device_video_param），对账粒度也不同
	// （前者按 config_type 一行，后者按码流一条）。共用路径会让权限表里出现
	// 一条语义含糊的记录，将来想给其中一条单独收权都做不到。
	_, videoParamRegistered := routes["GET "+videoParamAPIPath]
	require.True(t, videoParamRegistered, "video-params 与 device-configs 必须并存，不能互相取代")
}

// ⛔ 写权限不能由读权限顺带获得。与 video-params 同构，但**不能省**：
// 本组写接口会真的改设备里的 OSD / 遮挡 / 录像计划，误放行的后果比读设备事实严重得多。
func TestDeviceConfigWriteIsNotCoveredByReadPermission(t *testing.T) {
	m, err := model.NewModelFromString(videoParamCasbinModel)
	require.NoError(t, err)
	enforcer, err := casbin.NewEnforcer(m)
	require.NoError(t, err)

	_, err = enforcer.AddGroupingPolicy("user_1", "role_2", "*")
	require.NoError(t, err)
	// 只给读权限（迁移里读绑 gb28181:ptz:view 后落成的规则形态）。
	_, err = enforcer.AddPolicy("role_2", deviceConfigAPIPath, "GET", "*")
	require.NoError(t, err)

	allowed, err := enforcer.Enforce("user_1", "/api/gb28181/device-mgmt/channel/7/device-configs", "GET", "*")
	require.NoError(t, err)
	require.True(t, allowed, "读权限应能读到本通道的设备配置")

	allowed, err = enforcer.Enforce("user_1", "/api/gb28181/device-mgmt/channel/7/device-configs", "POST", "*")
	require.NoError(t, err)
	require.False(t, allowed, "读权限绝不能被当成写权限（写入有副作用，会真的改设备配置）")

	// 补上写权限（迁移里写绑 gb28181:ptz:control 后落成的规则形态）后才放行。
	_, err = enforcer.AddPolicy("role_2", deviceConfigAPIPath, "POST", "*")
	require.NoError(t, err)
	allowed, err = enforcer.Enforce("user_1", "/api/gb28181/device-mgmt/channel/7/device-configs", "POST", "*")
	require.NoError(t, err)
	require.True(t, allowed, "写权限应能下发设备配置")
}

// 相邻子资源不得互相串权：:id 段是 keyMatch2 通配，但末段不同就是不同资源。
// 这里特意选 video-params 当"邻居"（两者权限档位相同、前缀完全相同），
// 说明即便权限档一样，也**不会**因为前缀相同而顺手拿到另一条链路的读写。
func TestDeviceConfigPathDoesNotInheritSiblingSubResource(t *testing.T) {
	m, err := model.NewModelFromString(videoParamCasbinModel)
	require.NoError(t, err)
	enforcer, err := casbin.NewEnforcer(m)
	require.NoError(t, err)

	_, err = enforcer.AddGroupingPolicy("user_1", "role_2", "*")
	require.NoError(t, err)
	_, err = enforcer.AddPolicy("role_2", videoParamAPIPath, "GET", "*")
	require.NoError(t, err)

	allowed, err := enforcer.Enforce("user_1", "/api/gb28181/device-mgmt/channel/7/device-configs", "GET", "*")
	require.NoError(t, err)
	require.False(t, allowed, "视频参数的读权限不应顺带覆盖设备配置读取")
}

// ⛔ 路径漂移守卫：迁移里的 sys_api.path 是**字面量字符串**，gin 那边是注册时拼出来的，
// 两边一旦不一致，权限表就绑定到一个不存在的 path 上 —— 后果是接口 403，
// 而单看任何一边都发现不了（迁移语法合法、路由也注册了）。
// 所以这里把迁移 SQL 当数据读进来，直接比对字符串。
func TestDeviceConfigAPIPathMatchesPermissionMigration(t *testing.T) {
	migrationPath := filepath.Join(
		"..", "..", "..", "resource", "database", "gb28181", "migrations",
		"2026-09-19-device-config-family.sql",
	)
	raw, err := os.ReadFile(migrationPath)
	require.NoError(t, err, "找不到 device-config-family 迁移：%s", migrationPath)
	sql := string(raw)

	require.Contains(t, sql, "'"+deviceConfigAPIPath+"'",
		"迁移里的 API path 必须与 routes.go 注册的路径逐字符一致")

	// 同一份迁移里读/写各自在 sys_api 登记一次。
	//
	// ⛔ 数 `'<path>','<METHOD>'` 这个**成对字面量**，而不是数裸路径：
	// 路径在同一条 INSERT 里会出现两次（SELECT 的字面量 + NOT EXISTS 的守卫条件），
	// 数裸路径只会得到一个随语句形态变化的数字（这里是 8），断言不出真问题。
	// 成对形态则一一对应"sys_api 里该有且仅有一行 (path, method)"。
	require.Equal(t, 1, strings.Count(sql, "'"+deviceConfigAPIPath+"','GET'"),
		"读接口应在 sys_api 登记且仅登记一次（GET）")
	require.Equal(t, 1, strings.Count(sql, "'"+deviceConfigAPIPath+"','POST'"),
		"写接口应在 sys_api 登记且仅登记一次（POST）")

	readBlock, writeBlock := splitDeviceConfigPermissionBlocks(t, sql)
	require.Contains(t, readBlock, "'GET'")
	require.Contains(t, readBlock, "gb28181:ptz:view")
	require.NotContains(t, stripSQLLineComments(readBlock), "gb28181:ptz:control",
		"读接口不能绑写权限（会顺带把设备配置的写能力发出去）")

	require.Contains(t, writeBlock, "'POST'")
	require.Contains(t, writeBlock, "gb28181:ptz:control")
	require.NotContains(t, stripSQLLineComments(writeBlock), "gb28181:ptz:view",
		"写接口必须单独绑 control，不能靠 view 放行")
}

// stripSQLLineComments 丢掉 `--` 行注释。
//
// ⛔ 必须去注释后再判"某权限码有没有出现"：迁移里恰恰有一句
// 「不沿用读接口的 gb28181:ptz:view」的说明性注释，直接 NotContains 会命中它，
// 于是测试要求删掉正确注释才能变绿 —— 把断言写成了反的。
func stripSQLLineComments(block string) string {
	lines := strings.Split(block, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// splitDeviceConfigPermissionBlocks 把迁移切成「读接口段」「写接口段」两半。
// 用迁移里那句 `-- ---- 读接口…` / `-- ---- 写接口…` 分隔注释定位。
//
// ⛔ 标记里**不要带冒号**：注释原文用的是全角「：」，写成半角会静默 Index=-1，
// 于是断言失败的原因看起来像"注释被删了"，其实是匹配串自己写错。
// 找到就失败 —— 注释被删掉说明这段结构被人动过，测试应当提醒而不是静默放过。
func splitDeviceConfigPermissionBlocks(t *testing.T, sql string) (string, string) {
	t.Helper()
	readMarker := "-- ---- 读接口"
	writeMarker := "-- ---- 写接口"
	readIndex := strings.Index(sql, readMarker)
	writeIndex := strings.Index(sql, writeMarker)
	require.GreaterOrEqual(t, readIndex, 0, "迁移里应保留「读接口」分隔注释")
	require.Greater(t, writeIndex, readIndex, "迁移里应保留「写接口」分隔注释且排在读之后")
	return sql[readIndex:writeIndex], sql[writeIndex:]
}
