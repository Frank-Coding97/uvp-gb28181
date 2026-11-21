package migration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const serviceUpgradeMenuMigration = "2026-10-18-service-upgrade-menu"

func TestServiceUpgradeMenuUnderSystemAndAdminGrant(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver", "-sqlite"} {
		for _, existing := range []bool{false, true} {
			t.Run(suffix+map[bool]string{false: "/fresh", true: "/existing"}[existing], func(t *testing.T) {
				db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
				require.NoError(t, err)
				raw, err := db.DB()
				require.NoError(t, err)
				t.Cleanup(func() { _ = raw.Close() })
				for _, statement := range []string{
					`CREATE TABLE sys_menu(id INTEGER PRIMARY KEY AUTOINCREMENT,parent_id INTEGER,path TEXT,name TEXT,component TEXT,title TEXT,type INTEGER,permission TEXT,hide INTEGER DEFAULT 0,disable INTEGER DEFAULT 0,icon TEXT,sort INTEGER,created_at TEXT,updated_at TEXT,deleted_at TEXT,created_by INTEGER)`,
					`CREATE TABLE sys_role(id INTEGER PRIMARY KEY,status INTEGER,deleted_at TEXT)`,
					`CREATE TABLE sys_role_menu(role_id INTEGER,menu_id INTEGER,PRIMARY KEY(role_id,menu_id))`,
					`INSERT INTO sys_role VALUES(1,1,NULL),(2,1,NULL)`,
					`INSERT INTO sys_menu(id,parent_id,path,name,title,type) VALUES(77,0,'/system','system','system',1),(90,0,'/unrelated','unrelated','unrelated',2)`,
					`INSERT INTO sys_role_menu VALUES(2,90)`,
				} {
					require.NoError(t, db.Exec(statement).Error)
				}
				if existing {
					require.NoError(t, db.Exec(`INSERT INTO sys_menu(id,parent_id,path,name,component,title,type,permission) VALUES
						(100,0,'/system/service-upgrade/index','service-upgrade','system/service-upgrade/index','服务升级',2,''),
						(101,100,'','ServiceUpgradeRead','','查看服务升级',3,'system:upgrade:read'),
						(102,100,'','ServiceUpgradeManage','','管理服务升级',3,'system:upgrade:manage')`).Error)
				}
				body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", serviceUpgradeMenuMigration+suffix+".sql"))
				require.NoError(t, err)
				for i := 0; i < 2; i++ {
					runOpenAPIPermissionSQL(t, db, string(body), suffix)
					var count int64
					require.NoError(t, db.Raw(`SELECT COUNT(*) FROM sys_menu WHERE parent_id=77 AND path='/system/service-upgrade/index' AND component='system/service-upgrade/index' AND hide=0 AND disable=0 AND type=2`).Scan(&count).Error)
					require.EqualValues(t, 1, count, "服务升级必须作为系统管理下的可见菜单")
					require.NoError(t, db.Raw(`SELECT COUNT(*) FROM sys_role_menu WHERE role_id=1`).Scan(&count).Error)
					require.EqualValues(t, 4, count, "管理员需要系统目录、页面和两个按钮权限")
					require.NoError(t, db.Raw(`SELECT COUNT(*) FROM sys_role_menu WHERE role_id=2`).Scan(&count).Error)
					require.EqualValues(t, 1, count, "普通角色权限保持不变")
				}
				if existing {
					var id int
					require.NoError(t, db.Raw(`SELECT id FROM sys_menu WHERE path='/system/service-upgrade/index'`).Scan(&id).Error)
					require.Equal(t, 100, id, "保留存量菜单 ID")
				}
				require.NoError(t, db.Exec(`DELETE FROM sys_role_menu WHERE role_id=1`).Error)
				require.NoError(t, db.Exec(`UPDATE sys_role SET status=0 WHERE id=1`).Error)
				runOpenAPIPermissionSQL(t, db, string(body), suffix)
				var inactiveGrants int64
				require.NoError(t, db.Raw(`SELECT COUNT(*) FROM sys_role_menu WHERE role_id=1`).Scan(&inactiveGrants).Error)
				require.Zero(t, inactiveGrants, "停用管理员不自动授权")
			})
		}
	}
}

func TestServiceUpgradeMenuRunsOnEmptyAndExistingLedger(t *testing.T) {
	for dialect, suffix := range map[Dialect]string{DialectMySQL: "", DialectPostgres: "-postgresql", DialectSQLServer: "-sqlserver", DialectSQLite: "-sqlite"} {
		name := serviceUpgradeMenuMigration + suffix + ".sql"
		names, err := (&embedSource{dialect: dialect}).UpFiles()
		require.NoError(t, err)
		require.Contains(t, names, name)
		for _, existing := range []bool{false, true} {
			store, lock, src, exec := newFakes()
			if existing {
				store.applied["2026-10-16-service-upgrade-permission"+suffix+".sql"] = true
			}
			src.files[name] = "UPGRADE MENU"
			require.NoError(t, run(store, lock, src, exec, allTablesExistProbe()))
			require.Equal(t, []string{"UPGRADE MENU"}, exec.executed, "新安装不能将菜单补全误标为已执行")
			require.True(t, store.applied[name])
			require.NoError(t, run(store, lock, src, exec, allTablesExistProbe()))
			require.Len(t, exec.executed, 1)
		}
	}
}
