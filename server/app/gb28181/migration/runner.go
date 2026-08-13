package migration

import (
	"fmt"

	"gorm.io/gorm"

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
	return e.db.Exec(sqlText).Error
}

// Up 执行未应用的迁移:建版本表 → 取锁 → 基线化或增量执行 → 放锁。
func Up(db *gorm.DB, d Dialect) error {
	return run(NewStore(db), newDBLocker(db, d), &embedSource{dialect: d}, &dbExecutor{db: db})
}

// run 是 Up 的纯依赖版本,便于 fake 注入测试。
func run(store versionStore, lock locker, src migrationSource, exec migrationExecutor) error {
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

	// 首启基线化:版本表为空 → 存量迁移全部标记为已应用,不执行 SQL。
	// 存量库 schema 与全量快照 uvp-gb28181.sql 一致,增量无需重放;
	// 全新库先跑快照初始化再启动,同样走此分支。
	if len(applied) == 0 && len(names) > 0 {
		return store.MarkApplied(names)
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
