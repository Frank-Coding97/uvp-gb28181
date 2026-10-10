package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	appmodels "uvplatform.com/uvp-gb28181/app/models"
	openapimodels "uvplatform.com/uvp-gb28181/app/openapi/models"
)

// ⛔⛔ 现场根因（220 绿色包，2026-10-09 18:55，日志为证）：
//
//	select ... table=sys_openapi_capability  rows=0   ← 查不到，ID 留 0
//	select ... table=sys_openapi_capability  rows=0   ← 又一次
//	UPDATE sys_openapi_capability SET ...（**无 WHERE**）← gorm_missing_where_clause
//
// 也就是说：retireRemovedPlaybackCapability 的 First 没被识别成 NotFound，
// 于是零值对象（ID=0）继续往下走，Model(&capability).Updates() 拼不出
// `WHERE id = ...`，GORM 拒绝执行并返回 ErrMissingWhereClause。
//
// ⚠️ 注意 retireRemovedPlaybackCapability 明明写了
//
//	if errors.Is(query.Error, gorm.ErrRecordNotFound) { return nil }
//
// 它没拦住 ⇒ **query.Error 不是 ErrRecordNotFound**。
// 本测试锁住"查不到就必须停"这个契约，防止将来有人删掉那个判断。
func TestRetireRemovedPlaybackCapabilityStopsWhenLookupFindsNothing(t *testing.T) {
	db := newEmptyCatalogDB(t)

	// 表里没有 play:live:apply，First 必然查不到。
	require.NoError(t, retireRemovedPlaybackCapability(context.Background(), db, 0))

	// 关键：绝不能因为"查不到"而发出无 WHERE 的 UPDATE 把全表改掉。
	var count int64
	require.NoError(t, db.Model(&openapimodels.Capability{}).Count(&count).Error)
	require.Zero(t, count)
}

// 直接验证 First 查不到时 query.Error 的真实取值 —— 这是判断
// "为什么 errors.Is(NotFound) 没拦住"的唯一依据。
func TestFirstNotFoundErrorIdentity(t *testing.T) {
	db := newEmptyCatalogDB(t)
	var capability openapimodels.Capability
	query := db.Where("scope IN ? AND deleted_at IS NULL", []string{"play:live:apply"}).Order("id ASC").First(&capability)

	t.Logf("query.Error = %v", query.Error)
	t.Logf("errors.Is(ErrRecordNotFound) = %v", errors.Is(query.Error, gorm.ErrRecordNotFound))
	t.Logf("capability.ID = %d", capability.ID)
	t.Logf("RowsAffected = %d", query.RowsAffected)

	// 这是修正后的实现所依赖的前提。
	require.Error(t, query.Error, "查不到必须有 error")
	require.True(t, errors.Is(query.Error, gorm.ErrRecordNotFound), "查不到的error 必须是 ErrRecordNotFound，否则调用方的判断会失效")
}

// 同样的契约也适用于 disableRetiredPlaybackGroup（它同样 Model(&零值)）。
func TestDisableRetiredPlaybackGroupStopsWhenLookupFindsNothing(t *testing.T) {
	db := newEmptyCatalogDB(t)
	require.NoError(t, disableRetiredPlaybackGroup(context.Background(), db, 0))

	var count int64
	require.NoError(t, db.Model(&openapimodels.CapabilityGroup{}).Count(&count).Error)
	require.Zero(t, count, "查不到分组时不得发出无 WHERE 的 UPDATE")
}

// ⛔⛔ 这是现场故障的**直接复现**。
//
// 现场（220绿色包 2026-10-09 18:55）的 SQL 日志逐条：
//
//	select ... FROM `sys_openapi_capability` WHERE scope IN (...)   rows=0
//	UPDATE `sys_openapi_capability` SET status=?,updated_by=?,updated_at=?  ← 无 WHERE
//	→ gorm_missing_where_clause → panic → 平台完全不可用
//
// 空表时 First 不返回 error，但 capability.ID=0。修复前代码只看 error，
// 于是带着零值继续走，Model(&零值).Updates() 拼不出 WHERE。
// 修复后显式判 ID<=0 —— 本用例锁死这个行为：空表必须安静返回，
// 且**不得改动任何行**（无 WHERE 的 UPDATE 会把全表状态改掉）。
func TestRetireRemovedPlaybackCapabilityWithEmptyTableIsNoOp(t *testing.T) {
	db := newEmptyCatalogDB(t)

	// 造一条"会被无 WHERE UPDATE 误伤"的行，确保守卫真的拦住了。
	require.NoError(t, db.Create(&openapimodels.Capability{
		Scope: "unrelated:scope", Code: "unrelated", Name: "无关能力",
		Status: openapimodels.CatalogStatusDraft, RowVersion: 1,
	}).Error)

	require.NoError(t, retireRemovedPlaybackCapability(context.Background(), db, 0))

	var got []openapimodels.Capability
	require.NoError(t, db.Find(&got).Error)
	require.Len(t, got, 1)
	require.Equal(t, openapimodels.CatalogStatusDraft, got[0].Status,
		"查不到退役能力时，绝不能把无关行批量改成 disabled")
}

func newEmptyCatalogDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&appmodels.SysApi{},
		&openapimodels.CapabilityGroup{},
		&openapimodels.Capability{},
		&openapimodels.Operation{},
	))
	return db
}