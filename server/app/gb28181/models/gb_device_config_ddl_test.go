package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// deviceConfigColumns 是 gb_device_config 的全部列。
//
// 前四列是定位键（设备 / 目标编码 / 配置类型）与载荷；中间四列是迟到应答保护
// （source_operation_seq / source_sn / source_operation_id）与观测时刻；最后三列是元数据。
var deviceConfigColumns = []string{
	"id",
	"device_id",
	"target_code",
	"config_type",
	"payload_json",
	"source_operation_seq",
	"source_sn",
	"source_operation_id",
	"observed_at",
	"raw_summary",
	"created_at",
	"updated_at",
}

// deviceConfigAPIPath 是配置家族的读/写接口路径（与迁移、控制器、路由三处必须同名）。
const deviceConfigAPIPath = "/api/gb28181/device-mgmt/channel/:id/device-configs"

// TestDeviceConfigDDLIsAvailableForEverySupportedDatabase 三方言迁移都要能建出这张表，
// 且各自的幂等守卫、权限三件事齐全。
func TestDeviceConfigDDLIsAvailableForEverySupportedDatabase(t *testing.T) {
	root := dualVersionDDLRoot(t)
	migrations := filepath.Join(root, "resource", "database", "gb28181", "migrations")

	for _, migration := range []struct {
		name, up, down, guard string
	}{
		{"mysql", "2026-09-19-device-config-family.sql", "2026-09-19-device-config-family-down.sql", "if not exists"},
		{"postgresql", "2026-09-19-device-config-family-postgresql.sql", "2026-09-19-device-config-family-postgresql-down.sql", "if not exists"},
		{"sqlserver", "2026-09-19-device-config-family-sqlserver.sql", "2026-09-19-device-config-family-sqlserver-down.sql", "col_length"},
	} {
		t.Run(migration.name, func(t *testing.T) {
			upBody, err := os.ReadFile(filepath.Join(migrations, migration.up))
			require.NoErrorf(t, err, "迁移文件缺失: %s", migration.up)
			upLower := strings.ToLower(string(upBody))

			require.Contains(t, upLower, "gb_device_config", "%s 未建落库表", migration.up)
			for _, column := range deviceConfigColumns {
				require.Containsf(t, upLower, column, "%s 未涉及列 %s", migration.up, column)
			}
			require.Contains(t, upLower, "uk_device_config_target", "%s 缺少三元组唯一键", migration.up)

			// ⛔ sqlserver 那份的守卫是 OBJECT_ID，不含 "if not exists" 字样，
			// 所以它对 guard 的期望是大小写归一后的 "object_id"。
			guard := migration.guard
			if migration.name == "sqlserver" {
				guard = "object_id"
			}
			require.Containsf(t, upLower, guard, "%s 缺少幂等守卫 %s", migration.up, guard)

			// 读接口绑 gb28181:ptz:view、写接口绑 gb28181:ptz:control —— 两者都必须出现，
			// 且都必须走 sys_menu_api 精确绑定 + sys_casbin_rule 去重。
			for _, token := range []string{
				deviceConfigAPIPath,
				"gb28181:ptz:view",
				"gb28181:ptz:control",
				"sys_menu_api",
				"sys_casbin_rule",
				"select distinct 'p'",
				"not exists",
			} {
				require.Contains(t, upLower, token, "%s 权限段落缺少 %s", migration.up, token)
			}

			downBody, err := os.ReadFile(filepath.Join(migrations, migration.down))
			require.NoErrorf(t, err, "迁移文件缺失: %s", migration.down)
			downLower := strings.ToLower(string(downBody))
			require.Contains(t, downLower, "deleted_at", "%s 必须用软删除语义回收 API", migration.down)
			require.Contains(t, downLower, "sys_menu_api", migration.down)
			require.Contains(t, downLower, "sys_casbin_rule", migration.down)
			require.Contains(t, downLower, deviceConfigAPIPath, migration.down)
			require.Contains(t, downLower, "gb_device_config", "%s 未删落库表", migration.down)
		})
	}
}

// TestDeviceConfigPresentInEverySnapshot 三个全量快照都必须带上「本表 + 本接口权限」，
// 理由是 runner 的基线语义：
//
//	空版本表 + 基线探测表已存在 ⇒ 全部迁移被直接标记为已应用而**不执行**。
//
// 所以快照里没有的物件在快照建出来的新库上**永远不会出现**，增量迁移补不回来。
// 这条不是风格问题，是新建库能不能用的问题。
func TestDeviceConfigPresentInEverySnapshot(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, file := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(file, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, "resource", "database", file))
			require.NoError(t, err)
			text := strings.ToLower(string(body))

			require.Containsf(t, text, "gb_device_config",
				"%s 缺少 gb_device_config 建表；空版本表会让增量迁移被整体跳过", file)
			require.Containsf(t, text, deviceConfigAPIPath,
				"%s 缺少配置家族接口注册；空版本表会让增量迁移被整体跳过", file)
			require.Containsf(t, text, "gb28181:ptz:view", "%s 缺少读接口权限绑定", file)
			require.Containsf(t, text, "gb28181:ptz:control", "%s 缺少写接口权限绑定", file)

			for _, column := range []string{"payload_json", "config_type", "target_code"} {
				require.Containsf(t, text, column, "%s 缺少列 %s", file, column)
			}
		})
	}
}
