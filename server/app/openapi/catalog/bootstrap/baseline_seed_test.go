package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 用绿色包真实 SQLite 基线建库后跑 EnsureCoreCatalog。
//
// ⛔⛔ 为什么必须用真实基线而不是 AutoMigrate：
// 现场（220 绿色包 2026-10-09 19:06）在基线库上报的是
// `catalog_seed_conflict: group "device-management" is missing`
// —— 而同样的代码在 AutoMigrate 出来的库上全绿。
// 差别只可能来自基线里的**列定义**（类型/默认值/约束），
// 所以排查必须回到基线本身。
func TestEnsureCoreCatalogAgainstGreenPackageBaseline(t *testing.T) {
	baseline := locateBaseline(t)
	script, err := os.ReadFile(baseline)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "gp.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("PRAGMA foreign_keys = OFF").Error)
	require.NoError(t, db.Exec(string(script)).Error)
	require.NoError(t, db.Exec("PRAGMA foreign_keys = ON").Error)

	var tabs int64
	require.NoError(t, db.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tabs).Error)
	t.Logf("基线表数=%d", tabs)

	// 与现场同序：先建 runtime_state（生产里也是这么起来的）。
	result, err := EnsureCoreCatalog(context.Background(), db, SystemActorID)
	require.NoError(t, err, "基线库上 EnsureCoreCatalog 失败")
	t.Logf("groups=%d capabilities=%d operations=%d",
		result.GroupsCreated, result.CapabilitiesCreated, result.OperationsCreated)
	require.Positive(t, result.GroupsCreated, "分组必须被创建出来")

	// 每个 group 都必须带非零主键 —— bootstrap.go:234 就是靠它判"缺失"。
	var rows []struct {
		ID     int64
		Code   string
		Status string
	}
	require.NoError(t, db.Table("sys_openapi_capability_group").Select("id, code, status").Scan(&rows).Error)
	require.Len(t, rows, 2)
	for _, r := range rows {
		require.NotZero(t, r.ID, "分组 %s 的主键为 0 ⇒下一行group.ID==0 会报 seed_conflict", r.Code)
		t.Logf("group id=%d code=%s status=%s", r.ID, r.Code, r.Status)
	}
}

func locateBaseline(t *testing.T) string {
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