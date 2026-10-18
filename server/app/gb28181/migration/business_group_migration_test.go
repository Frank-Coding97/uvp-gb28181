package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/casbin/casbin/v2"
	casbinmodel "github.com/casbin/casbin/v2/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const cascadeBusinessGroupMigration = "2026-10-13-cascade-business-group"
const cascadePublishGroupMigration = "2026-10-13-cascade-publish-group-permission"

var cascadeBusinessGroupMigrationFiles = []struct {
	dialect Dialect
	up      string
	down    string
}{
	{DialectMySQL, cascadeBusinessGroupMigration + ".sql", cascadeBusinessGroupMigration + "-down.sql"},
	{DialectPostgres, cascadeBusinessGroupMigration + "-postgresql.sql", cascadeBusinessGroupMigration + "-postgresql-down.sql"},
	{DialectSQLServer, cascadeBusinessGroupMigration + "-sqlserver.sql", cascadeBusinessGroupMigration + "-sqlserver-down.sql"},
	{DialectSQLite, cascadeBusinessGroupMigration + "-sqlite.sql", cascadeBusinessGroupMigration + "-sqlite-down.sql"},
}

func TestCascadeBusinessGroupMigrationFilesAndRegistryCoverEveryDialect(t *testing.T) {
	for _, file := range cascadeBusinessGroupMigrationFiles {
		t.Run(string(file.dialect), func(t *testing.T) {
			up := strings.ToLower(readCascadeBusinessGroupMigration(t, file.up))
			_ = readCascadeBusinessGroupMigration(t, file.down)
			for _, token := range []string{
				"gb_cascade_business_node",
				"gb_cascade_business_binding",
				"uk_cascade_business_node_code",
				"uk_cascade_business_binding_channel",
			} {
				require.Contains(t, up, token)
			}
			if file.dialect == DialectSQLite {
				require.Contains(t, up, "create table if not exists")
			}

			found := false
			names, err := (&embedSource{dialect: file.dialect}).UpFiles()
			require.NoError(t, err)
			for _, name := range names {
				found = found || name == file.up
			}
			require.Truef(t, found, "%s must be registered for startup migrations", file.up)
			_, err = (&embedSource{dialect: file.dialect}).ReadSQL(file.down)
			require.NoErrorf(t, err, "%s must be embedded for downgrade tooling", file.down)
		})
	}
}

func TestCascadeBusinessGroupMigrationIsAppliedToLegacyBaselinesAndSkippedWhenPresent(t *testing.T) {
	for _, file := range cascadeBusinessGroupMigrationFiles {
		t.Run(string(file.dialect), func(t *testing.T) {
			probe := schemaProbe{hasTable: func(table string) (bool, error) {
				return table == "gb_cascade_business_node", nil
			}}
			needed, err := needsBaselineUpgrade(file.up, probe)
			require.NoError(t, err)
			require.True(t, needed, "legacy baseline without business tables must run the additive migration")

			probe.hasTable = func(table string) (bool, error) {
				return table == "gb_cascade_business_node" || table == "gb_cascade_business_binding", nil
			}
			needed, err = needsBaselineUpgrade(file.up, probe)
			require.NoError(t, err)
			require.False(t, needed, "fresh baseline with both business tables must mark the migration applied")
		})
	}
}

func TestCascadeBusinessGroupSQLiteMigrationPreservesPlatformDataAndIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	for _, statement := range []string{
		"CREATE TABLE gb_cascade_platform (id INTEGER PRIMARY KEY, name TEXT NOT NULL)",
		"INSERT INTO gb_cascade_platform(id, name) VALUES(1, 'existing-upstream')",
		"CREATE TABLE sys_api(id INTEGER PRIMARY KEY AUTOINCREMENT,title TEXT,path TEXT,method TEXT,api_group TEXT,created_at TEXT,updated_at TEXT,deleted_at TEXT,created_by INTEGER)",
		"CREATE TABLE sys_menu(id INTEGER PRIMARY KEY,permission TEXT)",
		"CREATE TABLE sys_menu_api(menu_id INTEGER,api_id INTEGER,PRIMARY KEY(menu_id,api_id))",
		"CREATE TABLE sys_casbin_rule(id INTEGER PRIMARY KEY AUTOINCREMENT,ptype TEXT,v0 TEXT,v1 TEXT,v2 TEXT,v3 TEXT,v4 TEXT,v5 TEXT)",
		"INSERT INTO sys_menu(id,permission) VALUES(140376,'gb28181:cascade:view'),(140379,'gb28181:cascade:share')",
	} {
		require.NoError(t, db.Exec(statement).Error, statement)
	}

	up := readCascadeBusinessGroupMigration(t, cascadeBusinessGroupMigration+"-sqlite.sql")
	executor := &dbExecutor{db: db}
	require.NoError(t, executor.ExecSQL(up))
	require.NoError(t, executor.ExecSQL(up), "re-running the additive migration must be safe")
	require.True(t, db.Migrator().HasTable("gb_cascade_business_node"))
	require.True(t, db.Migrator().HasTable("gb_cascade_business_binding"))
	var platformName string
	require.NoError(t, db.Raw("SELECT name FROM gb_cascade_platform WHERE id = 1").Scan(&platformName).Error)
	require.Equal(t, "existing-upstream", platformName)
}

func TestCascadeBusinessGroupEndpointsReuseViewAndSharePermissions(t *testing.T) {
	want := []struct {
		path       string
		method     string
		permission string
	}{
		{"/api/gb28181/cascade/platforms/:id/business-group", "GET", "gb28181:cascade:view"},
		{"/api/gb28181/cascade/platforms/:id/business-group", "PUT", "gb28181:cascade:share"},
	}
	for _, file := range cascadeBusinessGroupMigrationFiles {
		up := strings.ToLower(readCascadeBusinessGroupMigration(t, file.up))
		for _, endpoint := range want {
			require.Contains(t, up, strings.ToLower(endpoint.path), file.up)
			require.Contains(t, up, strings.ToLower(endpoint.method), file.up)
			require.Contains(t, up, endpoint.permission, file.up)
		}
		require.NotContains(t, up, "gb28181:cascade:business-group", file.up, "the feature must reuse existing permissions")
		require.NotContains(t, up, "insert into sys_menu ", file.up, "the feature must not add a menu permission")
	}
}

func TestCascadeBusinessGroupSQLiteMigrationMapsPermissionsAndEnforcesCasbin(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	for _, statement := range []string{
		"CREATE TABLE sys_api(id INTEGER PRIMARY KEY AUTOINCREMENT,title TEXT,path TEXT,method TEXT,api_group TEXT,created_at TEXT,updated_at TEXT,deleted_at TEXT,created_by INTEGER)",
		"CREATE TABLE sys_menu(id INTEGER PRIMARY KEY,permission TEXT)",
		"CREATE TABLE sys_menu_api(menu_id INTEGER,api_id INTEGER,PRIMARY KEY(menu_id,api_id))",
		"CREATE TABLE sys_casbin_rule(id INTEGER PRIMARY KEY AUTOINCREMENT,ptype TEXT,v0 TEXT,v1 TEXT,v2 TEXT,v3 TEXT,v4 TEXT,v5 TEXT)",
		"INSERT INTO sys_menu(id,permission) VALUES(140376,'gb28181:cascade:view'),(140379,'gb28181:cascade:share')",
	} {
		require.NoError(t, db.Exec(statement).Error, statement)
	}

	up := readCascadeBusinessGroupMigration(t, cascadeBusinessGroupMigration+"-sqlite.sql")
	require.NoError(t, (&dbExecutor{db: db}).ExecSQL(up))

	type endpointPermission struct {
		Method     string
		Permission string
	}
	var endpoints []endpointPermission
	require.NoError(t, db.Raw(`SELECT a.method, m.permission
		FROM sys_api a JOIN sys_menu_api ma ON ma.api_id=a.id JOIN sys_menu m ON m.id=ma.menu_id
		WHERE a.path=? ORDER BY a.method`, "/api/gb28181/cascade/platforms/:id/business-group").Scan(&endpoints).Error)
	require.Equal(t, []endpointPermission{
		{Method: "GET", Permission: "gb28181:cascade:view"},
		{Method: "PUT", Permission: "gb28181:cascade:share"},
	}, endpoints)

	modelConfig := "[request_definition]\nr = sub, obj, act, dom\n" +
		"[policy_definition]\np = sub, obj, act, dom\n" +
		"[role_definition]\ng = _, _, _\n" +
		"[policy_effect]\ne = some(where (p.eft == allow))\n" +
		"[matchers]\nm = g(r.sub, p.sub, r.dom) && keyMatch2(r.obj, p.obj) && regexMatch(r.act, p.act) && (r.dom == p.dom || p.dom == \"*\")"
	m, err := casbinmodel.NewModelFromString(modelConfig)
	require.NoError(t, err)
	enforcer, err := casbin.NewEnforcer(m)
	require.NoError(t, err)
	type policyRule struct {
		Subject string
		Object  string
		Action  string
		Domain  string
	}
	var policies []policyRule
	require.NoError(t, db.Raw("SELECT v0 AS subject, v1 AS object, v2 AS action, v3 AS domain FROM sys_casbin_rule WHERE ptype='p'").Scan(&policies).Error)
	for _, policy := range policies {
		_, err := enforcer.AddPolicy(policy.Subject, policy.Object, policy.Action, policy.Domain)
		require.NoError(t, err)
	}
	for _, method := range []string{"GET", "PUT"} {
		allowed, err := enforcer.Enforce("role_1", "/api/gb28181/cascade/platforms/42/business-group", method, "*")
		require.NoError(t, err)
		require.True(t, allowed, method+" must be allowed to the administrator")
		allowed, err = enforcer.Enforce("role_2", "/api/gb28181/cascade/platforms/42/business-group", method, "*")
		require.NoError(t, err)
		require.False(t, allowed, method+" must be denied without a matching permission")
	}
}

func TestCascadeBusinessGroupTablesAndEndpointsArePresentInEveryFreshBaseline(t *testing.T) {
	for _, name := range []string{
		"uvp-gb28181.sql",
		"postgresql_converted.sql",
		"sqlserver_converted.sql",
		"sqlitebaseline/baseline.sql",
	} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
			require.NoError(t, err)
			text := strings.ToLower(string(body))
			for _, token := range []string{
				"gb_cascade_business_node",
				"gb_cascade_business_binding",
				"/api/gb28181/cascade/platforms/:id/business-group",
				"gb28181:cascade:view",
				"gb28181:cascade:share",
				"/api/gb28181/cascade/platforms/:id/publish-group",
				"gb28181:cascade:manage",
			} {
				require.Contains(t, text, token, name)
			}
		})
	}
}

func TestCascadePublishGroupPermissionMigrationIsAvailableForEveryDatabase(t *testing.T) {
	for _, file := range []string{
		cascadePublishGroupMigration + ".sql",
		cascadePublishGroupMigration + "-postgresql.sql",
		cascadePublishGroupMigration + "-sqlserver.sql",
		cascadePublishGroupMigration + "-sqlite.sql",
	} {
		t.Run(file, func(t *testing.T) {
			up := strings.ToLower(readCascadeBusinessGroupMigration(t, file))
			require.Contains(t, up, "/api/gb28181/cascade/platforms/:id/publish-group")
			require.Contains(t, up, "gb28181:cascade:manage")
			require.NotContains(t, up, "gb28181:cascade:share")
			require.NotContains(t, up, "insert into sys_menu ", "publication control reuses the existing permission menu")
			down := strings.TrimSuffix(file, ".sql") + "-down.sql"
			_ = readCascadeBusinessGroupMigration(t, down)
		})
	}
	names, err := (&embedSource{dialect: DialectSQLite}).UpFiles()
	require.NoError(t, err)
	require.Contains(t, names, cascadePublishGroupMigration+"-sqlite.sql")
	name := cascadePublishGroupMigration + "-sqlite.sql"
	probe := schemaProbe{hasPermissionRegistration: func(path, method, permission string) (bool, error) {
		require.Equal(t, "/api/gb28181/cascade/platforms/:id/publish-group", path)
		require.Equal(t, "PUT", method)
		require.Equal(t, "gb28181:cascade:manage", permission)
		return false, nil
	}}
	needed, err := needsBaselineUpgrade(name, probe)
	require.NoError(t, err)
	require.True(t, needed, "legacy full baselines must execute missing API permission registration")
	probe.hasPermissionRegistration = func(string, string, string) (bool, error) { return true, nil }
	needed, err = needsBaselineUpgrade(name, probe)
	require.NoError(t, err)
	require.False(t, needed, "fresh baselines should mark existing permission registration applied")
}

func TestCascadePublishGroupSQLiteMigrationMapsOnlyManagePermissionIdempotently(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	for _, statement := range []string{
		"CREATE TABLE sys_api(id INTEGER PRIMARY KEY AUTOINCREMENT,title TEXT,path TEXT,method TEXT,api_group TEXT,created_at TEXT,updated_at TEXT,deleted_at TEXT,created_by INTEGER)",
		"CREATE TABLE sys_menu(id INTEGER PRIMARY KEY,permission TEXT)",
		"CREATE TABLE sys_menu_api(menu_id INTEGER,api_id INTEGER,PRIMARY KEY(menu_id,api_id))",
		"CREATE TABLE sys_casbin_rule(id INTEGER PRIMARY KEY AUTOINCREMENT,ptype TEXT,v0 TEXT,v1 TEXT,v2 TEXT,v3 TEXT,v4 TEXT,v5 TEXT)",
		"INSERT INTO sys_menu(id,permission) VALUES(140376,'gb28181:cascade:view'),(140379,'gb28181:cascade:share'),(140377,'gb28181:cascade:manage')",
	} {
		require.NoError(t, db.Exec(statement).Error, statement)
	}
	up := readCascadeBusinessGroupMigration(t, cascadePublishGroupMigration+"-sqlite.sql")
	require.NoError(t, (&dbExecutor{db: db}).ExecSQL(up))
	require.NoError(t, (&dbExecutor{db: db}).ExecSQL(up), "running initialization twice must be safe")
	var apiCount, manageCount, shareCount, policyCount int64
	path := "/api/gb28181/cascade/platforms/:id/publish-group"
	require.NoError(t, db.Raw("SELECT count(*) FROM sys_api WHERE path=? AND method='PUT'", path).Scan(&apiCount).Error)
	require.NoError(t, db.Raw(`SELECT count(*) FROM sys_menu_api ma JOIN sys_menu m ON m.id=ma.menu_id JOIN sys_api a ON a.id=ma.api_id
		WHERE a.path=? AND a.method='PUT' AND m.permission='gb28181:cascade:manage'`, path).Scan(&manageCount).Error)
	require.NoError(t, db.Raw(`SELECT count(*) FROM sys_menu_api ma JOIN sys_menu m ON m.id=ma.menu_id JOIN sys_api a ON a.id=ma.api_id
		WHERE a.path=? AND a.method='PUT' AND m.permission='gb28181:cascade:share'`, path).Scan(&shareCount).Error)
	require.EqualValues(t, 1, apiCount)
	require.EqualValues(t, 1, manageCount)
	require.EqualValues(t, 0, shareCount)
	require.NoError(t, db.Raw("SELECT count(*) FROM sys_casbin_rule WHERE ptype='p' AND v0='role_1' AND v1=? AND v2='PUT'", path).Scan(&policyCount).Error)
	require.EqualValues(t, 1, policyCount)
}

func readCascadeBusinessGroupMigration(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", name))
	require.NoError(t, err, "migration file missing: %s", name)
	return string(body)
}
