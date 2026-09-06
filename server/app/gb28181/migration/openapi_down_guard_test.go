package migration

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var openAPIDownGuardTargets = []struct {
	dialect Dialect
	name    string
	query   string
}{
	{dialect: DialectMySQL, name: "2026-09-05-openapi-aksk-schema.sql", query: "select database()"},
	{dialect: DialectPostgres, name: "2026-09-05-openapi-aksk-schema-postgresql.sql", query: "select current_database()"},
	{dialect: DialectSQLServer, name: "2026-09-05-openapi-aksk-schema-sqlserver.sql", query: "select db_name()"},
}

func TestOpenAPIDownGuardDefaultDeny(t *testing.T) {
	t.Setenv("UVP_OPENAPI_ALLOW_TEST_DOWN", "")
	for _, target := range openAPIDownGuardTargets {
		t.Run(target.name, func(t *testing.T) {
			err := checkOpenAPIDownGuard(nil, target.dialect, target.name)
			require.ErrorIs(t, err, errOpenAPIDownDenied)
		})
	}
}

func TestOpenAPIDownGuardAllowsOnlyEmptyIsolatedDatabase(t *testing.T) {
	t.Setenv("UVP_OPENAPI_ALLOW_TEST_DOWN", "1")
	for _, target := range openAPIDownGuardTargets {
		t.Run(target.name, func(t *testing.T) {
			state := newOpenAPIDownGuardDBState()
			db := newOpenAPIDownGuardDB(t, state, target.dialect)
			require.NoError(t, checkOpenAPIDownGuard(db, target.dialect, target.name))
			queries := state.queriesSnapshot()
			require.Equal(t, target.query, queries[0])
			for _, table := range []string{
				"sys_openapi_client",
				"sys_openapi_client_scope",
				"sys_openapi_nonce",
				"sys_openapi_audit",
				"gb_openapi_play_grant",
				"gb_openapi_viewer",
			} {
				require.Contains(t, queries, "select count(*) from "+table)
			}
			require.Contains(t, queries, "select count(*) from gb_device where (access_epoch is null or access_epoch <> 1) or legacy_revoked_before is not null")
			require.Contains(t, queries, "select count(*) from meta_node where current_boot_nonce is not null or (retired_boot_history is not null and retired_boot_history <> '[]') or runtime_epoch is null or runtime_epoch <> 0 or runtime_protocol_version is null or runtime_protocol_version <> 0 or runtime_confirmed_revision is null or runtime_confirmed_revision <> 0 or runtime_confirmed_at is not null or runtime_identity_status is null or runtime_identity_status <> 'unknown'")
		})
	}
}

func TestOpenAPIDownGuardReadsCurrentDatabaseNotDSN(t *testing.T) {
	t.Setenv("UVP_OPENAPI_ALLOW_TEST_DOWN", "1")
	for _, target := range openAPIDownGuardTargets {
		t.Run(target.name, func(t *testing.T) {
			state := newOpenAPIDownGuardDBState()
			state.databaseName = "business_database"
			db := newOpenAPIDownGuardDB(t, state, target.dialect)
			require.ErrorIs(t, checkOpenAPIDownGuard(db, target.dialect, target.name), errOpenAPIDownDenied)

			state = newOpenAPIDownGuardDBState()
			state.databaseName = "uvp_openapi_test_from_connection"
			db = newOpenAPIDownGuardDB(t, state, target.dialect)
			require.NoError(t, checkOpenAPIDownGuard(db, target.dialect, target.name))
		})
	}
}

func TestOpenAPIDownGuardRequiresExactSwitchAndDatabasePrefix(t *testing.T) {
	for _, value := range []string{"", "0", "true", "TRUE", " 1"} {
		t.Run("switch="+value, func(t *testing.T) {
			t.Setenv("UVP_OPENAPI_ALLOW_TEST_DOWN", value)
			state := newOpenAPIDownGuardDBState()
			db := newOpenAPIDownGuardDB(t, state, DialectMySQL)
			require.ErrorIs(t, checkOpenAPIDownGuard(db, DialectMySQL, openAPIDownGuardTargets[0].name), errOpenAPIDownDenied)
		})
	}

	t.Setenv("UVP_OPENAPI_ALLOW_TEST_DOWN", "1")
	for _, databaseName := range []string{
		"uvp_openapi_test_", "UVP_openapi_test_case", "uvp_openapi_test", "uvp_openapi_test_\uFF41",
		"uvp_openapi_test_case-foo", "uvp_openapi_test_case with space", "uvp_openapi_test_case\n", "uvp_openapi_test_Case",
	} {
		t.Run("database="+databaseName, func(t *testing.T) {
			state := newOpenAPIDownGuardDBState()
			state.databaseName = databaseName
			db := newOpenAPIDownGuardDB(t, state, DialectMySQL)
			require.ErrorIs(t, checkOpenAPIDownGuard(db, DialectMySQL, openAPIDownGuardTargets[0].name), errOpenAPIDownDenied)
		})
	}

	for _, databaseName := range []string{"uvp_openapi_test_case_1", "uvp_openapi_test_abc123"} {
		t.Run("valid database="+databaseName, func(t *testing.T) {
			state := newOpenAPIDownGuardDBState()
			state.databaseName = databaseName
			db := newOpenAPIDownGuardDB(t, state, DialectMySQL)
			require.NoError(t, checkOpenAPIDownGuard(db, DialectMySQL, openAPIDownGuardTargets[0].name))
		})
	}
}

func TestOpenAPIDownGuardRejectsUnknownDialectAndNonTargetIsUnchanged(t *testing.T) {
	t.Setenv("UVP_OPENAPI_ALLOW_TEST_DOWN", "1")
	require.ErrorIs(t, checkOpenAPIDownGuard(nil, DialectUnknown, openAPIDownGuardTargets[0].name), errOpenAPIDownDenied)

	for _, name := range []string{
		"2026-09-05-openapi-aksk-schema-down.sql",
		"/tmp/2026-09-05-openapi-aksk-schema.sql",
		"2026-09-05-openapi-aksk-schema.sql ",
		"2026-09-05-other-schema.sql",
	} {
		require.NoError(t, checkOpenAPIDownGuard(nil, DialectMySQL, name), name)
	}
}

func TestOpenAPIDownGuardRejectsProbeFailuresAndNonEmptySafetyState(t *testing.T) {
	t.Setenv("UVP_OPENAPI_ALLOW_TEST_DOWN", "1")
	t.Run("current database query failure", func(t *testing.T) {
		state := newOpenAPIDownGuardDBState()
		state.queryError = errors.New("database uvp_openapi_test_secret SQL SELECT secret")
		db := newOpenAPIDownGuardDB(t, state, DialectMySQL)
		err := checkOpenAPIDownGuard(db, DialectMySQL, openAPIDownGuardTargets[0].name)
		require.ErrorIs(t, err, errOpenAPIDownDenied)
		require.NotContains(t, err.Error(), "uvp_openapi_test_secret")
		require.NotContains(t, err.Error(), "SELECT")
		require.NotContains(t, err.Error(), "secret")
	})

	for _, table := range []string{
		"sys_openapi_client",
		"sys_openapi_client_scope",
		"sys_openapi_nonce",
		"sys_openapi_audit",
		"gb_openapi_play_grant",
		"gb_openapi_viewer",
	} {
		t.Run("non-empty "+table, func(t *testing.T) {
			state := newOpenAPIDownGuardDBState()
			state.counts[table] = 1
			db := newOpenAPIDownGuardDB(t, state, DialectMySQL)
			require.ErrorIs(t, checkOpenAPIDownGuard(db, DialectMySQL, openAPIDownGuardTargets[0].name), errOpenAPIDownDenied)
		})
	}

	t.Run("missing safety table is not empty", func(t *testing.T) {
		state := newOpenAPIDownGuardDBState()
		state.missingTables["gb_openapi_viewer"] = true
		db := newOpenAPIDownGuardDB(t, state, DialectMySQL)
		require.ErrorIs(t, checkOpenAPIDownGuard(db, DialectMySQL, openAPIDownGuardTargets[0].name), errOpenAPIDownDenied)
	})

	t.Run("device security version is advanced", func(t *testing.T) {
		state := newOpenAPIDownGuardDBState()
		state.deviceUnsafeCount = 1
		db := newOpenAPIDownGuardDB(t, state, DialectMySQL)
		require.ErrorIs(t, checkOpenAPIDownGuard(db, DialectMySQL, openAPIDownGuardTargets[0].name), errOpenAPIDownDenied)
	})

	t.Run("node runtime history is present", func(t *testing.T) {
		state := newOpenAPIDownGuardDBState()
		state.nodeUnsafeCount = 1
		db := newOpenAPIDownGuardDB(t, state, DialectMySQL)
		require.ErrorIs(t, checkOpenAPIDownGuard(db, DialectMySQL, openAPIDownGuardTargets[0].name), errOpenAPIDownDenied)
	})
}

func TestDownBlocksOpenAPISchemaBeforeOpeningDatabase(t *testing.T) {
	t.Setenv("UVP_OPENAPI_ALLOW_TEST_DOWN", "")
	err := Down(nil, DialectMySQL, openAPIDownGuardTargets[0].name)
	require.ErrorIs(t, err, errOpenAPIDownDenied)
}

func TestOpenAPIDownGuardUsesBoundedContext(t *testing.T) {
	t.Setenv(openAPIDownGuardSwitch, "1")
	state := newOpenAPIDownGuardDBState()
	state.blockQueries = true
	db := newOpenAPIDownGuardDB(t, state, DialectMySQL)

	started := time.Now()
	err := checkOpenAPIDownGuard(db, DialectMySQL, openAPISchemaMigration)
	require.ErrorIs(t, err, errOpenAPIDownDenied)
	require.Less(t, time.Since(started), 3*time.Second, "the destructive-down probe must have a bounded total deadline")
}

func TestOpenAPIDownSafetyStateSQLiteInitialValuesAndUpdates(t *testing.T) {
	tests := []struct {
		name   string
		update string
		wantOK bool
	}{
		{name: "initial values", wantOK: true},
		{name: "device access epoch NULL", update: "UPDATE gb_device SET access_epoch=NULL", wantOK: false},
		{name: "device access epoch advanced", update: "UPDATE gb_device SET access_epoch=2", wantOK: false},
		{name: "legacy revoked before", update: "UPDATE gb_device SET legacy_revoked_before='2026-09-06T00:00:00Z'", wantOK: false},
		{name: "current boot nonce empty", update: "UPDATE meta_node SET current_boot_nonce=''", wantOK: false},
		{name: "current boot nonce set", update: "UPDATE meta_node SET current_boot_nonce='boot-1'", wantOK: false},
		{name: "retired history empty array", update: "UPDATE meta_node SET retired_boot_history='[]'", wantOK: true},
		{name: "retired history object", update: "UPDATE meta_node SET retired_boot_history='{}'", wantOK: false},
		{name: "retired history empty string", update: "UPDATE meta_node SET retired_boot_history=''", wantOK: false},
		{name: "runtime epoch NULL", update: "UPDATE meta_node SET runtime_epoch=NULL", wantOK: false},
		{name: "runtime epoch advanced", update: "UPDATE meta_node SET runtime_epoch=1", wantOK: false},
		{name: "runtime protocol version NULL", update: "UPDATE meta_node SET runtime_protocol_version=NULL", wantOK: false},
		{name: "runtime protocol version advanced", update: "UPDATE meta_node SET runtime_protocol_version=1", wantOK: false},
		{name: "runtime confirmed revision NULL", update: "UPDATE meta_node SET runtime_confirmed_revision=NULL", wantOK: false},
		{name: "runtime confirmed revision advanced", update: "UPDATE meta_node SET runtime_confirmed_revision=1", wantOK: false},
		{name: "runtime confirmed at set", update: "UPDATE meta_node SET runtime_confirmed_at='2026-09-06T00:00:00Z'", wantOK: false},
		{name: "runtime identity status NULL", update: "UPDATE meta_node SET runtime_identity_status=NULL", wantOK: false},
		{name: "runtime identity status active", update: "UPDATE meta_node SET runtime_identity_status='active'", wantOK: false},
		{name: "runtime identity status unknown", update: "UPDATE meta_node SET runtime_identity_status='unknown'", wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newOpenAPISafetySQLite(t, "")
			if tt.update != "" {
				require.NoError(t, db.Exec(tt.update).Error)
			}
			err := rejectNonEmptyOpenAPISafetyState(db)
			if tt.wantOK {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errOpenAPIDownDenied)
		})
	}
}

func TestOpenAPIDownSafetyStateSQLiteMissingRequiredFieldRejects(t *testing.T) {
	for _, column := range []string{
		"access_epoch",
		"legacy_revoked_before",
		"current_boot_nonce",
		"retired_boot_history",
		"runtime_epoch",
		"runtime_protocol_version",
		"runtime_confirmed_revision",
		"runtime_confirmed_at",
		"runtime_identity_status",
	} {
		t.Run(column, func(t *testing.T) {
			db := newOpenAPISafetySQLite(t, column)
			require.ErrorIs(t, rejectNonEmptyOpenAPISafetyState(db), errOpenAPIDownDenied)
		})
	}
}

func TestOpenAPIDownGuardRejectsSQLiteEvenWithSwitch(t *testing.T) {
	t.Setenv(openAPIDownGuardSwitch, "1")
	db := newOpenAPISafetySQLite(t, "")
	require.ErrorIs(t, checkOpenAPIDownGuard(db, DialectUnknown, openAPISchemaMigration), errOpenAPIDownDenied)
}

func newOpenAPISafetySQLite(t *testing.T, omitColumn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })

	for _, table := range []string{
		"sys_openapi_client",
		"sys_openapi_client_scope",
		"sys_openapi_nonce",
		"sys_openapi_audit",
		"gb_openapi_play_grant",
		"gb_openapi_viewer",
	} {
		require.NoError(t, db.Exec("CREATE TABLE "+table+"(id INTEGER PRIMARY KEY)").Error)
	}

	deviceColumns := []string{"access_epoch INTEGER NULL", "legacy_revoked_before TEXT NULL"}
	if omitColumn == "access_epoch" {
		deviceColumns = deviceColumns[1:]
	}
	if omitColumn == "legacy_revoked_before" {
		deviceColumns = deviceColumns[:1]
	}
	require.NoError(t, db.Exec("CREATE TABLE gb_device("+strings.Join(deviceColumns, ",")+")").Error)

	nodeColumns := []string{
		"current_boot_nonce TEXT NULL",
		"retired_boot_history TEXT NULL",
		"runtime_epoch INTEGER NULL",
		"runtime_protocol_version INTEGER NULL",
		"runtime_confirmed_revision INTEGER NULL",
		"runtime_confirmed_at TEXT NULL",
		"runtime_identity_status TEXT NULL",
	}
	filteredNodeColumns := make([]string, 0, len(nodeColumns))
	for _, definition := range nodeColumns {
		if strings.HasPrefix(definition, omitColumn+" ") {
			continue
		}
		filteredNodeColumns = append(filteredNodeColumns, definition)
	}
	require.NoError(t, db.Exec("CREATE TABLE meta_node("+strings.Join(filteredNodeColumns, ",")+")").Error)

	if omitColumn == "" {
		require.NoError(t, db.Exec("INSERT INTO gb_device(access_epoch,legacy_revoked_before) VALUES(1,NULL)").Error)
		require.NoError(t, db.Exec("INSERT INTO meta_node(current_boot_nonce,retired_boot_history,runtime_epoch,runtime_protocol_version,runtime_confirmed_revision,runtime_confirmed_at,runtime_identity_status) VALUES(NULL,NULL,0,0,0,NULL,'unknown')").Error)
	}
	return db
}

type openAPIDownGuardDBState struct {
	mu                sync.Mutex
	databaseName      string
	counts            map[string]int64
	missingTables     map[string]bool
	deviceUnsafeCount int64
	nodeUnsafeCount   int64
	blockQueries      bool
	queryError        error
	queries           []string
}

func newOpenAPIDownGuardDBState() *openAPIDownGuardDBState {
	return &openAPIDownGuardDBState{
		databaseName:  "uvp_openapi_test_empty",
		counts:        make(map[string]int64),
		missingTables: make(map[string]bool),
	}
}

func (s *openAPIDownGuardDBState) queriesSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.queries...)
}

func (s *openAPIDownGuardDBState) query(query string) ([][]driver.Value, error) {
	return s.queryContext(context.Background(), query)
}

func (s *openAPIDownGuardDBState) queryContext(ctx context.Context, query string) ([][]driver.Value, error) {
	normalized := normalizeOpenAPIDownGuardSQL(query)
	s.mu.Lock()
	s.queries = append(s.queries, normalized)
	blockQueries := s.blockQueries
	err := s.queryError
	s.mu.Unlock()
	if blockQueries {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return nil, errors.New("probe did not finish")
		}
	}
	if err != nil {
		return nil, err
	}

	switch normalized {
	case "select database()", "select current_database()", "select db_name()":
		return [][]driver.Value{{s.databaseName}}, nil
	}
	if strings.HasPrefix(normalized, "select count(*) from gb_device where ") {
		return [][]driver.Value{{s.deviceUnsafeCount}}, nil
	}
	if strings.HasPrefix(normalized, "select count(*) from meta_node where ") {
		return [][]driver.Value{{s.nodeUnsafeCount}}, nil
	}
	if strings.HasPrefix(normalized, "select count(*) from ") {
		table := strings.TrimPrefix(normalized, "select count(*) from ")
		if space := strings.IndexByte(table, ' '); space >= 0 {
			table = table[:space]
		}
		if s.missingTables[table] {
			return nil, errors.New("missing table")
		}
		return [][]driver.Value{{s.counts[table]}}, nil
	}
	return nil, errors.New("unexpected probe")
}

func normalizeOpenAPIDownGuardSQL(query string) string {
	return strings.Join(strings.Fields(strings.ToLower(query)), " ")
}

var openAPIDownGuardDriverID atomic.Uint64

type openAPIDownGuardDriver struct{ state *openAPIDownGuardDBState }

func (d *openAPIDownGuardDriver) Open(string) (driver.Conn, error) {
	return &openAPIDownGuardConn{state: d.state}, nil
}

type openAPIDownGuardConn struct{ state *openAPIDownGuardDBState }

func (c *openAPIDownGuardConn) Prepare(query string) (driver.Stmt, error) {
	return &openAPIDownGuardStmt{conn: c, query: query}, nil
}

func (c *openAPIDownGuardConn) Close() error { return nil }

func (c *openAPIDownGuardConn) Begin() (driver.Tx, error) {
	return openAPIDownGuardTx{}, nil
}

func (c *openAPIDownGuardConn) Ping(context.Context) error { return nil }

func (c *openAPIDownGuardConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	values, err := c.state.queryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	var row []driver.Value
	if len(values) > 0 {
		row = values[0]
	}
	return &openAPIDownGuardRows{columns: []string{"value"}, row: row}, nil
}

func (c *openAPIDownGuardConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	return nil, fmt.Errorf("unexpected exec: %s", query)
}

type openAPIDownGuardStmt struct {
	conn  *openAPIDownGuardConn
	query string
}

func (s *openAPIDownGuardStmt) Close() error  { return nil }
func (s *openAPIDownGuardStmt) NumInput() int { return -1 }
func (s *openAPIDownGuardStmt) Exec([]driver.Value) (driver.Result, error) {
	return s.conn.ExecContext(context.Background(), s.query, nil)
}
func (s *openAPIDownGuardStmt) Query([]driver.Value) (driver.Rows, error) {
	return s.conn.QueryContext(context.Background(), s.query, nil)
}

type openAPIDownGuardRows struct {
	columns []string
	row     []driver.Value
	done    bool
}

func (r *openAPIDownGuardRows) Columns() []string { return r.columns }
func (r *openAPIDownGuardRows) Close() error      { return nil }
func (r *openAPIDownGuardRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(dest, r.row)
	return nil
}

type openAPIDownGuardTx struct{}

func (openAPIDownGuardTx) Commit() error   { return nil }
func (openAPIDownGuardTx) Rollback() error { return nil }

func newOpenAPIDownGuardDB(t *testing.T, state *openAPIDownGuardDBState, dialect Dialect) *gorm.DB {
	t.Helper()
	driverName := fmt.Sprintf("openapi_down_guard_%d", openAPIDownGuardDriverID.Add(1))
	sql.Register(driverName, &openAPIDownGuardDriver{state: state})
	sqlDB, err := sql.Open(driverName, "uvp_openapi_test_dsn")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	var dialector gorm.Dialector
	switch dialect {
	case DialectMySQL:
		dialector = mysql.New(mysql.Config{DriverName: driverName, DSN: "uvp_openapi_test_dsn", Conn: sqlDB, SkipInitializeWithVersion: true})
	case DialectPostgres:
		dialector = postgres.New(postgres.Config{DriverName: driverName, DSN: "dbname=uvp_openapi_test_dsn", Conn: sqlDB, PreferSimpleProtocol: true})
	case DialectSQLServer:
		dialector = sqlserver.New(sqlserver.Config{DriverName: driverName, DSN: "server=dsn-only", Conn: sqlDB})
	default:
		t.Fatalf("unsupported test dialect %q", dialect)
	}
	db, err := gorm.Open(dialector, &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	require.NoError(t, err)
	return db
}
