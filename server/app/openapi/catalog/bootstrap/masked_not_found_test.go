package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/utils/gormhelper"
	openapimodels "uvplatform.com/uvp-gb28181/app/openapi/models"
)

// ⛔⛔⛔ 现场根因（220 绿色包，2026-10-09 19:06/ 19:10）
//
// 生产用 gormhelper 打开 SQLite 时注册了这个回调（gormhelper/sqlite.go:121）：
//
//	gormDb.Callback().Query().Before("gorm:query").
//		Register("disable_raise_record_not_found", MaskNotDataError)
//	// MaskNotDataError 做的是：gormDB.Statement.RaiseErrorOnNotFound = false
//
// ⇒ **First 查不到行时，query.Error 是 nil，而不是 gorm.ErrRecordNotFound。**
//
// 于是 ensureGroup 里这段判断整个失效：
//
//	if query.Error == nil {                    // ← 查不到行时也成立！
//	    if group.DeletedAt != nil { ... }
//	    return group, false, nil               // ← 返回**零值 group**，跳过 Create
//	}
//	if !errors.Is(query.Error, gorm.ErrRecordNotFound) { ... }
//	// ↑ 永远到不了这里，Create 永远不执行
//
// 零值 group 被存进 groups map，随后 bootstrap.go:234 的
// `if !ok || group.ID == 0` 命中 ⇒ `catalog_seed_conflict: group %q is missing`
// ⇒ panic ⇒ 整个后端退出、平台完全不可用。
//
// ⛔ 为什么之前所有本地测试都抓不到：它们用裸 gorm.Open(...)
// 或 glebarez/sqlite，**都没有注册 MaskNotDataError 回调**，
// 于是 First 正常返回 ErrRecordNotFound，代码按预期工作。
// ⛔ 这也正是本仓记忆里那条 masked-not-found 坑的又一次复现：
// 全局回调把 not-found 掩成"成功 + 零值"，而业务代码按"有 error"来判成败。
// TestBootstrapAgainstMaskedNotFoundCallback 在**生产同款回调**下跑完整 seed。
//
// 修复前：EnsureCoreCatalog 返回
//
//	OpenAPI catalog bootstrap identity conflict: group "device-management" is missing
//
// 与现场 220 的 panic 逐字一致。修复后必须成功 —— 这条用例就是那个断言。
func TestBootstrapAgainstMaskedNotFoundCallback(t *testing.T) {
	db := openWithProductionCallbacks(t)

	var probe openapimodels.CapabilityGroup
	query := db.Session(&gorm.Session{}).Where("code = ?", "definitely-absent").First(&probe)
	t.Logf("MaskNotDataError 下：query.Error=%v  group.ID=%d  RowsAffected=%d",
		query.Error, probe.ID, query.RowsAffected)

	// 断言这个前提本身 —— 它是整条推理的基石，也是"为什么单测抓不到"的答案。
	require.NoError(t, query.Error, "MaskNotDataError 会把 not-found 掩成 nil error")
	require.NotErrorIs(t, query.Error, gorm.ErrRecordNotFound)
	require.Zero(t, probe.ID)
	require.Zero(t, query.RowsAffected, "RowsAffected 才能区分'取到行'与'没取到'")

	// 修复的核心承诺：带生产回调的空库上，seed 必须成功。
	result, err := EnsureCoreCatalog(context.Background(), db, SystemActorID)
	require.NoError(t, err, "带生产回调的库上 seed 仍失败 ⇒ 现场 panic 未解决")
	require.Equal(t, 2, result.GroupsCreated)
	require.Equal(t, 12, result.CapabilitiesCreated)
	require.Equal(t, 12, result.OperationsCreated)

	// 且必须真的落库、带非零主键。
	var groups []openapimodels.CapabilityGroup
	require.NoError(t, db.Find(&groups).Error)
	require.Len(t, groups, 2)
	for _, g := range groups {
		require.NotZero(t, g.ID)
		require.NotEmpty(t, g.Code)
	}
}

// TestEnsureGroupMustNotTreatMaskedNotFoundAsHit 是**修复的判据**：
// ensureGroup 不得把"掩码后的 not-found"当成"查到了"，必须去Create。
func TestEnsureGroupMustNotTreatMaskedNotFoundAsHit(t *testing.T) {
	db := openWithProductionCallbacks(t)

	group, created, err := ensureGroup(context.Background(), db,
		groupDefinition{code: "device-management", name: "设备管理", sort: 10}, 0)
	require.NoError(t, err)
	require.True(t, created, "空表 + 掩码回调 ⇒ 必须走 Create，不能当成已存在")
	require.NotZero(t, group.ID, "Create 后必须回填主键，否则 bootstrap.go:234 会报 group is missing")

	var count int64
	require.NoError(t, db.Model(&openapimodels.CapabilityGroup{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

// openWithProductionCallbacks 用**生产同款**回调打开绿色包基线库。
//
// ⛔ 这才是本次能复现的关键：先前所有测试都只gorm.Open，
// 少了 gormhelper 注册的这一批回调，于是走的是"理想行为"路径。
func openWithProductionCallbacks(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gp.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		PrepareStmt:            false,
		SkipDefaultTransaction: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.Exec("PRAGMA foreign_keys = OFF").Error)
	script, err := os.ReadFile(locateBaselineForMaskedTest(t))
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(script)).Error)
	require.NoError(t, db.Exec("PRAGMA foreign_keys = ON").Error)

	// 与 gormhelper/sqlite.go 生产初始化完全一致。
	require.NoError(t, db.Callback().Query().Before("gorm:query").
		Register("disable_raise_record_not_found", gormhelper.MaskNotDataError))
	require.NoError(t, db.Callback().Create().Before("gorm:before_create").
		Register("CreateBeforeHook", gormhelper.CreateBeforeHook))
	require.NoError(t, db.Callback().Update().Before("gorm:before_update").
		Register("UpdateBeforeHook", gormhelper.UpdateBeforeHook))
	require.NoError(t, db.Callback().Delete().Before("gorm:before_delete").
		Register("DeleteBeforeHook", gormhelper.DeleteBeforeHook))
	return db
}

func locateBaselineForMaskedTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	for dir := wd; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "resource", "database", "sqlitebaseline", "baseline.sql")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate
		}
	}
	t.Fatal("找不到绿色包 SQLite 基线 baseline.sql")
	return ""
}