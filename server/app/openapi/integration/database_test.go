package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

// This core gate intentionally does not claim the later HTTP/media/T16 suite.
// The database must be dedicated AND empty before any DDL is permitted.
func TestOpenAPIDatabaseCoreMigration(t *testing.T) {
	dialect, dsn := os.Getenv("UVP_OPENAPI_TEST_DIALECT"), os.Getenv("UVP_OPENAPI_TEST_DSN")
	if dialect == "" || dsn == "" {
		if os.Getenv("UVP_OPENAPI_INTEGRATION_REQUIRED") == "1" {
			t.Fatal("explicit isolated database configuration required")
		}
		t.Skip("no explicit isolated database target; not an acceptance pass")
	}
	var dialector gorm.Dialector
	var query, suffix string
	switch dialect {
	case "mysql":
		dialector = mysql.Open(dsn)
		query = "SELECT DATABASE()"
	case "postgresql":
		dialector = postgres.Open(dsn)
		query = "SELECT current_database()"
		suffix = "-postgresql"
	case "sqlserver":
		dialector = sqlserver.Open(dsn)
		query = "SELECT DB_NAME()"
		suffix = "-sqlserver"
	default:
		t.Fatal("unsupported test dialect")
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("test database connection failed (DSN and driver error suppressed)")
	}
	raw, err := db.DB()
	if err != nil {
		t.Fatal("test database handle unavailable")
	}
	defer raw.Close()
	raw.SetMaxOpenConns(10)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	var name string
	if err := db.Raw(query).Scan(&name).Error; err != nil {
		t.Fatal("cannot identify test database")
	}
	if !strings.HasPrefix(name, "uvp_openapi_test_") {
		t.Fatal("refusing non-test database")
	}
	tables, err := db.Migrator().GetTables()
	if err != nil {
		t.Fatal("cannot inventory test database")
	}
	if len(tables) != 0 {
		t.Fatal("refusing non-empty database; never drops pre-existing tables")
	}
	dir := os.Getenv("UVP_OPENAPI_TEST_MIGRATION_DIR")
	if dir == "" {
		dir = "../../../resource/database/gb28181/migrations"
	}
	stem := filepath.Join(dir, "2026-09-05-openapi-aksk-schema"+suffix)
	run := func(path string) {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, sql := range strings.Split(string(body), ";") {
			if strings.TrimSpace(sql) == "" {
				continue
			}
			if err := db.Exec(sql).Error; err != nil {
				t.Fatalf("migration statement %d failed: %v", i, err)
			}
		}
	}
	run(stem + ".sql")
	run(stem + ".sql")
	for _, table := range []any{&models.Client{}, &models.ClientScope{}, &models.Nonce{}, &models.Audit{}} {
		if !db.Migrator().HasTable(table) {
			t.Fatal("core table missing")
		}
	}
	now := time.Now().UTC()
	c := models.Client{AK: "uvp_000102030405060708090a0b0c0d0e0f", Name: "isolated", OwnerDeptID: 10, Status: models.StatusActive, SecretCiphertext: []byte("test-only-ciphertext"), SecretIV: []byte("test-only-iv"), SecretKeyID: "test", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := c
	duplicate.ID = 0
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate AK accepted")
	}
	scope := models.ClientScope{ClientID: c.ID, Scope: "device:list", Enabled: true, UpdatedAt: now}
	if err := db.Create(&scope).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&scope).Error; err == nil {
		t.Fatal("duplicate scope accepted")
	}
	gate := auth.NewAdmission(db, time.Now)
	var accepted, replayed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := auth.AdmissionRequest{ClientID: c.ID, SecretVersion: 1, AuthEpoch: 1, ScopeEpoch: 1, Scope: "device:list", Timestamp: fmt.Sprint(now.Unix()), Nonce: "000102030405060708090a0b0c0d0e0f", RequestID: fmt.Sprintf("native-%d", i)}
			err := gate.Admit(ctx, r, func(*gorm.DB) error { return nil })
			switch err {
			case nil:
				accepted.Add(1)
			case auth.ErrReplay:
				replayed.Add(1)
			default:
				t.Errorf("native admission failed: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 1 || replayed.Load() != 99 {
		t.Fatalf("accepted=%d replayed=%d; expected 1/99", accepted.Load(), replayed.Load())
	}
	var nonce models.Nonce
	if err := db.First(&nonce).Error; err != nil {
		t.Fatal(err)
	}
	if nonce.ExpiresAt.Sub(nonce.AcceptedAt) < 660*time.Second {
		t.Fatal("retention below 660 seconds")
	}
	if err := db.Exec("CREATE TABLE openapi_test_sentinel (id INT PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	run(stem + "-down.sql")
	if !db.Migrator().HasTable("openapi_test_sentinel") {
		t.Fatal("down removed unrelated table")
	}
	run(stem + ".sql")
	t.Logf("%s core up/up, unique keys, 100-way nonce admission, down/up passed; HTTP/media/full-schema not covered", dialect)
}
