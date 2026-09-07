package migration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

// versionStore runner 依赖的版本表操作,*Store 与测试 fake 均实现。
type versionStore interface {
	EnsureTable() error
	ListApplied() ([]string, error)
	MarkApplied([]string) error
	DeleteApplied(string) error
	IsEmpty() (bool, error)
}

// locker 跨实例迁移互斥锁,由 dbLocker 或测试 fake 提供。
type locker interface {
	Acquire() error
	Release() error
}

// migrationSource 提供当前方言的 up 迁移文件及其内容。
type migrationSource interface {
	UpFiles() ([]string, error)
	ReadSQL(name string) (string, error)
}

// baselineProbeTable 基线探测表:全量快照与最新迁移都包含该表。
// 空版本表时用它的存在性区分"快照库(基线化)"与"老基线库(拒绝)"
const baselineProbeTable = "gb_sip_trace_session_diagnosis"

// schemaProbe 探测表是否存在
type schemaProbe func(tableName string) (bool, error)

// migrationExecutor 执行单条迁移 SQL。
type migrationExecutor interface {
	ExecSQL(sqlText string) error
}

// embedSource 从 embed.FS 读迁移文件。
type embedSource struct {
	dialect Dialect
}

func (s *embedSource) UpFiles() ([]string, error) {
	entries, err := migrationsfs.FS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return FilterUpFiles(names, s.dialect), nil
}

func (s *embedSource) ReadSQL(name string) (string, error) {
	b, err := migrationsfs.FS.ReadFile("migrations/" + name)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// dbExecutor 用 *gorm.DB 执行迁移 SQL。
type dbExecutor struct {
	db *gorm.DB
}

func (e *dbExecutor) ExecSQL(sqlText string) error {
	// 迁移 SQL 可能包含 MySQL PREPARE/EXECUTE 这样的服务器端控制语句。
	// 即使按语句拆分,全局 PrepareStmt 仍会把 PREPARE 当成预处理协议发送并报 1295,
	// 所以迁移必须在关闭 GORM PrepareStmt 且解包底层连接池的 session 上逐条执行。
	db := e.db.Session(&gorm.Session{NewDB: true, PrepareStmt: false})
	if prepared, ok := db.Statement.ConnPool.(*gorm.PreparedStmtDB); ok {
		db.Statement.ConnPool = prepared.ConnPool
		db.Config.ConnPool = prepared.ConnPool
	}
	for _, stmt := range splitStatements(sqlText) {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// splitStatements 按分号拆分 SQL 文本为独立语句:
// 跳过空行与整行 -- 注释;语句以行尾分号结束;无分号结尾的残余片段按一条语句处理。
func splitStatements(sqlText string) []string {
	var stmts []string
	var buf strings.Builder
	flush := func() {
		text := strings.TrimSpace(buf.String())
		if text != "" {
			stmts = append(stmts, text)
		}
		buf.Reset()
	}
	for _, line := range strings.Split(sqlText, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		buf.WriteString(line)
		buf.WriteString("\n")
		if strings.HasSuffix(trimmed, ";") {
			flush()
		}
	}
	flush()
	return stmts
}

// Up 执行未应用的迁移:建版本表 → 取锁 → 基线化或增量执行 → 放锁。
func Up(db *gorm.DB, d Dialect) error {
	if db == nil || d == DialectUnknown || DialectOf(db.Dialector) != d {
		return fmt.Errorf("invalid or mismatched migration dialect %q", d)
	}
	if d == DialectSQLite {
		ctx, cancel := context.WithTimeout(db.Statement.Context, 2*time.Minute)
		defer cancel()
		return sqlitebootstrap.Migrate(ctx, db)
	}

	probe := func(tableName string) (bool, error) {
		return db.Migrator().HasTable(tableName), nil
	}
	return run(NewStore(db), newDBLocker(db, d), &embedSource{dialect: d}, &dbExecutor{db: db}, probe)
}

// run 是 Up 的纯依赖版本,便于 fake 注入测试。
func run(store versionStore, lock locker, src migrationSource, exec migrationExecutor, probe schemaProbe) error {
	if err := store.EnsureTable(); err != nil {
		return fmt.Errorf("建版本表失败: %w", err)
	}
	if err := lock.Acquire(); err != nil {
		return fmt.Errorf("获取迁移锁失败: %w", err)
	}
	defer lock.Release()

	applied, err := store.ListApplied()
	if err != nil {
		return fmt.Errorf("读版本表失败: %w", err)
	}
	appliedSet := make(map[string]bool, len(applied))
	for _, v := range applied {
		appliedSet[v] = true
	}

	names, err := src.UpFiles()
	if err != nil {
		return fmt.Errorf("列迁移文件失败: %w", err)
	}

	if len(applied) == 0 && len(names) > 0 {
		// 空版本表:用基线探测表区分两种场景 ——
		//   1. 快照库/已最新:探测表存在 → 基线化(全部标记已应用)
		//   2. 老基线库:探测表缺失 → 无法确定哪些迁移已应用,
		//      明确拒绝而不是"迁移成功但缺表"的假成功
		exists, err := probe(baselineProbeTable)
		if err != nil {
			return fmt.Errorf("基线探测失败: %w", err)
		}
		if exists {
			return store.MarkApplied(names)
		}
		return fmt.Errorf("检测到空迁移版本表且缺少 %s 表:无法确定存量库的迁移基线,请人工建立基线后重试", baselineProbeTable)
	}

	for _, name := range names {
		if appliedSet[name] {
			continue
		}
		sqlText, err := src.ReadSQL(name)
		if err != nil {
			return fmt.Errorf("读迁移文件 %s 失败: %w", name, err)
		}
		if err := exec.ExecSQL(sqlText); err != nil {
			return fmt.Errorf("迁移 %s 执行失败: %w\nSQL: %s", name, err, sqlText)
		}
		if err := store.MarkApplied([]string{name}); err != nil {
			return fmt.Errorf("标记迁移 %s 失败: %w", name, err)
		}
	}
	return nil
}
