package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// T3 — 扫码接入权限迁移的 DDL 契约测试.
//
// 迁移文件没有自动 runner(项目 56 个迁移全靠人工/部署脚本执行),所以用文本
// 断言守住三件容易出错的事:三方言同步、幂等 guard、以及**免鉴权端点不得入表**。
// spec: wiki/projects/uvp-gb28181/specs/qr-sip-provisioning.md

const qrMigrationBase = "2026-07-26-sip-qr-provisioning"

// qrMigrationDialects 三种数据库方言的文件后缀
var qrMigrationDialects = map[string]string{
	"mysql":      qrMigrationBase + ".sql",
	"postgresql": qrMigrationBase + "-postgresql.sql",
	"sqlserver":  qrMigrationBase + "-sqlserver.sql",
}

func readQRMigration(t *testing.T, filename string) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "resource", "database", "gb28181", "migrations", filename)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "迁移文件必须存在: %s", filename)
	return string(data)
}

// 3.1 三份文件都存在且非空
func TestQRPermissionsMigration_AllDialectsPresent(t *testing.T) {
	for dialect, filename := range qrMigrationDialects {
		content := readQRMigration(t, filename)
		require.NotEmpty(t, strings.TrimSpace(content), "%s 迁移不能为空", dialect)
	}
}

// 3.6 复用既有权限点,不新增 —— spec D10
func TestQRPermissionsMigration_ReusesExistingPermission(t *testing.T) {
	for dialect, filename := range qrMigrationDialects {
		content := readQRMigration(t, filename)
		require.Contains(t, content, "gb28181:sip:config:view",
			"%s 必须关联到既有权限点", dialect)
	}
}

// 3.7 API 路径在三份里都出现
func TestQRPermissionsMigration_ContainsTokenEndpoint(t *testing.T) {
	for dialect, filename := range qrMigrationDialects {
		content := readQRMigration(t, filename)
		require.Contains(t, content, "/api/gb28181/sip/qr/token",
			"%s 必须注册 token 生成端点", dialect)
	}
}

// 3.8 exchange 路径不得出现在任何一份 —— 关键回归防线
//
// 兑换端点是免鉴权的(走 public 组不经 Casbin)。若有人"顺手"给它加 Casbin 规则,
// 设备端会直接 403,而这个错误在联调时很难定位到迁移文件。
func TestQRPermissionsMigration_ExcludesPublicEndpoints(t *testing.T) {
	for dialect, filename := range qrMigrationDialects {
		content := readQRMigration(t, filename)

		// 注释里会提到 exchange 说明为何不入表,所以只检查 INSERT 语句部分
		statements := stripSQLComments(content)
		require.NotContains(t, statements, "qr/exchange",
			"%s: 兑换端点免鉴权,不得写入权限表", dialect)
		require.NotContains(t, statements, "'/gb28181/qr'",
			"%s: 引导页免鉴权,不得写入权限表", dialect)
	}
}

// 3.9 幂等 guard 齐全
func TestQRPermissionsMigration_Idempotent(t *testing.T) {
	for dialect, filename := range qrMigrationDialects {
		content := readQRMigration(t, filename)
		upper := strings.ToUpper(content)
		require.Contains(t, upper, "NOT EXISTS",
			"%s 必须有幂等 guard,迁移要能重复执行", dialect)
	}
}

// 3.10 三张表都被写(sys_api / sys_menu_api / sys_casbin_rule)
func TestQRPermissionsMigration_TouchesRequiredTables(t *testing.T) {
	required := []string{"sys_api", "sys_menu_api", "sys_casbin_rule"}
	for dialect, filename := range qrMigrationDialects {
		content := readQRMigration(t, filename)
		for _, table := range required {
			require.Contains(t, content, table,
				"%s 必须写 %s", dialect, table)
		}
	}
}

// 不新增 sys_menu 权限行 —— 证明 D10 已落实
func TestQRPermissionsMigration_NoNewMenuPermission(t *testing.T) {
	for dialect, filename := range qrMigrationDialects {
		content := readQRMigration(t, filename)
		statements := stripSQLComments(content)
		require.NotContains(t, statements, "qr:generate",
			"%s: 本期复用 config:view,不新建独立权限点", dialect)
		require.NotContains(t, strings.ToUpper(statements), "INSERT INTO SYS_MENU ",
			"%s: 不应插入新的 sys_menu 行", dialect)
	}
}

// stripSQLComments 去掉 -- 行注释,只留可执行语句.
// 注释里会解释"为何 exchange 不入表",不应被当成违规内容。
func stripSQLComments(sql string) string {
	var b strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
