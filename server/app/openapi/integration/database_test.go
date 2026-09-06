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
	"uvplatform.cn/uvp-gb28181/app/gb28181/migration"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
	openapiconfig "uvplatform.cn/uvp-gb28181/app/openapi/config"
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
	var migrationDialect migration.Dialect
	switch dialect {
	case "mysql":
		dialector = mysql.Open(dsn)
		query = "SELECT DATABASE()"
		migrationDialect = migration.DialectMySQL
	case "postgresql":
		dialector = postgres.Open(dsn)
		query = "SELECT current_database()"
		suffix = "-postgresql"
		migrationDialect = migration.DialectPostgres
	case "sqlserver":
		dialector = sqlserver.Open(dsn)
		query = "SELECT DB_NAME()"
		suffix = "-sqlserver"
		migrationDialect = migration.DialectSQLServer
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
	// Model an existing application database without importing business data.
	// The schema upgrade only adds security columns to these fixture tables.
	for _, table := range []string{"gb_device", "meta_node"} {
		if err := db.Exec("CREATE TABLE " + table + " (id BIGINT PRIMARY KEY, fixture_label VARCHAR(32) NOT NULL)").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT INTO " + table + " (id,fixture_label) VALUES (1,'keep-fixture')").Error; err != nil {
			t.Fatal(err)
		}
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
		if err := db.Connection(func(conn *gorm.DB) error {
			for i, statement := range nativeSchemaStatements(string(body)) {
				if err := conn.Exec(statement).Error; err != nil {
					return fmt.Errorf("migration statement %d failed: %w", i, err)
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	run(stem + ".sql")
	run(stem + ".sql")
	lockStem := filepath.Join(dir, "2026-09-06-openapi-must-auth-lock"+suffix)
	run(lockStem + ".sql")
	run(lockStem + ".sql")
	for _, table := range []any{&models.Client{}, &models.ClientScope{}, &models.Nonce{}, &models.Audit{}} {
		if !db.Migrator().HasTable(table) {
			t.Fatal("core table missing")
		}
	}
	for _, table := range []string{"gb_openapi_play_grant", "gb_openapi_viewer"} {
		if !db.Migrator().HasTable(table) {
			t.Fatal("media table missing")
		}
	}
	checkNativeMediaSchema(t, db)
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
	other := c
	other.ID = 0
	other.AK = "uvp_100102030405060708090a0b0c0d0e0f"
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	otherScope := scope
	otherScope.ClientID = other.ID
	if err := db.Create(&otherScope).Error; err != nil {
		t.Fatal(err)
	}
	request := auth.AdmissionRequest{ClientID: other.ID, SecretVersion: 1, AuthEpoch: 1, ScopeEpoch: 1, Scope: "device:list", Timestamp: fmt.Sprint(now.Unix()), Nonce: nonce.Value, RequestID: "native-other-client"}
	if err := gate.Admit(ctx, request, func(*gorm.DB) error { return nil }); err != nil {
		t.Fatal("different client could not reuse nonce:", err)
	}
	if err := db.Model(&models.Client{}).Where("id = ?", c.ID).Update("secret_version", 2).Error; err != nil {
		t.Fatal(err)
	}
	request.ClientID, request.SecretVersion, request.RequestID = c.ID, 2, "native-rotated-client"
	if err := gate.Admit(ctx, request, func(*gorm.DB) error { return nil }); err != auth.ErrReplay {
		t.Fatal("secret rotation did not preserve nonce:", err)
	}
	if err := db.Exec("CREATE TABLE openapi_test_sentinel (id INT PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	store := migration.NewStore(db)
	if err := store.EnsureTable(); err != nil {
		t.Fatal(err)
	}
	upName := filepath.Base(stem) + ".sql"
	if err := store.MarkApplied([]string{upName}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UVP_OPENAPI_ALLOW_TEST_DOWN", "")
	if err := migration.Down(db, migrationDialect, upName); err == nil {
		t.Fatal("destructive down allowed without explicit test switch")
	}
	t.Setenv("UVP_OPENAPI_ALLOW_TEST_DOWN", "1")
	if err := migration.Down(db, migrationDialect, upName); err == nil {
		t.Fatal("destructive down allowed with live safety rows")
	}
	// These rows were created only by this test after verifying an empty DB.
	// Remove fixture state, never arbitrary pre-existing data, then exercise
	// the real guarded operational Down entry point (not raw down SQL).
	for _, table := range []string{"gb_openapi_viewer", "gb_openapi_play_grant", "sys_openapi_nonce", "sys_openapi_audit", "sys_openapi_client_scope", "sys_openapi_client"} {
		if err := db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, update := range []struct{ set, reset string }{
		{"UPDATE gb_device SET access_epoch=2", "UPDATE gb_device SET access_epoch=1"},
		{"UPDATE meta_node SET current_boot_nonce='000102030405060708090a0b0c0d0e0f'", "UPDATE meta_node SET current_boot_nonce=NULL"},
	} {
		if err := db.Exec(update.set).Error; err != nil {
			t.Fatal(err)
		}
		if err := migration.Down(db, migrationDialect, upName); err == nil {
			t.Fatal("down erased advanced security state")
		}
		if err := db.Exec(update.reset).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := migration.Down(db, migrationDialect, upName); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable("openapi_test_sentinel") {
		t.Fatal("down removed unrelated table")
	}
	for _, table := range []string{"gb_device", "meta_node"} {
		var count int64
		if err := db.Table(table).Where("fixture_label = ?", "keep-fixture").Count(&count).Error; err != nil || count != 1 {
			t.Fatal("down altered existing fixture rows")
		}
	}
	run(stem + ".sql")
	// The lock's own down is permitted only while this entire fixture is neutral.
	lockName := filepath.Base(lockStem) + ".sql"
	if err := store.MarkApplied([]string{lockName}); err != nil {
		t.Fatal(err)
	}
	if err := migration.Down(db, migrationDialect, lockName); err != nil {
		t.Fatal(err)
	}
	run(lockStem + ".sql")
	checkNativeNodeRuntime(t, db)
	checkNativeQuota(t, db)
	checkNativeGrantViewer(t, db)
	checkNativeRevocation(t, db)
	checkNativeMustAuthLatch(t, db)
	run(lockStem + ".sql") // upgrades must never reset the latch
	state, err := openapiconfig.NewMustAuthStore(db, time.Now).Load(ctx)
	if err != nil || !state.MustAuthLocked || state.LockVersion != 1 {
		t.Fatal("upgrade reset or damaged the persistent security commitment")
	}
	for _, name := range []string{upName, lockName} {
		if err := migration.Down(db, migrationDialect, name); err == nil {
			t.Fatal("down allowed after the security commitment was latched")
		}
	}
	t.Logf("%s schema up/up, media constraints, 100-way nonce, guarded down/up passed; full initialization/HTTP/media runtime not covered", dialect)
}

// Match the production runner's line-terminated statements, so semicolons
// inside SQL strings do not split a PREPARE body into a different program.
func nativeSchemaStatements(body string) []string {
	var statements []string
	var pending strings.Builder
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		pending.WriteString(line + "\n")
		if strings.HasSuffix(trimmed, ";") {
			statements = append(statements, pending.String())
			pending.Reset()
		}
	}
	if strings.TrimSpace(pending.String()) != "" {
		statements = append(statements, pending.String())
	}
	return statements
}
