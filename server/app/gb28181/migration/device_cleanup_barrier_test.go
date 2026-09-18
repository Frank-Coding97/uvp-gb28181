package migration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

const deviceCleanupBarrierBase = "2026-09-06-device-cleanup-barrier"

var deviceCleanupBarrierMigrationFiles = []struct {
	dialect Dialect
	up      string
	down    string
}{
	{dialect: DialectMySQL, up: deviceCleanupBarrierBase + ".sql", down: deviceCleanupBarrierBase + "-down.sql"},
	{dialect: DialectPostgres, up: deviceCleanupBarrierBase + "-postgresql.sql", down: deviceCleanupBarrierBase + "-postgresql-down.sql"},
	{dialect: DialectSQLServer, up: deviceCleanupBarrierBase + "-sqlserver.sql", down: deviceCleanupBarrierBase + "-sqlserver-down.sql"},
}

func TestDeviceCleanupBarrierMigrationFiles(t *testing.T) {
	for _, target := range deviceCleanupBarrierMigrationFiles {
		t.Run(target.up, func(t *testing.T) {
			up, err := migrationsfs.FS.ReadFile("migrations/" + target.up)
			require.NoError(t, err)
			normalized := normalizeDeviceCleanupSQL(string(up))

			require.Contains(t, normalized, "cleanup_completed_epoch")
			require.Contains(t, normalized, "access_epoch")
			require.Contains(t, normalized, "default 1")
			require.Contains(t, normalized, "cleanup_completed_epoch > 0")
			require.Contains(t, normalized, "cleanup_completed_epoch <= access_epoch")
			require.NotContains(t, normalized, "update gb_device")
			require.NotContains(t, normalized, "insert into gb_device")
			require.NotContains(t, normalized, "drop table")
			require.NotContains(t, normalized, "drop column")
			require.Less(t,
				strings.Index(normalized, "access_epoch"),
				strings.Index(normalized, "cleanup_completed_epoch"),
				"access_epoch 必须先于清退完成水位被确认/使用",
			)

			switch target.dialect {
			case DialectMySQL:
				require.Contains(t, normalized, "information_schema.columns")
			case DialectPostgres:
				require.Contains(t, normalized, "if not exists")
				require.Contains(t, normalized, "raise exception")
			case DialectSQLServer:
				require.Contains(t, normalized, "col_length")
				require.Contains(t, normalized, "throw")
			}
		})

		t.Run(target.down, func(t *testing.T) {
			down, err := migrationsfs.FS.ReadFile("migrations/" + target.down)
			require.NoError(t, err)
			normalized := normalizeDeviceCleanupSQL(string(down))
			require.Contains(t, normalized, "select 1")
			for _, forbidden := range []string{
				"drop table",
				"drop column",
				"alter table",
				"delete from",
				"truncate",
				"update ",
			} {
				require.NotContains(t, normalized, forbidden, "清退屏障 down 不得删除或降低安全水位")
			}
		})
	}
}

func TestDeviceCleanupBarrierFreshBaselinesContainBarrier(t *testing.T) {
	for _, name := range []string{
		"uvp-gb28181.sql",
		"postgresql_converted.sql",
		"sqlserver_converted.sql",
	} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", name))
			require.NoError(t, err)
			normalized := normalizeDeviceCleanupSQL(string(body))
			require.Contains(t, normalized, "cleanup_completed_epoch")
			require.Contains(t, normalized, "default 1")
			require.Contains(t, normalized, "cleanup_completed_epoch > 0")
			require.Contains(t, normalized, "cleanup_completed_epoch <= access_epoch")
			require.Less(t,
				strings.Index(normalized, "access_epoch"),
				strings.Index(normalized, "cleanup_completed_epoch"),
				"完整初始化必须先建立 requested 水位再建立 completed 水位约束",
			)
		})
	}
}

func TestDeviceCleanupBarrierSQLiteSemantics(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:device-cleanup-barrier?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, db.Exec(`CREATE TABLE gb_device (
		id INTEGER PRIMARY KEY
	)`).Error)
	require.Error(t, applySQLiteDeviceCleanupBarrier(db), "缺少 access_epoch 时必须 fail closed")
	var columnCount int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM pragma_table_info('gb_device') WHERE name = ?", "cleanup_completed_epoch").Scan(&columnCount).Error)
	require.Zero(t, columnCount, "先决字段缺失时不得创建 completed 水位")

	// SQLite 演练表重新建立为与三种生产方言相同的约束形状。
	require.NoError(t, db.Exec("DROP TABLE gb_device").Error)
	require.NoError(t, db.Exec(`CREATE TABLE gb_device (
		id INTEGER PRIMARY KEY,
		access_epoch INTEGER NOT NULL DEFAULT 1 CHECK (access_epoch > 0)
	)`).Error)
	require.NoError(t, applySQLiteDeviceCleanupBarrier(db))
	require.NoError(t, applySQLiteDeviceCleanupBarrier(db), "重复 up 必须是幂等空操作")

	var completed int64
	require.NoError(t, db.Exec("INSERT INTO gb_device (id) VALUES (1)").Error)
	require.NoError(t, db.Raw("SELECT cleanup_completed_epoch FROM gb_device WHERE id = 1").Scan(&completed).Error)
	require.Equal(t, int64(1), completed, "新增设备的 completed 水位默认从 epoch 1 开始")
	require.NoError(t, db.Exec("INSERT INTO gb_device (id, access_epoch) VALUES (2, 7)").Error)
	require.NoError(t, db.Raw("SELECT cleanup_completed_epoch FROM gb_device WHERE id = 2").Scan(&completed).Error)
	require.Equal(t, int64(1), completed, "旧 access_epoch>1 只能保持 pending，不能伪造已清场")

	var access, pending int64
	row := db.Raw("SELECT access_epoch, cleanup_completed_epoch FROM gb_device WHERE id = 2").Row()
	require.NoError(t, row.Scan(&access, &pending))
	require.Greater(t, access, pending)
	require.NoError(t, db.Exec("UPDATE gb_device SET cleanup_completed_epoch = access_epoch WHERE id = 2").Error)
	require.Error(t, db.Exec("UPDATE gb_device SET cleanup_completed_epoch = 0 WHERE id = 2").Error)
	require.Error(t, db.Exec("UPDATE gb_device SET cleanup_completed_epoch = access_epoch + 1 WHERE id = 2").Error)
	require.Error(t, db.Exec("UPDATE gb_device SET cleanup_completed_epoch = NULL WHERE id = 2").Error)
	require.NoError(t, db.Exec("UPDATE gb_device SET cleanup_completed_epoch = 1 WHERE id = 2").Error)

	var state string
	require.NoError(t, db.Raw(`SELECT CASE
		WHEN cleanup_completed_epoch IS NULL OR cleanup_completed_epoch <= 0 OR cleanup_completed_epoch > access_epoch THEN 'corrupt'
		WHEN cleanup_completed_epoch = access_epoch THEN 'open'
		ELSE 'pending'
	END FROM gb_device WHERE id = 2`).Scan(&state).Error)
	require.Equal(t, "pending", state)
}

func applySQLiteDeviceCleanupBarrier(db *gorm.DB) error {
	var accessColumns, cleanupColumns int64
	if err := db.Raw("SELECT COUNT(*) FROM pragma_table_info('gb_device') WHERE name = ?", "access_epoch").Scan(&accessColumns).Error; err != nil {
		return err
	}
	if accessColumns != 1 {
		return errors.New("device cleanup barrier requires gb_device.access_epoch")
	}
	if err := db.Raw("SELECT COUNT(*) FROM pragma_table_info('gb_device') WHERE name = ?", "cleanup_completed_epoch").Scan(&cleanupColumns).Error; err != nil {
		return err
	}
	if cleanupColumns == 1 {
		return nil
	}
	return db.Exec("ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch INTEGER NOT NULL DEFAULT 1 CHECK (cleanup_completed_epoch > 0 AND cleanup_completed_epoch <= access_epoch)").Error
}

func normalizeDeviceCleanupSQL(sql string) string {
	var codeLines []string
	for _, line := range strings.Split(sql, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		codeLines = append(codeLines, line)
	}
	withoutDialectQuotes := strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.Join(codeLines, "\n"))
	return strings.Join(strings.Fields(strings.ToLower(withoutDialectQuotes)), " ")
}

func TestDeviceCleanupBarrierNoDownMutation(t *testing.T) {
	for _, target := range deviceCleanupBarrierMigrationFiles {
		down, err := migrationsfs.FS.ReadFile("migrations/" + target.down)
		require.NoError(t, err)
		code := normalizeDeviceCleanupSQL(string(down))
		require.Equal(t, "select 1;", strings.TrimSpace(code), fmt.Sprintf("%s down 只允许 forward-only no-op", target.down))
	}
}
