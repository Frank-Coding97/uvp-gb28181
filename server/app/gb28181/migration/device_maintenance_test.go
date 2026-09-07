package migration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"
)

func TestDeviceMaintenancePermissionsAreScopedAndIdempotent(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		t.Run(suffix, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", "2026-09-05-device-maintenance-permissions"+suffix+".sql"))
			require.NoError(t, err)
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			raw, err := db.DB()
			require.NoError(t, err)
			raw.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = raw.Close() })
			for _, sql := range []string{
				"CREATE TABLE sys_api(id INTEGER PRIMARY KEY, title TEXT,path TEXT,method TEXT,api_group TEXT,created_at TEXT,updated_at TEXT,created_by INTEGER,deleted_at TEXT)",
				"CREATE TABLE sys_menu(id INTEGER PRIMARY KEY,parent_id INTEGER,path TEXT,name TEXT,component TEXT,title TEXT,hide INTEGER,disable INTEGER,sort INTEGER,type INTEGER,permission TEXT,icon TEXT,created_at TEXT,updated_at TEXT,created_by INTEGER,deleted_at TEXT)",
				"CREATE TABLE sys_role_menu(role_id INTEGER,menu_id INTEGER)",
				"CREATE TABLE sys_menu_api(menu_id INTEGER,api_id INTEGER)",
				"CREATE TABLE sys_casbin_rule(ptype TEXT,v0 TEXT,v1 TEXT,v2 TEXT,v3 TEXT,v4 TEXT,v5 TEXT)",
			} {
				require.NoError(t, db.Exec(sql).Error)
			}
			// Retain an unrelated permission and an existing read-only role assignment.
			require.NoError(t, db.Exec("INSERT INTO sys_menu(id,permission) VALUES (40,'unrelated'),(41,'gb28181:device:maintenance:view')").Error)
			require.NoError(t, db.Exec("INSERT INTO sys_role_menu(role_id,menu_id) VALUES (2,41),(3,40)").Error)
			sql := strings.ReplaceAll(string(body), "N'", "'")
			sql = strings.ReplaceAll(sql, "CONCAT('role_',rm.role_id)", "'role_' || rm.role_id")
			for i := 0; i < 2; i++ {
				for _, statement := range splitStatements(sql) {
					require.NoError(t, db.Exec(statement).Error, statement)
				}
			}
			for query, want := range map[string]int64{
				"SELECT COUNT(*) FROM sys_api":                                         2,
				"SELECT COUNT(*) FROM sys_menu":                                        3,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_1'":               2,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2' AND v2='GET'":  1,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2' AND v2='POST'": 0,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_3'":               0,
				"SELECT COUNT(*) FROM sys_menu_api":                                    2,
			} {
				var got int64
				require.NoError(t, db.Raw(query).Scan(&got).Error)
				require.Equal(t, want, got, query)
			}
			var invalid int64
			require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_menu_api ma JOIN sys_menu m ON m.id=ma.menu_id JOIN sys_api a ON a.id=ma.api_id WHERE (m.permission='gb28181:device:maintenance:view' AND a.method<>'GET') OR (m.permission='gb28181:device:reboot' AND a.method<>'POST')").Scan(&invalid).Error)
			require.Zero(t, invalid)
			down, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", "2026-09-05-device-maintenance-permissions"+suffix+"-down.sql"))
			require.NoError(t, err)
			for _, statement := range splitStatements(strings.ReplaceAll(string(down), "N'", "'")) {
				require.NoError(t, db.Exec(statement).Error)
			}
			for query, want := range map[string]int64{
				"SELECT COUNT(*) FROM sys_api":                                      0,
				"SELECT COUNT(*) FROM sys_menu":                                     1,
				"SELECT COUNT(*) FROM sys_role_menu WHERE role_id=3 AND menu_id=40": 1,
				"SELECT COUNT(*) FROM sys_casbin_rule":                              0,
			} {
				var got int64
				require.NoError(t, db.Raw(query).Scan(&got).Error)
				require.Equal(t, want, got, query)
			}
		})
	}
}

func TestDeviceMaintenancePermissionsIncludedInFreshSchemas(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
		require.NoError(t, err)
		for _, token := range []string{"device-maintenance:start", "gb28181:device:maintenance:view", "gb28181:device:reboot", "/device/:id/maintenance-operations", "/device/:id/reboot"} {
			require.Contains(t, string(body), token, fmt.Sprintf("%s missing %s", name, token))
		}
	}
}
