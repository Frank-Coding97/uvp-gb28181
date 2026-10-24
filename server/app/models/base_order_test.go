package models

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 排序参数必须只接受「标识符 + 方向」，任何 SQL 表达式都进不来。
//
// ⛔⛔ 这是注入点的回归护栏：GORM 的 Order(string) 是原样拼接
//
//	（clause.Column{Raw: true}），过去 `?order=` 可以带出
//	`ORDER BY extractvalue(1,concat(0x7e,(select version())))`。
func TestNormalizeOrderAcceptsIdentifiersOnly(t *testing.T) {
	accepted := []string{
		"id",
		"id desc",
		"created_at ASC",
		"u.created_at desc",
		"id desc, name asc",
		"  id   desc  ",
	}
	for _, order := range accepted {
		paging := &BasePaging{Order: order}
		require.NotEmpty(t, paging.normalizeOrder(), "应接受的排序表达式: %q", order)
	}

	rejected := []string{
		"id desc,(select sleep(5))",
		"id desc, (select 1 from (select sleep(5))x)",
		"extractvalue(1,concat(0x7e,(select version())))",
		"id desc; drop table users",
		"(case when (select 1)=1 then id else name end)",
		"id desc -- comment",
		"id' or '1'='1",
		"`id` desc",
		"id desc, name asc, status desc, created_at desc", // 超过 3 段
		"",
		"   ",
	}
	for _, order := range rejected {
		paging := &BasePaging{Order: order}
		require.Empty(t, paging.normalizeOrder(), "必须拒绝的排序表达式: %q", order)
	}
}

// 端到端：非法 order 不应该出现在最终 SQL 里，合法 order 要照常生效。
func TestPaginateDropsInjectedOrderExpression(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open("file:order-whitelist?mode=memory&cache=shared"),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true},
	)
	require.NoError(t, err)

	injected := &BasePaging{PageNum: 1, PageSize: 10, Order: "id desc,(select sleep(5))"}
	stmt := db.Model(&SysAffix{}).Scopes(injected.Paginate()).Find(&[]SysAffix{}).Statement
	require.NotContains(t, stmt.SQL.String(), "sleep")
	require.NotContains(t, stmt.SQL.String(), "ORDER BY")

	legal := &BasePaging{PageNum: 1, PageSize: 10, Order: "created_at desc"}
	stmt = db.Model(&SysAffix{}).Scopes(legal.Paginate()).Find(&[]SysAffix{}).Statement
	require.Contains(t, stmt.SQL.String(), "ORDER BY created_at desc")
}
