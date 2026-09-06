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
			require.Contains(t, queries, "select count(*) from gb_device where access_epoch <> 1 or legacy_revoked_before is not null")
			require.Contains(t, queries, "select count(*) from meta_node where (current_boot_nonce is not null and current_boot_nonce <> '') or (retired_boot_history is not null and retired_boot_history <> '' and retired_boot_history <> '[]' and retired_boot_history <> '{}') or coalesce(runtime_epoch, 0) <> 0")
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
	} {
		t.Run("database="+databaseName, func(t *testing.T) {
			state := newOpenAPIDownGuardDBState()
			state.databaseName = databaseName
			db := newOpenAPIDownGuardDB(t, state, DialectMySQL)
			require.ErrorIs(t, checkOpenAPIDownGuard(db, DialectMySQL, openAPIDownGuardTargets[0].name), errOpenAPIDownDenied)
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

type openAPIDownGuardDBState struct {
	mu                sync.Mutex
	databaseName      string
	counts            map[string]int64
	missingTables     map[string]bool
	deviceUnsafeCount int64
	nodeUnsafeCount   int64
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
	normalized := normalizeOpenAPIDownGuardSQL(query)
	s.mu.Lock()
	s.queries = append(s.queries, normalized)
	err := s.queryError
	s.mu.Unlock()
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

func (c *openAPIDownGuardConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	values, err := c.state.query(query)
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
