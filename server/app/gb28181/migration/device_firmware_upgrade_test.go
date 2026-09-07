package migration

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"
)

func TestFirmwareUpgradeSchemaAndFreshInstallMatch(t *testing.T) {
	model, err := schema.Parse(&gbmodels.GbDeviceFirmwareUpgrade{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	for suffix, initial := range map[string]string{"": "uvp-gb28181.sql", "-postgresql": "postgresql_converted.sql", "-sqlserver": "sqlserver_converted.sql"} {
		t.Run(suffix, func(t *testing.T) {
			migration, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", "2026-09-05-device-firmware-upgrade"+suffix+".sql"))
			require.NoError(t, err)
			body := string(migration)
			for _, field := range model.DBNames {
				require.Contains(t, body, field, "missing persisted field")
			}
			for _, index := range []string{"uk_firmware_upgrade_operation", "uk_firmware_upgrade_device_idempotency", "uk_firmware_upgrade_device_session", "uk_firmware_upgrade_sn", "idx_firmware_upgrade_device_sn", "idx_firmware_upgrade_device_session", "idx_firmware_upgrade_device_status", "idx_firmware_upgrade_device_time", "idx_firmware_upgrade_deadline"} {
				require.Contains(t, body, index)
			}
			fresh, err := os.ReadFile(filepath.Join("../../../resource/database", initial))
			require.NoError(t, err)
			require.Contains(t, string(fresh), body, "fresh install must use the same migration block")
			if suffix == "" {
				require.Contains(t, body, "utf8mb4_bin")
			}
			if suffix == "-sqlserver" {
				require.Contains(t, body, "Latin1_General_100_BIN2")
			}
		})
	}
}

func TestFirmwareUpgradePermissionMigrationIsScopedAndReversible(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		t.Run(suffix, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			raw, err := db.DB()
			require.NoError(t, err)
			raw.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = raw.Close() })
			for _, statement := range []string{
				"CREATE TABLE sys_api(id INTEGER PRIMARY KEY,title TEXT,path TEXT,method TEXT,api_group TEXT,created_at TEXT,updated_at TEXT,created_by INTEGER,deleted_at TEXT)",
				"CREATE TABLE sys_menu(id INTEGER PRIMARY KEY,parent_id INTEGER,path TEXT,name TEXT,component TEXT,title TEXT,hide INTEGER,disable INTEGER,sort INTEGER,type INTEGER,permission TEXT,icon TEXT,created_at TEXT,updated_at TEXT,created_by INTEGER,deleted_at TEXT)",
				"CREATE TABLE sys_role_menu(role_id INTEGER,menu_id INTEGER)",
				"CREATE TABLE sys_menu_api(menu_id INTEGER,api_id INTEGER)",
				"CREATE TABLE sys_casbin_rule(ptype TEXT,v0 TEXT,v1 TEXT,v2 TEXT,v3 TEXT,v4 TEXT,v5 TEXT)",
				"INSERT INTO sys_menu(id,permission) VALUES(40,'unrelated'),(41,'gb28181:device:maintenance:view')",
				"INSERT INTO sys_role_menu(role_id,menu_id) VALUES(1,41),(2,41),(3,40)",
				"INSERT INTO sys_api(id,path,method) VALUES(90,'/api/gb28181/device-mgmt/device/:id/reboot','POST')",
				"CREATE TABLE gb_device_firmware_upgrade(id INTEGER PRIMARY KEY)",
				"INSERT INTO gb_device_firmware_upgrade(id) VALUES(1)",
			} {
				require.NoError(t, db.Exec(statement).Error)
			}
			path := filepath.Join("../../../resource/database/gb28181/migrations", "2026-09-05-device-firmware-upgrade"+suffix)
			bytes, err := os.ReadFile(path + ".sql")
			require.NoError(t, err)
			sections := strings.Split(string(bytes), "-- device-firmware-upgrade-permissions:start")
			require.Len(t, sections, 2)
			run := func(body string) {
				body = strings.ReplaceAll(body, "N'", "'")
				body = strings.ReplaceAll(body, "CONCAT('role_',rm.role_id)", "'role_' || rm.role_id")
				for _, statement := range strings.Split(body, ";") {
					if strings.TrimSpace(statement) != "" {
						require.NoError(t, db.Exec(statement).Error, statement)
					}
				}
			}
			run(sections[1])
			run(sections[1])
			for query, want := range map[string]int64{
				"SELECT COUNT(*) FROM sys_api":                                         3,
				"SELECT COUNT(*) FROM sys_menu":                                        3,
				"SELECT COUNT(*) FROM sys_menu_api":                                    2,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_1'":               2,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2' AND v2='GET'":  1,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2' AND v2='POST'": 0,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_3'":               0,
			} {
				var count int64
				require.NoError(t, db.Raw(query).Scan(&count).Error)
				require.Equal(t, want, count, query)
			}
			down, err := os.ReadFile(path + "-down.sql")
			require.NoError(t, err)
			run(string(down))
			run(string(down))
			for query, want := range map[string]int64{
				"SELECT COUNT(*) FROM sys_api":                    1,
				"SELECT COUNT(*) FROM sys_menu":                   2,
				"SELECT COUNT(*) FROM sys_role_menu":              3,
				"SELECT COUNT(*) FROM sys_menu_api":               0,
				"SELECT COUNT(*) FROM sys_casbin_rule":            0,
				"SELECT COUNT(*) FROM gb_device_firmware_upgrade": 1,
			} {
				var count int64
				require.NoError(t, db.Raw(query).Scan(&count).Error)
				require.Equal(t, want, count, query)
			}
		})
	}
}
