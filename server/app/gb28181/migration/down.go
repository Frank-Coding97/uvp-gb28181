package migration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	openAPIDownGuardSwitch          = "UVP_OPENAPI_ALLOW_TEST_DOWN"
	openAPITestDatabasePrefix       = "uvp_openapi_test_"
	openAPISchemaMigration          = "2026-09-05-openapi-aksk-schema.sql"
	openAPISchemaMigrationPostgres  = "2026-09-05-openapi-aksk-schema-postgresql.sql"
	openAPISchemaMigrationSQLServer = "2026-09-05-openapi-aksk-schema-sqlserver.sql"
	openAPIDownGuardTimeout         = time.Second
)

var errOpenAPIDownDenied = errors.New("openapi schema destructive down denied")

// Down 手动回滚单个迁移:执行 <up>-down.sql 并从版本表删除记录。
// 运维入口,由 -migrate-down 标志触发;删除记录后下次启动 Up 会重新
// 应用该迁移(手动回滚语义,文档已注明)。
func Down(db *gorm.DB, d Dialect, upFileName string) error {
	if err := checkOpenAPIDownGuard(db, d, upFileName); err != nil {
		return err
	}
	store := NewStore(db)
	exec := &dbExecutor{db: db}
	return downWith(store, exec, &embedSource{dialect: d}, d, upFileName)
}

// checkOpenAPIDownGuard is the hard gate for the only destructive OpenAPI
// schema rollback. It deliberately returns one fixed error so database names,
// SQL text, and driver details never cross the operational boundary.
func checkOpenAPIDownGuard(db *gorm.DB, d Dialect, upFileName string) error {
	if !isOpenAPISchemaMigration(upFileName) {
		return nil
	}
	if db == nil || os.Getenv(openAPIDownGuardSwitch) != "1" {
		return errOpenAPIDownDenied
	}

	identityQuery, ok := openAPIDatabaseIdentityQuery(d)
	if !ok {
		return errOpenAPIDownDenied
	}
	ctx, cancel := context.WithTimeout(context.Background(), openAPIDownGuardTimeout)
	defer cancel()
	guardDB := db.WithContext(ctx)
	var databaseName string
	identity := guardDB.Raw(identityQuery)
	row := identity.Row()
	if identity.Error != nil || row == nil || row.Scan(&databaseName) != nil || !isOpenAPITestDatabase(databaseName) {
		return errOpenAPIDownDenied
	}
	if err := rejectNonEmptyOpenAPISafetyState(guardDB); err != nil {
		return errOpenAPIDownDenied
	}
	return nil
}

func isOpenAPISchemaMigration(name string) bool {
	switch name {
	case openAPISchemaMigration, openAPISchemaMigrationPostgres, openAPISchemaMigrationSQLServer:
		return true
	default:
		return false
	}
}

func openAPIDatabaseIdentityQuery(d Dialect) (string, bool) {
	switch d {
	case DialectMySQL:
		return "SELECT DATABASE()", true
	case DialectPostgres:
		return "SELECT current_database()", true
	case DialectSQLServer:
		return "SELECT DB_NAME()", true
	default:
		return "", false
	}
}

func isOpenAPITestDatabase(name string) bool {
	if !strings.HasPrefix(name, openAPITestDatabasePrefix) || len(name) == len(openAPITestDatabasePrefix) {
		return false
	}
	for i := 0; i < len(name); i++ {
		b := name[i]
		if !(b >= 'a' && b <= 'z') && !(b >= '0' && b <= '9') && b != '_' {
			return false
		}
	}
	return true
}

// rejectNonEmptyOpenAPISafetyState fails closed on missing tables/columns as
// well as rows. This keeps a future grant/viewer/node schema from being
// mistaken for an empty test database merely because its probe is unavailable.
func rejectNonEmptyOpenAPISafetyState(db *gorm.DB) error {
	for _, table := range []string{
		"sys_openapi_client",
		"sys_openapi_client_scope",
		"sys_openapi_nonce",
		"sys_openapi_audit",
		"gb_openapi_play_grant",
		"gb_openapi_viewer",
	} {
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM " + table).Scan(&count).Error; err != nil || count != 0 {
			return errOpenAPIDownDenied
		}
	}

	var deviceUnsafe int64
	if err := db.Raw("SELECT COUNT(*) FROM gb_device WHERE (access_epoch IS NULL OR access_epoch <> 1) OR legacy_revoked_before IS NOT NULL").Scan(&deviceUnsafe).Error; err != nil || deviceUnsafe != 0 {
		return errOpenAPIDownDenied
	}

	var nodeUnsafe int64
	if err := db.Raw("SELECT COUNT(*) FROM meta_node WHERE current_boot_nonce IS NOT NULL OR (retired_boot_history IS NOT NULL AND retired_boot_history <> '[]') OR runtime_epoch IS NULL OR runtime_epoch <> 0 OR runtime_protocol_version IS NULL OR runtime_protocol_version <> 0 OR runtime_confirmed_revision IS NULL OR runtime_confirmed_revision <> 0 OR runtime_confirmed_at IS NOT NULL OR runtime_identity_status IS NULL OR runtime_identity_status <> 'unknown'").Scan(&nodeUnsafe).Error; err != nil || nodeUnsafe != 0 {
		return errOpenAPIDownDenied
	}
	return nil
}

// downWith 是 Down 的纯依赖版本,便于 fake 注入测试。
func downWith(store versionStore, exec migrationExecutor, src migrationSource, d Dialect, upFileName string) error {
	downName := DownFileName(upFileName)
	sqlText, err := src.ReadSQL(downName)
	if err != nil {
		return fmt.Errorf("down 迁移文件 %s 不存在(方言 %s): %w", downName, d, err)
	}
	if err := exec.ExecSQL(sqlText); err != nil {
		return fmt.Errorf("down 迁移 %s 执行失败: %w\nSQL: %s", downName, err, sqlText)
	}
	if err := store.DeleteApplied(upFileName); err != nil {
		return fmt.Errorf("删除版本记录 %s 失败: %w", upFileName, err)
	}
	return nil
}
