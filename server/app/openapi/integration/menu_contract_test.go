package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const openAPIClientMenuStem = "2026-09-06-openapi-client-menu"

var openAPIClientMenuButtons = []struct {
	permission string
	name       string
	title      string
}{
	{permission: "gb28181:openapi:client:read", name: "Permission_gb28181_openapi_client_read", title: "查看 OpenAPI 客户端"},
	{permission: "gb28181:openapi:client:create", name: "Permission_gb28181_openapi_client_create", title: "创建 OpenAPI 客户端"},
	{permission: "gb28181:openapi:client:grant", name: "Permission_gb28181_openapi_client_grant", title: "分配 OpenAPI 客户端能力"},
	{permission: "gb28181:openapi:client:rotate", name: "Permission_gb28181_openapi_client_rotate", title: "轮换 OpenAPI 客户端密钥"},
	{permission: "gb28181:openapi:client:status", name: "Permission_gb28181_openapi_client_status", title: "启停或撤销 OpenAPI 客户端"},
	{permission: "gb28181:openapi:client:audit", name: "Permission_gb28181_openapi_client_audit", title: "查看 OpenAPI 客户端审计"},
}

const (
	openAPIClientMenuPath      = "/gb28181/openapi-client"
	openAPIClientMenuName      = "gb28181-openapi-client"
	openAPIClientMenuComponent = "gb28181/openapi-client/index"
)

func TestOpenAPIClientMenuMigrationSQLiteLifecycle(t *testing.T) {
	db := openAPIClientMenuFixture(t)
	runOpenAPIClientMenuSQL(t, db, openAPIClientMenuStem+".sql")

	pageID := openAPIClientMenuPageID(t, db)
	var page struct {
		ParentID  int64
		Path      string
		Name      string
		Component string
		Title     string
		KeepAlive int
		Type      int
	}
	require.NoError(t, db.Raw("SELECT parent_id,path,name,component,title,keep_alive,type FROM sys_menu WHERE id=?", pageID).Scan(&page).Error)
	require.Equal(t, int64(0), page.ParentID)
	require.Equal(t, openAPIClientMenuPath, page.Path)
	require.Equal(t, openAPIClientMenuName, page.Name)
	require.Equal(t, openAPIClientMenuComponent, page.Component)
	require.Equal(t, "OpenAPI 客户端", page.Title)
	require.Zero(t, page.KeepAlive)
	require.Equal(t, 2, page.Type)

	for _, button := range openAPIClientMenuButtons {
		var parentID int64
		require.NoError(t, db.Raw("SELECT parent_id FROM sys_menu WHERE permission=? AND deleted_at IS NULL", button.permission).Scan(&parentID).Error)
		require.Equal(t, pageID, parentID, button.permission)
	}
	assertOpenAPIClientMenuRootGrant(t, db, pageID, 1)
	require.Equal(t, int64(0), openAPIClientMenuCount(t, db, "SELECT COUNT(*) FROM sys_role_menu WHERE role_id=2 AND menu_id=?", pageID))
	require.Equal(t, int64(0), openAPIClientMenuCount(t, db, "SELECT COUNT(*) FROM sys_role_menu WHERE role_id=3 AND menu_id=?", pageID))

	apiLinksBefore := openAPIClientMenuCount(t, db, "SELECT COUNT(*) FROM sys_menu_api")
	snapshotBefore := openAPIClientMenuSnapshot(t, db)
	runOpenAPIClientMenuSQL(t, db, openAPIClientMenuStem+".sql")
	require.Equal(t, snapshotBefore, openAPIClientMenuSnapshot(t, db), "re-running up must be idempotent")
	require.Equal(t, apiLinksBefore, openAPIClientMenuCount(t, db, "SELECT COUNT(*) FROM sys_menu_api"), "up must not create or rewrite button API links")

	runOpenAPIClientMenuSQL(t, db, openAPIClientMenuStem+"-down.sql")
	require.Equal(t, int64(0), openAPIClientMenuCount(t, db, "SELECT COUNT(*) FROM sys_menu WHERE path=? AND deleted_at IS NULL", openAPIClientMenuPath))
	require.Equal(t, int64(0), openAPIClientMenuCount(t, db, "SELECT COUNT(*) FROM sys_role_menu WHERE menu_id=?", pageID))
	require.Equal(t, apiLinksBefore, openAPIClientMenuCount(t, db, "SELECT COUNT(*) FROM sys_menu_api"), "down must preserve button API links")
	for _, button := range openAPIClientMenuButtons {
		var parentID int64
		require.NoError(t, db.Raw("SELECT parent_id FROM sys_menu WHERE permission=? AND deleted_at IS NULL", button.permission).Scan(&parentID).Error)
		require.Zero(t, parentID, button.permission)
	}
	require.Equal(t, int64(1), openAPIClientMenuCount(t, db, "SELECT COUNT(*) FROM sys_menu WHERE permission='unrelated:permission' AND deleted_at IS NULL"))

	runOpenAPIClientMenuSQL(t, db, openAPIClientMenuStem+".sql")
	require.Equal(t, int64(1), openAPIClientMenuCount(t, db, "SELECT COUNT(*) FROM sys_menu WHERE path=? AND deleted_at IS NULL", openAPIClientMenuPath))
}

func TestOpenAPIClientMenuMigrationRejectsTargetPageConflict(t *testing.T) {
	db := openAPIClientMenuFixture(t)
	require.NoError(t, db.Exec("INSERT INTO sys_menu(parent_id,path,name,component,title,type,keep_alive,hide,disable,permission) VALUES(0,?,'foreign-page','foreign/component','foreign',2,1,0,0,'')", openAPIClientMenuPath).Error)
	err := runOpenAPIClientMenuSQLResult(db, openAPIClientMenuMigrationPath(openAPIClientMenuStem+".sql"))
	require.Error(t, err, "a path collision with a different page must abort the migration")
	var parentID int64
	require.NoError(t, db.Raw("SELECT parent_id FROM sys_menu WHERE permission=?", openAPIClientMenuButtons[0].permission).Scan(&parentID).Error)
	require.Zero(t, parentID, "a conflict must not move an existing button")
	require.Equal(t, int64(0), openAPIClientMenuCount(t, db, "SELECT COUNT(*) FROM sys_menu WHERE path=? AND component=? AND deleted_at IS NULL", openAPIClientMenuPath, openAPIClientMenuComponent))
}

func TestOpenAPIClientMenuMigrationRejectsButtonParentConflict(t *testing.T) {
	db := openAPIClientMenuFixture(t)
	require.NoError(t, db.Exec("UPDATE sys_menu SET parent_id=99 WHERE permission=?", openAPIClientMenuButtons[0].permission).Error)
	err := runOpenAPIClientMenuSQLResult(db, openAPIClientMenuMigrationPath(openAPIClientMenuStem+".sql"))
	require.Error(t, err, "a button already owned by another parent must abort the migration")
	require.Equal(t, int64(0), openAPIClientMenuCount(t, db, "SELECT COUNT(*) FROM sys_menu WHERE path=? AND deleted_at IS NULL", openAPIClientMenuPath))
	var parentID int64
	require.NoError(t, db.Raw("SELECT parent_id FROM sys_menu WHERE permission=?", openAPIClientMenuButtons[0].permission).Scan(&parentID).Error)
	require.Equal(t, int64(99), parentID)
}

func TestOpenAPIClientMenuMigrationDoesNotGrantInactiveRoot(t *testing.T) {
	db := openAPIClientMenuFixture(t)
	require.NoError(t, db.Exec("UPDATE sys_role SET status=0 WHERE id=1").Error)
	runOpenAPIClientMenuSQL(t, db, openAPIClientMenuStem+".sql")
	pageID := openAPIClientMenuPageID(t, db)
	require.Equal(t, int64(0), openAPIClientMenuCount(t, db, "SELECT COUNT(*) FROM sys_role_menu WHERE menu_id=?", pageID))
}

func TestOpenAPIClientMenuMigrationDialectContracts(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		t.Run(suffix, func(t *testing.T) {
			up := strings.ToLower(openAPIClientMenuReadFile(t, openAPIClientMenuStem+suffix+".sql"))
			down := strings.ToLower(openAPIClientMenuReadFile(t, openAPIClientMenuStem+suffix+"-down.sql"))
			require.Contains(t, up, "openapi-client-menu:begin")
			require.Contains(t, up, "openapi-client-menu:end")
			require.Contains(t, down, "openapi-client-menu:down-begin")
			require.Contains(t, down, "openapi-client-menu:down-end")
			require.Contains(t, up, openAPIClientMenuPath)
			require.Contains(t, up, openAPIClientMenuComponent)
			require.Contains(t, up, "keep_alive")
			require.NotContains(t, up, "insert into sys_api")
			require.NotContains(t, up, "sys_menu_api")
			require.NotContains(t, up, "sys_casbin")
			require.NotContains(t, down, "delete from sys_menu_api")
			require.NotContains(t, down, "delete from sys_api")
			for _, button := range openAPIClientMenuButtons {
				require.Contains(t, up, strings.ToLower(button.permission))
				require.Contains(t, down, strings.ToLower(button.permission))
			}
			// Existing T04 root identity is deliberately stable and status-gated.
			normalized := strings.NewReplacer("[", "", "]", "", "`", "").Replace(up)
			require.Contains(t, normalized, "r.id=1")
			require.Contains(t, normalized, "r.status=1")
			require.Contains(t, normalized, "r.deleted_at is null")
			require.NotContains(t, normalized, "r.name")
			// A conditional duplicate-key guard is required so a foreign page or
			// button parent cannot be silently skipped or overwritten.
			require.Regexp(t, regexp.MustCompile(`(?s)insert[[:space:]]+into[[:space:]].*guard`), up)
		})
	}
}

func openAPIClientMenuFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	for _, statement := range []string{
		"CREATE TABLE sys_role(id INTEGER PRIMARY KEY,name TEXT,status INTEGER,deleted_at TEXT)",
		"CREATE TABLE sys_menu(id INTEGER PRIMARY KEY AUTOINCREMENT,parent_id INTEGER NOT NULL DEFAULT 0,path TEXT NOT NULL,name TEXT NOT NULL,redirect TEXT,component TEXT,title TEXT,is_full INTEGER DEFAULT 0,hide INTEGER DEFAULT 0,disable INTEGER DEFAULT 0,keep_alive INTEGER DEFAULT 0,affix INTEGER DEFAULT 0,link TEXT DEFAULT '',iframe INTEGER DEFAULT 0,svg_icon TEXT DEFAULT '',icon TEXT DEFAULT '',sort INTEGER DEFAULT 0,type INTEGER DEFAULT 2,is_link INTEGER DEFAULT 0,permission TEXT DEFAULT '',created_at TEXT,updated_at TEXT,created_by INTEGER,deleted_at TEXT)",
		"CREATE TABLE sys_role_menu(role_id INTEGER,menu_id INTEGER,PRIMARY KEY(role_id,menu_id))",
		"CREATE TABLE sys_menu_api(menu_id INTEGER,api_id INTEGER,PRIMARY KEY(menu_id,api_id))",
		"INSERT INTO sys_role(id,name,status) VALUES(1,'系统管理员',1),(2,'普通角色',1),(3,'游客',1)",
		"INSERT INTO sys_menu(id,parent_id,path,name,component,title,type,permission,hide,keep_alive,deleted_at) VALUES(90,0,'/unrelated','unrelated','unrelated/index','无关页面',2,'unrelated:permission',0,0,NULL)",
		"INSERT INTO sys_role_menu(role_id,menu_id) VALUES(2,90)",
	} {
		require.NoError(t, db.Exec(statement).Error, statement)
	}
	for i, button := range openAPIClientMenuButtons {
		require.NoError(t, db.Exec("INSERT INTO sys_menu(parent_id,path,name,component,title,sort,type,permission,hide,keep_alive,icon,deleted_at) VALUES(0,'',?,'',?,100,3,?,1,0,'',NULL)", button.name, button.title, button.permission).Error)
		require.NoError(t, db.Exec("INSERT INTO sys_menu_api(menu_id,api_id) SELECT id,? FROM sys_menu WHERE permission=?", 100+i, button.permission).Error)
	}
	return db
}

func openAPIClientMenuMigrationPath(name string) string {
	return filepath.Join("../../../resource/database/gb28181/migrations", name)
}

func openAPIClientMenuReadFile(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(openAPIClientMenuMigrationPath(name))
	require.NoError(t, err)
	return string(body)
}

func runOpenAPIClientMenuSQL(t *testing.T, db *gorm.DB, name string) {
	t.Helper()
	require.NoError(t, runOpenAPIClientMenuSQLResult(db, openAPIClientMenuMigrationPath(name)))
}

func runOpenAPIClientMenuSQLResult(db *gorm.DB, path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sql := regexp.MustCompile(`(?m)^\s*--.*$`).ReplaceAllString(string(body), "")
	for _, statement := range strings.Split(sql, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func openAPIClientMenuPageID(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var id int64
	require.NoError(t, db.Raw("SELECT id FROM sys_menu WHERE path=? AND deleted_at IS NULL", openAPIClientMenuPath).Scan(&id).Error)
	require.NotZero(t, id)
	return id
}

func openAPIClientMenuCount(t *testing.T, db *gorm.DB, query string, args ...interface{}) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Raw(query, args...).Scan(&count).Error)
	return count
}

func assertOpenAPIClientMenuRootGrant(t *testing.T, db *gorm.DB, pageID, roleID int64) {
	t.Helper()
	require.Equal(t, int64(1), openAPIClientMenuCount(t, db, "SELECT COUNT(*) FROM sys_role_menu WHERE role_id=? AND menu_id=?", roleID, pageID), roleID)
}

func openAPIClientMenuSnapshot(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var rows []struct {
		ID       int64
		ParentID int64
		Path     string
		Name     string
		Deleted  *string
	}
	require.NoError(t, db.Raw("SELECT id,parent_id,path,name,deleted_at FROM sys_menu ORDER BY id").Scan(&rows).Error)
	rowsText := make([]string, 0, len(rows))
	for _, row := range rows {
		deleted := ""
		if row.Deleted != nil {
			deleted = *row.Deleted
		}
		rowsText = append(rowsText, fmt.Sprintf("%d|%d|%s|%s|%s", row.ID, row.ParentID, row.Path, row.Name, deleted))
	}
	return strings.TrimSpace(strings.ReplaceAll(strings.Join(rowsText, "\n"), "\r", ""))
}
