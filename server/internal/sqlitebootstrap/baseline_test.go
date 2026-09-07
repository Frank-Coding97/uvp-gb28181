package sqlitebootstrap

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"
	"time"
	"uvplatform.cn/uvp-gb28181/app/gb28181/civilcode"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gormhelper.NewSQLiteClient(filepath.Join(t.TempDir(), "baseline.db"))
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	return db
}
func digest(sql string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(sql))) }

func TestBaselineIsAtomicAndNeverReplacesExistingData(t *testing.T) {
	db := testDB(t)
	script := "CREATE TABLE users(id INTEGER PRIMARY KEY, name TEXT NOT NULL); INSERT INTO users VALUES(1, 'seed');"
	first, err := applyBaseline(context.Background(), db, "test-baseline", script, digest(script), nil)
	require.NoError(t, err)
	require.True(t, first)
	require.NoError(t, db.Exec("UPDATE users SET name='user edited' WHERE id=1").Error)
	first, err = applyBaseline(context.Background(), db, "test-baseline", script, digest(script), nil)
	require.NoError(t, err)
	require.False(t, first)
	var value string
	require.NoError(t, db.Raw("SELECT name FROM users WHERE id=1").Scan(&value).Error)
	require.Equal(t, "user edited", value)
	var n int64
	require.NoError(t, db.Table("gb_schema_migrations").Count(&n).Error)
	require.EqualValues(t, 1, n)
}
func TestBaselineFailureRollsBackDDLSeedAndMarker(t *testing.T) {
	db := testDB(t)
	script := "CREATE TABLE sample(id INTEGER PRIMARY KEY); INSERT INTO sample VALUES(1); INSERT INTO absent VALUES(1);"
	_, err := applyBaseline(context.Background(), db, "test-baseline", script, digest(script), nil)
	require.Error(t, err)
	require.False(t, db.Migrator().HasTable("sample"))
	require.False(t, db.Migrator().HasTable("gb_schema_migrations"))
	good := "CREATE TABLE sample(id INTEGER PRIMARY KEY);"
	_, err = applyBaseline(context.Background(), db, "test-baseline", good, digest(good), nil)
	require.NoError(t, err)
}
func TestBaselineRejectsUnknownDatabaseAndChangedChecksum(t *testing.T) {
	db := testDB(t)
	script := "CREATE TABLE sample(id INTEGER PRIMARY KEY);"
	_, err := applyBaseline(context.Background(), db, "test-baseline", script, "wrong", nil)
	require.ErrorContains(t, err, "checksum")
	require.NoError(t, db.Exec("CREATE TABLE existing_data(id INTEGER)").Error)
	_, err = applyBaseline(context.Background(), db, "test-baseline", script, digest(script), nil)
	require.ErrorContains(t, err, "non-empty")
	require.False(t, db.Migrator().HasTable("gb_schema_migrations"))
}
func TestBaselineSeedFailureRollsBack(t *testing.T) {
	db := testDB(t)
	script := "CREATE TABLE sample(id INTEGER PRIMARY KEY);"
	_, err := applyBaseline(context.Background(), db, "test-baseline", script, digest(script), func(tx *gorm.DB) error {
		return tx.Exec("INSERT INTO sample VALUES(1); INSERT INTO missing VALUES(2)").Error
	})
	require.Error(t, err)
	require.False(t, db.Migrator().HasTable("sample"))
}

func TestBaselineBatchedCivilSeedUsesSameConnection(t *testing.T) {
	db := testDB(t)
	script := `CREATE TABLE sys_civil_code(code TEXT PRIMARY KEY,name TEXT NOT NULL,short_name TEXT,parent_code TEXT,level INTEGER NOT NULL,pinyin TEXT,created_at DATETIME,updated_at DATETIME);`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	seeded := 0
	_, err := applyBaseline(ctx, db, "test-civil", script, digest(script), func(tx *gorm.DB) error { var e error; seeded, e = civilcode.SeedIfEmpty(tx); return e })
	require.NoError(t, err)
	require.Greater(t, seeded, 3000)
	var count int64
	require.NoError(t, db.Table("sys_civil_code").Count(&count).Error)
	require.EqualValues(t, seeded, count)
}
func TestBaselineCancellationRollsBackAndReleasesConnection(t *testing.T) {
	db := testDB(t)
	script := "CREATE TABLE sample(id INTEGER PRIMARY KEY);"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := applyBaseline(ctx, db, "test-cancel", script, digest(script), func(tx *gorm.DB) error { cancel(); return ctx.Err() })
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, db.Migrator().HasTable("sample"))
	_, err = applyBaseline(context.Background(), db, "test-cancel", script, digest(script), nil)
	require.NoError(t, err)
}

func TestBaselineRejectsNonInternalSQLiteLikeTableNames(t *testing.T) {
	db := testDB(t)
	require.NoError(t, db.Exec("CREATE TABLE sqliteXuser_data(id INTEGER)").Error)
	script := "CREATE TABLE sample(id INTEGER PRIMARY KEY);"
	_, err := applyBaseline(context.Background(), db, "test-baseline", script, digest(script), nil)
	require.ErrorContains(t, err, "non-empty")
}
