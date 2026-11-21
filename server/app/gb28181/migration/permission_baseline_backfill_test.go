package migration

// 存量库权限基线补齐（2026-11-10-permission-baseline-backfill*）。
//
// 为什么要有这个用例：2026-11-10 线上设备列表页报「您没有权限访问此资源」，
// 根因是老库升级后 sys_api / sys_casbin_rule 没跟上 baseline/seeds 真源。
// 更隐蔽的是：专门修地图 403 的 2026-10-29-map-view-permissions* 脚本
// **写了却没进 embed 白名单**，所以升级包里根本没有它 —— 只有「脚本内容」的
// 用例全绿，线上照样 403。这里把「必须在打包清单里」也钉死。

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const permissionBackfillMigration = "2026-11-10-permission-baseline-backfill"

var permissionBackfillDialects = []Dialect{DialectMySQL, DialectPostgres, DialectSQLServer, DialectSQLite}

func permissionBackfillSuffix(d Dialect) string {
	switch d {
	case DialectPostgres:
		return "-postgresql"
	case DialectSQLServer:
		return "-sqlserver"
	case DialectSQLite:
		return "-sqlite"
	default:
		return ""
	}
}

// 设备列表页进页面就要调的三个接口：缺任一条管理员都会被 casbin 判 403。
var permissionBackfillDeviceListAPIs = []struct{ path, method string }{
	{"/api/gb28181/device-mgmt/firmware-repository", "GET"},
	{"/api/gb28181/device-mgmt/map/center", "GET"},
	{"/api/gb28181/device-mgmt/map/channel-tree", "GET"},
}

// ⛔ 只测「文件内容」不够：必须证明这些文件真的被打进二进制。
func TestPermissionBackfillMigrationsAreEmbedded(t *testing.T) {
	for _, d := range permissionBackfillDialects {
		suffix := permissionBackfillSuffix(d)
		up := permissionBackfillMigration + suffix + ".sql"
		down := permissionBackfillMigration + suffix + "-down.sql"
		src := &embedSource{dialect: d}

		files, err := src.UpFiles()
		require.NoError(t, err)
		require.Containsf(t, files, up, "方言 %v 的补齐迁移没进 embed 白名单", d)
		_, err = src.ReadSQL(up)
		require.NoErrorf(t, err, "方言 %v 的 up 脚本读不到", d)
		_, err = src.ReadSQL(DownFileName(up))
		require.NoErrorf(t, err, "方言 %v 缺回滚脚本 %s", d, down)
	}
}

// 2026-10-29 那批地图权限迁移当初被漏打包，正是本次故障的放大器。
func TestMapViewPermissionMigrationsAreEmbedded(t *testing.T) {
	for _, d := range permissionBackfillDialects {
		suffix := permissionBackfillSuffix(d)
		up := "2026-10-29-map-view-permissions" + suffix + ".sql"
		src := &embedSource{dialect: d}
		files, err := src.UpFiles()
		require.NoError(t, err)
		require.Containsf(t, files, up, "方言 %v 的地图权限迁移没进 embed 白名单", d)
		_, err = src.ReadSQL(DownFileName(up))
		require.NoErrorf(t, err, "方言 %v 的地图权限回滚脚本读不到", d)
	}
}

// 四个方言必须覆盖同一组设备列表接口，否则某个数据库上升完级还是 403。
func TestPermissionBackfillDialectsCoverDeviceListAPIs(t *testing.T) {
	for _, d := range permissionBackfillDialects {
		body := readWorkRecordingPermissionMigration(t, permissionBackfillMigration+permissionBackfillSuffix(d)+".sql")
		for _, api := range permissionBackfillDeviceListAPIs {
			require.Containsf(t, body, api.path, "方言 %v 漏了 %s", d, api.path)
		}
	}
}

// 真在 SQLite 上跑两遍：证明文法成立、幂等、且设备列表页三条授权确实补齐。
func TestPermissionBackfillAppliesAndIsIdempotent(t *testing.T) {
	db := openWorkRecordingPermissionSQLite(t)
	seedWorkRecordingPermissionSchema(t, db)
	up := readWorkRecordingPermissionMigration(t, permissionBackfillMigration+"-sqlite.sql")

	for _, stmt := range splitStatements(up) {
		require.NoError(t, db.Exec(stmt).Error, stmt)
	}
	// 1 条无关记录 + 补齐的 5 条登记 / 32 条授权。
	var apiCount, ruleCount int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_api").Scan(&apiCount).Error)
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_casbin_rule").Scan(&ruleCount).Error)
	require.EqualValues(t, 6, apiCount)
	require.EqualValues(t, 33, ruleCount)

	for _, api := range permissionBackfillDeviceListAPIs {
		var n int64
		require.NoError(t, db.Raw(
			"SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype='p' AND v0='role_1' AND v1=? AND v2=?",
			api.path, api.method).Scan(&n).Error)
		require.EqualValuesf(t, 1, n, "管理员缺授权：%s %s", api.method, api.path)
	}

	// 第二遍必须完全退化为 no-op。
	for _, stmt := range splitStatements(up) {
		require.NoError(t, db.Exec(stmt).Error, stmt)
	}
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_api").Scan(&apiCount).Error)
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_casbin_rule").Scan(&ruleCount).Error)
	require.EqualValues(t, 6, apiCount)
	require.EqualValues(t, 33, ruleCount)

	// 无关的菜单/接口/角色不能被顺手改到。
	var untouched int64
	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2' AND v1='/api/unrelated'").Scan(&untouched).Error)
	require.EqualValues(t, 1, untouched)

	// 回滚：本迁移是「补齐基线」型，down 故意 no-op（详见 -sqlite-down.sql 顶部注释）。
	// 补的那 5 条登记 / 32 条授权本来就在 baseline/seeds 里，up 只补「恰好缺的」，
	// down 无从区分「本次插入」与「库里原本就有」（两表都没有能标记来源的列）。
	// 实测：老库上按清单删会连带删掉 24 条与本迁移无关的既有授权，降级后照样 403。
	// 所以这里断言的是「一行都不能少」，不是「回到 1 行」。
	down := readWorkRecordingPermissionMigration(t, permissionBackfillMigration+"-sqlite-down.sql")
	for _, stmt := range splitStatements(down) {
		require.NoError(t, db.Exec(stmt).Error, stmt)
	}
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_api").Scan(&apiCount).Error)
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_casbin_rule").Scan(&ruleCount).Error)
	require.EqualValues(t, 6, apiCount)
	require.EqualValues(t, 33, ruleCount)
}

// 旧 down 的真实事故场景：这些行**本来就全在**时，按书面清单删会误伤。
//
// 构造：先跑 up（此刻 5 条登记 + 32 条授权齐全，等价于新装基线库、或老库里本来就配过）
// → 再跑 down → 一条都不许少。旧实现会删到只剩 1 条 api / 1 条授权，本用例立刻红。
//
// 为什么值得单独一个用例：它锁的不是"up 幂等"，而是"down 不许误删预存行"这个方向 ——
// 将来若有人想给 down 恢复删除语义，先看到这个名字。
func TestPermissionBackfillDownLeavesPreexistingRows(t *testing.T) {
	db := openWorkRecordingPermissionSQLite(t)
	seedWorkRecordingPermissionSchema(t, db)

	up := readWorkRecordingPermissionMigration(t, permissionBackfillMigration+"-sqlite.sql")
	for _, stmt := range splitStatements(up) {
		require.NoError(t, db.Exec(stmt).Error, stmt)
	}

	down := readWorkRecordingPermissionMigration(t, permissionBackfillMigration+"-sqlite-down.sql")
	for _, stmt := range splitStatements(down) {
		require.NoError(t, db.Exec(stmt).Error, stmt)
	}

	var apiCount, ruleCount, untouched int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_api").Scan(&apiCount).Error)
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_casbin_rule").Scan(&ruleCount).Error)
	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2' AND v1='/api/unrelated'").Scan(&untouched).Error)

	require.EqualValues(t, 6, apiCount, "down 删掉了不属于本迁移的接口登记")
	require.EqualValues(t, 33, ruleCount, "down 删掉了不属于本迁移的授权")
	require.EqualValues(t, 1, untouched)
}
