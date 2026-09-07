package migration

import (
	"context"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
	"uvplatform.cn/uvp-gb28181/internal/sqlitedialect"
)

func TestSQLiteMigrationFilesNeverFallThroughToMySQL(t *testing.T) {
	names := []string{"2026-09-08-a.sql", "2026-09-08-a-sqlite.sql", "2026-09-08-a-sqlite-down.sql", "2026-09-08-a-postgresql.sql", "2026-09-08-a-sqlserver.sql", "readme.md"}
	require.Equal(t, []string{"2026-09-08-a.sql"}, FilterUpFiles(names, DialectMySQL))
	require.Equal(t, []string{"2026-09-08-a-sqlite.sql"}, FilterUpFiles(names, Dialect("sqlite")))
	require.Empty(t, FilterUpFiles(names, DialectUnknown))
	require.Empty(t, FilterUpFiles(names, Dialect("unsupported")))
}

func TestSQLiteMigrationDialectIsExplicit(t *testing.T) {
	require.Equal(t, Dialect("sqlite"), DialectOf(sqlitedialect.Open(":memory:")))
}

func TestRunMigrationsRejectsUnknownDialect(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{}}
	require.ErrorContains(t, RunMigrations(map[string]*gorm.DB{"unsupported": db}), "unsupported")
}

func TestUnknownMigrationEntryRejectsBeforeOpeningDatabase(t *testing.T) {
	require.Error(t, Up(nil, DialectUnknown))
	require.Error(t, Down(nil, DialectUnknown, "2026-09-08-a.sql"))
}

func TestRunMigrationsDispatchesSQLite(t *testing.T) {
	previous := runUp
	t.Cleanup(func() { runUp = previous })
	called := false
	runUp = func(_ *gorm.DB, dialect Dialect) error {
		called = true
		require.Equal(t, DialectSQLite, dialect)
		return nil
	}
	db := &gorm.DB{Config: &gorm.Config{Dialector: sqlitedialect.Open(":memory:")}}
	require.NoError(t, RunMigrations(map[string]*gorm.DB{"sqlite": db}))
	require.True(t, called)
}

func TestSQLiteDownRequiresCompleteBackup(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{Dialector: sqlitedialect.Open(":memory:")}}
	require.ErrorContains(t, Down(db, DialectSQLite, "2026-09-08-example-sqlite.sql"), "backup")
	require.ErrorContains(t, Up(db, DialectMySQL), "mismatched")
}

func TestSQLiteUpRequiresBaselineThenValidatesWithoutMySQLHistory(t *testing.T) {
	db, err := gormhelper.NewSQLiteClient(filepath.Join(t.TempDir(), "migration.db"))
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	require.ErrorContains(t, Up(db, DialectSQLite), "baseline")
	_, err = sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)
	require.NoError(t, Up(db, DialectSQLite))
	require.NoError(t, Up(db, DialectSQLite))
	var count int64
	require.NoError(t, db.Table("gb_schema_migrations").Count(&count).Error)
	require.EqualValues(t, 2, count)
}
