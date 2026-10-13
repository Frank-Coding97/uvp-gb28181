package migration

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const openAPIPermissionStem = "2026-09-05-openapi-aksk-permissions"

var openAPIAdminRoutes = []struct {
	path   string
	method string
	code   string
}{
	{path: "/api/gb28181/openapi-clients", method: "GET", code: "gb28181:openapi:client:read"},
	{path: "/api/gb28181/openapi-clients", method: "POST", code: "gb28181:openapi:client:create"},
	{path: "/api/gb28181/openapi-clients/capabilities", method: "GET", code: "gb28181:openapi:client:read"},
	{path: "/api/gb28181/openapi-clients/:id", method: "GET", code: "gb28181:openapi:client:read"},
	{path: "/api/gb28181/openapi-clients/:id/scopes", method: "PUT", code: "gb28181:openapi:client:grant"},
	{path: "/api/gb28181/openapi-clients/:id/rotate-secret", method: "POST", code: "gb28181:openapi:client:rotate"},
	{path: "/api/gb28181/openapi-clients/:id/enable", method: "POST", code: "gb28181:openapi:client:status"},
	{path: "/api/gb28181/openapi-clients/:id/disable", method: "POST", code: "gb28181:openapi:client:status"},
	{path: "/api/gb28181/openapi-clients/:id/revoke", method: "POST", code: "gb28181:openapi:client:status"},
	{path: "/api/gb28181/openapi-clients/:id/audits", method: "GET", code: "gb28181:openapi:client:audit"},
	{path: "/api/gb28181/openapi-clients/:id/revocation-status", method: "GET", code: "gb28181:openapi:client:status"},
}

var openAPIPermissionCodes = []string{
	"gb28181:openapi:client:read",
	"gb28181:openapi:client:create",
	"gb28181:openapi:client:grant",
	"gb28181:openapi:client:rotate",
	"gb28181:openapi:client:status",
	"gb28181:openapi:client:audit",
}

func TestOpenAPIPermissionMigrationSeedsOnlyRootAndIsIdempotent(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		t.Run(suffix, func(t *testing.T) {
			db := openAPIPermissionFixture(t)
			body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", openAPIPermissionStem+suffix+".sql"))
			require.NoError(t, err)

			runOpenAPIPermissionSQL(t, db, string(body), suffix)
			first := openAPIPermissionSnapshot(t, db)
			runOpenAPIPermissionSQL(t, db, string(body), suffix)
			second := openAPIPermissionSnapshot(t, db)
			require.Equal(t, first, second, "re-running the permission migration must be idempotent")
			assertOpenAPIPermissionState(t, db)

			var apiCount int64
			require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_api WHERE path LIKE '/api/gb28181/openapi-clients%' AND deleted_at IS NULL").Scan(&apiCount).Error)
			require.EqualValues(t, len(openAPIAdminRoutes), apiCount)

			var menuCount int64
			require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_menu WHERE permission LIKE 'gb28181:openapi:client:%' AND deleted_at IS NULL").Scan(&menuCount).Error)
			require.EqualValues(t, len(openAPIPermissionCodes), menuCount)

			var unrelated int64
			require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_menu WHERE permission='unrelated:permission'").Scan(&unrelated).Error)
			require.EqualValues(t, 1, unrelated)
		})
	}
}

func TestOpenAPIPermissionDoesNotGrantInactiveRoot(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		t.Run(suffix, func(t *testing.T) {
			db := openAPIPermissionFixture(t)
			require.NoError(t, db.Exec("UPDATE sys_role SET name='系统管理员', status=0 WHERE id=1").Error)
			body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", openAPIPermissionStem+suffix+".sql"))
			require.NoError(t, err)
			runOpenAPIPermissionSQL(t, db, string(body), suffix)

			var granted int64
			require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id WHERE m.permission LIKE 'gb28181:openapi:client:%'").Scan(&granted).Error)
			require.Zero(t, granted, "an inactive root and same-name ordinary role must receive no default grant")
		})
	}
}

func TestOpenAPIPermissionDownDoesNotDeleteUnrelatedData(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		t.Run(suffix, func(t *testing.T) {
			db := openAPIPermissionFixture(t)
			body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", openAPIPermissionStem+suffix+".sql"))
			require.NoError(t, err)
			runOpenAPIPermissionSQL(t, db, string(body), suffix)

			before := openAPIPermissionSnapshot(t, db)
			down, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", openAPIPermissionStem+suffix+"-down.sql"))
			require.NoError(t, err)
			runOpenAPIPermissionSQL(t, db, string(down), suffix)
			require.Equal(t, before, openAPIPermissionSnapshot(t, db), "down must not erase pre-existing permission data")
		})
	}
}

func TestOpenAPIPermissionInitializationParity(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
			require.NoError(t, err)
			text := strings.ToLower(string(body))
			require.Contains(t, text, "openapi-aksk-permissions:begin")
			require.Contains(t, text, "openapi-aksk-permissions:end")
			for _, route := range openAPIAdminRoutes {
				require.Contains(t, text, strings.ToLower(route.path), route.path)
				require.Contains(t, text, strings.ToLower(route.method), route.method+" "+route.path)
			}
			for _, code := range openAPIPermissionCodes {
				require.Contains(t, text, code)
			}
			// Fresh-install seeds identify root by the stable id, never by a mutable name.
			section := text[strings.Index(text, "openapi-aksk-permissions:begin"):strings.Index(text, "openapi-aksk-permissions:end")]
			require.Contains(t, section, "r.id=1")
			require.Contains(t, section, "r.status=1")
			require.NotContains(t, section, "r.name")
		})
	}
}

func TestOpenAPIPermissionInitializationsExecuteLikeMigrations(t *testing.T) {
	initials := []struct {
		name   string
		suffix string
	}{
		{name: "uvp-gb28181.sql", suffix: ""},
		{name: "postgresql_converted.sql", suffix: "-postgresql"},
		{name: "sqlserver_converted.sql", suffix: "-sqlserver"},
	}
	for _, initial := range initials {
		t.Run(initial.name, func(t *testing.T) {
			initialBody, err := os.ReadFile(filepath.Join("../../../resource/database", initial.name))
			require.NoError(t, err)
			section := extractOpenAPIPermissionSection(t, string(initialBody))
			migrationBody, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", openAPIPermissionStem+initial.suffix+".sql"))
			require.NoError(t, err)

			freshDB := openAPIPermissionFixture(t)
			runOpenAPIPermissionSQL(t, freshDB, section, initial.suffix)
			first := openAPIPermissionSnapshot(t, freshDB)
			runOpenAPIPermissionSQL(t, freshDB, section, initial.suffix)
			require.Equal(t, first, openAPIPermissionSnapshot(t, freshDB), "fresh-install block must be idempotent")
			assertOpenAPIPermissionState(t, freshDB)

			migrationDB := openAPIPermissionFixture(t)
			runOpenAPIPermissionSQL(t, migrationDB, string(migrationBody), initial.suffix)
			require.Equal(t, openAPIPermissionSnapshot(t, migrationDB), first, "fresh-install and up migration must seed the same natural-key graph")
		})
	}
}

func openAPIPermissionFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	for _, statement := range []string{
		"CREATE TABLE sys_role(id INTEGER PRIMARY KEY,name TEXT,status INTEGER,deleted_at TEXT)",
		"CREATE TABLE sys_api(id INTEGER PRIMARY KEY AUTOINCREMENT,title TEXT,path TEXT,method TEXT,api_group TEXT,created_at TEXT,updated_at TEXT,created_by INTEGER,deleted_at TEXT)",
		"CREATE TABLE sys_menu(id INTEGER PRIMARY KEY AUTOINCREMENT,parent_id INTEGER,path TEXT,name TEXT,component TEXT,title TEXT,hide INTEGER,disable INTEGER,sort INTEGER,type INTEGER,permission TEXT,icon TEXT,created_at TEXT,updated_at TEXT,created_by INTEGER,deleted_at TEXT)",
		"CREATE TABLE sys_role_menu(role_id INTEGER,menu_id INTEGER,PRIMARY KEY(role_id,menu_id))",
		"CREATE TABLE sys_menu_api(menu_id INTEGER,api_id INTEGER,PRIMARY KEY(menu_id,api_id))",
		"CREATE TABLE sys_casbin_rule(id INTEGER PRIMARY KEY AUTOINCREMENT,ptype TEXT,v0 TEXT,v1 TEXT,v2 TEXT,v3 TEXT,v4 TEXT,v5 TEXT)",
		"INSERT INTO sys_role(id,name,status) VALUES(1,'重命名后的根角色',1),(2,'普通用户',1),(3,'游客',1),(4,'系统管理员',1),(5,'系统管理员',0),(6,'系统管理员',1)",
		"INSERT INTO sys_menu(id,permission) VALUES(90,'unrelated:permission')",
		"INSERT INTO sys_role_menu(role_id,menu_id) VALUES(2,90)",
		"INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) VALUES('p','role_2','/unrelated','GET','*','','')",
	} {
		require.NoError(t, db.Exec(statement).Error, statement)
	}
	return db
}

func assertOpenAPIPermissionState(t *testing.T, db *gorm.DB) {
	t.Helper()
	var roleMenuCount int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id WHERE rm.role_id=1 AND m.permission LIKE 'gb28181:openapi:client:%'").Scan(&roleMenuCount).Error)
	require.EqualValues(t, len(openAPIPermissionCodes), roleMenuCount)

	var ordinaryRoleCount int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id WHERE rm.role_id<>1 AND m.permission LIKE 'gb28181:openapi:client:%'").Scan(&ordinaryRoleCount).Error)
	require.Zero(t, ordinaryRoleCount, "same-name ordinary and guest roles must not be promoted")

	var casbinCount int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype='p' AND v0='role_1' AND v1 LIKE '/api/gb28181/openapi-clients%'").Scan(&casbinCount).Error)
	require.EqualValues(t, len(openAPIAdminRoutes), casbinCount)

	var nonRootCasbinCount int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype='p' AND v0<>'role_1' AND v1 LIKE '/api/gb28181/openapi-clients%'").Scan(&nonRootCasbinCount).Error)
	require.Zero(t, nonRootCasbinCount)

	assertOpenAPIPermissionRoutes(t, db)
}

func assertOpenAPIPermissionRoutes(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, route := range openAPIAdminRoutes {
		var linked int64
		require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_menu_api ma JOIN sys_menu m ON m.id=ma.menu_id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission=? AND a.path=? AND a.method=?", route.code, route.path, route.method).Scan(&linked).Error)
		require.EqualValues(t, 1, linked, route.method+" "+route.path)
	}
}

func extractOpenAPIPermissionSection(t *testing.T, body string) string {
	t.Helper()
	const begin = "openapi-aksk-permissions:begin"
	const end = "openapi-aksk-permissions:end"
	lower := strings.ToLower(body)
	markerStart := strings.Index(lower, begin)
	markerEnd := strings.Index(lower, end)
	require.GreaterOrEqual(t, markerStart, 0)
	require.Greater(t, markerEnd, markerStart)
	start := strings.Index(body[markerStart:], "\n")
	require.GreaterOrEqual(t, start, 0)
	start += markerStart + 1
	return body[start:markerEnd]
}

func runOpenAPIPermissionSQL(t *testing.T, db *gorm.DB, body, suffix string) {
	t.Helper()
	sql := strings.ReplaceAll(body, "N'", "'")
	if suffix == "-sqlserver" {
		sql = strings.ReplaceAll(sql, "'role_' + CAST(rm.[role_id] AS varchar(20))", "'role_' || CAST(rm.[role_id] AS TEXT)")
		sql = strings.ReplaceAll(sql, "'role_' + CAST(rm.role_id AS varchar(20))", "'role_' || CAST(rm.role_id AS TEXT)")
		sql = strings.ReplaceAll(sql, "'role_' + CAST(rm.[role_id] AS VARCHAR(20))", "'role_' || CAST(rm.[role_id] AS TEXT)")
	}
	sql = strings.ReplaceAll(sql, "CONCAT('role_',rm.role_id)", "'role_' || rm.role_id")
	sql = strings.ReplaceAll(sql, "CONCAT('role_',rm.[role_id])", "'role_' || rm.[role_id]")
	sql = strings.ReplaceAll(sql, "rm.role_id::text", "rm.role_id")
	sql = strings.ReplaceAll(sql, "rm.[role_id]", "rm.role_id")
	sql = regexp.MustCompile(`(?m)^--.*$`).ReplaceAllString(sql, "")
	for _, statement := range strings.Split(sql, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		require.NoError(t, db.Exec(statement).Error, statement)
	}
}

func openAPIPermissionSnapshot(t *testing.T, db *gorm.DB) map[string]string {
	t.Helper()
	queries := map[string]string{
		"api":       "SELECT id,title,path,method,api_group,created_by,deleted_at FROM sys_api ORDER BY id",
		"menu":      "SELECT id,parent_id,path,name,title,type,permission,created_by,deleted_at FROM sys_menu ORDER BY id",
		"role_menu": "SELECT role_id,menu_id FROM sys_role_menu ORDER BY role_id,menu_id",
		"menu_api":  "SELECT menu_id,api_id FROM sys_menu_api ORDER BY menu_id,api_id",
		"casbin":    "SELECT ptype,v0,v1,v2,v3,v4,v5 FROM sys_casbin_rule ORDER BY id",
	}
	result := make(map[string]string, len(queries))
	for name, query := range queries {
		var rows []map[string]any
		require.NoError(t, db.Raw(query).Scan(&rows).Error, name)
		values := make([]string, 0, len(rows))
		for _, row := range rows {
			values = append(values, fmt.Sprint(row))
		}
		sort.Strings(values)
		result[name] = strings.Join(values, "\n")
	}
	return result
}
