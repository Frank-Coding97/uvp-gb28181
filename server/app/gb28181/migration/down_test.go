package migration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 5.1:down 执行 + 删版本记录
func TestDownExecutesAndDeletes(t *testing.T) {
	store := &fakeStore{applied: map[string]bool{"2026-07-20-a.sql": true}}
	src := &fakeSource{files: map[string]string{
		"2026-07-20-a.sql":      "CREATE TABLE t",
		"2026-07-20-a-down.sql": "DROP TABLE t",
	}}
	exec := &fakeExec{}

	err := downWith(store, exec, src, DialectMySQL, "2026-07-20-a.sql")
	require.NoError(t, err)
	require.Len(t, exec.executed, 1)
	require.Contains(t, exec.executed[0], "DROP TABLE t")
	require.False(t, store.applied["2026-07-20-a.sql"], "版本记录应被删除")
}

// 5.2:down 文件缺失
func TestDownMissingFile(t *testing.T) {
	store := &fakeStore{applied: map[string]bool{}}
	src := &fakeSource{files: map[string]string{}}
	exec := &fakeExec{}

	err := downWith(store, exec, src, DialectPostgres, "2026-07-20-a-postgresql.sql")
	require.Error(t, err)
	require.Contains(t, err.Error(), "postgres", "错误应含方言")
	require.Contains(t, err.Error(), "2026-07-20-a-postgresql-down.sql", "错误应含 down 文件名")
	require.Empty(t, exec.executed)
}

// down 执行失败不删版本记录
func TestDownExecFailureKeepsRecord(t *testing.T) {
	store := &fakeStore{applied: map[string]bool{"2026-07-20-a.sql": true}}
	src := &fakeSource{files: map[string]string{
		"2026-07-20-a-down.sql": "DROP TABLE t BROKEN",
	}}
	exec := &fakeExec{failOn: "BROKEN"}

	err := downWith(store, exec, src, DialectMySQL, "2026-07-20-a.sql")
	require.Error(t, err)
	require.True(t, store.applied["2026-07-20-a.sql"], "执行失败不应删除版本记录")
}
