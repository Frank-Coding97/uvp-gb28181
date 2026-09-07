package migration

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbroutes "uvplatform.cn/uvp-gb28181/app/gb28181/routes"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/utils/casbinhelper"
)

type guestAPI struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}
type guestPreset struct {
	Pages       []string   `json:"pages"`
	Permissions []string   `json:"permissions"`
	AllowedAPIs []guestAPI `json:"allowedAPIs"`
}

func readGuestJSON(t *testing.T, name string, target any) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181", name))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, target))
}
func guestFixture(t *testing.T) (*gorm.DB, guestPreset) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { raw.Close() })
	for _, s := range []string{
		"CREATE TABLE sys_role(id INTEGER PRIMARY KEY,name TEXT,sort INTEGER,status INTEGER,description TEXT,parent_id INTEGER,data_scope INTEGER,checked_depts TEXT,created_at TEXT,updated_at TEXT,created_by INTEGER,deleted_at TEXT)",
		"CREATE TABLE sys_menu(id INTEGER PRIMARY KEY,parent_id INTEGER,path TEXT,name TEXT,component TEXT,title TEXT,hide INTEGER,disable INTEGER,sort INTEGER,type INTEGER,permission TEXT,icon TEXT,created_at TEXT,updated_at TEXT,created_by INTEGER,deleted_at TEXT)",
		"CREATE TABLE sys_api(id INTEGER PRIMARY KEY,title TEXT,path TEXT,method TEXT,api_group TEXT,created_at TEXT,updated_at TEXT,created_by INTEGER,deleted_at TEXT)",
		"CREATE TABLE sys_menu_api(menu_id INTEGER,api_id INTEGER,PRIMARY KEY(menu_id,api_id))",
		"CREATE TABLE sys_role_menu(role_id INTEGER,menu_id INTEGER,PRIMARY KEY(role_id,menu_id))",
		"CREATE TABLE sys_casbin_rule(id INTEGER PRIMARY KEY,ptype TEXT,v0 TEXT,v1 TEXT,v2 TEXT,v3 TEXT,v4 TEXT,v5 TEXT)",
		"INSERT INTO sys_role(id,name,parent_id,status,data_scope,description) VALUES(1,'管理员',0,1,1,''),(2,'原角色',0,1,2,''),(3,'游客',0,1,4,'')",
		"INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) VALUES('p','role_2','/unrelated','PUT','*','',''),('g','user_92','role_2','','*','',''),('p','role_3','/unsafe','POST','*','','')",
	} {
		require.NoError(t, db.Exec(s).Error)
	}
	var c struct {
		Buttons []struct {
			ParentPath string `json:"parentPath"`
		} `json:"buttons"`
	}
	readGuestJSON(t, "button-permissions.json", &c)
	var p guestPreset
	readGuestJSON(t, "guest-permissions.json", &p)
	paths := map[string]bool{"/system": true}
	for _, b := range c.Buttons {
		paths[b.ParentPath] = true
	}
	for _, path := range p.Pages {
		paths[path] = true
	}
	for path := range paths {
		require.NoError(t, db.Exec("INSERT INTO sys_menu(parent_id,path,type,permission,disable) VALUES(0,?,2,'',0)", path).Error)
	}
	body, err := os.ReadFile("../../../resource/database/gb28181/migrations/2026-09-05-button-permission-catalog.sql")
	require.NoError(t, err)
	runGuestSQL(t, db, string(body))
	require.NoError(t, db.Exec("UPDATE sys_menu SET permission='gb28181:recording:view' WHERE path='/gb28181/cloud-recordings'").Error)
	require.NoError(t, db.Exec("UPDATE sys_menu SET permission='gb28181:alarm:view' WHERE path='/gb28181/alarm-management'").Error)
	cloud := []guestAPI{{"POST", "/api/gb28181/cloud-recordings/files/:id/downloads"}, {"GET", "/api/gb28181/cloud-recordings/downloads/:taskId"}, {"DELETE", "/api/gb28181/cloud-recordings/downloads/:taskId"}}
	for _, a := range p.AllowedAPIs {
		if strings.Contains(a.Path, "/cloud-recordings/") || strings.Contains(a.Path, "/zlm/nodes") || strings.Contains(a.Path, "/alarms") {
			cloud = append(cloud, a)
		}
	}
	for _, a := range cloud {
		require.NoError(t, db.Exec("INSERT INTO sys_api(path,method) SELECT ?,? WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path=? AND method=?)", a.Path, a.Method, a.Path, a.Method).Error)
		page := "/gb28181/cloud-recordings"
		if strings.Contains(a.Path, "/alarms") {
			page = "/gb28181/alarm-management"
		}
		require.NoError(t, db.Exec("INSERT OR IGNORE INTO sys_menu_api SELECT m.id,a.id FROM sys_menu m,sys_api a WHERE m.path=? AND a.path=? AND a.method=?", page, a.Path, a.Method).Error)
	}
	require.NoError(t, db.Exec("INSERT INTO sys_role_menu SELECT 1,id FROM sys_menu WHERE permission='gb28181:recording:view'").Error)
	require.NoError(t, db.Exec("INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT DISTINCT 'p','role_1',a.path,a.method,'*','','' FROM sys_role_menu r JOIN sys_menu_api ma ON ma.menu_id=r.menu_id JOIN sys_api a ON a.id=ma.api_id WHERE r.role_id=1").Error)
	return db, p
}
func runGuestSQL(t *testing.T, db *gorm.DB, sql string) {
	t.Helper()
	sql = regexp.MustCompile(`(?m)^--.*$`).ReplaceAllString(sql, "")
	sql = strings.ReplaceAll(sql, "N'", "'")
	sql = regexp.MustCompile(`CONCAT\('role_',\(SELECT MIN\(id\) FROM sys_role WHERE name='游客' AND deleted_at IS NULL\)\)`).ReplaceAllString(sql, "'role_' || (SELECT MIN(id) FROM sys_role WHERE name='游客' AND deleted_at IS NULL)")
	for _, s := range strings.Split(sql, ";") {
		if strings.TrimSpace(s) != "" {
			require.NoError(t, db.Exec(s).Error, s)
		}
	}
}
func guestSnapshot(t *testing.T, db *gorm.DB, query string) string {
	t.Helper()
	var rows []map[string]any
	require.NoError(t, db.Raw(query).Scan(&rows).Error)
	body, err := json.Marshal(rows)
	require.NoError(t, err)
	return string(body)
}
func TestGuestPermissionsMigrationAndRoleSaveRemainReadonly(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		t.Run(suffix, func(t *testing.T) {
			db, p := guestFixture(t)
			original := guestSnapshot(t, db, "SELECT * FROM sys_casbin_rule WHERE v0<>'role_3' ORDER BY id")
			body, err := os.ReadFile("../../../resource/database/gb28181/migrations/2026-09-05-guest-readonly-permissions" + suffix + ".sql")
			require.NoError(t, err)
			runGuestSQL(t, db, string(body))
			require.Equal(t, original, guestSnapshot(t, db, "SELECT * FROM sys_casbin_rule WHERE v0<>'role_3' ORDER BY id"))
			snapshots := map[string]string{}
			for _, table := range []string{"sys_role", "sys_menu", "sys_api", "sys_menu_api", "sys_role_menu", "sys_casbin_rule"} {
				snapshots[table] = guestSnapshot(t, db, "SELECT * FROM "+table+" ORDER BY 1,2")
			}
			runGuestSQL(t, db, string(body))
			for table, want := range snapshots {
				require.Equal(t, want, guestSnapshot(t, db, "SELECT * FROM "+table+" ORDER BY 1,2"), table)
			}
			var actual []guestAPI
			require.NoError(t, db.Raw("SELECT v1 AS path,v2 AS method FROM sys_casbin_rule WHERE ptype='p' AND v0='role_3'").Scan(&actual).Error)
			require.ElementsMatch(t, p.AllowedAPIs, actual)
			var fromMenus []guestAPI
			require.NoError(t, db.Raw("SELECT DISTINCT a.path,a.method FROM sys_role_menu rm JOIN sys_menu_api ma ON ma.menu_id=rm.menu_id JOIN sys_api a ON a.id=ma.api_id WHERE rm.role_id=3 AND a.deleted_at IS NULL").Scan(&fromMenus).Error)
			require.ElementsMatch(t, p.AllowedAPIs, fromMenus, "重新保存角色必须生成同一安全集合")
			var scope int
			require.NoError(t, db.Raw("SELECT data_scope FROM sys_role WHERE id=3").Scan(&scope).Error)
			require.Equal(t, 4, scope)
		})
	}
}

type guestConfig struct{ app.YmlConfigInterf }

func (guestConfig) GetString(k string) string {
	if k == "casbin.tableprefix" {
		return "sys_"
	}
	if k == "casbin.tablename" {
		return "casbin_rule"
	}
	return ""
}
func (guestConfig) GetInt(string) int          { return 0 }
func (guestConfig) GetUintSlice(string) []uint { return nil }
func TestGuestCasbinMiddlewareRejectsEveryUnlistedGBRoute(t *testing.T) {
	db, p := guestFixture(t)
	body, err := os.ReadFile("../../../resource/database/gb28181/migrations/2026-09-05-guest-readonly-permissions.sql")
	require.NoError(t, err)
	runGuestSQL(t, db, string(body))
	configBody, err := os.ReadFile("../../../config/config.example.yml")
	require.NoError(t, err)
	var cfg struct {
		Casbin struct {
			Model string `yaml:"modelconfig"`
		} `yaml:"casbin"`
	}
	require.NoError(t, yaml.Unmarshal(configBody, &cfg))
	require.NotEmpty(t, cfg.Casbin.Model)
	oldConfig, oldLog := app.ConfigYml, app.ZapLog
	app.ConfigYml = guestConfig{}
	app.ZapLog = zap.NewNop()
	t.Cleanup(func() { app.ConfigYml = oldConfig; app.ZapLog = oldLog })
	helper := casbinhelper.NewCasbinHelper()
	require.NoError(t, helper.InitCasbin(db, cfg.Casbin.Model))
	t.Cleanup(helper.Close)
	require.NoError(t, helper.AddRolesForUserByID(999, []uint{3}, ""))
	gin.SetMode(gin.TestMode)
	inventory := gin.New()
	gbroutes.RegisterRoutes(inventory.Group("/api"))
	allowed := map[string]bool{}
	for _, a := range p.AllowedAPIs {
		allowed[a.Method+" "+a.Path] = true
	}
	reached := map[string]bool{}
	for _, route := range inventory.Routes() {
		key := route.Method + " " + route.Path
		reached[key] = true
		t.Run(key, func(t *testing.T) {
			called := false
			engine := gin.New()
			engine.Use(func(c *gin.Context) {
				claims := &app.Claims{}
				claims.UserID = 999
				c.Set(consts.BindContextKeyName, claims)
			})
			engine.Use(helper.CasbinMiddleware())
			engine.Handle(route.Method, route.Path, func(c *gin.Context) { called = true; c.Status(204) })
			path := regexp.MustCompile(`:[^/]+`).ReplaceAllString(route.Path, "sample")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(route.Method, path, nil))
			if allowed[key] {
				require.Equal(t, 204, response.Code)
				require.True(t, called)
			} else {
				require.Equal(t, 403, response.Code, fmt.Sprint(response.Body))
				require.False(t, called, "拒绝必须在业务handler执行前")
			}
		})
	}
	for _, a := range p.AllowedAPIs {
		if strings.HasPrefix(a.Path, "/api/gb28181/") {
			require.True(t, reached[a.Method+" "+a.Path], a)
		}
	}
}

func TestGuestMigrationMovesExistingProbePoliciesAndLinks(t *testing.T) {
	db, _ := guestFixture(t)
	oldPath, newPath := "/api/gb28181/play/:deviceId/probe", "/api/gb28181/stream-probes/:streamId"
	require.NoError(t, db.Exec("INSERT INTO sys_api(path,method) VALUES(?,'POST')", oldPath).Error)
	require.NoError(t, db.Exec("INSERT INTO sys_menu_api SELECT m.id,a.id FROM sys_menu m,sys_api a WHERE m.permission='gb28181:play:diagnose' AND a.path=?", oldPath).Error)
	require.NoError(t, db.Exec("INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) VALUES('p','role_2',?,'POST','*','','')", oldPath).Error)
	body, err := os.ReadFile("../../../resource/database/gb28181/migrations/2026-09-05-guest-readonly-permissions.sql")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		runGuestSQL(t, db, string(body))
	}
	var n int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_api WHERE path=? AND deleted_at IS NULL", oldPath).Scan(&n).Error)
	require.Zero(t, n)
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_menu_api ma JOIN sys_api a ON a.id=ma.api_id WHERE a.path=?", oldPath).Scan(&n).Error)
	require.Zero(t, n)
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2' AND v1=? AND v2='POST'", newPath).Scan(&n).Error)
	require.EqualValues(t, 1, n)
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2' AND v1='/unrelated'").Scan(&n).Error)
	require.EqualValues(t, 1, n)
}
