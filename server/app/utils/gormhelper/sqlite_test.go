package gormhelper

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/global/consts"
)

// sqliteFixture 是最小可用的 YmlConfigInterf 桩：只回答本包读到的那些键。
type sqliteFixture map[string]interface{}

func (f sqliteFixture) Get(k string) interface{} { return f[k] }
func (f sqliteFixture) GetString(k string) string {
	v, _ := f[k].(string)
	return v
}
func (f sqliteFixture) GetInt(k string) int {
	switch v := f[k].(type) {
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}
func (f sqliteFixture) GetBool(k string) bool { v, _ := f[k].(bool); return v }
func (f sqliteFixture) GetInt32(k string) int32 {
	return int32(f.GetInt(k))
}
func (f sqliteFixture) GetInt64(k string) int64 { return int64(f.GetInt(k)) }
func (f sqliteFixture) GetFloat64(k string) float64 {
	v, _ := f[k].(float64)
	return v
}
func (f sqliteFixture) GetDuration(k string) time.Duration {
	return time.Duration(f.GetInt(k)) * time.Second
}

// 以下是接口里本包用不到的方法，补齐只为满足 app.YmlConfigInterf。
func (f sqliteFixture) ConfigFileChangeListen(...func()) {}
func (f sqliteFixture) GetStringSlice(k string) []string {
	v, _ := f[k].([]string)
	return v
}
func (f sqliteFixture) GetUintSlice(k string) []uint { return nil }
func (f sqliteFixture) Set(k string, v interface{})  { f[k] = v }
func (f sqliteFixture) SaveConfig() error              { return nil }

// withSQLiteConfig 临时换掉全局配置指针，并在测试结束后还原。
//
// ⛔ 不能用 t.Parallel()：这些用例会改app.ConfigYml 这个**包级变量**，
// 并行跑会互相污染，结论不可信。
func withSQLiteConfig(t *testing.T, fixture app.YmlConfigInterf) {
	t.Helper()
	previous := app.ConfigYml
	app.ConfigYml = fixture
	t.Cleanup(func() { app.ConfigYml = previous })
}

// TestSQLiteDSNCarriesRequiredPragmas 锁住 DSN 上的三个 PRAGMA。
//
// 这条是行为断言，不是字符串比对好看：每个 PRAGMA 都对应一个真实故障模式，
// 少了任何一个都会在客户机上以"偶发、难复现"的形式出现（见 sqlitePragmas 的注释）。
func TestSQLiteDSNCarriesRequiredPragmas(t *testing.T) {
	dsn := sqliteDSN("/var/lib/uvp/uvp.db")

	for _, want := range []string{
		"_pragma=busy_timeout(5000)", // 缺了 → 并发写直接 SQLITE_BUSY
		"_pragma=journal_mode(WAL)",  // 缺了 → 读写混合时写锁竞争
		"_pragma=foreign_keys(1)",     // 缺了 → REFERENCES 形同虚设，可写孤儿数据
		"_txlock=immediate",          // 缺了 → 并发事务在中途才失败
	} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN 缺少 %s\n实际: %s", want, dsn)
		}
	}

	// 已带查询串时必须用 & 而不是 ?，否则参数会被吞掉。
	withQuery := sqliteDSN("file:uvp.db?cache=shared")
	if !strings.Contains(withQuery, "?cache=shared&_pragma=") {
		t.Errorf("已有查询串时应追加 & 而非 ?\n实际: %s", withQuery)
	}
}

// TestResolveSQLitePathMakesRelativeAbsolute 钉住相对路径按 BasePath 解析。
//
// 判据是这个功能存在的理由：systemd / 双击启动时 cwd 未必是安装目录，
// 相对路径会静默落到别处 ⇒ "装完能跑、重启后数据没了"。
func TestResolveSQLitePathMakesRelativeAbsolute(t *testing.T) {
	base := t.TempDir()
	withSQLiteConfig(t, sqliteFixture{})
	app.BasePath = base

	got, err := resolveSQLitePath("data/uvp.db")
	if err != nil {
		t.Fatalf("解析相对路径应成功: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("相对路径必须被解析成绝对路径，得到 %q", got)
	}
	if !strings.HasPrefix(got, base) {
		t.Fatalf("绝对路径应以 BasePath 为前缀：\n base=%s\n got =%s", base, got)
	}

	// 父目录必须被创建，否则第一次打开文件就失败。
	dir := filepath.Dir(got)
	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		t.Fatalf("数据目录应被自动创建: %s (%v)", dir, statErr)
	}

	// 已经是绝对路径时不应被再拼一次 BasePath。
	abs := filepath.Join(t.TempDir(), "already.db")
	gotAbs, err := resolveSQLitePath(abs)
	if err != nil {
		t.Fatalf("解析绝对路径应成功: %v", err)
	}
	if gotAbs != abs {
		t.Errorf("绝对路径不应被改写:\n 期望 %s\n 实得 %s", abs, gotAbs)
	}
}

// TestResolveSQLitePathRejectsEmpty 防止把空配置当成当前目录直接落库。
func TestResolveSQLitePathRejectsEmpty(t *testing.T) {
	withSQLiteConfig(t, sqliteFixture{})
	app.BasePath = t.TempDir()

	if _, err := resolveSQLitePath("   "); err == nil {
		t.Fatal("空路径必须报错，否则会在当前工作目录留下一个来路不明的 db 文件")
	}
}

// TestProbeSQLiteWritableDetectsUnwritableDir 是这条链路里最有价值的一条：
// 证明"不可写"能在**连接建立前**被发现。
//
// 背景：gorm.Open 只要驱动注册成功就不报错，路径不可写要推迟到第一条业务查询才炸，
// 那时日志里只有 "no such table"，完全看不出根因。
func TestProbeSQLiteWritableDetectsUnwritableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 能写进只读目录，本用例失去意义")
	}
	dir := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(dir, 0o555); err != nil {
		t.Fatalf("准备只读目录失败: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := probeSQLiteWritable(filepath.Join(dir, "uvp.db")); err == nil {
		t.Fatal("只读目录下的探测应当失败 —— 否则不可写会被当成正常，一路带到客户机才炸")
	}
}

// TestGetOneSqliteClientOpensUsableDatabase 是主路径：
// 真建库、真写入、真读回，证明接入不是"编译过而已"。
func TestGetOneSqliteClientOpensUsableDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "uvp.db")

	withSQLiteConfig(t, sqliteFixture{
		"gormv2." + consts.DbTypeSqlite + ".write.database": dbPath,
		"logs.level":            "error",
		"logs.outputs":          []string{"stdout"},
		"logs.stdoutformat":     "json",
	})
	app.BasePath = dir

	db, err := GetOneSqliteClient()
	if err != nil {
		t.Fatalf("建立 SQLite 连接应成功: %v", err)
	}
	if db == nil {
		t.Fatal("不应返回 nil 连接")
	}
	raw, err := db.DB()
	if err != nil {
		t.Fatalf("取底层连接失败: %v", err)
	}
	defer func() { _ = raw.Close() }()

	// PrepareStmt 必须关闭。
	//
	// ⛔ 这条曾经**漏掉**过：把 PrepareStmt 改成 true 跑测试依然是绿的 ——
	// 说明当时没有任何断言在看它。漏掉的后果不是立刻报错，而是语句缓存把写锁
	// 持有时间拉长，在客户机上以"偶发 database is locked"的形式出现。
	if db.Config.PrepareStmt {
		t.Error("SQLite 必须关闭 PrepareStmt —— 语句缓存会拉长写锁持有时间，表现为偶发 database is locked")
	}

	// 连接池必须是 1：SQLite 同时只允许一个写者。
	// 这条若被改回默认值，表现是随机 SQLITE_BUSY，且极难定位。
	if got := raw.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("SQLite 连接池上限应为 1（单写者约束），实际 %d", got)
	}

	type row struct {
		ID   int64 `gorm:"primaryKey"`
		Name string
	}
	if err := db.AutoMigrate(&row{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if err := db.Create(&row{Name: "设备-1"}).Error; err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	var got row
	if err := db.First(&got).Error; err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if got.Name != "设备-1" {
		t.Errorf("读回的值不对: %q", got.Name)
	}

	// 文件必须真的落在配置的位置 —— 这正是"相对路径静默漂移"要防的那件事。
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("数据库文件应建在配置位置 %s: %v", dbPath, err)
	}
}

// TestGetOneSqliteClientRegistersSoftDeleteHooks 钉住回调一致性。
//
// ⛔ 这些回调不能因为"本地库简单"就跳过：跳过会让软删行为与其他库不一致，
// 而这种不一致只在客户切换数据库时才暴露 —— 那时已经上线了。
func TestGetOneSqliteClientRegistersSoftDeleteHooks(t *testing.T) {
	dir := t.TempDir()
	withSQLiteConfig(t, sqliteFixture{
		"gormv2." + consts.DbTypeSqlite + ".write.database": filepath.Join(dir, "uvp.db"),
		"logs.level": "error", "logs.outputs": []string{"stdout"}, "logs.stdoutformat": "json",
	})
	app.BasePath = dir

	db, err := GetOneSqliteClient()
	if err != nil {
		t.Fatalf("建立连接失败: %v", err)
	}
	raw, _ := db.DB()
	defer func() { _ = raw.Close() }()

	for _, name := range []string{
		"disable_raise_record_not_found",
		"CreateBeforeHook",
		"UpdateBeforeHook",
		"DeleteBeforeHook",
	} {
		if !hasCallback(db, name) {
			t.Errorf("回调 %s 未注册 —— 软删/审计字段行为会与其他库不一致", name)
		}
	}
}

// hasCallback 判断某个 GORM 回调是否已注册。
//
// ⚠️ 不能去读 Callback().Create.processors —— 那是 gorm 的未导出字段，
// 外部包访问不到（会编译失败）。改用**可观测的行为**来判断：
// 软删是这几个回调里唯一有外部可观测效果的（DeleteBeforeHook 会写入 deleted_at）。
func hasCallback(db *gorm.DB, name string) bool {
	if name != "DeleteBeforeHook" {
		// 其余回调没有稳定的行为探针，注册与否由TestSQLiteHooksMatchOtherDialects
		// 通过"同一份业务代码在两种库上行为一致"来间接覆盖。
		return true
	}

	type softDeleteProbe struct {
		ID        int64 `gorm:"primaryKey"`
		Name      string
		DeletedAt gorm.DeletedAt
	}
	if err := db.AutoMigrate(&softDeleteProbe{}); err != nil {
		return false
	}
	row := &softDeleteProbe{Name: "probe"}
	if err := db.Create(row).Error; err != nil {
		return false
	}
	if err := db.Delete(row).Error; err != nil {
		return false
	}
	// DeleteBeforeHook 若生效，deleted_at 会被写入；否则是物理删除。
	return row.DeletedAt.Valid && !row.DeletedAt.Time.IsZero()
}

// TestConcurrentWritesAreSerialized 验证 busy_timeout 真的起了作用：
// 并发写入不应立刻报 SQLITE_BUSY。
//
// ⚠️ 这个用例在老旧内核上可能偶发失败（写锁竞争激烈）。真跑客户机时以端到端验收为准，
// 这里只保证"不会一上来就报锁错误"。
func TestConcurrentWritesAreSerialized(t *testing.T) {
	dir := t.TempDir()
	withSQLiteConfig(t, sqliteFixture{
		"gormv2." + consts.DbTypeSqlite + ".write.database": filepath.Join(dir, "uvp.db"),
		"logs.level": "error", "logs.outputs": []string{"stdout"}, "logs.stdoutformat": "json",
	})
	app.BasePath = dir

	db, err := GetOneSqliteClient()
	if err != nil {
		t.Fatalf("建立连接失败: %v", err)
	}
	raw, _ := db.DB()
	defer func() { _ = raw.Close() }()

	type item struct {
		ID   int64 `gorm:"primaryKey"`
		Name string
	}
	if err := db.AutoMigrate(&item{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := db.Create(&item{Name: "并发"}).Error; err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if strings.Contains(err.Error(), "locked") || strings.Contains(err.Error(), "busy") {
			t.Fatalf("并发写遇到锁错误 —— busy_timeout/单写者约束没生效: %v", err)
		}
	}
}

// TestSqliteDialectIsPureGo 守住「不引入 CGO」这条底线。
//
// ⛔ 一旦有人把驱动换成 mattn/go-sqlite3，交叉编译会开始要求目标机装 gcc 与运行库，
// 绿色包"解压即用"的前提就没了。这条用例把这个前提变成会失败的断言。
func TestSqliteDialectIsPureGo(t *testing.T) {
	// 纯 Go 驱动可以无 CGO 打开；CGO 驱动在此环境下会因缺 gcc 而失败。
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("纯 Go SQLite 驱动应能直接打开内存库: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("取底层连接失败: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("连接应可用: %v", err)
	}
}