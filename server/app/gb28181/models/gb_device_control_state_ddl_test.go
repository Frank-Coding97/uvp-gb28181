package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// deviceControlFactsColumns 是设备自报事实列 —— 服务 DeviceStatus 应答里
// Online / Status / Encode / DeviceTime 四项，以及设备声明的报警输入数量。
//
// 这五列**全部可空，而且可空本身就是语义**：NULL 表示"设备这次应答没有这一项"，
// 而 alarm_input_count=0 表示"设备明确报了自己一个报警输入都没有"。
// 前者是未知，后者是已知的能力缺失，界面上是两句不同的话，落库时不能塌成同一个值。
var deviceControlFactsColumns = []string{
	"online_state",
	"selftest_state",
	"encode_state",
	"device_time",
	"alarm_input_count",
}

// addColumnForm / dropColumnForm 返回本方言「加列 / 删列」的字面写法。
// 用它而不是裸列名，是为了证明列名真的出现在 DDL 里，而不是只出现在 SQL 注释里。
type columnDialect struct {
	name, up, down string
	add, drop      func(column string) string
}

func deviceControlFactsDialects() []columnDialect {
	return []columnDialect{
		{
			name: "mysql",
			up:   "2026-09-19-z-device-status-facts.sql",
			down: "2026-09-19-z-device-status-facts-down.sql",
			add:  func(c string) string { return "add column `" + c + "`" },
			drop: func(c string) string { return "drop column `" + c + "`" },
		},
		{
			name: "postgresql",
			up:   "2026-09-19-z-device-status-facts-postgresql.sql",
			down: "2026-09-19-z-device-status-facts-postgresql-down.sql",
			add:  func(c string) string { return "add column if not exists " + c },
			drop: func(c string) string { return "drop column if exists " + c },
		},
		{
			name: "sqlserver",
			up:   "2026-09-19-z-device-status-facts-sqlserver.sql",
			down: "2026-09-19-z-device-status-facts-sqlserver-down.sql",
			add:  func(c string) string { return "add [" + c + "]" },
			drop: func(c string) string { return "drop column [" + c + "]" },
		},
	}
}

func TestDeviceControlFactsDDLIsAvailableForEverySupportedDatabase(t *testing.T) {
	root := dualVersionDDLRoot(t)
	migrations := filepath.Join(root, "resource", "database", "gb28181", "migrations")

	for _, dialect := range deviceControlFactsDialects() {
		t.Run(dialect.name, func(t *testing.T) {
			upBody, err := os.ReadFile(filepath.Join(migrations, dialect.up))
			require.NoErrorf(t, err, "迁移文件缺失: %s", dialect.up)
			up := strings.ToLower(string(upBody))
			require.Contains(t, up, "gb_device_control_state", "%s 未涉及落库表", dialect.up)
			for _, column := range deviceControlFactsColumns {
				require.Containsf(t, up, dialect.add(column),
					"%s 没有真的加列 %s", dialect.up, column)
			}

			downBody, err := os.ReadFile(filepath.Join(migrations, dialect.down))
			require.NoErrorf(t, err, "迁移文件缺失: %s", dialect.down)
			down := strings.ToLower(string(downBody))
			for _, column := range deviceControlFactsColumns {
				require.Containsf(t, down, dialect.drop(column), "%s 没有回收列 %s", dialect.down, column)
			}
		})
	}
}

// 三个全量快照都必须带上这五列。理由与 TestVideoParamPresentInEverySnapshot 相同：
// runner 在「空版本表 + 基线探测表已存在」时把全部迁移直接标记为已应用而**不执行**，
// 所以快照里没有的列，在快照建出来的新库上永远不会出现 —— 后端 gorm 读它就会报错。
func TestDeviceControlFactsPresentInEverySnapshot(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, snapshot := range []struct {
		file string
		form func(column string) string
	}{
		{file: "uvp-gb28181.sql", form: func(c string) string { return "`" + c + "`" }},
		{file: "postgresql_converted.sql", form: func(c string) string { return c }},
		{file: "sqlserver_converted.sql", form: func(c string) string { return "[" + c + "]" }},
	} {
		t.Run(snapshot.file, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, "resource", "database", snapshot.file))
			require.NoError(t, err)
			text := strings.ToLower(string(body))

			start := strings.Index(text, "gb_device_control_state")
			require.GreaterOrEqual(t, start, 0, "%s 里连 gb_device_control_state 都没有", snapshot.file)
			end := start + 1800
			if end > len(text) {
				end = len(text)
			}
			definition := text[start:end]

			for _, column := range deviceControlFactsColumns {
				pos := strings.Index(definition, snapshot.form(column))
				require.GreaterOrEqualf(t, pos, 0, "%s 的表定义缺少列 %s", snapshot.file, column)

				lineEnd := strings.Index(definition[pos:], "\n")
				line := definition[pos:]
				if lineEnd >= 0 {
					line = line[:lineEnd]
				}
				require.NotContainsf(t, line, "not null",
					"%s 的 %s 被写成了 NOT NULL；NULL 本身就是语义（设备没报这一项）", snapshot.file, column)
			}
		})
	}
}
