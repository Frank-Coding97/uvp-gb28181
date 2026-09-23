package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

type permissionRule struct {
	ID    uint   `gorm:"primaryKey"`
	Ptype string `gorm:"size:100"`
	V0    string `gorm:"size:100"`
	V1    string `gorm:"size:255"`
	V2    string `gorm:"size:100"`
	V3    string `gorm:"size:100"`
	V4    string `gorm:"size:100"`
	V5    string `gorm:"size:100"`
}

func (permissionRule) TableName() string { return "sys_casbin_rule" }

// Execute only against an explicitly supplied, empty, dedicated permissions DB.
// This tests native seed SQL, not full application initialization or root HTTP.
func TestOpenAPIDatabasePermissions(t *testing.T) {
	dialect, dsn := os.Getenv("UVP_OPENAPI_TEST_DIALECT"), os.Getenv("UVP_OPENAPI_TEST_DSN")
	if dialect == "" || dsn == "" {
		if os.Getenv("UVP_OPENAPI_INTEGRATION_REQUIRED") == "1" {
			t.Fatal("isolated database required")
		}
		t.Skip("no explicit isolated database target; not an acceptance pass")
	}
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
		t.Fatal("unsupported test dialect")
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal("database connection failed; credentials suppressed")
	}
	raw, err := db.DB()
	require.NoError(t, err)
	defer raw.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	var name string
	require.NoError(t, db.Raw(query).Scan(&name).Error)
	require.True(t, strings.HasPrefix(name, "uvp_openapi_test_permissions_"), "refusing non-permissions-test database")
	tables, err := db.Migrator().GetTables()
	require.NoError(t, err)
	require.Empty(t, tables, "refusing any pre-existing table; nothing is dropped")
	require.NoError(t, db.AutoMigrate(&basemodels.SysMenu{}, &basemodels.SysApi{}, &basemodels.SysRole{}, &basemodels.SysRoleMenu{}, &basemodels.SysMenuApi{}, &permissionRule{}))
	for _, row := range []basemodels.SysRole{{BaseModel: basemodels.BaseModel{ID: 1}, Name: "root", Status: 1}, {BaseModel: basemodels.BaseModel{ID: 2}, Name: "ordinary", Status: 1}, {BaseModel: basemodels.BaseModel{ID: 3}, Name: "guest", Status: 1}} {
		require.NoError(t, db.Create(&row).Error)
	}
	sentinel := permissionRule{Ptype: "p", V0: "role_2", V1: "/unrelated", V2: "GET", V3: "*"}
	require.NoError(t, db.Create(&sentinel).Error)
	dir := os.Getenv("UVP_OPENAPI_TEST_MIGRATION_DIR")
	if dir == "" {
		dir = "../../../resource/database/gb28181/migrations"
	}
	stem := filepath.Join(dir, "2026-09-05-openapi-aksk-permissions"+suffix)
	run := func(path string) {
		t.Helper()
		body, err := os.ReadFile(path)
		require.NoError(t, err)
		// Match the repository migration runner's line-terminated statements.
		var statement strings.Builder
		for _, line := range strings.Split(string(body), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "--") {
				continue
			}
			statement.WriteString(line + "\n")
			if strings.HasSuffix(trimmed, ";") {
				require.NoError(t, db.Exec(statement.String()).Error)
				statement.Reset()
			}
		}
		if strings.TrimSpace(statement.String()) != "" {
			require.NoError(t, db.Exec(statement.String()).Error)
		}
	}
	counts := func() []int64 {
		t.Helper()
		result := []int64{}
		for _, table := range []string{"sys_api", "sys_menu", "sys_menu_api", "sys_role_menu", "sys_casbin_rule"} {
			var n int64
			require.NoError(t, db.Table(table).Count(&n).Error)
			result = append(result, n)
		}
		return result
	}
	run(stem + ".sql")
	first := counts()
	run(stem + ".sql")
	require.Equal(t, first, counts(), "repeated up must be idempotent")
	var n int64
	require.NoError(t, db.Table("sys_api").Where("path LIKE ?", "/api/gb28181/openapi-clients%").Count(&n).Error)
	require.EqualValues(t, 11, n)
	require.NoError(t, db.Table("sys_casbin_rule").Where("ptype = ? AND v0 = ? AND v1 LIKE ?", "p", "role_1", "/api/gb28181/openapi-clients%").Count(&n).Error)
	require.EqualValues(t, 11, n)
	require.NoError(t, db.Table("sys_casbin_rule").Where("v0 IN ? AND v1 LIKE ?", []string{"role_2", "role_3"}, "/api/gb28181/openapi-clients%").Count(&n).Error)
	require.Zero(t, n, "ordinary/guest role must not be elevated")
	run(stem + "-down.sql")
	require.NoError(t, db.Table("sys_casbin_rule").Where("id = ? AND v1 = ?", sentinel.ID, "/unrelated").Count(&n).Error)
	require.EqualValues(t, 1, n)
	run(stem + ".sql")
	require.Equal(t, first, counts())
	t.Logf("%s native permission up/up/down/up and non-elevation passed; full initialization/root HTTP not covered", dialect)
}
