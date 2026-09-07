package sqlitebootstrap

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/resource/database/sqlitebaseline"
)

func TestMigrateRequiresMatchingBaseline(t *testing.T) {
	db := testDB(t)
	err := Migrate(context.Background(), db)
	require.ErrorContains(t, err, "baseline")
}

func markMatchingBaseline(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec("CREATE TABLE gb_schema_migrations(version TEXT PRIMARY KEY NOT NULL, applied_at DATETIME NOT NULL, checksum TEXT NOT NULL)").Error)
	require.NoError(t, db.Exec("INSERT INTO gb_schema_migrations(version, applied_at, checksum) VALUES(?, ?, ?)", sqlitebaseline.Version, time.Now().UTC(), sqlitebaseline.SHA256).Error)
}

func testMigrationSpec(version, script string) migrationSpec {
	return migrationSpec{Version: version + sqliteMigrationSuffix, SQL: script, SHA256: digest(script)}
}

func migrationMarkerCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Raw("SELECT count(*) FROM gb_schema_migrations").Scan(&count).Error)
	return count
}

func TestMigrateMatchesBaselineAndAppliesEachWhitelistedSpecOnce(t *testing.T) {
	db := testDB(t)
	markMatchingBaseline(t, db)
	script := "CREATE TABLE migration_once(id INTEGER PRIMARY KEY, note TEXT NOT NULL); INSERT INTO migration_once VALUES(1, 'BEGIN; PRAGMA; ATTACH;');"
	spec := testMigrationSpec("2026-09-07-test-once", script)
	spec.Before = func(ctx context.Context, tx *gorm.DB, conn *sql.Conn) error {
		var count int
		return conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='migration_once'").Scan(&count)
	}
	spec.After = func(ctx context.Context, tx *gorm.DB, conn *sql.Conn) error {
		var note string
		return conn.QueryRowContext(ctx, "SELECT note FROM migration_once WHERE id=1").Scan(&note)
	}
	require.NoError(t, migrateWithSpecs(context.Background(), db, []migrationSpec{spec}))
	require.NoError(t, migrateWithSpecs(context.Background(), db, []migrationSpec{spec}))
	require.EqualValues(t, 2, migrationMarkerCount(t, db))
	var rows int64
	require.NoError(t, db.Raw("SELECT count(*) FROM migration_once").Scan(&rows).Error)
	require.EqualValues(t, 1, rows)

	prodDB := testDB(t)
	markMatchingBaseline(t, prodDB)
	require.NoError(t, Migrate(context.Background(), prodDB), "the current compile-time SQLite increment whitelist is empty")
}

func TestMigrateRejectsEmptyUnknownAndMismatchedMarkers(t *testing.T) {
	t.Run("empty database", func(t *testing.T) {
		db := testDB(t)
		err := Migrate(context.Background(), db)
		require.ErrorContains(t, err, "baseline")
		require.False(t, db.Migrator().HasTable(migrationMarkerTable))
	})
	t.Run("baseline checksum", func(t *testing.T) {
		db := testDB(t)
		markMatchingBaseline(t, db)
		require.NoError(t, db.Exec("UPDATE gb_schema_migrations SET checksum='tampered' WHERE version=?", sqlitebaseline.Version).Error)
		require.ErrorContains(t, Migrate(context.Background(), db), "checksum")
	})
	t.Run("unknown marker", func(t *testing.T) {
		db := testDB(t)
		markMatchingBaseline(t, db)
		require.NoError(t, db.Exec("INSERT INTO gb_schema_migrations(version, applied_at, checksum) VALUES('future-version', ?, 'future-checksum')", time.Now().UTC()).Error)
		require.ErrorContains(t, Migrate(context.Background(), db), "unknown")
	})
}

func TestMigrateCancellationRestoresForeignKeysAndRollsBack(t *testing.T) {
	db := testDB(t)
	markMatchingBaseline(t, db)
	ctx, cancel := context.WithCancel(context.Background())
	script := "CREATE TABLE canceled_increment(id INTEGER PRIMARY KEY);"
	spec := testMigrationSpec("2026-09-07-canceled", script)
	spec.After = func(context.Context, *gorm.DB, *sql.Conn) error {
		cancel()
		return context.Canceled
	}
	require.ErrorIs(t, migrateWithSpecs(ctx, db, []migrationSpec{spec}), context.Canceled)
	require.Equal(t, 1, foreignKeysValue(t, db))
	require.False(t, db.Migrator().HasTable("canceled_increment"))
	good := testMigrationSpec("2026-09-07-canceled", script)
	require.NoError(t, migrateWithSpecs(context.Background(), db, []migrationSpec{good}))
}

func TestMigrateRequiresForeignKeysInitiallyEnabled(t *testing.T) {
	db := testDB(t)
	require.NoError(t, db.Exec("PRAGMA foreign_keys=OFF").Error)
	markMatchingBaseline(t, db)
	require.Equal(t, 0, foreignKeysValue(t, db))
	require.ErrorContains(t, Migrate(context.Background(), db), "foreign_keys=0")
	require.Equal(t, 0, foreignKeysValue(t, db))
}

func TestMigrateSQLValidationIgnoresCommentsAndLiterals(t *testing.T) {
	valid := `-- BEGIN; PRAGMA foreign_keys=OFF; ATTACH 'x'; TRIGGER
CREATE TABLE "PRAGMA"("BEGIN" TEXT, value TEXT);
INSERT INTO "PRAGMA" VALUES('COMMIT; ROLLBACK; DETACH; TRIGGER', CASE WHEN 1=1 THEN 'ok' ELSE 'no' END); /* SAVEPOINT; */`
	require.NoError(t, validateMigrationSQL(valid))
	for _, script := range []string{
		"BEGIN; CREATE TABLE bad(id INTEGER);",
		"CREATE TABLE bad(id INTEGER); COMMIT;",
		"END;",
		"CREATE TABLE bad(id INTEGER); END;",
		"PRAGMA foreign_keys=OFF; CREATE TABLE bad(id INTEGER);",
		"ATTACH DATABASE 'other.db' AS other;",
		"DETACH DATABASE other;",
		"SAVEPOINT nested; CREATE TABLE bad(id INTEGER); RELEASE nested;",
		"VACUUM;",
		"CREATE TRIGGER bad AFTER INSERT ON source BEGIN SELECT 1; END;",
		"/* unterminated comment",
	} {
		require.Error(t, validateMigrationSQL(script), script)
	}
}

func TestMigrateSpecNamesAreAfterBaselineAndStrictlyOrdered(t *testing.T) {
	good := testMigrationSpec("2026-09-07-first", "CREATE TABLE ordered(id INTEGER PRIMARY KEY);")
	require.NoError(t, validateMigrationSpecs([]migrationSpec{good}))
	for _, spec := range []migrationSpec{
		{Version: "2026-09-06-before-sqlite.sql", SQL: "CREATE TABLE old(id INTEGER);", SHA256: digest("CREATE TABLE old(id INTEGER);")},
		{Version: "2026-09-07-missing-suffix.sql", SQL: "CREATE TABLE bad(id INTEGER);", SHA256: digest("CREATE TABLE bad(id INTEGER);")},
		{Version: "2026-13-08-bad-sqlite.sql", SQL: "CREATE TABLE bad(id INTEGER);", SHA256: digest("CREATE TABLE bad(id INTEGER);")},
	} {
		require.Error(t, validateMigrationSpecs([]migrationSpec{spec}), spec.Version)
	}
	second := testMigrationSpec("2026-09-07-second", "CREATE TABLE ordered_second(id INTEGER PRIMARY KEY);")
	require.NoError(t, validateMigrationSpecs([]migrationSpec{good, second}))
	require.Error(t, validateMigrationSpecs([]migrationSpec{second, good}))
}

func createParentChildFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db := testDB(t)
	require.NoError(t, db.Exec("CREATE TABLE parent(id INTEGER PRIMARY KEY, value TEXT NOT NULL)").Error)
	require.NoError(t, db.Exec("CREATE TABLE child(id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES parent(id) ON DELETE CASCADE)").Error)
	require.NoError(t, db.Exec("CREATE INDEX idx_parent_value ON parent(value)").Error)
	require.NoError(t, db.Exec("INSERT INTO parent(id, value) VALUES(1, 'parent')").Error)
	require.NoError(t, db.Exec("INSERT INTO child(id, parent_id) VALUES(1, 1)").Error)
	markMatchingBaseline(t, db)
	return db
}

func foreignKeysValue(t *testing.T, db *gorm.DB) int {
	t.Helper()
	var value int
	require.NoError(t, db.Raw("PRAGMA foreign_keys").Scan(&value).Error)
	return value
}

func parentRebuildMigrationSpec() migrationSpec {
	script := `CREATE TABLE parent_new(id INTEGER PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO parent_new SELECT id, value FROM parent;
DROP TABLE parent;
ALTER TABLE parent_new RENAME TO parent;
CREATE INDEX idx_parent_value ON parent(value);`
	return testMigrationSpec("2026-09-07-parent-rebuild-kill", script)
}

func TestMigrateRebuildsParentWithForeignKeysOffAndRestoresEnforcement(t *testing.T) {
	db := createParentChildFixture(t)
	script := `CREATE TABLE parent_new(id INTEGER PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO parent_new SELECT id, value FROM parent;
DROP TABLE parent;
ALTER TABLE parent_new RENAME TO parent;`
	spec := testMigrationSpec("2026-09-07-parent-rebuild", script)
	require.NoError(t, migrateWithSpecs(context.Background(), db, []migrationSpec{spec}))
	require.Equal(t, 1, foreignKeysValue(t, db))
	var childCount int64
	require.NoError(t, db.Raw("SELECT count(*) FROM child").Scan(&childCount).Error)
	require.EqualValues(t, 1, childCount)
	require.NoError(t, db.Exec("DELETE FROM parent WHERE id=1").Error)
	require.NoError(t, db.Raw("SELECT count(*) FROM child").Scan(&childCount).Error)
	require.Zero(t, childCount, "the rebuilt parent must retain ON DELETE CASCADE")
}

func TestMigrateRejectsLostParentAndRollsBackSchemaDataIndexAndMarker(t *testing.T) {
	db := createParentChildFixture(t)
	script := `CREATE TABLE parent_new(id INTEGER PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO parent_new SELECT id, value FROM parent WHERE id <> 1;
DROP TABLE parent;
ALTER TABLE parent_new RENAME TO parent;`
	spec := testMigrationSpec("2026-09-07-lost-parent", script)
	err := migrateWithSpecs(context.Background(), db, []migrationSpec{spec})
	require.ErrorContains(t, err, "foreign key")
	require.Equal(t, 1, foreignKeysValue(t, db))
	var value string
	require.NoError(t, db.Raw("SELECT value FROM parent WHERE id=1").Scan(&value).Error)
	require.Equal(t, "parent", value)
	var childCount int64
	require.NoError(t, db.Raw("SELECT count(*) FROM child").Scan(&childCount).Error)
	require.EqualValues(t, 1, childCount)
	require.True(t, db.Migrator().HasIndex("parent", "idx_parent_value"))
	require.EqualValues(t, 1, migrationMarkerCount(t, db))
}

func TestMigrateSpecializedCheckAndStageFailuresRollBack(t *testing.T) {
	t.Run("child copy check", func(t *testing.T) {
		db := createParentChildFixture(t)
		script := `CREATE TABLE child_new(id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES parent(id) ON DELETE CASCADE);
INSERT INTO child_new SELECT id, parent_id FROM child WHERE id <> 1;
DROP TABLE child;
ALTER TABLE child_new RENAME TO child;`
		spec := testMigrationSpec("2026-09-07-lost-child", script)
		spec.After = func(ctx context.Context, tx *gorm.DB, conn *sql.Conn) error {
			var count int
			if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM child").Scan(&count); err != nil {
				return err
			}
			if count != 1 {
				return fmt.Errorf("child row count=%d, want 1", count)
			}
			return nil
		}
		require.ErrorContains(t, migrateWithSpecs(context.Background(), db, []migrationSpec{spec}), "after-check")
		require.EqualValues(t, 1, migrationMarkerCount(t, db))
		require.True(t, db.Migrator().HasIndex("parent", "idx_parent_value"))
	})
	t.Run("index stage", func(t *testing.T) {
		db := createParentChildFixture(t)
		script := "CREATE INDEX idx_new ON parent(value); CREATE INDEX idx_new ON parent(value);"
		spec := testMigrationSpec("2026-09-07-index-failure", script)
		require.Error(t, migrateWithSpecs(context.Background(), db, []migrationSpec{spec}))
		require.False(t, db.Migrator().HasIndex("parent", "idx_new"))
		require.True(t, db.Migrator().HasIndex("parent", "idx_parent_value"))
		require.EqualValues(t, 1, migrationMarkerCount(t, db))
	})
	t.Run("marker stage", func(t *testing.T) {
		db := createParentChildFixture(t)
		spec := testMigrationSpec("2026-09-07-marker-failure", "DROP TABLE gb_schema_migrations;")
		require.ErrorContains(t, migrateWithSpecs(context.Background(), db, []migrationSpec{spec}), "marker")
		require.True(t, db.Migrator().HasTable(migrationMarkerTable))
		require.True(t, db.Migrator().HasIndex("parent", "idx_parent_value"))
		require.EqualValues(t, 1, migrationMarkerCount(t, db))
	})
}

func TestMigrateCommitsEarlierIncrementAndCanContinueAfterLaterFailure(t *testing.T) {
	db := testDB(t)
	markMatchingBaseline(t, db)
	first := testMigrationSpec("2026-09-07-first", "CREATE TABLE first_increment(id INTEGER PRIMARY KEY);")
	badSecond := testMigrationSpec("2026-09-07-second", "ALTER TABLE missing_table ADD COLUMN value TEXT;")
	require.Error(t, migrateWithSpecs(context.Background(), db, []migrationSpec{first, badSecond}))
	require.True(t, db.Migrator().HasTable("first_increment"))
	require.EqualValues(t, 2, migrationMarkerCount(t, db))
	goodSecond := testMigrationSpec("2026-09-07-second", "CREATE TABLE second_increment(id INTEGER PRIMARY KEY);")
	require.NoError(t, migrateWithSpecs(context.Background(), db, []migrationSpec{first, goodSecond}))
	require.EqualValues(t, 3, migrationMarkerCount(t, db))
}

const (
	childMigrationPathEnv        = "SQLITE_BOOTSTRAP_CHILD_PATH"
	childMigrationSignalEnv      = "SQLITE_BOOTSTRAP_CHILD_SIGNAL"
	childMigrationHoldEnv        = "SQLITE_BOOTSTRAP_CHILD_HOLD_MS"
	childMigrationModeEnv        = "SQLITE_BOOTSTRAP_CHILD_MODE"
	childMigrationModeConcurrent = "concurrent"
	childMigrationModeRebuild    = "rebuild"
)

func TestSQLiteMigrationSubprocess(t *testing.T) {
	path := os.Getenv(childMigrationPathEnv)
	if path == "" {
		return
	}
	db, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	defer raw.Close()
	mode := os.Getenv(childMigrationModeEnv)
	var spec migrationSpec
	switch mode {
	case childMigrationModeConcurrent:
		script := "CREATE TABLE concurrent_once(id INTEGER PRIMARY KEY); INSERT INTO concurrent_once VALUES(1);"
		spec = testMigrationSpec("2026-09-07-concurrent", script)
	case childMigrationModeRebuild:
		spec = parentRebuildMigrationSpec()
	default:
		t.Fatalf("unknown child migration mode %q", mode)
	}
	holdMS, _ := strconv.Atoi(os.Getenv(childMigrationHoldEnv))
	if holdMS > 0 {
		spec.After = func(ctx context.Context, tx *gorm.DB, conn *sql.Conn) error {
			if mode == childMigrationModeRebuild {
				var value string
				if err := conn.QueryRowContext(ctx, "SELECT value FROM parent WHERE id=1").Scan(&value); err != nil {
					return err
				}
				if value != "before-kill" {
					return fmt.Errorf("rebuilt parent value=%q, want before-kill", value)
				}
			}
			if signal := os.Getenv(childMigrationSignalEnv); signal != "" {
				if err := os.WriteFile(signal, []byte("entered"), 0o600); err != nil {
					return err
				}
			}
			time.Sleep(time.Duration(holdMS) * time.Millisecond)
			return nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, migrateWithSpecs(ctx, db, []migrationSpec{spec}))
}

type migrationChild struct {
	cmd      *exec.Cmd
	output   *bytes.Buffer
	waitOnce sync.Once
	waitErr  error
}

func (c *migrationChild) Wait() error {
	c.waitOnce.Do(func() { c.waitErr = c.cmd.Wait() })
	return c.waitErr
}

func (c *migrationChild) Kill() error {
	if c.cmd.Process == nil {
		return nil
	}
	return c.cmd.Process.Kill()
}

func (c *migrationChild) cleanup() {
	_ = c.Kill()
	_ = c.Wait()
}

func startMigrationChild(t *testing.T, ctx context.Context, path, signal string, holdMS int, mode string) (*migrationChild, *bytes.Buffer) {
	t.Helper()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSQLiteMigrationSubprocess$", "-test.v")
	cmd.Env = append(os.Environ(),
		childMigrationPathEnv+"="+path,
		childMigrationSignalEnv+"="+signal,
		childMigrationHoldEnv+"="+strconv.Itoa(holdMS),
		childMigrationModeEnv+"="+mode,
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	require.NoError(t, cmd.Start())
	child := &migrationChild{cmd: cmd, output: &output}
	t.Cleanup(child.cleanup)
	return child, &output
}

func waitForMigrationSignal(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for migration signal %s", path)
}

func TestMigrateConcurrentSubprocessesSerializeOnOneFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.db")
	db, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	markMatchingBaseline(t, db)
	raw, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	signal := filepath.Join(t.TempDir(), "entered")
	first, firstOutput := startMigrationChild(t, ctx, path, signal, 800, childMigrationModeConcurrent)
	require.NoError(t, waitForMigrationSignal(signal, 5*time.Second))
	second, secondOutput := startMigrationChild(t, ctx, path, "", 0, childMigrationModeConcurrent)
	firstErr := make(chan error, 1)
	secondErr := make(chan error, 1)
	go func() { firstErr <- first.Wait() }()
	go func() { secondErr <- second.Wait() }()
	require.NoError(t, <-firstErr, firstOutput.String())
	require.NoError(t, <-secondErr, secondOutput.String())

	db, err = gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	defer func() { raw, _ := db.DB(); _ = raw.Close() }()
	var rows int64
	require.NoError(t, db.Raw("SELECT count(*) FROM concurrent_once").Scan(&rows).Error)
	require.EqualValues(t, 1, rows)
	require.EqualValues(t, 2, migrationMarkerCount(t, db))
}

func TestMigrateRecoversAfterKilledParentRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "killed-rebuild.db")
	db, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	markMatchingBaseline(t, db)
	require.NoError(t, db.Exec("CREATE TABLE parent(id INTEGER PRIMARY KEY, value TEXT NOT NULL)").Error)
	require.NoError(t, db.Exec("CREATE TABLE child(id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES parent(id) ON DELETE CASCADE)").Error)
	require.NoError(t, db.Exec("CREATE INDEX idx_parent_value ON parent(value)").Error)
	require.NoError(t, db.Exec("INSERT INTO parent(id, value) VALUES(1, 'before-kill')").Error)
	require.NoError(t, db.Exec("INSERT INTO child(id, parent_id) VALUES(1, 1)").Error)
	raw, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	signal := filepath.Join(t.TempDir(), "entered")
	child, output := startMigrationChild(t, ctx, path, signal, 30_000, childMigrationModeRebuild)
	require.NoError(t, waitForMigrationSignal(signal, 5*time.Second))
	require.NoError(t, child.Kill())
	require.Error(t, child.Wait(), output.String())

	db, err = gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	defer func() { raw, _ := db.DB(); _ = raw.Close() }()
	var value string
	require.NoError(t, db.Raw("SELECT value FROM parent WHERE id=1").Scan(&value).Error)
	require.Equal(t, "before-kill", value)
	var childCount int64
	require.NoError(t, db.Raw("SELECT count(*) FROM child").Scan(&childCount).Error)
	require.EqualValues(t, 1, childCount)
	require.True(t, db.Migrator().HasIndex("parent", "idx_parent_value"))
	require.Equal(t, 1, foreignKeysValue(t, db))
	require.EqualValues(t, 1, migrationMarkerCount(t, db))
	spec := parentRebuildMigrationSpec()
	require.NoError(t, migrateWithSpecs(context.Background(), db, []migrationSpec{spec}))
	require.True(t, db.Migrator().HasIndex("parent", "idx_parent_value"))
	require.EqualValues(t, 2, migrationMarkerCount(t, db))
	require.NoError(t, db.Exec("DELETE FROM parent WHERE id=1").Error)
	require.NoError(t, db.Raw("SELECT count(*) FROM child").Scan(&childCount).Error)
	require.Zero(t, childCount)
}

func TestMigrateRejectsNilDatabaseDialector(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{Dialector: nil}}
	require.ErrorContains(t, Migrate(context.Background(), db), "database")
}
