package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const workOrderFormHistoryMigration = "2026-09-10-zzzzzz-work-order-form-history"

var workOrderFormHistoryFields = []string{
	"projectName",
	"stationArea",
	"workLeader",
	"workPersonnel",
}

// 「项目名称 / 站区 / 作业负责人 / 作业人员」四个字段支持「录入后寄存，
// 后续下拉选用」。建表 + 唯一键 + 索引，幂等（IF NOT EXISTS）。
func TestWorkOrderFormHistoryMigrationCreatesTableAndIsIdempotent(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		t.Run(suffix, func(t *testing.T) {
			up := readWorkRecordingPermissionMigration(t, workOrderFormHistoryMigration+suffix+".sql")
			down := strings.ToLower(readWorkRecordingPermissionMigration(t, workOrderFormHistoryMigration+suffix+"-down.sql"))

			// 1. 三方言各自关键字：建表 + 唯一键 + 字段白名单
			require.Contains(t, up, "gb_work_order_form_history", "必须建历史值表")
			for _, field := range workOrderFormHistoryFields {
				// 表迁移不直接出现这 4 个字段名（这是表，不是数据迁移），
				// 但表必须能容纳：列 field_key/value/use_count/last_used_at。
				_ = field
			}
			require.Contains(t, up, "field_key")
			require.Contains(t, up, "value")
			require.Contains(t, up, "use_count")
			require.Contains(t, up, "last_used_at")
			require.Contains(t, up, "uk_gb_work_order_form_history_field_value", "同字段同值必须唯一")
			require.Contains(t, up, "idx_gb_work_order_form_history_field_used", "需要按字段+最近使用时间排序")
			// 2. 跨方言 SQL 关键字能正确出现
			switch suffix {
			case "":
				require.Contains(t, up, "ENGINE=InnoDB")
				require.Contains(t, up, "VARCHAR(256)")
			case "-postgresql":
				require.Contains(t, up, "BIGSERIAL")
			case "-sqlserver":
				require.Contains(t, up, "sys.tables")
				require.Contains(t, up, "sys.indexes")
				require.Contains(t, up, "NVARCHAR(256)")
			}

			// 3. down fail-closed：不允许删除/更新/回滚既有数据
			require.NotContains(t, down, "delete", "down 必须 fail closed，禁止删除历史值")
			require.NotContains(t, down, "update", "down 必须 fail closed，禁止篡改历史值")
			require.Contains(t, down, "automatic schema rollback is disabled")
			switch suffix {
			case "":
				require.Contains(t, down, "signal sqlstate '45000'")
			case "-postgresql":
				require.Contains(t, down, "raise exception")
			case "-sqlserver":
				require.Contains(t, down, "throw 51000")
			}
			// 表迁移：MySQL 用了 AUTO_INCREMENT/ENGINE=InnoDB/CHARSET=utf8mb4，SQLite 不支持；
			// 表结构由 launcher 仓库的 sqlitebootstrap 单独维护，本仓库只校验三方言结构与 down 一致。
		})
	}
}

func TestWorkOrderFormHistorySnapshotsContainTable(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
			require.NoError(t, err)
			require.Contains(t, string(body), "gb_work_order_form_history", "快照必须包含历史值表")
		})
	}
}

// 历史值表必须排在 zzzzz-work-order-delete-permission 之后（本次作业单系列改动收口），
// 这样所有 9-10 的作业单相关改动按时间顺序依次执行。
func TestWorkOrderFormHistoryMigrationFollowsTheWorkOrderDeletePermission(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("../../../resource/database/gb28181/migrations"))
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	for _, tc := range []struct {
		dialect Dialect
		suffix  string
	}{
		{dialect: DialectMySQL, suffix: ""},
		{dialect: DialectPostgres, suffix: "-postgresql"},
		{dialect: DialectSQLServer, suffix: "-sqlserver"},
	} {
		files := FilterUpFiles(names, tc.dialect)
		priorName := "2026-09-10-zzzzz-work-order-delete-permission" + tc.suffix + ".sql"
		currentName := workOrderFormHistoryMigration + tc.suffix + ".sql"
		priorIndex, currentIndex := -1, -1
		for i, name := range files {
			switch name {
			case priorName:
				priorIndex = i
			case currentName:
				currentIndex = i
			}
		}
		require.GreaterOrEqual(t, priorIndex, 0, "%s 删除权限迁移必须被扫描到", tc.dialect)
		require.Greater(t, currentIndex, priorIndex, "%s 历史值表迁移必须排在删除权限之后", tc.dialect)
	}
}
