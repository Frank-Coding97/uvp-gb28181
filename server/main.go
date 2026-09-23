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

	"uvplatform.cn/uvp-gb28181/app/gb28181"
	"uvplatform.cn/uvp-gb28181/app/gb28181/migration"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	openapimedia "uvplatform.cn/uvp-gb28181/app/openapi/media"
	"uvplatform.cn/uvp-gb28181/app/openapi/processauthority"
	"uvplatform.cn/uvp-gb28181/app/routes"
	"uvplatform.cn/uvp-gb28181/app/scheduler"
	"uvplatform.cn/uvp-gb28181/app/utils/ginhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
	_ "uvplatform.cn/uvp-gb28181/bootstrap"

	_ "uvplatform.cn/uvp-gb28181/docs/swagger" // swagger docs
	_ "uvplatform.cn/uvp-gb28181/plugins"
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
	// 迁移入口同样等待配置回调退出，然后由 main 关闭根日志。
	migrateUp := migrateUpRequested(os.Args[1:])
	downFile := parseArgs(os.Args[1:])
	if migrateUp || downFile != "" {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err = errors.Join(err, stopApplication(ctx))
		}()
		if migrateUp {
			return runMigrateUp()
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
	app.JobScheduler.Start()
	// 获取Gin引擎实例
	engine := ginhelper.GetEngine()
	// 初始化系统路由
	openAPI := routes.InitRoutes(engine)
	// 初始化插件路由
	ginhelper.InitPluginRoutes(engine)
	// Prime process-lifetime trust before SIP recovery starts; both consumers
	// reuse this exact snapshot, including a sticky startup failure.
	controlBindings, controlBindingsErr := gb28181.LoadStartupOpenAPIControlBindingsOnce()
	// 启动 GB28181 SIP 服务(双栈 UDP+TCP,在 HTTP 阻塞前旁挂)
	gb28181.Start(authority)
	maintenanceContext, cancelMaintenance := context.WithCancel(context.Background())
	defer cancelMaintenance()
	var stopRevocation func()
	revocationErr := controlBindingsErr
	if revocationErr == nil {
		stopRevocation, revocationErr = openapimedia.StartRevocationWithBindings(maintenanceContext, app.DB(), gb28181.ZLMRegistry(),
			controlBindings, func(result openapimedia.RevocationTickResult, err error) {
				if err != nil {
					app.ZapLog.Error("OpenAPI revocation maintenance unavailable", zap.String("event", "startup.openapi_revocation_maintenance_failed"), zap.Error(err))
				} else if result.Alarms > 0 {
					app.ZapLog.Error("OpenAPI revocation remains pending past deadline", zap.String("event", "startup.openapi_revocation_pending_overdue"), zap.Int("alarms", result.Alarms), zap.Int("pending", result.Pending))
				}
			})
	}
	if revocationErr != nil {
		// Keep metadata/admin available; missing trust never means legacy control.
		if errors.Is(revocationErr, openapimedia.ErrRevocationNotConfigured) {
			app.ZapLog.Warn("OpenAPI revocation control is not configured; pending cleanup is not running", zap.String("event", "startup.openapi_revocation_unconfigured"))
		} else {
			app.ZapLog.Error("OpenAPI revocation startup unavailable; pending cleanup is not running", zap.String("event", "startup.openapi_revocation_startup_failed"), zap.Error(revocationErr))
		}
	}
	maintenanceDone := make(chan struct{})
	go func() {
		defer close(maintenanceDone)
		openAPI.RunMaintenance(maintenanceContext, func(err error) {
			app.ZapLog.Error("OpenAPI maintenance unavailable", zap.String("event", "startup.openapi_maintenance_failed"), zap.Error(err))
		})
	}()
	// 启动服务器(阻塞直到收到退出信号)
	var stopOnce sync.Once
	stopOpenAPI := func(ctx context.Context) error {
		cancelMaintenance()
		if stopRevocation != nil {
			stopRevocation()
		}
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

func databaseIdentitySQL(dialect migration.Dialect) string {
	switch dialect {
	case migration.DialectPostgres:
		return "SELECT current_database() AS database_name, version() AS database_version"
	case migration.DialectSQLServer:
		return "SELECT DB_NAME() AS database_name, CAST(SERVERPROPERTY('ProductVersion') AS varchar(128)) AS database_version"
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

// primaryDB 返回主数据库连接与方言(优先级 MySQL > PostgreSQL > SQL Server,
// 多库同时启用时 down 仅作用于主库)。
func primaryDB() (*gorm.DB, migration.Dialect, error) {
	if app.GormDbMysql != nil {
		return app.GormDbMysql, migration.DialectMySQL, nil
	}
	if app.GormDbPostgreSql != nil {
		return app.GormDbPostgreSql, migration.DialectPostgres, nil
	}
	if app.GormDbSqlserver != nil {
		return app.GormDbSqlserver, migration.DialectSQLServer, nil
	}
	return nil, migration.DialectUnknown, errors.New("未初始化任何数据库连接")
}
