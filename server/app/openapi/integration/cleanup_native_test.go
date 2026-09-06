package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	playauth "uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

type cleanupBarrierConfig struct {
	dialect   string
	dsn       string
	migration string
	upName    string
	downName  string
}

func resolveCleanupBarrierConfig(dialect, dsn, migrationDir string, required bool) (cleanupBarrierConfig, bool, error) {
	if dialect == "" && dsn == "" && migrationDir == "" {
		if required {
			return cleanupBarrierConfig{}, false, errors.New("explicit cleanup-barrier database and migration directory are required")
		}
		return cleanupBarrierConfig{}, true, nil
	}
	if dialect == "" || dsn == "" || migrationDir == "" {
		if required {
			return cleanupBarrierConfig{}, false, errors.New("UVP_OPENAPI_TEST_DIALECT, UVP_OPENAPI_TEST_DSN, and UVP_OPENAPI_TEST_MIGRATION_DIR must all be set")
		}
		return cleanupBarrierConfig{}, true, nil
	}
	if !filepath.IsAbs(migrationDir) {
		return cleanupBarrierConfig{}, false, errors.New("UVP_OPENAPI_TEST_MIGRATION_DIR must be an absolute directory")
	}
	var upName, downName string
	switch dialect {
	case "mysql":
		upName = "2026-09-06-device-cleanup-barrier.sql"
		downName = "2026-09-06-device-cleanup-barrier-down.sql"
	case "postgresql":
		upName = "2026-09-06-device-cleanup-barrier-postgresql.sql"
		downName = "2026-09-06-device-cleanup-barrier-postgresql-down.sql"
	default:
		return cleanupBarrierConfig{}, false, fmt.Errorf("unsupported cleanup-barrier dialect %q", dialect)
	}
	return cleanupBarrierConfig{dialect: dialect, dsn: dsn, migration: migrationDir, upName: upName, downName: downName}, false, nil
}

func TestOpenAPIDeviceCleanupBarrierMigration(t *testing.T) {
	cfg, skip, err := resolveCleanupBarrierConfig(
		os.Getenv("UVP_OPENAPI_TEST_DIALECT"),
		os.Getenv("UVP_OPENAPI_TEST_DSN"),
		os.Getenv("UVP_OPENAPI_TEST_MIGRATION_DIR"),
		os.Getenv("UVP_OPENAPI_INTEGRATION_REQUIRED") == "1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if skip {
		t.Skip("no explicit cleanup-barrier database target; not an acceptance pass")
	}

	body, err := os.ReadFile(filepath.Join(cfg.migration, cfg.upName))
	require.NoError(t, err)
	downBody, err := os.ReadFile(filepath.Join(cfg.migration, cfg.downName))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	connection, err := openFullInitializationConnection(ctx, fullInitializationConfig{dialect: cfg.dialect, dsn: cfg.dsn})
	require.NoError(t, err, "cleanup-barrier database connection failed (DSN suppressed)")
	defer func() {
		_ = connection.conn.Close()
		_ = connection.db.Close()
	}()
	require.NoError(t, connection.conn.PingContext(ctx))

	databaseName, err := scanInitializationString(ctx, connection.conn, initializationDatabaseNameQuery(cfg.dialect))
	require.NoError(t, err)
	require.True(t, isDedicatedInitializationDatabase(databaseName), "refusing non-test database %q", databaseName)
	tables, err := listInitializationTables(ctx, connection.conn, cfg.dialect)
	require.NoError(t, err)
	require.NoError(t, requireEmptyInitializationInventory(tables))

	createCleanupBarrierFixture(t, ctx, connection.conn, cfg.dialect)
	applyCleanupBarrierScript(t, ctx, connection.conn, body)
	assertCleanupBarrierState(t, ctx, connection.conn, cfg.dialect, 2, 1)

	// The exact up script is safe to replay: it must preserve the pending
	// waterline and must not create a second check constraint.
	applyCleanupBarrierScript(t, ctx, connection.conn, body)
	assertCleanupBarrierState(t, ctx, connection.conn, cfg.dialect, 2, 1)
	require.EqualValues(t, 1, cleanupBarrierConstraintCount(t, ctx, connection.conn, cfg.dialect))

	for _, invalid := range []int64{0, 3} {
		placeholder := initializationPlaceholder(cfg.dialect, 1)
		_, updateErr := connection.conn.ExecContext(ctx, "UPDATE gb_device SET cleanup_completed_epoch = "+placeholder+" WHERE id = 1", invalid)
		require.Error(t, updateErr, "invalid cleanup waterline %d must be rejected", invalid)
		assertCleanupBarrierState(t, ctx, connection.conn, cfg.dialect, 2, 1)
	}

	// The down file is intentionally forward-only. It must not remove or
	// lower a pending barrier, so a subsequent up remains an exact no-op.
	applyCleanupBarrierScript(t, ctx, connection.conn, downBody)
	assertCleanupBarrierState(t, ctx, connection.conn, cfg.dialect, 2, 1)
	applyCleanupBarrierScript(t, ctx, connection.conn, body)
	assertCleanupBarrierState(t, ctx, connection.conn, cfg.dialect, 2, 1)

	assertNativeCleanupStoreConcurrentComplete(t, ctx, connection, cfg)
	assertCleanupBarrierState(t, ctx, connection.conn, cfg.dialect, 2, 2)

	t.Logf("%s cleanup barrier migration up/up/down/up and native concurrent Complete passed on dedicated %s database; final waterline is 2/2", cfg.dialect, databaseName)
}

func createCleanupBarrierFixture(t *testing.T, ctx context.Context, conn *sql.Conn, dialect string) {
	t.Helper()
	// Keep the fixture deliberately smaller than the production device table:
	// the migration contract only requires these two security columns.
	_, err := conn.ExecContext(ctx, "CREATE TABLE gb_device (id BIGINT PRIMARY KEY, device_id VARCHAR(20) NOT NULL, access_epoch BIGINT NOT NULL, deleted_at TIMESTAMP NULL)")
	require.NoError(t, err, "create %s cleanup fixture", dialect)
	_, err = conn.ExecContext(ctx, "INSERT INTO gb_device (id, device_id, access_epoch, deleted_at) VALUES (1, '34020000001320000001', 2, NULL)")
	require.NoError(t, err)
}

func assertNativeCleanupStoreConcurrentComplete(t *testing.T, ctx context.Context, connection *fullInitializationConnection, cfg cleanupBarrierConfig) {
	t.Helper()
	connection.db.SetMaxOpenConns(4)
	connection.db.SetMaxIdleConns(4)
	var dialector gorm.Dialector
	switch cfg.dialect {
	case "mysql":
		dialector = mysql.New(mysql.Config{Conn: connection.db})
	case "postgresql":
		dialector = postgres.New(postgres.Config{Conn: connection.db, PreferSimpleProtocol: true})
	default:
		t.Fatalf("unsupported cleanup store dialect %q", cfg.dialect)
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	store := playauth.NewDeviceCleanupStore(db)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { errs <- store.Complete(ctx, "34020000001320000001", 2) }()
	}
	for i := 0; i < 2; i++ {
		require.NoError(t, <-errs, "native concurrent completion %d", i)
	}
}

func applyCleanupBarrierScript(t *testing.T, ctx context.Context, conn *sql.Conn, body []byte) {
	t.Helper()
	for index, statement := range nativeSchemaStatements(string(body)) {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			t.Fatalf("cleanup-barrier statement %d failed: %v", index, err)
		}
	}
}

func assertCleanupBarrierState(t *testing.T, ctx context.Context, conn *sql.Conn, dialect string, wantAccess, wantCompleted int64) {
	t.Helper()
	var access, completed int64
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT access_epoch, cleanup_completed_epoch FROM gb_device WHERE id = 1").Scan(&access, &completed))
	require.Equal(t, wantAccess, access)
	require.Equal(t, wantCompleted, completed)
	require.EqualValues(t, 1, cleanupBarrierConstraintCount(t, ctx, conn, dialect))
}

func cleanupBarrierConstraintCount(t *testing.T, ctx context.Context, conn *sql.Conn, dialect string) int64 {
	t.Helper()
	query := ""
	switch dialect {
	case "mysql":
		query = "SELECT COUNT(*) FROM information_schema.table_constraints WHERE constraint_schema = DATABASE() AND table_name = 'gb_device' AND constraint_name = 'ck_gb_device_cleanup_completed_epoch'"
	case "postgresql":
		query = "SELECT COUNT(*) FROM pg_constraint WHERE conrelid = 'gb_device'::regclass AND conname = 'ck_gb_device_cleanup_completed_epoch'"
	case "sqlserver":
		query = "SELECT COUNT(*) FROM sys.check_constraints WHERE parent_object_id = OBJECT_ID(N'dbo.gb_device') AND name = N'ck_gb_device_cleanup_completed_epoch'"
	default:
		t.Fatalf("unsupported cleanup-barrier dialect %q", dialect)
	}
	var count int64
	require.NoError(t, conn.QueryRowContext(ctx, query).Scan(&count))
	return count
}
