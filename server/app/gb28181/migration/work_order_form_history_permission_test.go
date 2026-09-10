package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// form-history 接口（GET /api/gb28181/work-orders/form-history）必须挂到 view 权限下，
// 弹窗打开就拉历史，语义与列表查询同源。
const formHistoryPermissionMigration = "2026-09-10-zzzzzz-work-order-form-history-permission"

func TestWorkOrderFormHistoryPermissionRegistersAPIAndCasbinRule(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		t.Run(suffix, func(t *testing.T) {
			up := strings.ToLower(readWorkRecordingPermissionMigration(t, formHistoryPermissionMigration+suffix+".sql"))
			down := strings.ToLower(readWorkRecordingPermissionMigration(t, formHistoryPermissionMigration+suffix+"-down.sql"))

			require.Contains(t, up, "/api/gb28181/work-orders/form-history", "必须注册 form-history API 路径")
			require.Contains(t, up, "get", "form-history 接口是 GET")
			require.Contains(t, up, "gb28181:work-order:view", "必须挂到 view 权限的 sys_menu_api")
			require.Contains(t, up, "role_1", "管理员必须立刻能用 form-history")
			require.Contains(t, up, "not exists", "幂等（MySQL/PG 用 where not exists，SQL Server 用 if not exists/not exists 守卫）")
			require.Contains(t, up, "select distinct 'p'", "Casbin 写入前必须去重")
			require.Contains(t, up, "sys_api", "从 sys_api 取接口 id（带/不带反引号都匹配）")
			require.NotContains(t, up, "cross join sys_api", "禁止笛卡尔积")
			// down fail closed：与 form-history 表迁移风格一致
			require.NotContains(t, down, "delete")
			require.NotContains(t, down, "update")
			require.Contains(t, down, "automatic schema rollback is disabled")
			switch suffix {
			case "":
				require.Contains(t, down, "signal sqlstate '45000'")
			case "-postgresql":
				require.Contains(t, down, "raise exception")
			case "-sqlserver":
				require.Contains(t, down, "throw 51000")
			}
		})
	}
}

func TestWorkOrderFormHistoryPermissionSnapshotsContainPath(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
			require.NoError(t, err)
			require.Contains(t, string(body), "/api/gb28181/work-orders/form-history")
		})
	}
}
