package casbinhelper

import (
	"context"
	"path/filepath"
	"testing"

	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const readonlyHealthTable = "sys_casbin_rule"

type readonlyHealthSchemaRow struct {
	Name string `gorm:"column:name"`
	SQL  string `gorm:"column:sql"`
}

func newReadonlyHealthDB(t *testing.T, withTable bool) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "casbin-health.db")), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	if withTable {
		require.NoError(t, db.Exec(`CREATE TABLE sys_casbin_rule (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 ptype TEXT, v0 TEXT, v1 TEXT, v2 TEXT, v3 TEXT, v4 TEXT, v5 TEXT
)`).Error)
		require.NoError(t, db.Exec(`INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3) VALUES
('p','role_1','/api/users','GET','*'),('g','user_1','role_1','*','')`).Error)
	}
	return db
}

func readonlyHealthSchema(t *testing.T, db *gorm.DB) []readonlyHealthSchemaRow {
	t.Helper()
	var rows []readonlyHealthSchemaRow
	require.NoError(t, db.Raw(`SELECT name, COALESCE(sql, '') AS sql
FROM sqlite_master WHERE type IN ('table','index') ORDER BY name`).Scan(&rows).Error)
	return rows
}

func readonlyHealthPolicy(t *testing.T, db *gorm.DB) []gormadapter.CasbinRule {
	t.Helper()
	var rows []gormadapter.CasbinRule
	require.NoError(t, db.Table(readonlyHealthTable).Order("id").Find(&rows).Error)
	return rows
}

func TestValidatePolicyReadOnlyLoadsSQLitePolicyWithoutMutation(t *testing.T) {
	db := newReadonlyHealthDB(t, true)
	key := struct{}{}
	ctx := context.WithValue(context.Background(), key, "caller-context")
	db = db.WithContext(ctx)
	beforeSchema := readonlyHealthSchema(t, db)
	beforePolicy := readonlyHealthPolicy(t, db)
	beforeContext := db.Statement.Context

	require.NoError(t, ValidatePolicyReadOnly(ctx, db, testModelConfig, "", readonlyHealthTable))
	require.Equal(t, beforeSchema, readonlyHealthSchema(t, db))
	require.Equal(t, beforePolicy, readonlyHealthPolicy(t, db))
	require.Equal(t, beforeContext, db.Statement.Context)
}

func TestValidatePolicyReadOnlyMissingTableDoesNotMigrate(t *testing.T) {
	db := newReadonlyHealthDB(t, false)
	beforeSchema := readonlyHealthSchema(t, db)

	err := ValidatePolicyReadOnly(context.Background(), db, testModelConfig, "", readonlyHealthTable)
	require.Error(t, err)
	require.Equal(t, beforeSchema, readonlyHealthSchema(t, db))
	require.False(t, db.Migrator().HasTable(readonlyHealthTable))
}

func TestValidatePolicyReadOnlyRejectsInvalidModelAndClosedDB(t *testing.T) {
	db := newReadonlyHealthDB(t, true)
	const modelSecret = "readonly-model-secret"
	err := ValidatePolicyReadOnly(context.Background(), db, "[bad]\n"+modelSecret, "", readonlyHealthTable)
	require.Error(t, err)
	require.NotContains(t, err.Error(), modelSecret)

	sqlDB, dbErr := db.DB()
	require.NoError(t, dbErr)
	require.NoError(t, sqlDB.Close())
	err = ValidatePolicyReadOnly(context.Background(), db, testModelConfig, "", readonlyHealthTable)
	require.Error(t, err)
	require.NotContains(t, err.Error(), modelSecret)
}

func TestValidatePolicyReadOnlyRejectsNilDB(t *testing.T) {
	err := ValidatePolicyReadOnly(context.Background(), nil, testModelConfig, "", readonlyHealthTable)
	require.Error(t, err)
}
