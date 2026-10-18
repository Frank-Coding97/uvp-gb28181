package migration

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
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
	hasTable                  func(tableName string) (bool, error)
	hasColumn                 func(tableName, columnName string) (bool, error)
	columnLength              func(tableName, columnName string) (int64, error)
	hasPermissionRegistration func(path, method, permission string) (bool, error)
}

type baselineColumnRequirement struct {
	table     string
	column    string
	minLength int64
}

var baselineColumnMigrations = map[string]baselineColumnRequirement{
	"2026-10-13-cascade-auto-push-catalog.sql":                {table: "gb_cascade_platform", column: "auto_push_catalog"},
	"2026-10-13-cascade-auto-push-catalog-postgresql.sql":     {table: "gb_cascade_platform", column: "auto_push_catalog"},
	"2026-10-13-cascade-auto-push-catalog-sqlserver.sql":      {table: "gb_cascade_platform", column: "auto_push_catalog"},
	"2026-10-13-cascade-auto-push-catalog-sqlite.sql":         {table: "gb_cascade_platform", column: "auto_push_catalog"},
	"2026-10-13-cascade-recording-enabled.sql":                {table: "gb_cascade_platform", column: "recording_enabled"},
	"2026-10-13-cascade-recording-enabled-postgresql.sql":     {table: "gb_cascade_platform", column: "recording_enabled"},
	"2026-10-13-cascade-recording-enabled-sqlserver.sql":      {table: "gb_cascade_platform", column: "recording_enabled"},
	"2026-10-13-cascade-recording-enabled-sqlite.sql":         {table: "gb_cascade_platform", column: "recording_enabled"},
	"2026-10-13-cascade-rtcp-keepalive.sql":                   {table: "gb_cascade_platform", column: "rtcp_keepalive"},
	"2026-10-13-cascade-rtcp-keepalive-postgresql.sql":        {table: "gb_cascade_platform", column: "rtcp_keepalive"},
	"2026-10-13-cascade-rtcp-keepalive-sqlserver.sql":         {table: "gb_cascade_platform", column: "rtcp_keepalive"},
	"2026-10-13-cascade-rtcp-keepalive-sqlite.sql":            {table: "gb_cascade_platform", column: "rtcp_keepalive"},
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
	"2026-10-09-sip-platform-sdp-ip.sql":                      {table: "gb_sip_config", column: "sdp_ip"},
	"2026-10-09-sip-platform-sdp-ip-postgresql.sql":           {table: "gb_sip_config", column: "sdp_ip"},
	"2026-10-09-sip-platform-sdp-ip-sqlserver.sql":            {table: "gb_sip_config", column: "sdp_ip"},
	"2026-10-09-sip-platform-sdp-ip-sqlite.sql":               {table: "gb_sip_config", column: "sdp_ip"},
	"2026-10-09-meta-node-sdp-ip.sql":                         {table: "meta_node", column: "sdp_ip"},
	"2026-10-09-meta-node-sdp-ip-postgresql.sql":              {table: "meta_node", column: "sdp_ip"},
	"2026-10-09-meta-node-sdp-ip-sqlserver.sql":               {table: "meta_node", column: "sdp_ip"},
	"2026-10-09-meta-node-sdp-ip-sqlite.sql":                  {table: "meta_node", column: "sdp_ip"},
	"2026-10-09-job-result-summary.sql":                       {table: "sys_job_results", column: "summary"},
	"2026-10-09-job-result-summary-postgresql.sql":            {table: "sys_job_results", column: "summary"},
	"2026-10-09-job-result-summary-sqlserver.sql":             {table: "sys_job_results", column: "summary"},
	"2026-10-09-job-result-summary-sqlite.sql":                {table: "sys_job_results", column: "summary"},
	// rtp_receive_mode 与 rtp_proxy_port 成对出现，只需登记其中一列即可。
	"2026-10-09-meta-node-receive-mode.sql":                {table: "meta_node", column: "rtp_receive_mode"},
	"2026-10-09-meta-node-receive-mode-postgresql.sql":     {table: "meta_node", column: "rtp_receive_mode"},
	"2026-10-09-meta-node-receive-mode-sqlserver.sql":      {table: "meta_node", column: "rtp_receive_mode"},
	"2026-10-09-meta-node-receive-mode-sqlite.sql":         {table: "meta_node", column: "rtp_receive_mode"},
	"2026-10-12-catalog-node-source-device.sql":            {table: "gb_catalog_node", column: "source_device_id"},
	"2026-10-12-catalog-node-source-device-postgresql.sql": {table: "gb_catalog_node", column: "source_device_id"},
	"2026-10-12-catalog-node-source-device-sqlserver.sql":  {table: "gb_catalog_node", column: "source_device_id"},
	"2026-10-12-catalog-node-source-device-sqlite.sql":     {table: "gb_catalog_node", column: "source_device_id"},
	"2026-10-13-channel-parent-ids.sql":                    {table: "gb_channel", column: "parent_id", minLength: 512},
	"2026-10-13-channel-parent-ids-postgresql.sql":         {table: "gb_channel", column: "parent_id", minLength: 512},
	"2026-10-13-channel-parent-ids-sqlserver.sql":          {table: "gb_channel", column: "parent_id", minLength: 512},
	"2026-10-13-channel-parent-ids-sqlite.sql":             {table: "gb_channel", column: "parent_id", minLength: 512},
}

// baselineTableMigrations lists additive table migrations that must still run
// when an older full baseline has an empty migration ledger.
type baselineTableRequirement struct {
	tables []string
}

var baselineTableMigrations = map[string]baselineTableRequirement{
	"2026-10-11-openapi-device-capture-session.sql":               {tables: []string{"sys_openapi_device_capture_session"}},
	"2026-10-11-openapi-device-capture-session-postgresql.sql":    {tables: []string{"sys_openapi_device_capture_session"}},
	"2026-10-11-openapi-device-capture-session-sqlserver.sql":     {tables: []string{"sys_openapi_device_capture_session"}},
	"2026-10-11-openapi-device-capture-session-sqlite.sql":        {tables: []string{"sys_openapi_device_capture_session"}},
	"2026-10-11-openapi-device-event-subscription.sql":            {tables: []string{"sys_openapi_device_subscription"}},
	"2026-10-11-openapi-device-event-subscription-postgresql.sql": {tables: []string{"sys_openapi_device_subscription"}},
	"2026-10-11-openapi-device-event-subscription-sqlserver.sql":  {tables: []string{"sys_openapi_device_subscription"}},
	"2026-10-11-openapi-device-event-subscription-sqlite.sql":     {tables: []string{"sys_openapi_device_subscription"}},
	"2026-10-13-cascade-business-group.sql":                       {tables: []string{"gb_cascade_business_node", "gb_cascade_business_binding"}},
	"2026-10-13-cascade-business-group-postgresql.sql":            {tables: []string{"gb_cascade_business_node", "gb_cascade_business_binding"}},
	"2026-10-13-cascade-business-group-sqlserver.sql":             {tables: []string{"gb_cascade_business_node", "gb_cascade_business_binding"}},
	"2026-10-13-cascade-business-group-sqlite.sql":                {tables: []string{"gb_cascade_business_node", "gb_cascade_business_binding"}},
}

type baselinePermissionRequirement struct {
	path       string
	method     string
	permission string
}

var baselinePermissionMigrations = map[string]baselinePermissionRequirement{
	"2026-10-13-cascade-publish-group-permission.sql":                   {path: "/api/gb28181/cascade/platforms/:id/publish-group", method: "PUT", permission: "gb28181:cascade:manage"},
	"2026-10-13-cascade-publish-group-permission-postgresql.sql":        {path: "/api/gb28181/cascade/platforms/:id/publish-group", method: "PUT", permission: "gb28181:cascade:manage"},
	"2026-10-13-cascade-publish-group-permission-sqlserver.sql":         {path: "/api/gb28181/cascade/platforms/:id/publish-group", method: "PUT", permission: "gb28181:cascade:manage"},
	"2026-10-13-cascade-publish-group-permission-sqlite.sql":            {path: "/api/gb28181/cascade/platforms/:id/publish-group", method: "PUT", permission: "gb28181:cascade:manage"},
	"2026-10-16-service-upgrade-permission.sql":                         {path: "/api/system/upgrade/status", method: "GET", permission: "system:upgrade:read"},
	"2026-10-16-service-upgrade-permission-postgresql.sql":              {path: "/api/system/upgrade/status", method: "GET", permission: "system:upgrade:read"},
	"2026-10-16-service-upgrade-permission-sqlserver.sql":               {path: "/api/system/upgrade/status", method: "GET", permission: "system:upgrade:read"},
	"2026-10-16-service-upgrade-permission-sqlite.sql":                  {path: "/api/system/upgrade/status", method: "GET", permission: "system:upgrade:read"},
	"2026-10-16-service-upgrade-migration-plan.sql":                     {path: "/api/system/upgrade/migration-plan", method: "GET", permission: "system:upgrade:read"},
	"2026-10-16-service-upgrade-migration-plan-postgresql.sql":          {path: "/api/system/upgrade/migration-plan", method: "GET", permission: "system:upgrade:read"},
	"2026-10-16-service-upgrade-migration-plan-sqlserver.sql":           {path: "/api/system/upgrade/migration-plan", method: "GET", permission: "system:upgrade:read"},
	"2026-10-16-service-upgrade-migration-plan-sqlite.sql":              {path: "/api/system/upgrade/migration-plan", method: "GET", permission: "system:upgrade:read"},
	"2026-10-18-service-upgrade-async-import-permission.sql":            {path: "/api/system/upgrade/import/:upload_id", method: "GET", permission: "system:upgrade:read"},
	"2026-10-18-service-upgrade-async-import-permission-postgresql.sql": {path: "/api/system/upgrade/import/:upload_id", method: "GET", permission: "system:upgrade:read"},
	"2026-10-18-service-upgrade-async-import-permission-sqlserver.sql":  {path: "/api/system/upgrade/import/:upload_id", method: "GET", permission: "system:upgrade:read"},
	"2026-10-18-service-upgrade-async-import-permission-sqlite.sql":     {path: "/api/system/upgrade/import/:upload_id", method: "GET", permission: "system:upgrade:read"},
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
	if !exists {
		return true, nil
	}
	if requirement.minLength == 0 {
		return false, nil
	}
	if probe.columnLength == nil {
		return false, fmt.Errorf("列迁移缺少列长度探测器")
	}
	length, err := probe.columnLength(requirement.table, requirement.column)
	if err != nil {
		return false, fmt.Errorf("探测 %s.%s 长度失败: %w", requirement.table, requirement.column, err)
	}
	return length < requirement.minLength, nil
}

func needsBaselineTableUpgrade(name string, probe schemaProbe) (bool, error) {
	requirement, ok := baselineTableMigrations[name]
	if !ok {
		return false, nil
	}
	if probe.hasTable == nil {
		return false, fmt.Errorf("表迁移缺少表探测器")
	}
	for _, table := range requirement.tables {
		exists, err := probe.hasTable(table)
		if err != nil {
			return false, fmt.Errorf("探测 %s 表失败: %w", table, err)
		}
		if !exists {
			return true, nil
		}
	}
	return false, nil
}

func needsBaselineUpgrade(name string, probe schemaProbe) (bool, error) {
	// 菜单补全必须在空版本表的快照库上执行，不能因 API 已注册而跳过角色授权。
	switch name {
	case "2026-10-18-https-management.sql", "2026-10-18-https-management-postgresql.sql",
		"2026-10-18-https-management-sqlserver.sql", "2026-10-18-https-management-sqlite.sql":
		return true, nil
	case "2026-10-18-service-upgrade-menu.sql", "2026-10-18-service-upgrade-menu-postgresql.sql",
		"2026-10-18-service-upgrade-menu-sqlserver.sql", "2026-10-18-service-upgrade-menu-sqlite.sql":
		return true, nil
	}
	if _, ok := baselineColumnMigrations[name]; ok {
		return needsBaselineColumnUpgrade(name, probe)
	}
	if requirement, ok := baselinePermissionMigrations[name]; ok {
		if probe.hasPermissionRegistration == nil {
			return false, fmt.Errorf("权限迁移缺少注册项探测器")
		}
		registered, err := probe.hasPermissionRegistration(requirement.path, requirement.method, requirement.permission)
		if err != nil {
			return false, fmt.Errorf("探测 API 权限注册失败: %w", err)
		}
		return !registered, nil
	}
	return needsBaselineTableUpgrade(name, probe)
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
		execute := func(execDB *gorm.DB) error {
			for _, stmt := range splitStatements(sqlText) {
				if err := execDB.Exec(stmt).Error; err != nil {
					return err
				}
			}
			return nil
		}
		if conn.Dialector.Name() == string(DialectSQLite) {
			return conn.Transaction(execute)
		}
		return execute(conn)
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
		columnLength: func(tableName, columnName string) (int64, error) {
			if d == DialectSQLite && tableName == "gb_channel" && columnName == "parent_id" {
				return sqliteColumnLimit(db, tableName)
			}
			columns, err := db.Migrator().ColumnTypes(tableName)
			if err != nil {
				return 0, err
			}
			for _, column := range columns {
				if strings.EqualFold(column.Name(), columnName) {
					length, ok := column.Length()
					if !ok {
						return 0, fmt.Errorf("数据库未返回列长度")
					}
					return length, nil
				}
			}
			return 0, fmt.Errorf("列不存在")
		},
		hasPermissionRegistration: func(path, method, permission string) (bool, error) {
			var count int64
			err := db.Raw(`SELECT COUNT(*) FROM sys_api a
				JOIN sys_menu_api ma ON ma.api_id = a.id
				JOIN sys_menu m ON m.id = ma.menu_id
				WHERE a.path = ? AND a.method = ? AND m.permission = ?
				AND EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype = 'p' AND c.v0 = 'role_1' AND c.v1 = a.path AND c.v2 = a.method)`,
				path, method, permission).Scan(&count).Error
			return count > 0, err
		},
	}
	return run(NewStore(db), newDBLocker(db, d), &embedSource{dialect: d}, &dbExecutor{db: db}, probe)
}

var sqliteParentIDLengthLimit = regexp.MustCompile(`length\s*\(\s*parent_id\s*\)\s*<=\s*(\d+)`)

func sqliteColumnLimit(db *gorm.DB, tableName string) (int64, error) {
	var definition string
	if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", tableName).Scan(&definition).Error; err != nil {
		return 0, err
	}
	if definition == "" {
		return 0, fmt.Errorf("表定义不存在")
	}
	normalized := strings.ToLower(strings.NewReplacer(`"`, "", "`", "", "[", "", "]", "").Replace(definition))
	match := sqliteParentIDLengthLimit.FindStringSubmatch(normalized)
	if len(match) != 2 {
		// SQLite TEXT has no declared width; without this CHECK it accepts full values.
		return 512, nil
	}
	return strconv.ParseInt(match[1], 10, 64)
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

		// New full baselines already contain the additive objects, but older
		// snapshots have an empty ledger too. Probe explicitly mapped column and
		// table migrations before baseline-marking so an upgrade cannot silently
		// skip them.
		pending := make(map[string]bool)
		for _, name := range names {
			needed, err := needsBaselineUpgrade(name, probe)
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
		_, isColumnMigration := baselineColumnMigrations[name]
		_, isTableMigration := baselineTableMigrations[name]
		if isColumnMigration || isTableMigration {
			// An interrupted additive migration may have created its schema object
			// before the version row was written. Record it as applied instead of
			// replaying a dialect-specific migration statement.
			needed, err := needsBaselineUpgrade(name, probe)
			if err != nil {
				return err
			}
			if !needed {
				if err := store.MarkApplied([]string{name}); err != nil {
					return fmt.Errorf("标记迁移 %s 失败: %w", name, err)
				}
				continue
			}
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
