package executors

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openCompactTestDB 建一个文件型 SQLite（内存库不支持 auto_vacuum）。
// autoVacuum=true 时按线上 DSN 的方式在建库前设成 INCREMENTAL。
func openCompactTestDB(t *testing.T, autoVacuum bool) (*gorm.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "compact.db")
	dsn := path
	if autoVacuum {
		dsn += "?_pragma=auto_vacuum(2)&_pragma=journal_mode(WAL)"
	}
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	require.NoError(t, raw.Ping())

	require.NoError(t, db.Exec("CREATE TABLE blobs (id INTEGER PRIMARY KEY, body BLOB)").Error)
	return db, path
}

// fillThenDelete 写入 size 行、每行 bodyBytes 字节，然后删掉除 keep 行以外的全部，
// 让文件里留下一批空闲页 —— 这正是日志清理之后数据库的样子。
func fillThenDelete(t *testing.T, db *gorm.DB, rows, bodyBytes, keep int) {
	t.Helper()
	body := strings.Repeat("x", bodyBytes)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	for i := 0; i < rows; i++ {
		require.NoError(t, tx.Exec("INSERT INTO blobs (body) VALUES (?)", body).Error)
	}
	require.NoError(t, tx.Commit().Error)
	require.NoError(t, db.Exec("DELETE FROM blobs WHERE id > ?", keep).Error)
}

func freePages(t *testing.T, db *gorm.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.Raw("PRAGMA freelist_count").Scan(&n).Error)
	return n
}

// diskBytes 统计主库 + WAL 的实际落盘字节数。
//
// ⛔ 不能只 stat 主文件：WAL 模式下新写入先落在 -wal 里，主文件可能还是 4KB，
// 只看主文件会得出「文件没缩小」或「文件只有 4KB」这种自相矛盾的结论。
func diskBytes(t *testing.T, db *gorm.DB, path string) int64 {
	t.Helper()
	// 先 checkpoint 再量，保证比较的是真实落盘状态。
	require.NoError(t, db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error)
	var total int64
	for _, name := range []string{path, path + "-wal"} {
		info, err := os.Stat(name)
		if err == nil {
			total += info.Size()
			continue
		}
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	return total
}

// 开了 INCREMENTAL 的库：执行器必须真的把空闲页搬走、文件变小。
func TestDatabaseCompactExecutorReclaimsFreePages(t *testing.T) {
	db, path := openCompactTestDB(t, true)
	fillThenDelete(t, db, 400, 8192, 50)

	beforeFree := freePages(t, db)
	require.Greater(t, beforeFree, 100, "样本没产生足够的空闲页，测试无意义")
	beforeBytes := diskBytes(t, db, path)

	executor := &DatabaseCompactExecutor{DB: db, ChunkPages: 50, MinFreePages: 10}
	require.NoError(t, executor.Execute(context.Background(), nil))

	afterFree := freePages(t, db)
	afterBytes := diskBytes(t, db, path)

	require.Less(t, afterFree, beforeFree, "空闲页没有被搬走")
	require.Less(t, afterBytes, beforeBytes, "磁盘占用没有下降 —— 页搬走了却没截断，等于白干")
}

// 老库（auto_vacuum=0，即 1.1.25 及以前装出来的）：只跳过，绝不自作主张跑整库 VACUUM。
func TestDatabaseCompactExecutorSkipsWithoutIncrementalMode(t *testing.T) {
	db, _ := openCompactTestDB(t, false)
	fillThenDelete(t, db, 100, 8192, 10)
	// 先把统计信息建好：ANALYZE 自己会占用/释放少量页，
	// 不先做掉就会和"本用例只关心有没有搬页"的判据混在一起。
	require.NoError(t, db.Exec("ANALYZE").Error)

	var mode int
	require.NoError(t, db.Raw("PRAGMA auto_vacuum").Scan(&mode).Error)
	require.Equal(t, 0, mode)

	before := freePages(t, db)
	executor := &DatabaseCompactExecutor{DB: db, ChunkPages: 50, MinFreePages: 1}
	require.NoError(t, executor.Execute(context.Background(), nil))
	require.Equal(t, before, freePages(t, db), "模式未开启时不应该搬任何页")
}

// 空闲页太少时不动手，避免每晚做无用功。
func TestDatabaseCompactExecutorSkipsBelowThreshold(t *testing.T) {
	db, _ := openCompactTestDB(t, true)
	fillThenDelete(t, db, 60, 4096, 55)
	// 同上：先把统计信息建好，避免 ANALYZE 自身的页变动干扰"没搬页"的判据。
	require.NoError(t, db.Exec("ANALYZE").Error)

	before := freePages(t, db)
	executor := &DatabaseCompactExecutor{DB: db, MinFreePages: 100000}
	require.NoError(t, executor.Execute(context.Background(), nil))
	require.Equal(t, before, freePages(t, db))
}

// 从未 ANALYZE 过的库（线上就是这个状态）：执行器必须把 sqlite_stat1 建出来。
//
// 关心点：这一步跟空闲页多少、auto_vacuum 是不是 2 都无关 —— 它排在最前面，
// 所以哪怕库里一个空闲页都没有，统计信息也必须被建立。少了它，优化器只能
// 按内置默认假设估行数，多索引表上会选错索引。
func TestDatabaseCompactExecutorSeedsPlannerStats(t *testing.T) {
	db, _ := openCompactTestDB(t, true)
	fillThenDelete(t, db, 40, 4096, 35)

	require.False(t, hasPlannerStats(t, db), "样本库不该自带统计信息")

	// MinFreePages 设得极大 ⇒ 一定走"空闲页不足"的跳过分支，
	// 用来证明统计信息维护不依赖回收真的发生。
	executor := &DatabaseCompactExecutor{DB: db, MinFreePages: 1000000}
	require.NoError(t, executor.Execute(context.Background(), nil))
	require.True(t, hasPlannerStats(t, db), "首次运行应生成 sqlite_stat1，否则优化器永远在猜行数")

	// 第二次走 PRAGMA optimize 分支，不能报错。
	require.NoError(t, executor.Execute(context.Background(), nil))
}

func hasPlannerStats(t *testing.T, db *gorm.DB) bool {
	t.Helper()
	var n int
	require.NoError(t, db.Raw(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='sqlite_stat1'").Scan(&n).Error)
	return n > 0
}
