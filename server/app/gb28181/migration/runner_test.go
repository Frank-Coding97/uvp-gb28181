package migration

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---- fakes ----

type fakeStore struct {
	applied   map[string]bool
	marks     [][]string
	ensureErr error
}

func (f *fakeStore) EnsureTable() error { return f.ensureErr }
func (f *fakeStore) ListApplied() ([]string, error) {
	out := make([]string, 0, len(f.applied))
	for v := range f.applied {
		out = append(out, v)
	}
	sort.Strings(out)
	return out, nil
}
func (f *fakeStore) MarkApplied(vs []string) error {
	f.marks = append(f.marks, vs)
	for _, v := range vs {
		f.applied[v] = true
	}
	return nil
}
func (f *fakeStore) DeleteApplied(v string) error {
	delete(f.applied, v)
	return nil
}
func (f *fakeStore) IsEmpty() (bool, error) { return len(f.applied) == 0, nil }

type fakeLocker struct {
	acquireErr error
	acquired   bool
	released   bool
}

func (f *fakeLocker) Acquire() error { f.acquired = true; return f.acquireErr }
func (f *fakeLocker) Release() error { f.released = true; return nil }

type fakeSource struct {
	files map[string]string // name -> sql text
}

func (f *fakeSource) UpFiles() ([]string, error) {
	out := make([]string, 0, len(f.files))
	for name := range f.files {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}
func (f *fakeSource) ReadSQL(name string) (string, error) {
	sqlText, ok := f.files[name]
	if !ok {
		return "", fmt.Errorf("文件不存在: %s", name)
	}
	return sqlText, nil
}

type fakeExec struct {
	executed []string
	failOn   string // SQL 包含该子串时返回 error
}

func (f *fakeExec) ExecSQL(sqlText string) error {
	f.executed = append(f.executed, sqlText)
	if f.failOn != "" && strings.Contains(sqlText, f.failOn) {
		return errors.New("exec boom")
	}
	return nil
}

func newFakes() (*fakeStore, *fakeLocker, *fakeSource, *fakeExec) {
	return &fakeStore{applied: map[string]bool{}},
		&fakeLocker{},
		&fakeSource{files: map[string]string{}},
		&fakeExec{}
}

// ---- 3.1 首启基线化 ----

func TestRunFirstStartBaselines(t *testing.T) {
	store, lock, src, exec := newFakes()
	src.files["a.sql"] = "SQL FOR a"
	src.files["b.sql"] = "SQL FOR b"

	err := run(store, lock, src, exec, func(string) (bool, error) { return true, nil })
	require.NoError(t, err)
	require.Empty(t, exec.executed, "快照库(探测表存在)基线化不应执行任何 SQL")
	require.Len(t, store.marks, 1)
	require.Equal(t, []string{"a.sql", "b.sql"}, store.marks[0])
}

// ---- 3.1b 老基线库(探测表缺失)拒绝 ----

func TestRunFirstStartRejectsMissingBaselineTable(t *testing.T) {
	store, lock, src, exec := newFakes()
	src.files["a.sql"] = "SQL FOR a"

	err := run(store, lock, src, exec, func(string) (bool, error) { return false, nil })
	require.Error(t, err, "老基线库不得记录假成功")
	require.Empty(t, exec.executed)
	require.Empty(t, store.marks)
}

// ---- 3.2 增量执行 ----

func TestRunIncrementalAppliesPendingOnly(t *testing.T) {
	store, lock, src, exec := newFakes()
	store.applied["a.sql"] = true
	src.files["a.sql"] = "SQL FOR a"
	src.files["b.sql"] = "SQL FOR b"

	err := run(store, lock, src, exec, func(string) (bool, error) { return true, nil })
	require.NoError(t, err)
	require.Len(t, exec.executed, 1)
	require.Contains(t, exec.executed[0], "SQL FOR b")
	require.Equal(t, [][]string{{"b.sql"}}, store.marks)
}

// ---- 3.3 失败不标记 ----

func TestRunFailureNotMarked(t *testing.T) {
	store, lock, src, exec := newFakes()
	store.applied["a.sql"] = true
	src.files["a.sql"] = "SQL FOR a"
	src.files["b.sql"] = "SQL FOR b IS BROKEN"
	exec.failOn = "IS BROKEN"

	err := run(store, lock, src, exec, func(string) (bool, error) { return true, nil })
	require.Error(t, err)
	require.Contains(t, err.Error(), "b.sql", "错误应含文件名")
	require.Contains(t, err.Error(), "SQL FOR b IS BROKEN", "错误应含 SQL 内容")
	require.False(t, store.applied["b.sql"], "失败的迁移不应被标记")
}

// ---- 3.4 幂等 ----

func TestRunIdempotent(t *testing.T) {
	store, lock, src, exec := newFakes()
	store.applied["a.sql"] = true
	src.files["a.sql"] = "SQL FOR a"
	src.files["b.sql"] = "SQL FOR b"

	require.NoError(t, run(store, lock, src, exec, func(string) (bool, error) { return true, nil }))
	require.NoError(t, run(store, lock, src, exec, func(string) (bool, error) { return true, nil }))
	require.Len(t, exec.executed, 1, "第二次运行不应执行任何 SQL")
}

// ---- 3.6 锁失败透传 ----

func TestRunLockFailureBlocks(t *testing.T) {
	store, lock, src, exec := newFakes()
	lock.acquireErr = errors.New("lock timeout")
	src.files["a.sql"] = "SQL FOR a"

	err := run(store, lock, src, exec, func(string) (bool, error) { return true, nil })
	require.Error(t, err)
	require.Contains(t, err.Error(), "lock timeout")
	require.Empty(t, exec.executed, "锁失败不应执行任何迁移")
	require.Len(t, store.marks, 0)
}
