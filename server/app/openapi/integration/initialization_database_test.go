package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"
	"github.com/stretchr/testify/require"
)

const fullInitializationDatabasePrefix = "uvp_openapi_test_"

type fullInitializationConfig struct {
	dialect  string
	dsn      string
	initDir  string
	fileName string
}

type fullInitializationConnection struct {
	db   *sql.DB
	conn *sql.Conn
}

func resolveFullInitializationConfig(dialect, dsn, initDir string, required bool) (fullInitializationConfig, bool, error) {
	if dialect == "" && dsn == "" && initDir == "" {
		if required {
			return fullInitializationConfig{}, false, errors.New("explicit full-initialization database and SQL directory are required")
		}
		return fullInitializationConfig{}, true, nil
	}
	if dialect == "" || dsn == "" || initDir == "" {
		if required {
			return fullInitializationConfig{}, false, errors.New("UVP_OPENAPI_TEST_DIALECT, UVP_OPENAPI_TEST_DSN, and UVP_OPENAPI_TEST_INIT_DIR must all be set")
		}
		return fullInitializationConfig{}, true, nil
	}
	if !filepath.IsAbs(initDir) {
		return fullInitializationConfig{}, false, errors.New("UVP_OPENAPI_TEST_INIT_DIR must be an absolute directory")
	}
	fileName, ok := fullInitializationFileName(dialect)
	if !ok {
		return fullInitializationConfig{}, false, fmt.Errorf("unsupported full-initialization dialect %q", dialect)
	}
	return fullInitializationConfig{dialect: dialect, dsn: dsn, initDir: initDir, fileName: fileName}, false, nil
}

func fullInitializationFileName(dialect string) (string, bool) {
	switch dialect {
	case "mysql":
		return "uvp-gb28181.sql", true
	case "postgresql":
		return "postgresql_converted.sql", true
	case "sqlserver":
		return "sqlserver_converted.sql", true
	default:
		return "", false
	}
}

func isDedicatedInitializationDatabase(name string) bool {
	return strings.HasPrefix(name, fullInitializationDatabasePrefix) && len(name) > len(fullInitializationDatabasePrefix)
}

func requireEmptyInitializationInventory(tables []string) error {
	if len(tables) != 0 {
		return fmt.Errorf("refusing non-empty database inventory: %s", strings.Join(tables, ", "))
	}
	return nil
}

var forbiddenInitializationSQL = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{name: "USE", pattern: regexp.MustCompile(`(?im)(?:^|;)[\t \r\n]*USE\b`)},
	{name: "CREATE DATABASE/SCHEMA", pattern: regexp.MustCompile(`(?im)(?:^|;)[\t \r\n]*CREATE\s+(?:DATABASE|SCHEMA)\b`)},
	{name: "ALTER DATABASE", pattern: regexp.MustCompile(`(?im)(?:^|;)[\t \r\n]*ALTER\s+DATABASE\b`)},
	{name: "DROP DATABASE/SCHEMA", pattern: regexp.MustCompile(`(?im)(?:^|;)[\t \r\n]*DROP\s+(?:DATABASE|SCHEMA)\b`)},
	{name: "user permission", pattern: regexp.MustCompile(`(?i)\b(?:GRANT|REVOKE|CREATE\s+USER|ALTER\s+USER|DROP\s+USER|CREATE\s+LOGIN|ALTER\s+LOGIN|DROP\s+LOGIN|CREATE\s+ROLE|ALTER\s+ROLE|DROP\s+ROLE|SET\s+ROLE)\b`)},
	{name: "external file", pattern: regexp.MustCompile(`(?i)\b(?:LOAD\s+DATA|BULK\s+INSERT|COPY\s+[^;\n]+\s+(?:FROM|TO)|INTO\s+(?:OUTFILE|DUMPFILE)|OPENROWSET|OPENDATASOURCE|XP_CMDSHELL|BACKUP\s+DATABASE|RESTORE\s+DATABASE)\b`)},
}

func validateInitializationSQL(body string) error {
	text := stripInitializationSQLCommentsAndLiterals(body)
	for _, forbidden := range forbiddenInitializationSQL {
		if forbidden.pattern.MatchString(text) {
			return fmt.Errorf("full initialization SQL contains forbidden %s statement", forbidden.name)
		}
	}
	return nil
}

func stripInitializationSQLCommentsAndLiterals(body string) string {
	var out strings.Builder
	out.Grow(len(body))
	for i := 0; i < len(body); {
		switch {
		case body[i] == '-' && i+1 < len(body) && body[i+1] == '-':
			for i < len(body) && body[i] != '\n' {
				i++
			}
		case body[i] == '#':
			for i < len(body) && body[i] != '\n' {
				i++
			}
		case body[i] == '/' && i+1 < len(body) && body[i+1] == '*':
			i += 2
			for i+1 < len(body) && !(body[i] == '*' && body[i+1] == '/') {
				if body[i] == '\n' {
					out.WriteByte('\n')
				}
				i++
			}
			if i+1 < len(body) {
				i += 2
			}
		case body[i] == '\'' || body[i] == '"' || body[i] == '`':
			quote := body[i]
			out.WriteByte(' ')
			i++
			for i < len(body) {
				if body[i] == '\n' {
					out.WriteByte('\n')
				}
				if body[i] == quote {
					if i+1 < len(body) && body[i+1] == quote {
						out.WriteString("  ")
						i += 2
						continue
					}
					i++
					break
				}
				if body[i] == '\\' && quote == '\'' && i+1 < len(body) {
					out.WriteString("  ")
					i += 2
					continue
				}
				out.WriteByte(' ')
				i++
			}
		default:
			out.WriteByte(body[i])
			i++
		}
	}
	return out.String()
}

func TestOpenAPIDatabaseFullInitialization(t *testing.T) {
	cfg, skip, err := resolveFullInitializationConfig(
		os.Getenv("UVP_OPENAPI_TEST_DIALECT"),
		os.Getenv("UVP_OPENAPI_TEST_DSN"),
		os.Getenv("UVP_OPENAPI_TEST_INIT_DIR"),
		os.Getenv("UVP_OPENAPI_INTEGRATION_REQUIRED") == "1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if skip {
		t.Skip("no explicit full-initialization database target; not an acceptance pass")
	}

	path := filepath.Join(cfg.initDir, cfg.fileName)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateInitializationSQL(string(body)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	connection, err := openFullInitializationConnection(ctx, cfg)
	if err != nil {
		t.Fatal("full initialization database connection failed (DSN suppressed)")
	}
	defer func() {
		_ = connection.conn.Close()
		_ = connection.db.Close()
	}()
	if err := connection.conn.PingContext(ctx); err != nil {
		t.Fatal("full initialization database ping failed")
	}

	databaseName, err := scanInitializationString(ctx, connection.conn, initializationDatabaseNameQuery(cfg.dialect))
	if err != nil {
		t.Fatal("cannot identify full initialization database")
	}
	if !isDedicatedInitializationDatabase(databaseName) {
		t.Fatalf("refusing non-test database %q", databaseName)
	}

	tables, err := listInitializationTables(ctx, connection.conn, cfg.dialect)
	if err != nil {
		t.Fatal("cannot inventory full initialization database")
	}
	if err := requireEmptyInitializationInventory(tables); err != nil {
		t.Fatal(err)
	}

	// Do not split this script: PostgreSQL contains DO blocks and MySQL needs
	// multiStatements. Each dialect is configured for one physical connection
	// and the complete file is sent as one native driver Exec.
	if _, err := connection.conn.ExecContext(ctx, string(body)); err != nil {
		t.Fatalf("full initialization SQL failed for %s (%s%s): %v", cfg.dialect, cfg.fileName, initializationErrorLocation(err, string(body)), err)
	}
	assertFullInitializationState(t, connection.conn, ctx, cfg.dialect)
	// A dedicated initialization database may be replayed during provisioning;
	// the second execution must complete without duplicating security objects.
	if _, err := connection.conn.ExecContext(ctx, string(body)); err != nil {
		t.Fatalf("second full initialization SQL failed for %s (%s%s): %v", cfg.dialect, cfg.fileName, initializationErrorLocation(err, string(body)), err)
	}
	assertFullInitializationState(t, connection.conn, ctx, cfg.dialect)
	t.Logf("%s full initialization executed on dedicated %s database; no default OpenAPI client/nonce/grant/viewer rows", cfg.dialect, databaseName)
}

// PostgreSQL positions are one-based character offsets, not byte offsets.
// Report only the source line, never the failing statement or seed values.
func initializationErrorLocation(err error, body string) string {
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Position <= 0 {
		return ""
	}
	line, position := 1, int32(1)
	for _, r := range body {
		if position == pgError.Position {
			return fmt.Sprintf(":%d", line)
		}
		if r == '\n' {
			line++
		}
		position++
	}
	return ""
}

func TestFullInitializationErrorLocationDoesNotExposeSQL(t *testing.T) {
	err := &pgconn.PgError{Position: 6}
	require.Equal(t, ":2", initializationErrorLocation(err, "中文;\n( ) secret-seed"))
	require.Empty(t, initializationErrorLocation(errors.New("driver failed"), "secret-seed"))
}

func openFullInitializationConnection(ctx context.Context, cfg fullInitializationConfig) (*fullInitializationConnection, error) {
	var db *sql.DB
	var err error
	switch cfg.dialect {
	case "mysql":
		parsed, parseErr := mysql.ParseDSN(cfg.dsn)
		if parseErr != nil {
			return nil, parseErr
		}
		parsed.MultiStatements = true
		db, err = sql.Open("mysql", parsed.FormatDSN())
	case "postgresql":
		parsed, parseErr := pgx.ParseConfig(cfg.dsn)
		if parseErr != nil {
			return nil, parseErr
		}
		parsed.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
		db = sql.OpenDB(stdlib.GetConnector(*parsed))
	case "sqlserver":
		db, err = sql.Open("sqlserver", cfg.dsn)
	default:
		return nil, fmt.Errorf("unsupported full-initialization dialect %q", cfg.dialect)
	}
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	conn, err := db.Conn(ctx)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &fullInitializationConnection{db: db, conn: conn}, nil
}

func initializationDatabaseNameQuery(dialect string) string {
	switch dialect {
	case "mysql":
		return "SELECT DATABASE()"
	case "postgresql":
		return "SELECT current_database()"
	case "sqlserver":
		return "SELECT DB_NAME()"
	default:
		return ""
	}
}

func listInitializationTables(ctx context.Context, conn *sql.Conn, dialect string) ([]string, error) {
	query := ""
	switch dialect {
	case "mysql":
		query = "SELECT TABLE_NAME FROM information_schema.tables WHERE table_schema = DATABASE()"
	case "postgresql":
		query = "SELECT table_schema || '.' || table_name FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog', 'information_schema') AND table_schema NOT LIKE 'pg_toast%'"
	case "sqlserver":
		query = "SELECT TABLE_SCHEMA + '.' + TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_CATALOG = DB_NAME()"
	default:
		return nil, fmt.Errorf("unsupported full-initialization dialect %q", dialect)
	}
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

func scanInitializationString(ctx context.Context, conn *sql.Conn, query string) (string, error) {
	var value string
	err := conn.QueryRowContext(ctx, query).Scan(&value)
	return value, err
}

func assertFullInitializationState(t *testing.T, conn *sql.Conn, ctx context.Context, dialect string) {
	t.Helper()
	tables, err := listInitializationTables(ctx, conn, dialect)
	require.NoError(t, err)
	tableSet := make(map[string]struct{}, len(tables))
	for _, table := range tables {
		normalized := strings.ToLower(strings.TrimSpace(table))
		if dot := strings.LastIndex(normalized, "."); dot >= 0 {
			normalized = normalized[dot+1:]
		}
		tableSet[strings.Trim(normalized, "`[]\"")] = struct{}{}
	}
	for _, table := range []string{
		"sys_openapi_client",
		"sys_openapi_client_scope",
		"sys_openapi_nonce",
		"sys_openapi_audit",
		"gb_openapi_play_grant",
		"gb_openapi_viewer",
		"gb_device_operation_intent",
	} {
		if _, ok := tableSet[table]; !ok {
			t.Fatalf("full initialization missing OpenAPI table %s", table)
		}
	}

	for _, column := range []struct{ table, name string }{
		{table: "gb_device_operation_intent", name: "rtp_steps_json"},
		{table: "gb_device_operation_intent", name: "sip_steps_json"},
		{table: "gb_device", name: "access_epoch"},
		{table: "gb_device", name: "cleanup_completed_epoch"},
		{table: "gb_device", name: "legacy_revoked_before"},
		{table: "meta_node", name: "current_boot_nonce"},
		{table: "meta_node", name: "retired_boot_history"},
		{table: "meta_node", name: "runtime_epoch"},
		{table: "meta_node", name: "runtime_protocol_version"},
		{table: "meta_node", name: "runtime_confirmed_revision"},
		{table: "meta_node", name: "runtime_confirmed_at"},
		{table: "meta_node", name: "runtime_identity_status"},
	} {
		count, queryErr := initializationColumnCount(ctx, conn, dialect, column.table, column.name)
		require.NoError(t, queryErr, column.table+"."+column.name)
		require.EqualValues(t, 1, count, "missing security column %s.%s", column.table, column.name)
	}
	require.EqualValues(t, 1, cleanupBarrierConstraintCount(t, ctx, conn, dialect), "missing cleanup barrier constraint")

	securityRows, err := initializationRowCount(ctx, conn, "sys_openapi_security_state")
	require.NoError(t, err)
	require.EqualValues(t, 1, securityRows, "must-auth singleton row count")
	var id int64
	var locked bool
	var lockedAt sql.NullTime
	var lockVersion int64
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT id, must_auth_locked, locked_at, lock_version FROM sys_openapi_security_state").Scan(&id, &locked, &lockedAt, &lockVersion))
	require.EqualValues(t, 1, id)
	require.False(t, locked)
	require.False(t, lockedAt.Valid)
	require.Zero(t, lockVersion)

	for _, table := range []string{"sys_openapi_client", "sys_openapi_client_scope", "sys_openapi_nonce", "sys_openapi_audit", "gb_openapi_play_grant", "gb_openapi_viewer", "gb_device_operation_intent"} {
		count, countErr := initializationRowCount(ctx, conn, table)
		require.NoError(t, countErr, table)
		require.Zero(t, count, "full initialization must not seed %s", table)
	}

	routes := []struct{ path, method string }{
		{path: "/api/gb28181/openapi-clients", method: "GET"},
		{path: "/api/gb28181/openapi-clients", method: "POST"},
		{path: "/api/gb28181/openapi-clients/capabilities", method: "GET"},
		{path: "/api/gb28181/openapi-clients/:id", method: "GET"},
		{path: "/api/gb28181/openapi-clients/:id/scopes", method: "PUT"},
		{path: "/api/gb28181/openapi-clients/:id/rotate-secret", method: "POST"},
		{path: "/api/gb28181/openapi-clients/:id/enable", method: "POST"},
		{path: "/api/gb28181/openapi-clients/:id/disable", method: "POST"},
		{path: "/api/gb28181/openapi-clients/:id/revoke", method: "POST"},
		{path: "/api/gb28181/openapi-clients/:id/audits", method: "GET"},
		{path: "/api/gb28181/openapi-clients/:id/revocation-status", method: "GET"},
	}
	for i, route := range routes {
		query := "SELECT COUNT(*) FROM sys_api WHERE path = " + initializationPlaceholder(dialect, 1) + " AND method = " + initializationPlaceholder(dialect, 2) + " AND deleted_at IS NULL"
		var count int64
		require.NoError(t, conn.QueryRowContext(ctx, query, route.path, route.method).Scan(&count), "route %d", i)
		require.EqualValues(t, 1, count, "missing OpenAPI route %s %s", route.method, route.path)
	}
	for i, route := range routes {
		rootTupleQuery := "SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype = " + initializationPlaceholder(dialect, 1) + " AND v0 = " + initializationPlaceholder(dialect, 2) + " AND v1 = " + initializationPlaceholder(dialect, 3) + " AND v2 = " + initializationPlaceholder(dialect, 4) + " AND v3 = " + initializationPlaceholder(dialect, 5)
		var rootTupleCount int64
		require.NoError(t, conn.QueryRowContext(ctx, rootTupleQuery, "p", "role_1", route.path, route.method, "*").Scan(&rootTupleCount), "root tuple %d", i)
		require.EqualValues(t, 1, rootTupleCount, "missing or duplicate root tuple %s %s", route.method, route.path)
	}
}

func initializationColumnCount(ctx context.Context, conn *sql.Conn, dialect, table, column string) (int64, error) {
	var query string
	switch dialect {
	case "mysql":
		query = "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = " + initializationPlaceholder(dialect, 1) + " AND column_name = " + initializationPlaceholder(dialect, 2)
	case "postgresql":
		query = "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = " + initializationPlaceholder(dialect, 1) + " AND column_name = " + initializationPlaceholder(dialect, 2)
	case "sqlserver":
		query = "SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_CATALOG = DB_NAME() AND TABLE_SCHEMA = 'dbo' AND TABLE_NAME = " + initializationPlaceholder(dialect, 1) + " AND COLUMN_NAME = " + initializationPlaceholder(dialect, 2)
	default:
		return 0, fmt.Errorf("unsupported full-initialization dialect %q", dialect)
	}
	var count int64
	err := conn.QueryRowContext(ctx, query, table, column).Scan(&count)
	return count, err
}

func initializationRowCount(ctx context.Context, conn *sql.Conn, table string) (int64, error) {
	var count int64
	err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count)
	return count, err
}

func initializationPlaceholder(dialect string, n int) string {
	switch dialect {
	case "postgresql":
		return "$" + strconv.Itoa(n)
	case "sqlserver":
		return "@p" + strconv.Itoa(n)
	default:
		return "?"
	}
}

func TestFullInitializationSafetyGate(t *testing.T) {
	cases := []struct {
		name     string
		dialect  string
		dsn      string
		dir      string
		required bool
		wantSkip bool
		wantErr  bool
	}{
		{name: "no config is skipped", wantSkip: true},
		{name: "required config is fatal", required: true, wantErr: true},
		{name: "unsupported dialect is fatal", dialect: "sqlite", dsn: "dsn", dir: "/tmp/init", wantErr: true},
		{name: "relative init dir is fatal", dialect: "mysql", dsn: "dsn", dir: "relative", wantErr: true},
		{name: "valid mysql config runs", dialect: "mysql", dsn: "dsn", dir: "/tmp/init"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, skip, err := resolveFullInitializationConfig(tc.dialect, tc.dsn, tc.dir, tc.required)
			require.Equal(t, tc.wantSkip, skip)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.wantSkip {
				require.Empty(t, cfg)
				return
			}
			require.Equal(t, tc.dialect, cfg.dialect)
			require.Equal(t, tc.dsn, cfg.dsn)
			require.Equal(t, tc.dir, cfg.initDir)
		})
	}
}

func TestFullInitializationDatabaseSafetyGate(t *testing.T) {
	for _, tc := range []struct {
		dialect string
		name    string
	}{
		{dialect: "mysql", name: "uvp-gb28181.sql"},
		{dialect: "postgresql", name: "postgresql_converted.sql"},
		{dialect: "sqlserver", name: "sqlserver_converted.sql"},
	} {
		name, ok := fullInitializationFileName(tc.dialect)
		require.True(t, ok)
		require.Equal(t, tc.name, name)
	}
	require.True(t, isDedicatedInitializationDatabase("uvp_openapi_test_mysql_001"))
	require.True(t, isDedicatedInitializationDatabase("uvp_openapi_test_pg_001"))
	for _, name := range []string{"uvp_openapi_prod", "openapi_test_mysql_001", "uvp_openapi_test_", ""} {
		require.False(t, isDedicatedInitializationDatabase(name), name)
	}
	require.NoError(t, requireEmptyInitializationInventory(nil))
	require.Error(t, requireEmptyInitializationInventory([]string{"sys_user"}))
}

func TestFullInitializationSQLSafetyGate(t *testing.T) {
	for _, sql := range []string{
		"USE uvp_openapi_test_mysql;",
		"CREATE DATABASE another;",
		"ALTER DATABASE uvp_openapi_test_mysql SET READ_ONLY;",
		"DROP DATABASE uvp_openapi_test_mysql;",
		"GRANT ALL ON *.* TO 'root'@'%';",
		"REVOKE ALL PRIVILEGES, GRANT OPTION FROM 'root'@'%';",
		"LOAD DATA INFILE '/tmp/users.csv' INTO TABLE sys_user;",
		"BULK INSERT sys_user FROM '/tmp/users.csv';",
		"COPY sys_user FROM '/tmp/users.csv';",
		"COPY sys_user TO '/tmp/users.csv';",
		"SELECT * INTO OUTFILE '/tmp/users.csv' FROM sys_user;",
	} {
		require.Error(t, validateInitializationSQL(sql), sql)
	}
	for _, sql := range []string{
		"CREATE TABLE safe_table (id BIGINT); USE uvp_openapi_test_mysql;",
		"CREATE TABLE safe_table (id BIGINT); CREATE DATABASE another;",
		"CREATE TABLE safe_table (id BIGINT); ALTER DATABASE uvp_openapi_test_mysql SET READ_ONLY;",
		"CREATE TABLE safe_table (id BIGINT); DROP DATABASE uvp_openapi_test_mysql;",
	} {
		require.Error(t, validateInitializationSQL(sql), sql)
	}
	require.NoError(t, validateInitializationSQL("-- use is only a comment\nCREATE TABLE safe_table (id BIGINT);"))
}

func TestFullInitializationSQLFilesPassStaticSafetyGate(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
			require.NoError(t, err)
			require.NoError(t, validateInitializationSQL(string(body)))
		})
	}
}

func TestPostgreSQLFullInitializationMenuReparentingPreservesParentWithoutAnchor(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("../../../resource/database", "postgresql_converted.sql"))
	require.NoError(t, err)
	anchorExpression := "parent_id=COALESCE((SELECT dm.parent_id FROM sys_menu dm WHERE dm.path IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND dm.deleted_at IS NULL ORDER BY CASE WHEN dm.path='/gb28181/device-mgmt/index' THEN 0 ELSE 1 END,dm.id LIMIT 1),parent_id)"
	require.Equal(t, 2, strings.Count(string(body), anchorExpression), "cloud-recording reparenting must preserve an existing parent when the optional device-menu anchor is absent")
}

func TestPostgreSQLFullInitializationDropsJobResultsBeforeJobs(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("../../../resource/database", "postgresql_converted.sql"))
	require.NoError(t, err)
	childDrop := strings.Index(string(body), "DROP TABLE IF EXISTS sys_job_results;")
	parentDrop := strings.Index(string(body), "DROP TABLE IF EXISTS sys_jobs;")
	require.NotEqual(t, -1, childDrop)
	require.NotEqual(t, -1, parentDrop)
	require.Less(t, childDrop, parentDrop, "PostgreSQL full initialization must drop the foreign-key child before its parent")
}
