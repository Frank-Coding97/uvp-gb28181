package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181"
	"uvplatform.cn/uvp-gb28181/app/gb28181/migration"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	openapimedia "uvplatform.cn/uvp-gb28181/app/openapi/media"
	"uvplatform.cn/uvp-gb28181/app/routes"
	"uvplatform.cn/uvp-gb28181/app/utils/ginhelper"
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
	// 运维入口:-migrate-up 仅执行主数据库待处理迁移并退出,不启动 Casbin、任务调度、HTTP 或 SIP。
	if migrateUpRequested(os.Args[1:]) {
		if err := runMigrateUp(); err != nil {
			log.Fatal("migrate-up 失败: " + err.Error())
		}
		return
	}
	// 运维入口:-migrate-down=<文件名> 手动回滚单个迁移后退出
	if downFile := parseArgs(os.Args[1:]); downFile != "" {
		if err := runMigrateDown(downFile); err != nil {
			log.Fatal("migrate-down 失败: " + err.Error())
		}
		return
	}
	// 获取Gin引擎实例
	engine := ginhelper.GetEngine()
	// 初始化系统路由
	openAPI := routes.InitRoutes(engine)
	// 初始化插件路由
	ginhelper.InitPluginRoutes(engine)
	// 启动 GB28181 SIP 服务(双栈 UDP+TCP,在 HTTP 阻塞前旁挂)
	gb28181.Start()
	maintenanceContext, cancelMaintenance := context.WithCancel(context.Background())
	defer cancelMaintenance()
	stopRevocation, revocationErr := openapimedia.StartConfiguredRevocation(maintenanceContext, app.DB(), gb28181.ZLMRegistry(),
		app.ConfigYml.GetString("openapi.revocation_bindings_file"), func(result openapimedia.RevocationTickResult, err error) {
			if err != nil {
				app.ZapLog.Error("OpenAPI revocation maintenance unavailable", zap.Error(err))
			} else if result.Alarms > 0 {
				app.ZapLog.Error("OpenAPI revocation remains pending past deadline", zap.Int("alarms", result.Alarms), zap.Int("pending", result.Pending))
			}
		})
	if revocationErr != nil {
		// Keep metadata/admin available; missing trust never means legacy control.
		if errors.Is(revocationErr, openapimedia.ErrRevocationNotConfigured) {
			app.ZapLog.Warn("OpenAPI revocation control is not configured; pending cleanup is not running")
		} else {
			app.ZapLog.Error("OpenAPI revocation startup unavailable; pending cleanup is not running", zap.Error(revocationErr))
		}
	} else {
		defer stopRevocation()
	}
	maintenanceDone := make(chan struct{})
	go func() {
		defer close(maintenanceDone)
		openAPI.RunMaintenance(maintenanceContext, func(err error) {
			app.ZapLog.Error("OpenAPI maintenance unavailable", zap.Error(err))
		})
	}()
	defer func() { cancelMaintenance(); <-maintenanceDone }()
	// 启动服务器(阻塞直到收到退出信号)
	_ = ginhelper.StartServer(engine)
	cancelMaintenance()
	if stopRevocation != nil {
		stopRevocation()
	}
	<-maintenanceDone
	// 优雅关闭 GB28181 SIP 服务
	gb28181.Stop()

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
	log.Printf("migrate-up 目标: database=%s version=%s dialect=%s", identity.DatabaseName, identity.DatabaseVersion, dialect)
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
	log.Printf("migrate-up 完成: before=%d after=%d newly_applied=%v", len(before), len(after), migrationDifference(before, after))
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
