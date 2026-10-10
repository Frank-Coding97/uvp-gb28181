package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/glebarez/sqlite" // 与后端同一个纯 Go SQLite 驱动，保证建库产物与运行期一致
)

func runDBInit(args []string) error {
	fs := newFlagSet("db-init")
	baseline := fs.String("baseline", "", "baseline.sql 路径")
	dbPath := fs.String("db", "", "uvp.db 输出路径")
	quiet := fs.Bool("quiet", false, "只输出阶段行，不画进度")
	splitDigestOnly := fs.Bool("split-digest", false, "只打印切分指纹（stmts=N sha256=…）后退出，不碰数据库")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*baseline) == "" {
		return errors.New("db-init: 缺少 --baseline")
	}
	absBaseline, err := resolvePath(*baseline)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(absBaseline)
	if err != nil {
		return fmt.Errorf("读取 baseline.sql 失败: %w", err)
	}
	stmts := splitSQL(string(raw))
	if len(stmts) == 0 {
		return errors.New("建库失败：从 baseline.sql 里切不出任何语句（文件被截断？）")
	}
	if *splitDigestOnly {
		// 与 ctl.sh 的 UVP_DB_SPLIT_DIGEST=1 同格式，供契约测试对拍。
		fmt.Printf("stmts=%d sha256=%s\n", len(stmts), splitDigest(stmts))
		return nil
	}
	if strings.TrimSpace(*dbPath) == "" {
		return errors.New("db-init: 缺少 --db")
	}
	absDB, err := resolvePath(*dbPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absDB), 0o755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}
	return initDatabase(absDB, stmts, !*quiet)
}

// sqliteDSN 与后端 gormhelper.sqliteDSN 保持同一组 PRAGMA（每一项都有原因，见那里的注释）。
func sqliteDSN(path string) string {
	base := filepath.ToSlash(path)
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + strings.Join([]string{
		"_pragma=busy_timeout(5000)",
		"_pragma=journal_mode(WAL)",
		"_pragma=foreign_keys(1)",
	}, "&") + "&_txlock=immediate"
}

// initDatabase 与 Linux 版 ctl.sh 的 DB_INIT_PY 逐条等价。
func initDatabase(dbPath string, stmts []string, verbose bool) error {
	// 幂等：库已在且 sys_users 表在 ⇒ 直接跳过（与 ctl.sh 同判据）。
	if done, err := databaseReady(dbPath); err == nil && done {
		success("数据库已就绪（%s），跳过初始化", dbPath)
		return nil
	} else if err == nil {
		note("检测到 %s 存在但库不完整（可能是上次建库中断），将重建", dbPath)
	}

	phases, counted, dupes, total := planStatements(stmts)
	counts := map[string]int{}
	for i, ok := range counted {
		if ok {
			counts[phases[i]]++
		}
	}
	labels := map[string]string{
		phaseTable: "建表", phaseIndex: "建索引", phaseSeed: "灌种子数据",
		phasePragma: "设置", phaseOther: "其它",
	}

	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		return fmt.Errorf("建库失败: 打开数据库: %w", err)
	}
	defer func() { _ = db.Close() }()
	// ⛔ 必须钉成 1 条连接：PRAGMA 是**连接级**状态，连接池换一条就等于没设。
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("建库失败: 获取连接: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// ⛔ 建库前显式开外键：SQLite 的 foreign_keys 默认 OFF，不设的话建表里的
	//   REFERENCES 全成摆设，「引用完整性悄悄没了」。
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("建库失败: 开启外键: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return fmt.Errorf("建库失败: 设置 busy_timeout: %w", err)
	}

	// ---- 执行：整库一个事务，PRAGMA 必须搬出事务 ----
	// ⛔⛔ `PRAGMA foreign_keys` 在事务内是**静默空操作**，所以基线开头那两句
	//   PRAGMA 必须先 COMMIT、执行完再重新 BEGIN，否则等于没开（Linux 版同一个坑）。
	seen := map[string]int{}
	reported := map[string]bool{}
	processed := 0
	var execErr error

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("建库失败: 开启事务: %w", err)
	}
	rollback := func() {
		_ = tx.Rollback()
	}
	for i, stmt := range stmts {
		if phases[i] == phasePragma {
			if err := tx.Commit(); err != nil {
				execErr = err
				break
			}
			if _, err := conn.ExecContext(ctx, stmt); err != nil {
				execErr = err
				break
			}
			tx, err = conn.BeginTx(ctx, nil)
			if err != nil {
				execErr = err
				break
			}
		} else if _, err := tx.ExecContext(ctx, stmt); err != nil {
			execErr = err
			break
		}
		processed = i + 1
		if counted[i] {
			seen[phases[i]]++
		}
		// 非交互场景逐条打会把日志刷爆（400+ 行）：只在每个阶段跑完时打一行。
		ph := phases[i]
		if verbose && !reported[ph] && seen[ph] == counts[ph] && counts[ph] > 0 {
			reported[ph] = true
			note("%s %d/%d", labels[ph], seen[ph], counts[ph])
		}
	}
	if execErr != nil {
		rollback()
		_ = conn.Close()
		_ = db.Close()
		if rmErr := os.Remove(dbPath); rmErr != nil && !os.IsNotExist(rmErr) {
			fmt.Fprintf(os.Stderr, indent+"! 清理半成品库失败: %v\n", rmErr)
		}
		head := ""
		if processed < len(stmts) {
			head = strings.ReplaceAll(stmts[processed], "\n", " ")
			if len(head) > 200 {
				head = head[:200]
			}
		}
		return fmt.Errorf("建库失败，已回滚（未留下半成品库）\n%s%T: %v\n%s第 %d 条语句: %s",
			indent, execErr, execErr, indent, processed+1, head)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("建库失败: 提交事务: %w", err)
	}
	if len(dupes) > 0 {
		note("有 %d 条 CREATE 语句不会生效（SQLite 库级唯一，同名对象已被前面的语句建过）：%s",
			len(dupes), strings.Join(dupes, ", "))
	}

	// ---- 建库后自检（与 verify_baseline.py / ctl.sh 同口径）----
	tables, indexes, seeded, admins, fkOn, integrity, dangling, err := selfCheck(ctx, conn)
	if err != nil {
		return fmt.Errorf("建库后自检失败: %w", err)
	}
	if integrity != "ok" {
		return fmt.Errorf("建库后自检失败: integrity_check=%s", integrity)
	}
	if fkOn != 1 {
		return fmt.Errorf("建库后自检失败: 外键未开启（PRAGMA foreign_keys=%d）", fkOn)
	}
	if len(dangling) > 0 {
		limit := dangling
		if len(limit) > 5 {
			limit = limit[:5]
		}
		return fmt.Errorf("建库后自检失败: 外键悬挂 %v", limit)
	}
	if admins != 1 {
		return fmt.Errorf("建库后自检失败: admin 账号数=%d（应为 1）", admins)
	}
	success("%d 张表 · %d 个索引 · %d 行种子数据 · 管理员账号已就位（共执行 %d 条语句）",
		tables, indexes, seeded, total)
	return nil
}

func databaseReady(dbPath string) (bool, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return false, err
	}
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		return false, err
	}
	defer func() { _ = db.Close() }()
	var n int
	err = db.QueryRowContext(context.Background(),
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='sys_users'").Scan(&n)
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func selfCheck(ctx context.Context, conn *sql.Conn) (
	tables, indexes, seeded, admins, fkOn int, integrity string, dangling []string, err error,
) {
	one := func(query string) (int, error) {
		var v int
		e := conn.QueryRowContext(ctx, query).Scan(&v)
		return v, e
	}
	if tables, err = one("SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'"); err != nil {
		return
	}
	if indexes, err = one("SELECT count(*) FROM sqlite_master WHERE type='index' AND name NOT LIKE 'sqlite_%'"); err != nil {
		return
	}
	if admins, err = one("SELECT count(*) FROM sys_users WHERE username='admin'"); err != nil {
		return
	}
	if fkOn, err = one("PRAGMA foreign_keys"); err != nil {
		return
	}
	rows, e := conn.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
	if e != nil {
		err = e
		return
	}
	var names []string
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			_ = rows.Close()
			err = e
			return
		}
		names = append(names, name)
	}
	if e = rows.Close(); e != nil {
		err = e
		return
	}
	for _, name := range names {
		var n int
		if e = conn.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %q", name)).Scan(&n); e != nil {
			err = e
			return
		}
		seeded += n
	}
	var integrityValue string
	if e = conn.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrityValue); e != nil {
		err = e
		return
	}
	integrity = integrityValue
	drows, e := conn.QueryContext(ctx, "PRAGMA foreign_key_check")
	if e != nil {
		err = e
		return
	}
	defer func() { _ = drows.Close() }()
	cols, e := drows.Columns()
	if e != nil {
		err = e
		return
	}
	for drows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if e = drows.Scan(ptrs...); e != nil {
			err = e
			return
		}
		parts := make([]string, len(vals))
		for i, v := range vals {
			parts[i] = fmt.Sprint(v)
		}
		dangling = append(dangling, strings.Join(parts, "|"))
	}
	err = drows.Err()
	return
}
