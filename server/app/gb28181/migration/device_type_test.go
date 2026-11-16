package migration

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 2026-11-16：设备类型（device_type）单列。锁四件事：四方言文件齐全且登记、
// 老库能补上且默认 IPC、新装基线库不重复执行、列可写入用户维护的值。
var deviceTypeMigrations = []struct {
	dialect Dialect
	up      string
	down    string
}{
	{DialectMySQL, "2026-11-16-device-type.sql", "2026-11-16-device-type-down.sql"},
	{DialectPostgres, "2026-11-16-device-type-postgresql.sql", "2026-11-16-device-type-postgresql-down.sql"},
	{DialectSQLServer, "2026-11-16-device-type-sqlserver.sql", "2026-11-16-device-type-sqlserver-down.sql"},
	{DialectSQLite, "2026-11-16-device-type-sqlite.sql", "2026-11-16-device-type-sqlite-down.sql"},
}

func TestDeviceTypeMigrationsCoverEveryDialect(t *testing.T) {
	for _, item := range deviceTypeMigrations {
		t.Run(string(item.dialect), func(t *testing.T) {
			up, err := (&embedSource{dialect: item.dialect}).ReadSQL(item.up)
			require.NoError(t, err)
			require.Contains(t, up, "device_type", item.up+" 必须含 device_type")
			require.Contains(t, up, "gb_device")
			// 默认值必须落在 SQL 里：存量行/新建行都该是 IPC，不能留空串。
			require.Contains(t, up, "ipc", item.up+" 必须带默认值 ipc")
			// 登记表必须有它：空版本表的基线库靠列探测判「已应用」。
			require.Equal(t, "gb_device", baselineColumnMigrations[item.up].table)
			require.Equal(t, "device_type", baselineColumnMigrations[item.up].column)
			_, err = (&embedSource{dialect: item.dialect}).ReadSQL(item.down)
			require.NoError(t, err)
		})
	}
}

// 存量库（缺列）跑一遍 SQLite 迁移后列存在、默认值生效、可改成用户维护的类型。
func TestDeviceTypeColumnAddedToLegacySQLiteDatabase(t *testing.T) {
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
	require.NoError(t, db.Exec(`INSERT INTO "gb_device" ("device_id") VALUES ('34020000002000000001')`).Error)

	executor := &dbExecutor{db: db}
	for _, item := range deviceTypeMigrations {
		if item.dialect != DialectSQLite {
			continue
		}
		up, err := (&embedSource{dialect: DialectSQLite}).ReadSQL(item.up)
		require.NoError(t, err)
		require.NoError(t, executor.ExecSQL(up), item.up)
	}
	require.True(t, db.Migrator().HasColumn("gb_device", "device_type"))

	// 存量行必须回填成 ipc，而不是空串。
	var existing string
	require.NoError(t, db.Raw(`SELECT "device_type" FROM "gb_device" WHERE "device_id" = '34020000002000000001'`).Scan(&existing).Error)
	require.Equal(t, "ipc", existing)

	require.NoError(t, db.Exec(`UPDATE "gb_device" SET "device_type" = 'nvr' WHERE "device_id" = '34020000002000000001'`).Error)
}

// 新装基线库已有该列 ⇒ runner 必须判定「不需要执行」；反向删列后判定必须变 true。
func TestDeviceTypeColumnSkipsWhenBaselineAlreadyHasIt(t *testing.T) {
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
		"device_type" TEXT NOT NULL DEFAULT 'ipc'
	)`).Error)

	needed, err := needsBaselineColumnUpgrade("2026-11-16-device-type-sqlite.sql", probe)
	require.NoError(t, err)
	require.False(t, needed, "baseline 已含 device_type，不应重复执行")

	// 反向：删掉登记列，判定必须变 true（证明上面的 false 不是恒假）。
	require.NoError(t, db.Exec(`ALTER TABLE "gb_device" DROP COLUMN "device_type"`).Error)
	needed, err = needsBaselineColumnUpgrade("2026-11-16-device-type-sqlite.sql", probe)
	require.NoError(t, err)
	require.True(t, needed)
}
