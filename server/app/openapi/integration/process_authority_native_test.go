package integration

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/processauthority"
	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

func TestOpenAPIProcessAuthorityNative(t *testing.T) {
	if os.Getenv("UVP_OPENAPI_AUTHORITY_NATIVE") != "1" {
		t.Skip("explicit isolated authority database target required; not an acceptance pass")
	}
	dialect, dsn := os.Getenv("UVP_OPENAPI_TEST_DIALECT"), os.Getenv("UVP_OPENAPI_TEST_DSN")
	require.NotEmpty(t, dsn)
	var driver gorm.Dialector
	var query, suffix string
	switch dialect {
	case "mysql":
		driver, query = mysql.Open(dsn), "SELECT DATABASE()"
	case "postgresql":
		driver, query, suffix = postgres.Open(dsn), "SELECT current_database()", "-postgresql"
	case "sqlserver":
		driver, query, suffix = sqlserver.Open(dsn), "SELECT DB_NAME()", "-sqlserver"
	default:
		t.Fatal("explicit supported dialect required")
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("isolated authority database connection failed (DSN suppressed)")
	}
	base, err := db.DB()
	require.NoError(t, err)
	defer base.Close()
	base.SetMaxOpenConns(8)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db = db.WithContext(ctx)
	var name string
	require.NoError(t, db.Raw(query).Scan(&name).Error)
	require.True(t, strings.HasPrefix(name, "uvp_openapi_test_"), "refusing non-test database")
	tables, err := db.Migrator().GetTables()
	require.NoError(t, err)
	require.Empty(t, tables, "refusing any preexisting database content")
	stem := "migrations/2026-09-08-openapi-process-authority" + suffix
	run := func(file string) {
		t.Helper()
		body, err := migrationsfs.FS.ReadFile(file)
		require.NoError(t, err)
		require.NoError(t, db.Connection(func(conn *gorm.DB) error {
			for _, statement := range nativeSchemaStatements(string(body)) {
				if err := conn.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		}))
	}
	run(stem + ".sql")
	run(stem + ".sql")
	stateDir := t.TempDir()
	require.NoError(t, os.Chmod(stateDir, 0700))
	lock, err := processauthority.AcquireLocalLock(stateDir)
	require.NoError(t, err, "test TMPDIR must be a supported persistent local filesystem")
	defer lock.Close()
	domain, err := lock.DomainID()
	require.NoError(t, err)
	old := models.ProcessGeneration{GenerationID: strings.Repeat("d", 32), DomainID: domain, StartedAt: time.Now().UTC().Add(-time.Hour)}
	require.NoError(t, db.Create(&old).Error)
	require.NoError(t, db.Omit("Generation").Create(&models.ProcessAuthority{ID: 1, DomainID: domain, CurrentGenerationID: old.GenerationID, RowVersion: 1}).Error)
	authority, err := processauthority.Register(ctx, db, lock)
	require.NoError(t, err)
	defer authority.Seal()
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return authority.RequireRetiredTx(tx, old.GenerationID) }))
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := db.Transaction(authority.CheckTx); err != nil {
				t.Errorf("native fence failed: %v", err)
			} else {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 20, wins.Load())
	var current models.ProcessAuthority
	require.NoError(t, db.Take(&current).Error)
	require.EqualValues(t, 23, current.RowVersion)
	for _, bad := range []map[string]any{{"id": 2}, {"row_version": 0}, {"domain_id": strings.Repeat("e", 32)}, {"current_generation_id": strings.Repeat("f", 32)}} {
		require.Error(t, db.Model(&models.ProcessAuthority{}).Where("id=1").Updates(bad).Error)
	}
	for _, bad := range []string{"", strings.Repeat("0", 32), strings.Repeat("A", 32), strings.Repeat("a", 31) + " "} {
		require.Error(t, db.Create(&models.ProcessGeneration{GenerationID: bad, DomainID: domain, StartedAt: old.StartedAt}).Error)
	}
	run(stem + "-down.sql")
	run(stem + ".sql")
	var count int64
	require.NoError(t, db.Model(&models.ProcessGeneration{}).Count(&count).Error)
	require.EqualValues(t, 2, count, "rollback retains immutable generations")
	require.NoError(t, db.Transaction(authority.CheckTx))
	t.Log("native up twice, confirmed root registration, same-domain old generation, 20 concurrent serialized fences, constraints and retained down/up passed")
}
