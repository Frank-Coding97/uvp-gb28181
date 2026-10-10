package gormhelper

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/global/consts"
)

// sqlitePragmas 是拼进 DSN 的 PRAGMA。
//
// 每一项都有原因，不是默认值抄来的：
//   - busy_timeout(5000)：并发写时默认**不等待**、直接 SQLITE_BUSY。
//     录像索引、点播会话这些写入天然并发，必须给等待窗口。
//   - journal_mode(WAL)：默认 rollback journal 在读写混合下写锁竞争严重，
//     WAL 允许读写并行，是单机场景的正确选择。
//     ⛔ WAL 会产生 -wal / -shm 两个副文件，停机前要正常关闭（由启停脚本负责），
//     直接 kill -9 会留下需要下次启动回放的日志。
//   - foreign_keys(1)：SQLite 默认**关外键**。开着才能让建表里的 REFERENCES 真正生效，
//     否则跨表引用可以写成孤儿数据而没有任何报错。
//   - _txlock=immediate：立刻拿写锁，而不是延迟到事务首次写入才拿 ——
//     否则并发事务会在中途才失败，重试逻辑没法推理。
var sqlitePragmas = []string{
	"_pragma=busy_timeout(5000)",
	"_pragma=journal_mode(WAL)",
	"_pragma=foreign_keys(1)",
}

func sqliteDSN(dataSource string) string {
	sep := "?"
	if strings.Contains(dataSource, "?") {
		sep = "&"
	}
	return dataSource + sep + strings.Join(sqlitePragmas, "&") + "&_txlock=immediate"
}

// resolveSQLitePath 把配置里的路径解析成绝对路径，并确保父目录存在。
//
// ⛔ 相对路径是这里最容易出事的地方：systemd / 双击启动时 cwd 未必是安装目录，
// 一个相对的 "./data/uvp.db" 会静默落到别处，表现为「装完能跑、重启后数据没了」。
// 所以统一按 app.BasePath 解析。
func resolveSQLitePath(configured string) (string, error) {
	path := strings.TrimSpace(configured)
	if path == "" {
		return "", errors.New("SQLite 数据库路径未配置（gormv2.sqlite.write.database）")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(app.BasePath, path)
	}
	path = filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("创建 SQLite 数据目录失败: %w", err)
	}
	return path, nil
}

// GetOneSqliteClient 返回 SQLite 连接。
//
// 与 GetSqlDriver 的差别全部由 SQLite 特性决定，不是风格选择：
//  1. **PrepareStmt 关闭**：SQLite 不支持预编译语句复用。开着既慢，
//     又会因语句缓存把写锁持有时间拉长，表现成随机的 "database is locked"。
//  2. **不接读写分离**：dbresolver 的 Replica 需要第二个独立连接源，
//     SQLite 是本地单文件、没有副本可言。
//  3. **连接池上限 1**：SQLite 同一时刻只允许一个写者。给它开多连接池，
//     表现是随机 SQLITE_BUSY，且极难定位。
//  4. **不设 ConnMaxLifetime**：本地文件句柄应常驻，定期丢弃会让 WAL 反复 checkpoint。
func GetOneSqliteClient() (*gorm.DB, error) {
	const sqliteDialect = consts.DbTypeSqlite

	configured := app.ConfigYml.GetString("gormv2." + sqliteDialect + ".write.database")
	path, err := resolveSQLitePath(configured)
	if err != nil {
		app.Log(context.Background()).Named("db").Error("SQLite 路径解析失败",
			zap.String("event", "db.dialector.init_failed"),
			zap.String("dialect", sqliteDialect),
			zap.String("configured", configured), zap.Error(err))
		return nil, err
	}

	// 可写性必须在建连接**之前**验证：gorm.Open 只要驱动注册成功就不会报错，
	// 「目录只读 / 磁盘满 / 路径不存在」会推迟到第一条业务查询才炸，
	// 那时日志里只有 "no such table"，完全看不出根因是写不了文件。
	if err := probeSQLiteWritable(path); err != nil {
		app.Log(context.Background()).Named("db").Error("SQLite 路径不可写",
			zap.String("event", "db.dialector.init_failed"),
			zap.String("dialect", sqliteDialect),
			zap.String("path", path), zap.Error(err))
		return nil, err
	}

	gormDb, err := gorm.Open(sqlite.Open(sqliteDSN(path)), &gorm.Config{
		PrepareStmt:            false,
		SkipDefaultTransaction: true,
		Logger:                 redefineLog(sqliteDialect),
	})
	if err != nil {
		return nil, err
	}

	rawDb, err := gormDb.DB()
	if err != nil {
		return nil, err
	}
	rawDb.SetMaxOpenConns(1)
	rawDb.SetMaxIdleConns(1)

	if err := installLogContext(gormDb); err != nil {
		return nil, err
	}

	// 与其它库保持一致的 GORM 回调。⛔ 不要因为"本地库简单"就跳过：
	// 跳过会导致软删字段行为与其他库不一致，而这种不一致只在客户切库时才暴露。
	_ = gormDb.Callback().Query().Before("gorm:query").Register("disable_raise_record_not_found", MaskNotDataError)
	_ = gormDb.Callback().Create().Before("gorm:before_create").Register("CreateBeforeHook", CreateBeforeHook)
	_ = gormDb.Callback().Update().Before("gorm:before_update").Register("UpdateBeforeHook", UpdateBeforeHook)
	_ = gormDb.Callback().Delete().Before("gorm:before_delete").Register("DeleteBeforeHook", DeleteBeforeHook)

	return gormDb, nil
}

// probeSQLiteWritable 直接对目标文件做一次真实的追加写。
//
// 为什么不用 os.Stat / os.OpenFile 探针：那只能证明文件存在，不证明 SQLite 能写。
// 真正会咬人的是「文件存在但所在目录只读」（数据文件改不了、WAL 建不了），
// 只有对同一路径实际动一次手才测得出来。
func probeSQLiteWritable(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("SQLite 数据库文件不可写: %w", err)
	}
	return f.Close()
}