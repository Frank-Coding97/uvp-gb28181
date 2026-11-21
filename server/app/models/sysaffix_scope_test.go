package models

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 附件按 ID 查询必须套用数据范围：越权时与"不存在"表现一致。
//
// ⛔⛔ 列表接口走 datascope、按 ID 接口直查 —— 这个不对称就是越权本身：
//
//	有对应接口权限的用户枚举 ID 就能读到别人附件的 path/url，甚至改名删除。
func TestGetAffixByIDScopedHidesOtherUsersRows(t *testing.T) {
	db := newChunkScopeDB(t)

	owned := &SysAffix{Name: "自己的.txt", Path: "/srv/uploads/a.txt", Size: 1, CreatedBy: 11}
	other := &SysAffix{Name: "别人的.txt", Path: "/srv/uploads/b.txt", Size: 1, CreatedBy: 22}
	require.NoError(t, db.Create(owned).Error)
	require.NoError(t, db.Create(other).Error)

	// 模拟 datascope.GetDataScope 在无全表权限时的过滤方式
	scopeFor := func(userID uint) func(*gorm.DB) *gorm.DB {
		return func(query *gorm.DB) *gorm.DB {
			return query.Where("created_by = ?", userID)
		}
	}

	var got SysAffix
	require.NoError(t, got.getByIDScoped(db, owned.ID, scopeFor(11)))
	require.Equal(t, "自己的.txt", got.Name)

	var denied SysAffix
	require.Error(t, denied.getByIDScoped(db, other.ID, scopeFor(11)),
		"别人的附件必须查不到（枚举 ID 不能读到 path/url）")
	require.Zero(t, denied.ID)

	// 不加范围（nil scope）时保持原语义 —— 后台任务仍能按 ID 取
	var internal SysAffix
	require.NoError(t, internal.getByIDScoped(db, other.ID, nil))
	require.Equal(t, "别人的.txt", internal.Name)
}
