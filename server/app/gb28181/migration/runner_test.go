package migration

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---- fakes ----

type fakeStore struct {
	applied   map[string]bool
	marks     [][]string
	ensureErr error
}

func (f *fakeStore) EnsureTable() error { return f.ensureErr }
func (f *fakeStore) ListApplied() ([]string, error) {
	out := make([]string, 0, len(f.applied))
	for v := range f.applied {
		out = append(out, v)
	}
	sort.Strings(out)
	return out, nil
}
func (f *fakeStore) MarkApplied(vs []string) error {
	f.marks = append(f.marks, vs)
	for _, v := range vs {
		f.applied[v] = true
	}
	return nil
}
func (f *fakeStore) DeleteApplied(v string) error {
	delete(f.applied, v)
	return nil
}
func (f *fakeStore) IsEmpty() (bool, error) { return len(f.applied) == 0, nil }

type fakeLocker struct {
	acquireErr error
	acquired   bool
	released   bool
}

func (f *fakeLocker) Acquire() error { f.acquired = true; return f.acquireErr }
func (f *fakeLocker) Release() error { f.released = true; return nil }

type fakeSource struct {
	files map[string]string // name -> sql text
}

func (f *fakeSource) UpFiles() ([]string, error) {
	out := make([]string, 0, len(f.files))
	for name := range f.files {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}
func (f *fakeSource) ReadSQL(name string) (string, error) {
	sqlText, ok := f.files[name]
	if !ok {
		return "", fmt.Errorf("文件不存在: %s", name)
	}
	return sqlText, nil
}

type fakeExec struct {
	executed []string
	failOn   string // SQL 包含该子串时返回 error
}

func (f *fakeExec) ExecSQL(sqlText string) error {
	f.executed = append(f.executed, sqlText)
	if f.failOn != "" && strings.Contains(sqlText, f.failOn) {
		return errors.New("exec boom")
	}
	return nil
}

func newFakes() (*fakeStore, *fakeLocker, *fakeSource, *fakeExec) {
	return &fakeStore{applied: map[string]bool{}},
		&fakeLocker{},
		&fakeSource{files: map[string]string{}},
		&fakeExec{}
}

// ---- 3.1 首启基线化 ----

func TestRunFirstStartBaselines(t *testing.T) {
	store, lock, src, exec := newFakes()
	src.files["a.sql"] = "SQL FOR a"
	src.files["b.sql"] = "SQL FOR b"

	err := run(store, lock, src, exec, allTablesExistProbe())
	require.NoError(t, err)
	require.Empty(t, exec.executed, "快照库(探测表存在)基线化不应执行任何 SQL")
	require.Len(t, store.marks, 1)
	require.Equal(t, []string{"a.sql", "b.sql"}, store.marks[0])
}

// ---- 3.1b 老基线库(探测表缺失)拒绝 ----

func TestRunFirstStartRejectsMissingBaselineTable(t *testing.T) {
	store, lock, src, exec := newFakes()
	src.files["a.sql"] = "SQL FOR a"

	err := run(store, lock, src, exec, noTablesExistProbe())
	require.Error(t, err, "老基线库不得记录假成功")
	require.Empty(t, exec.executed)
	require.Empty(t, store.marks)
}

func TestRunFirstStartRejectsLegacySnapshotWithoutCatalogProjection(t *testing.T) {
	store, lock, src, exec := newFakes()
	src.files["a.sql"] = "SQL FOR a"
	probe := func(table string) (bool, error) {
		return table != "sys_openapi_runtime_state", nil
	}

	err := run(store, lock, src, exec, schemaProbe{hasTable: probe})
	require.Error(t, err)
	require.Contains(t, err.Error(), "sys_openapi_runtime_state")
	require.Empty(t, exec.executed)
	require.Empty(t, store.marks)
}

// ---- 3.2 增量执行 ----

func TestRunIncrementalAppliesPendingOnly(t *testing.T) {
	store, lock, src, exec := newFakes()
	store.applied["a.sql"] = true
	src.files["a.sql"] = "SQL FOR a"
	src.files["b.sql"] = "SQL FOR b"

	err := run(store, lock, src, exec, allTablesExistProbe())
	require.NoError(t, err)
	require.Len(t, exec.executed, 1)
	require.Contains(t, exec.executed[0], "SQL FOR b")
	require.Equal(t, [][]string{{"b.sql"}}, store.marks)
}

// ---- 3.3 失败不标记 ----

func TestRunFailureNotMarked(t *testing.T) {
	store, lock, src, exec := newFakes()
	store.applied["a.sql"] = true
	src.files["a.sql"] = "SQL FOR a"
	src.files["b.sql"] = "SQL FOR b IS BROKEN"
	exec.failOn = "IS BROKEN"

	err := run(store, lock, src, exec, allTablesExistProbe())
	require.Error(t, err)
	require.Contains(t, err.Error(), "b.sql", "错误应含文件名")
	require.Contains(t, err.Error(), "SQL FOR b IS BROKEN", "错误应含 SQL 内容")
	require.False(t, store.applied["b.sql"], "失败的迁移不应被标记")
}

// ---- 3.4 幂等 ----

func TestRunIdempotent(t *testing.T) {
	store, lock, src, exec := newFakes()
	store.applied["a.sql"] = true
	src.files["a.sql"] = "SQL FOR a"
	src.files["b.sql"] = "SQL FOR b"

	require.NoError(t, run(store, lock, src, exec, allTablesExistProbe()))
	require.NoError(t, run(store, lock, src, exec, allTablesExistProbe()))
	require.Len(t, exec.executed, 1, "第二次运行不应执行任何 SQL")
}

func TestRunResumesSummaryColumnMigrationAfterColumnWasAdded(t *testing.T) {
	store, lock, src, exec := newFakes()
	store.applied["previous.sql"] = true
	name := "2026-10-09-job-result-summary-sqlite.sql"
	src.files[name] = "ALTER TABLE sys_job_results ADD COLUMN summary TEXT"
	probe := schemaProbe{
		hasTable: func(table string) (bool, error) { return table == "sys_job_results", nil },
		hasColumn: func(table, column string) (bool, error) {
			return table == "sys_job_results" && column == "summary", nil
		},
	}

	require.NoError(t, run(store, lock, src, exec, probe))
	require.Empty(t, exec.executed, "已有 summary 列时重试不得重复执行 ADD COLUMN")
	require.True(t, store.applied[name], "已存在的列应补记迁移版本")
}

func TestJobResultSummaryMigrationNamesMapToColumn(t *testing.T) {
	for _, name := range []string{
		"2026-10-09-job-result-summary.sql",
		"2026-10-09-job-result-summary-postgresql.sql",
		"2026-10-09-job-result-summary-sqlserver.sql",
		"2026-10-09-job-result-summary-sqlite.sql",
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, baselineColumnRequirement{table: "sys_job_results", column: "summary"}, baselineColumnMigrations[name])
		})
	}
}

// ---- 3.6 锁失败透传 ----

func TestRunLockFailureBlocks(t *testing.T) {
	store, lock, src, exec := newFakes()
	lock.acquireErr = errors.New("lock timeout")
	src.files["a.sql"] = "SQL FOR a"

	err := run(store, lock, src, exec, allTablesExistProbe())
	require.Error(t, err)
	require.Contains(t, err.Error(), "lock timeout")
	require.Empty(t, exec.executed, "锁失败不应执行任何迁移")
	require.Len(t, store.marks, 0)
}

func TestRunEmptyLedgerAppliesMediaNetworkMigrationToOlderBaseline(t *testing.T) {
	store, lock, src, exec := newFakes()
	passwordMigration := "2026-10-08-initial-admin-password-change-sqlite.sql"
	hookMigration := "2026-10-08-sip-platform-hook-ip-sqlite.sql"
	streamMigration := "2026-10-08-sip-platform-stream-ip-sqlite.sql"
	src.files[passwordMigration] = "PASSWORD MIGRATION"
	src.files[hookMigration] = "HOOK IP MIGRATION"
	src.files[streamMigration] = "STREAM IP MIGRATION"
	probe := schemaProbe{
		hasTable: func(string) (bool, error) { return true, nil },
		hasColumn: func(_ string, column string) (bool, error) {
			return column != "hook_ip" && column != "stream_ip", nil
		},
	}

	err := run(store, lock, src, exec, probe)
	require.NoError(t, err)
	require.Equal(t, []string{"HOOK IP MIGRATION", "STREAM IP MIGRATION"}, exec.executed)
	require.Equal(t, [][]string{{passwordMigration}, {hookMigration}, {streamMigration}}, store.marks,
		"旧基线只执行媒体地址字段迁移，密码迁移继续按原行为基线化")
}

func TestRunEmptyLedgerSkipsMediaNetworkMigrationForCurrentBaseline(t *testing.T) {
	store, lock, src, exec := newFakes()
	hookMigration := "2026-10-08-sip-platform-hook-ip-sqlite.sql"
	streamMigration := "2026-10-08-sip-platform-stream-ip-sqlite.sql"
	src.files[hookMigration] = "HOOK IP MIGRATION"
	src.files[streamMigration] = "STREAM IP MIGRATION"
	probe := schemaProbe{
		hasTable:  func(string) (bool, error) { return true, nil },
		hasColumn: func(string, string) (bool, error) { return true, nil },
	}

	err := run(store, lock, src, exec, probe)
	require.NoError(t, err)
	require.Empty(t, exec.executed)
	require.Equal(t, [][]string{{hookMigration, streamMigration}}, store.marks)
}

func TestRunEmptyLedgerAppliesOnlyMissingMediaNetworkColumn(t *testing.T) {
	store, lock, src, exec := newFakes()
	hookMigration := "2026-10-08-sip-platform-hook-ip-sqlite.sql"
	streamMigration := "2026-10-08-sip-platform-stream-ip-sqlite.sql"
	src.files[hookMigration] = "HOOK IP MIGRATION"
	src.files[streamMigration] = "STREAM IP MIGRATION"
	probe := schemaProbe{
		hasTable: func(string) (bool, error) { return true, nil },
		hasColumn: func(_ string, column string) (bool, error) {
			return column == "hook_ip", nil
		},
	}

	err := run(store, lock, src, exec, probe)
	require.NoError(t, err)
	require.Equal(t, []string{"STREAM IP MIGRATION"}, exec.executed)
	require.Equal(t, [][]string{{hookMigration}, {streamMigration}}, store.marks)
}

func TestUpSQLiteMigratesMediaColumnsFromEmptyLedger(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	for _, table := range baselineRequiredTables {
		require.NoError(t, db.Exec(`CREATE TABLE "`+table+`" (id INTEGER)`).Error)
	}
	require.NoError(t, db.Exec(`CREATE TABLE "gb_sip_config" (
		id INTEGER PRIMARY KEY,
		advertise_ip_inferred INTEGER NOT NULL DEFAULT 0
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE "meta_node" (
		id INTEGER PRIMARY KEY,
		host TEXT NOT NULL DEFAULT ''
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE "sys_users" (
		id INTEGER PRIMARY KEY,
		must_change_password INTEGER NOT NULL DEFAULT 0
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE "sys_job_results" (
		id INTEGER PRIMARY KEY,
		status TEXT NOT NULL
	)`).Error)

	require.NoError(t, Up(db, DialectSQLite))
	require.True(t, db.Migrator().HasColumn("gb_sip_config", "hook_ip"))
	require.True(t, db.Migrator().HasColumn("gb_sip_config", "stream_ip"))
	require.True(t, db.Migrator().HasColumn("sys_job_results", "summary"))

	var applied int64
	require.NoError(t, db.Table("gb_schema_migrations").Count(&applied).Error)
	require.GreaterOrEqual(t, applied, int64(6))
}

func allTablesExistProbe() schemaProbe {
	return schemaProbe{
		hasTable:  func(string) (bool, error) { return true, nil },
		hasColumn: func(string, string) (bool, error) { return true, nil },
	}
}

func noTablesExistProbe() schemaProbe {
	return schemaProbe{
		hasTable:  func(string) (bool, error) { return false, nil },
		hasColumn: func(string, string) (bool, error) { return false, nil },
	}
}
