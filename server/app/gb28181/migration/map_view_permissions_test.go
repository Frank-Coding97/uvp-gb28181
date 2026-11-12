package migration

// 地图视图接口的权限补齐迁移（2026-10-29-map-view-permissions*）。
//
// 为什么要有这个用例：这 7 个接口当初只写进了 baseline/seeds（全量初始化），
// 没写增量迁移 —— 新装的库有权限、升级上来的库一条都没有，管理员进地图视图
// 直接吃 403。这类"只在 seeds 里存在"的漏洞编译期发现不了，只能靠迁移用例锁住。

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 与前端 web/src/views/gb28181/device-mgmt/api.ts 里实际调用的路径一一对应。
var mapViewPermissionPaths = []string{
	"/api/gb28181/device-mgmt/map/markers",
	"/api/gb28181/device-mgmt/map/clusters",
	"/api/gb28181/device-mgmt/map/no-coord-count",
	"/api/gb28181/device-mgmt/map/channel-tree",
	"/api/gb28181/device-mgmt/map/trajectory",
	"/api/gb28181/device-mgmt/map/center",
}

const mapViewDeviceViewMenuID = 140452

func TestMapViewPermissionMigrationFilesExist(t *testing.T) {
	for _, name := range []string{
		"2026-10-29-map-view-permissions.sql",
		"2026-10-29-map-view-permissions-postgresql.sql",
		"2026-10-29-map-view-permissions-sqlserver.sql",
		"2026-10-29-map-view-permissions-sqlite.sql",
		"2026-10-29-map-view-permissions-down.sql",
		"2026-10-29-map-view-permissions-postgresql-down.sql",
		"2026-10-29-map-view-permissions-sqlserver-down.sql",
		"2026-10-29-map-view-permissions-sqlite-down.sql",
	} {
		require.NotEmptyf(t, readWorkRecordingPermissionMigration(t, name), "迁移文件 %s 缺失或为空", name)
	}
}

// 真在 SQLite 上跑一遍：既能证明文法在 ANSI 方言下成立，
// 也能顺手证明"重复执行不产生重复行"（升级时可能因手动补数据撞车）。
func TestMapViewPermissionMigrationAppliesAndIsIdempotent(t *testing.T) {
	db := openWorkRecordingPermissionSQLite(t)
	seedWorkRecordingPermissionSchema(t, db)
	// 升级库上地图要挂的那张菜单：权限串与 baseline/seeds 里一致。
	require.NoError(t, db.Exec(
		"INSERT INTO sys_menu(id,parent_id,path,name,type,permission,deleted_at) VALUES(?,0,'/gb28181/device-mgmt','device-mgmt',2,'gb28181:device:view',NULL)",
		mapViewDeviceViewMenuID).Error)
	// 已存在但漏授权的痕迹：sys_api 有行、casbin 没行（升级库的典型半残状态）。
	require.NoError(t, db.Exec(
		"INSERT INTO sys_api(id,title,path,method,api_group,deleted_at) VALUES(9400,'查询地图标记点','/api/gb28181/device-mgmt/map/markers','GET','设备管理',NULL)").Error)

	up := readWorkRecordingPermissionMigration(t, "2026-10-29-map-view-permissions-sqlite.sql")
	for _, stmt := range splitStatements(up) {
		require.NoError(t, db.Exec(stmt).Error, stmt)
	}
	// 第二遍：所有语句都必须退化成空操作。
	for _, stmt := range splitStatements(up) {
		require.NoError(t, db.Exec(stmt).Error, stmt)
	}

	var apiCount, menuAPICount, adminCount, guestCount int64
	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM sys_api WHERE path LIKE '/api/gb28181/device-mgmt/map/%'").Scan(&apiCount).Error)
	// 7 条：markers / clusters / no-coord-count / channel-tree / trajectory / center(GET) / center(PUT)
	require.EqualValues(t, 7, apiCount, "地图接口应登记 7 条 sys_api")

	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM sys_menu_api ma JOIN sys_api a ON a.id=ma.api_id WHERE ma.menu_id=? AND a.path LIKE '/api/gb28181/device-mgmt/map/%'",
		mapViewDeviceViewMenuID).Scan(&menuAPICount).Error)
	require.EqualValues(t, 7, menuAPICount, "地图接口应全部挂到 gb28181:device:view 菜单")

	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype='p' AND v0='role_1' AND v1 LIKE '/api/gb28181/device-mgmt/map/%'").Scan(&adminCount).Error)
	require.EqualValues(t, 7, adminCount, "系统管理员应拿到全部 7 条")

	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype='p' AND v0='role_3' AND v1 LIKE '/api/gb28181/device-mgmt/map/%'").Scan(&guestCount).Error)
	require.EqualValues(t, 6, guestCount, "游客只拿 GET，改默认视野仍是管理员动作")

	// 游客不得拿到写动作 —— 这条单独钉死，不能被"6 条够数"糊过去。
	var guestWrite int64
	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype='p' AND v0='role_3' AND v1='/api/gb28181/device-mgmt/map/center' AND v2='PUT'").Scan(&guestWrite).Error)
	require.Zero(t, guestWrite)

	// 无关的菜单/接口/角色不能被顺手改到。
	var untouched int64
	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2' AND v1='/api/unrelated'").Scan(&untouched).Error)
	require.EqualValues(t, 1, untouched)
	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM sys_menu_api WHERE menu_id=20 AND api_id=900").Scan(&untouched).Error)
	require.EqualValues(t, 1, untouched)
}

// 三个方言文件都必须覆盖同一组路径，否则某个数据库上还是会 403。
func TestMapViewPermissionDialectsCoverEveryPath(t *testing.T) {
	for _, name := range []string{
		"2026-10-29-map-view-permissions.sql",
		"2026-10-29-map-view-permissions-postgresql.sql",
		"2026-10-29-map-view-permissions-sqlserver.sql",
		"2026-10-29-map-view-permissions-sqlite.sql",
	} {
		body := readWorkRecordingPermissionMigration(t, name)
		for _, path := range mapViewPermissionPaths {
			require.Containsf(t, body, path, "迁移 %s 漏了 %s", name, path)
		}
		require.Containsf(t, body, "gb28181:device:view", "迁移 %s 没挂到 gb28181:device:view 菜单", name)
	}
}
