package migration

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 2026-11-03：设备详情「上报字符集」三列（reported_charset / wire_charset /
// reported_charset_at）。锁四件事：四方言文件齐全且登记、老库能补上、
// 新装基线库不重复执行、只读展示列不参与任何约束。
var deviceReportedCharsetMigrations = []struct {
	dialect Dialect
	up      string
	down    string
}{
	{DialectMySQL, "2026-11-03-device-reported-charset.sql", "2026-11-03-device-reported-charset-down.sql"},
	{DialectPostgres, "2026-11-03-device-reported-charset-postgresql.sql", "2026-11-03-device-reported-charset-postgresql-down.sql"},
	{DialectSQLServer, "2026-11-03-device-reported-charset-sqlserver.sql", "2026-11-03-device-reported-charset-sqlserver-down.sql"},
	{DialectSQLite, "2026-11-03-device-reported-charset-sqlite.sql", "2026-11-03-device-reported-charset-sqlite-down.sql"},
}

func TestDeviceReportedCharsetMigrationsCoverEveryDialect(t *testing.T) {
	for _, item := range deviceReportedCharsetMigrations {
		t.Run(string(item.dialect), func(t *testing.T) {
			up, err := (&embedSource{dialect: item.dialect}).ReadSQL(item.up)
			require.NoError(t, err)
			for _, column := range []string{"reported_charset", "wire_charset", "reported_charset_at"} {
				require.Contains(t, up, column, item.up+" 必须含 "+column)
			}
			require.Contains(t, up, "gb_device")
			// 登记表必须有它：空版本表的基线库靠列探测判「已应用」。
			require.Equal(t, "gb_device", baselineColumnMigrations[item.up].table)
			require.Equal(t, "reported_charset", baselineColumnMigrations[item.up].column)
			_, err = (&embedSource{dialect: item.dialect}).ReadSQL(item.down)
			require.NoError(t, err)
		})
	}
}

// 存量库（缺三列）跑一遍 SQLite 迁移后，三列都在且可写入。
func TestDeviceReportedCharsetColumnsAddedToLegacySQLiteDatabase(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })

	require.NoError(t, db.Exec(`CREATE TABLE "gb_device" (
		"id" INTEGER PRIMARY KEY AUTOINCREMENT,
		"device_id" TEXT NOT NULL
	)`).Error)

	executor := &dbExecutor{db: db}
	for _, item := range deviceReportedCharsetMigrations {
		if item.dialect != DialectSQLite {
			continue
		}
		up, err := (&embedSource{dialect: DialectSQLite}).ReadSQL(item.up)
		require.NoError(t, err)
		require.NoError(t, executor.ExecSQL(up), item.up)
	}
	for _, column := range []string{"reported_charset", "wire_charset", "reported_charset_at"} {
		require.True(t, db.Migrator().HasColumn("gb_device", column), column)
	}
	require.NoError(t, db.Exec(`INSERT INTO "gb_device" ("device_id","reported_charset","wire_charset")
		VALUES ('34020000002000000001','GB2312','UTF-8')`).Error)
}

// 新装基线库已有三列 ⇒ runner 必须判定「不需要执行」；反向删列后判定必须变 true。
func TestDeviceReportedCharsetColumnsSkipWhenBaselineAlreadyHasThem(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })

	probe := schemaProbe{
		hasTable:  func(table string) (bool, error) { return db.Migrator().HasTable(table), nil },
		hasColumn: func(table, column string) (bool, error) { return db.Migrator().HasColumn(table, column), nil },
	}
	require.NoError(t, db.Exec(`CREATE TABLE "gb_device" (
		"id" INTEGER PRIMARY KEY AUTOINCREMENT,
		"device_id" TEXT NOT NULL,
		"reported_charset" TEXT NOT NULL DEFAULT '',
		"wire_charset" TEXT NOT NULL DEFAULT '',
		"reported_charset_at" DATETIME NULL
	)`).Error)

	needed, err := needsBaselineColumnUpgrade("2026-11-03-device-reported-charset-sqlite.sql", probe)
	require.NoError(t, err)
	require.False(t, needed, "baseline 已含三列，不应重复执行")

	// 反向：删掉登记列，判定必须变 true（证明上面的 false 不是恒假）。
	require.NoError(t, db.Exec(`ALTER TABLE "gb_device" DROP COLUMN "reported_charset"`).Error)
	needed, err = needsBaselineColumnUpgrade("2026-11-03-device-reported-charset-sqlite.sql", probe)
	require.NoError(t, err)
	require.True(t, needed)
}
