package migration

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"gorm.io/gorm"

	migrationsfs "uvplatform.com/uvp-gb28181/resource/database/gb28181"
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

// baselineRequiredTables are the minimum schema fingerprint for a release
// baseline that may safely skip all embedded incremental migrations. The
// OpenAPI projection tables are included because older baselines contained
// the legacy probe table but not the capability catalog; probing only the
// legacy table would silently mark the catalog migration as applied.
var baselineRequiredTables = []string{
	baselineProbeTable,
	"sys_openapi_capability_group",
	"sys_openapi_capability",
	"sys_openapi_operation",
	"sys_openapi_release",
	"sys_openapi_release_item",
	"sys_openapi_runtime_state",
}

// schemaProbe probes the release baseline fingerprint and the few upgrade
// columns that must still run when an older snapshot has an empty ledger.
type schemaProbe struct {
	hasTable  func(tableName string) (bool, error)
	hasColumn func(tableName, columnName string) (bool, error)
}

type baselineColumnRequirement struct {
	table  string
	column string
}

var baselineColumnMigrations = map[string]baselineColumnRequirement{
	"2026-10-08-initial-admin-password-change.sql":            {table: "sys_users", column: "must_change_password"},
	"2026-10-08-initial-admin-password-change-postgresql.sql": {table: "sys_users", column: "must_change_password"},
	"2026-10-08-initial-admin-password-change-sqlserver.sql":  {table: "sys_users", column: "must_change_password"},
	"2026-10-08-initial-admin-password-change-sqlite.sql":     {table: "sys_users", column: "must_change_password"},
	"2026-10-08-sip-platform-hook-ip.sql":                     {table: "gb_sip_config", column: "hook_ip"},
	"2026-10-08-sip-platform-hook-ip-postgresql.sql":          {table: "gb_sip_config", column: "hook_ip"},
	"2026-10-08-sip-platform-hook-ip-sqlserver.sql":           {table: "gb_sip_config", column: "hook_ip"},
	"2026-10-08-sip-platform-hook-ip-sqlite.sql":              {table: "gb_sip_config", column: "hook_ip"},
	"2026-10-08-sip-platform-stream-ip.sql":                   {table: "gb_sip_config", column: "stream_ip"},
	"2026-10-08-sip-platform-stream-ip-postgresql.sql":        {table: "gb_sip_config", column: "stream_ip"},
	"2026-10-08-sip-platform-stream-ip-sqlserver.sql":         {table: "gb_sip_config", column: "stream_ip"},
	"2026-10-08-sip-platform-stream-ip-sqlite.sql":            {table: "gb_sip_config", column: "stream_ip"},
	"2026-10-08-meta-node-hook-ip.sql":                        {table: "meta_node", column: "hook_ip"},
	"2026-10-08-meta-node-hook-ip-postgresql.sql":             {table: "meta_node", column: "hook_ip"},
	"2026-10-08-meta-node-hook-ip-sqlserver.sql":              {table: "meta_node", column: "hook_ip"},
	"2026-10-08-meta-node-hook-ip-sqlite.sql":                 {table: "meta_node", column: "hook_ip"},
}

// needsBaselineColumnUpgrade applies only to additive column migrations whose
// presence can be checked safely. All other embedded migrations keep the
// normal empty-ledger baseline behavior.
func needsBaselineColumnUpgrade(name string, probe schemaProbe) (bool, error) {
	requirement, ok := baselineColumnMigrations[name]
	if !ok {
		return false, nil
	}
	if probe.hasColumn == nil {
		return false, fmt.Errorf("列迁移缺少列探测器")
	}
	exists, err := probe.hasTable(requirement.table)
	if err != nil {
		return false, fmt.Errorf("探测 %s 表失败: %w", requirement.table, err)
	}
	if !exists {
		return false, fmt.Errorf("检测到旧基线缺少 %s 表，无法应用列迁移", requirement.table)
	}
	exists, err = probe.hasColumn(requirement.table, requirement.column)
	if err != nil {
		return false, fmt.Errorf("探测 %s.%s 失败: %w", requirement.table, requirement.column, err)
	}
	return !exists, nil
}

// migrationExecutor 执行单条迁移 SQL。
type migrationExecutor interface {
	ExecSQL(sqlText string) error
}

// embedSource 从 embed.FS 读迁移文件。
type embedSource struct {
	dialect Dialect
}

// UpFiles 返回当前方言待执行的增量迁移文件名。
//
// ⭐ 历史迁移已归档清理(2026-09-23 起 resource/database/gb28181 下不再有
// migrations 目录,embed 里自然也没有),新环境一律由三方言全量初始化脚本建库。
// 因此「目录不存在」是**预期状态**而不是故障:必须当成「没有增量迁移」返回,
// 否则启动链路会在 phase=migration 直接退出 —— 现象是后端起不来,而报错被
// 收敛成 *fmt.wrapError 不带原文,只能看到 `class=unknown type=*fmt.wrapError`。
// 其它读目录错误(权限等)仍然向上返回。
func (s *embedSource) UpFiles() ([]string, error) {
	entries, err := migrationsfs.FS.ReadDir("migrations")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
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
		db.ConnPool = prepared.ConnPool
	}
	// Session variables and PREPARE handles belong to a physical connection.
	// Pin the entire file; separate pool calls may silently switch sessions.
	return db.Connection(func(conn *gorm.DB) error {
		for _, stmt := range splitStatements(sqlText) {
			if err := conn.Exec(stmt).Error; err != nil {
				return err
			}
		}
		return nil
	})
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
	probe := schemaProbe{
		hasTable: func(tableName string) (bool, error) {
			return db.Migrator().HasTable(tableName), nil
		},
		hasColumn: func(tableName, columnName string) (bool, error) {
			return db.Migrator().HasColumn(tableName, columnName), nil
		},
	}
	return run(NewStore(db), newDBLocker(db, d), &embedSource{dialect: d}, &dbExecutor{db: db}, probe)
}

// run 是 Up 的纯依赖版本,便于 fake 注入测试。
func run(store versionStore, lock locker, src migrationSource, exec migrationExecutor, probe schemaProbe) (runErr error) {
	if err := store.EnsureTable(); err != nil {
		return fmt.Errorf("建版本表失败: %w", err)
	}
	if err := lock.Acquire(); err != nil {
		return fmt.Errorf("获取迁移锁失败: %w", err)
	}
	defer func() {
		if releaseErr := lock.Release(); releaseErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("释放迁移锁失败: %w", releaseErr))
		}
	}()

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
		for _, table := range baselineRequiredTables {
			exists, err := probe.hasTable(table)
			if err != nil {
				return fmt.Errorf("基线探测失败(%s): %w", table, err)
			}
			if !exists {
				return fmt.Errorf("检测到空迁移版本表且缺少 %s 表:无法确定存量库的迁移基线,请人工建立基线后重试", table)
			}
		}

		// New full baselines already contain all additive columns, but older
		// snapshots have an empty ledger too. Probe only migrations with an
		// explicit column requirement before baseline-marking so an upgrade cannot
		// silently skip those columns.
		pending := make(map[string]bool)
		for _, name := range names {
			needed, err := needsBaselineColumnUpgrade(name, probe)
			if err != nil {
				return err
			}
			pending[name] = needed
		}
		baselineApplied := make([]string, 0, len(names))
		for _, name := range names {
			if !pending[name] {
				baselineApplied = append(baselineApplied, name)
			}
		}
		if len(baselineApplied) > 0 {
			if err := store.MarkApplied(baselineApplied); err != nil {
				return fmt.Errorf("标记基线迁移失败: %w", err)
			}
		}
		for _, name := range names {
			if !pending[name] {
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
