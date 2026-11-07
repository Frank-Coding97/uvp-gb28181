package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/controllers"
	"uvplatform.com/uvp-gb28181/app/gb28181"
	"uvplatform.com/uvp-gb28181/app/gb28181/migration"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/global/consts"
	"uvplatform.com/uvp-gb28181/app/middleware"
	"uvplatform.com/uvp-gb28181/app/openapi/processauthority"
	openapiroutes "uvplatform.com/uvp-gb28181/app/openapi/routes"
	"uvplatform.com/uvp-gb28181/app/routes"
	"uvplatform.com/uvp-gb28181/app/scheduler"
	"uvplatform.com/uvp-gb28181/app/utils/ginhelper"
	"uvplatform.com/uvp-gb28181/app/utils/logging"
	bootstrap "uvplatform.com/uvp-gb28181/bootstrap"
	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"

	_ "uvplatform.com/uvp-gb28181/docs/swagger" // swagger docs
	_ "uvplatform.com/uvp-gb28181/plugins"

	// ⛔⛔ 时区数据库必须**内置进二进制**，不能指望目标机自带：
	// Go 在 Windows 上的 platformZoneSources 是空的，LoadLocation 只认
	// ZONEINFO 环境变量 / 编译期嵌入的 tzdata / 编译机 GOROOT 下的
	// lib/time/zoneinfo.zip（这个路径被写死成打包机的路径）。所以交叉编译或
	// 换一台没装 Go 的 Windows 机器部署时，time.LoadLocation("Asia/Shanghai")
	// 直接返回 "unknown time zone"，config.RecordQuery.Location 变成 nil，
	// 最终表现为「SIP 服务启动失败: 装配设备录像查询 service 失败:
	// invalid_argument: recordquery: invalid argument」——从报错完全看不出是时区。
	// 代价约 450KB 二进制体积，换掉一整类"打包机能跑、现场跑不起来"的问题。
	_ "time/tzdata"
)

// @title UVP-GB28181 API
// @version 1.0
// @description UVP 国标 GB28181 上级平台 API 文档
// @termsOfService https://github.com/Frank-Coding97/uvp-gb28181

// @contact.name UVP Support
// @contact.url https://github.com/Frank-Coding97/uvp-gb28181/issues

// @license.name MIT
// @license.url https://github.com/Frank-Coding97/uvp-gb28181/blob/main/LICENSE

// @host localhost:8080
// @BasePath /api
func main() {
	finishApplication(runApplication())
}

func finishApplication(err error) {
	if upgradePreflightRequested(os.Args[1:]) || upgradeReadinessRequested(os.Args[1:]) {
		if err != nil {
			logging.ReportStartupFailure(os.Stderr, nil, "dependency", err)
			os.Exit(1)
		}
		return
	}
	if app.LogRuntime != nil {
		app.LogRuntime.Repeats().Close()
	}
	root := app.Log(context.Background()).Named("lifecycle")
	if err != nil {
		root.Warn("Application shutdown incomplete", zap.String("event", "lifecycle.shutdown_incomplete"), logging.Error(err))
	} else {
		root.Info("Application work stopped", zap.String("event", "lifecycle.stopped"))
	}
	if app.LogRuntime != nil {
		err = errors.Join(err, app.LogRuntime.Close())
	}
	if err != nil {
		os.Exit(1)
	}
}

func stopApplication(ctx context.Context) error {
	return ginhelper.Shutdown(ctx, app.Log(ctx),
		ginhelper.ShutdownStep{Component: "config_callbacks", Stop: func(ctx context.Context) error {
			if config, ok := app.ConfigYml.(interface{ StopContext(context.Context) error }); ok {
				return config.StopContext(ctx)
			}
			return nil
		}},
		ginhelper.ShutdownStep{Component: "scheduler", Stop: func(ctx context.Context) error {
			if app.JobScheduler == nil {
				return nil
			}
			return app.JobScheduler.StopContext(ctx)
		}},
		ginhelper.ShutdownStep{Component: "job_results", Stop: scheduler.StopResultHandlerContext},
		ginhelper.ShutdownStep{Component: "sip_requests", Stop: gb28181.QuiesceRequests},
		ginhelper.ShutdownStep{Component: "http_background", Stop: app.BackgroundWork.StopContext},
		ginhelper.ShutdownStep{Component: "gb28181", Stop: gb28181.StopContext},
		ginhelper.ShutdownStep{Component: "casbin", Stop: func(ctx context.Context) error {
			if policy, ok := app.CasbinV2.(interface{ CloseContext(context.Context) error }); ok {
				return policy.CloseContext(ctx)
			}
			return nil
		}},
	)
}

func runApplication() (err error) {
	if upgradePreflightRequested(os.Args[1:]) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return upgrade.WritePreflightSnapshot(ctx, os.Stdout, controllers.CollectUpgradePreflight)
	}
	for _, arg := range os.Args[1:] {
		if arg == "-upgrade-readiness" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return upgrade.WriteRuntimeReadiness(ctx, app.ConfigYml.GetString("httpserver.port"), os.Stdout)
		}
	}
	// 迁移入口同样等待配置回调退出，然后由 main 关闭根日志。
	migrateUp := migrateUpRequested(os.Args[1:])
	downFile := parseArgs(os.Args[1:])
	publishCatalog := publishCatalogRequested(os.Args[1:])
	if migrateUp || downFile != "" || publishCatalog {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err = errors.Join(err, stopApplication(ctx))
		}()
		if migrateUp {
			return runMigrateUp()
		}
		if publishCatalog {
			return runPublishCatalog()
		}
		return runMigrateDown(downFile)
	}
	// Own the local domain for the whole API process, not one SIP generation.
	// Migrations are complete; no HTTP routes or device effect runtime exists.
	stateDir, err := processauthority.PrepareStateDir(app.ConfigYml.GetString("processauthority.state_dir"))
	if err != nil {
		logProcessAuthorityStartupFailure("state_prepare_failed", err)
		return fmt.Errorf("进程授权目录准备失败: %w", err)
	}
	authorityLock, err := processauthority.AcquireLocalLock(stateDir)
	if err != nil {
		logProcessAuthorityStartupFailure(processAuthorityFailureReason(err), err)
		return fmt.Errorf("进程授权目录或排他锁不可用: %w", err)
	}
	registerCtx, cancelRegister := context.WithTimeout(context.Background(), 10*time.Second)
	authority, err := processauthority.Register(registerCtx, app.DB(), authorityLock)
	cancelRegister()
	if err != nil {
		reason := "database_registration_failed"
		if errors.Is(err, processauthority.ErrProcessAuthorityDomainMismatch) {
			reason = processAuthorityFailureReason(err)
		}
		logProcessAuthorityStartupFailure(reason, err)
		return errors.Join(fmt.Errorf("进程授权注册失败: %w", err), authorityLock.Close())
	}
	candidateMode, err := upgrade.CandidateModeFromEnv()
	if err != nil {
		return fmt.Errorf("读取升级候选门禁失败: %w", err)
	}
	// 获取Gin引擎实例
	engine := ginhelper.GetEngine()
	// Candidate validation must cover every ingress registered below, including
	// hook, OpenAPI, and plugin routes that do not sit in the admin group.
	// Normal mode passes through without changing the existing route behavior.
	engine.Use(middleware.UpgradeCandidateMiddleware())
	// 初始化系统路由
	openAPI := routes.InitRoutes(engine)
	// 初始化插件路由
	ginhelper.InitPluginRoutes(engine)
	maintenanceContext, cancelMaintenance := context.WithCancel(context.Background())
	defer cancelMaintenance()
	maintenanceDone := make(chan struct{})
	var maintenanceDoneOnce sync.Once
	finishMaintenance := func() { maintenanceDoneOnce.Do(func() { close(maintenanceDone) }) }
	initializeBusinessJobs := func() error {
		if !candidateMode {
			return nil
		}
		// bootstrap deliberately skipped the DB-backed system-job upsert and
		// load while the release was a candidate. The updater has now admitted
		// the release, so complete that write/read boundary before starting the
		// scheduler or device runtime.
		return bootstrap.InitializeBusinessJobs()
	}
	startBusinessRuntime := upgrade.NewBusinessRuntimeStarter(maintenanceContext, initializeBusinessJobs,
		app.JobScheduler.Start,
		func(ctx context.Context) {
			defer finishMaintenance()
			openAPI.RunMaintenance(ctx, func(err error) {
				app.ZapLog.Error("OpenAPI maintenance unavailable", zap.String("event", "startup.openapi_maintenance_failed"), zap.Error(err))
			})
		},
		// 启动 GB28181 SIP 服务(双栈 UDP+TCP,在 HTTP 阻塞前旁挂)
		func() { gb28181.Start(authority) },
	)
	if candidateMode {
		go func() {
			if upgrade.WaitForNormalGate(maintenanceContext, 250*time.Millisecond, startBusinessRuntime) {
				return
			}
			finishMaintenance()
		}()
	} else {
		startBusinessRuntime()
	}
	// 启动服务器(阻塞直到收到退出信号)
	var stopOnce sync.Once
	stopOpenAPI := func(ctx context.Context) error {
		cancelMaintenance()
		select {
		case <-maintenanceDone:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	shutdown := func(ctx context.Context) error {
		var openAPIErr error
		stopOnce.Do(func() { openAPIErr = stopOpenAPI(ctx) })
		shutdownErr := errors.Join(openAPIErr, stopApplication(ctx))
		if shutdownErr != nil {
			return shutdownErr
		}
		// All HTTP, maintenance and GB owners have joined. Do not release this
		// process authority in GB Stop/Reload, or before the last owner exits.
		authority.Seal()
		return authorityLock.Close()
	}
	return ginhelper.StartServer(engine, shutdown)
}

func upgradePreflightRequested(args []string) bool {
	for _, arg := range args {
		if arg == "-upgrade-preflight" {
			return true
		}
	}
	return false
}

func upgradeReadinessRequested(args []string) bool {
	for _, arg := range args {
		if arg == "-upgrade-readiness" {
			return true
		}
	}
	return false
}

func processAuthorityFailureReason(err error) string {
	if errors.Is(err, processauthority.ErrLocalAuthorityBusy) {
		return "already_running"
	}
	if errors.Is(err, processauthority.ErrProcessAuthorityDomainMismatch) {
		return "database_domain_mismatch"
	}
	return "state_unavailable"
}

func logProcessAuthorityStartupFailure(reason string, err error) {
	app.Log(context.Background()).Named("startup").Error("Process authority startup failed",
		zap.String("event", "startup.failed"),
		zap.String("phase", "process_authority"),
		zap.String("reason", reason),
		logging.Error(err))
}

func migrateUpRequested(args []string) bool {
	for _, arg := range args {
		if arg == "-migrate-up" {
			return true
		}
	}
	return false
}

func publishCatalogRequested(args []string) bool {
	for _, arg := range args {
		if arg == "-publish-catalog" {
			return true
		}
	}
	return false
}

type databaseIdentity struct {
	DatabaseName    string `gorm:"column:database_name"`
	DatabaseVersion string `gorm:"column:database_version"`
}

// runMigrateUp 使用项目迁移器升级主数据库,并输出不含凭据的目标与版本证据。
func runMigrateUp() error {
	db, dialect, err := primaryDB()
	if err != nil {
		return err
	}
	var identity databaseIdentity
	if err := db.Raw(databaseIdentitySQL(dialect)).Scan(&identity).Error; err != nil {
		return fmt.Errorf("读取数据库身份失败: %w", err)
	}
	app.Log(context.Background()).Named("migration").Info("migrate-up 目标",
		zap.String("event", "migration.target"),
		zap.String("database", identity.DatabaseName),
		zap.String("database_version", identity.DatabaseVersion),
		zap.String("dialect", string(dialect)))
	before, err := appliedMigrationVersions(db)
	if err != nil {
		return fmt.Errorf("读取迁移基线失败: %w", err)
	}
	if err := migration.Up(db, dialect); err != nil {
		return err
	}
	after, err := appliedMigrationVersions(db)
	if err != nil {
		return fmt.Errorf("读取迁移结果失败: %w", err)
	}
	// Joined rather than zap.Strings: the sanitize core refuses third-party array
	// marshalers, so a []string would sink as [omitted:zap.stringArray] and the
	// applied version list would be unreadable at exactly the moment it matters.
	// Contract: docs/logging-governance/contracts/sanitize-policy.md
	newlyApplied := zap.Skip()
	if added := migrationDifference(before, after); len(added) > 0 {
		newlyApplied = zap.String("newly_applied", strings.Join(added, ","))
	}
	app.Log(context.Background()).Named("migration").Info("migrate-up 完成",
		zap.String("event", "migration.completed"),
		zap.Int("before_count", len(before)), zap.Int("after_count", len(after)),
		newlyApplied)
	return nil
}

// runPublishCatalog 是「显式发布 OpenAPI 能力目录」的一次性入口:播种代码自带的
// 目录行,然后把当前可编辑目录按现状发布成一版 release。它给运维一个不用重启服务
// 就能修复 catalog 的落点 —— 既包括「表已迁移但从未发布过 release」,也包括「已发布
// 的 release 里少了本进程能提供的 scope」。它会打印结果,因为「补了几版、active 是
// 哪一版、还有哪些核心 scope 缺失」是这次调用的全部意义。
//
// 与启动链路的区别:启动只做加法(宁可少发一版也不砍掉已发布的 scope),这里按现
// 状发布,所以运维可以借此下掉一个 scope。两者都在「发布面没变」时不产生新版本。
func runPublishCatalog() error {
	db, _, err := primaryDB()
	if err != nil {
		return err
	}
	// 预算给到 10 分钟:这是一次性运维动作,不在请求路径上,而它的写量是
	// 「播种目录行 + 一版 release + 逐条 release_item」的量级。按 60s 卡会在
	// 高延迟链路上必然失败 —— 实测 192.168.10.52 的达梦(VPN 单趟 RTT ~90ms)
	// 每条语句均值 273ms,一百多条写就把 60s 吃穿,报出来的是
	// context deadline exceeded,看上去像数据库有问题,其实是预算不够。
	// 本地或同网段部署通常几秒内完成,放宽不会掩盖真实故障:真卡死时
	// 10 分钟同样会超时,只是不再误报。
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	outcome, err := openapiroutes.PublishCatalog(ctx, db)
	if err != nil {
		app.Log(context.Background()).Named("openapi").Error("能力目录发布失败",
			zap.String("event", "startup.failed"),
			zap.String("phase", "openapi_catalog_publish"),
			logging.Error(err))
		return fmt.Errorf("能力目录发布失败: %w", err)
	}
	// 复用 migration.completed:这是一次「数据维护步骤完成」,与 -migrate-up 同类,
	// 而事件名是冻结基线里的注册项。消息文本把两者区分开。
	app.Log(context.Background()).Named("openapi").Info("能力目录发布完成",
		zap.String("event", "migration.completed"),
		zap.String("action", outcome.Action),
		zap.Int64("effective_version", outcome.Version),
		zap.Int("item_count", outcome.ItemCount),
		zap.Int("count", outcome.Seeded.OperationsCreated))
	if len(outcome.CoreScopesMissing) > 0 {
		return fmt.Errorf("能力目录仍缺少核心 scope: %v", outcome.CoreScopesMissing)
	}
	return nil
}

func databaseIdentitySQL(dialect migration.Dialect) string {
	switch dialect {
	case migration.DialectPostgres:
		return "SELECT current_database() AS database_name, version() AS database_version"
	case migration.DialectDM:
		// 达梦没有 DATABASE() / VERSION()。CUR_DATABASE() 给库名，
		// SVR_VERSION 给「DM Database Server x64 V8」。V$INSTANCE 在 DSC
		// 集群下有多行，ROWNUM=1 只取本节点。
		// ⛔ 别名必须加双引号：不加引号时达梦把别名折叠成大写 DATABASE_NAME，
		//   而 GORM 按 `column:database_name` 匹配，大小写对不上就静默扫成空串
		//   —— 日志里表现为 database="" database_version=""，看着像连了空库。
		return `SELECT CUR_DATABASE() AS "database_name", SVR_VERSION AS "database_version" FROM V$INSTANCE WHERE ROWNUM = 1`
	case migration.DialectSQLServer:
		return "SELECT DB_NAME() AS database_name, CAST(SERVERPROPERTY('ProductVersion') AS varchar(128)) AS database_version"
	case migration.DialectSQLite:
		// ⛔ SQLite 既没有 DATABASE() 也没有 VERSION()（后者是 SQLite 3.47+ 才有的
		//   PRAGMA 扩展，不保证存在）。这列是给人看的证据，所以给固定说明值 ——
		//   比选个可能不存在的 PRAGMA 更稳。
		//   ⚠️ 别退回default 分支（MySQL 的 SELECT DATABASE()）：SQLite 会报
		//   no such function: DATABASE，而它在 -migrate-up 的第一个查询就执行。
		return "SELECT 'uvp.db' AS database_name, 'sqlite' AS database_version"
	default:
		return "SELECT DATABASE() AS database_name, VERSION() AS database_version"
	}
}

func appliedMigrationVersions(db *gorm.DB) ([]string, error) {
	if !db.Migrator().HasTable("gb_schema_migrations") {
		return nil, nil
	}
	var versions []string
	err := db.Table("gb_schema_migrations").Order("version").Pluck("version", &versions).Error
	return versions, err
}

func migrationDifference(before, after []string) []string {
	existing := make(map[string]struct{}, len(before))
	for _, version := range before {
		existing[version] = struct{}{}
	}
	added := make([]string, 0)
	for _, version := range after {
		if _, ok := existing[version]; !ok {
			added = append(added, version)
		}
	}
	return added
}

// parseArgs 解析命令行参数,返回 -migrate-down 指定的迁移文件名(空串=正常启动)。
func parseArgs(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-migrate-down=") {
			return strings.TrimPrefix(arg, "-migrate-down=")
		}
	}
	return ""
}

// runMigrateDown 对主数据库手动回滚单个迁移。
func runMigrateDown(downFile string) error {
	db, d, err := primaryDB()
	if err != nil {
		return err
	}
	return migration.Down(db, d, downFile)
}

// primaryDB 返回主数据库连接与方言(优先级 MySQL > PostgreSQL > SQL Server > SQLite,
// 多库同时启用时 down 仅作用于主库)。
func primaryDB() (*gorm.DB, migration.Dialect, error) {
	// 先按 gormv2.usedbtype 选：bootstrap 允许同时初始化多个连接（每个库各有
	// 自己的 isinitglobalgorm* 开关），所以「谁非 nil 就取第一个」在多连接并存时
	// 会选错库 —— 同时配了 MySQL 与国产库时，-migrate-up 会一直作用在 MySQL 上，
	// 国产库那边的表始终建不出来。
	type handle struct {
		db      *gorm.DB
		dialect migration.Dialect
	}
	byType := map[string]handle{
		consts.DbTypeMySql:      {app.GormDbMysql, migration.DialectMySQL},
		consts.DbTypePostgreSql: {app.GormDbPostgreSql, migration.DialectPostgres},
		// 金仓说 PG 协议、PolarDB 说 MySQL 协议，方言跟着对方走，
		// 迁移文件也复用对方那一套。
		consts.DbTypeKingbase:  {app.GormDbKingbase, migration.DialectPostgres},
		consts.DbTypePolarDB:   {app.GormDbPolarDB, migration.DialectMySQL},
		consts.DbTypeDM:        {app.GormDbDM, migration.DialectDM},
		consts.DbTypeSqlServer: {app.GormDbSqlserver, migration.DialectSQLServer},
		consts.DbTypeSqlite:    {app.GormDbSqlite, migration.DialectSQLite},
	}
	if app.ConfigYml != nil {
		configured := consts.NormalizeDbType(app.ConfigYml.GetString("gormv2.usedbtype"))
		if h, ok := byType[configured]; ok && h.db != nil {
			return h.db, h.dialect, nil
		}
	}
	// 配置没写或连接的库没初始化时，退回既有优先级顺序，保持旧行为不变。
	for _, name := range []string{
		consts.DbTypeMySql, consts.DbTypePostgreSql, consts.DbTypeSqlServer,
		consts.DbTypeSqlite, consts.DbTypeKingbase, consts.DbTypePolarDB, consts.DbTypeDM,
	} {
		if h := byType[name]; h.db != nil {
			return h.db, h.dialect, nil
		}
	}
	return nil, migration.DialectUnknown, errors.New("未初始化任何数据库连接")
}
