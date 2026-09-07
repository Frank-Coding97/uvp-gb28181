package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181"
	"uvplatform.cn/uvp-gb28181/app/gb28181/migration"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/routes"
	"uvplatform.cn/uvp-gb28181/app/utils/ginhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	_ "uvplatform.cn/uvp-gb28181/bootstrap"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"

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
	for _, arg := range os.Args[1:] {
		if arg == "-bootstrap-db" {
			if err := runBootstrapDB(); err != nil {
				log.Fatal(err)
			}
			return
		}
		if arg == "-db-check" {
			if err := runDBCheck(); err != nil {
				log.Fatal(err)
			}
			return
		}
	}
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
	var admission *ginhelper.StandaloneAdmission
	if app.DataPath != "" {
		admission = ginhelper.NewStandaloneAdmission()
		engine.Use(admission.Middleware())
	}
	// 初始化系统路由
	routes.InitRoutes(engine)
	// 初始化插件路由
	ginhelper.InitPluginRoutes(engine)
	if admission != nil {
		if err := runStandaloneServer(engine, admission); err != nil {
			log.Fatal(err)
		}
		return
	}
	// 启动 GB28181 SIP 服务(双栈 UDP+TCP,在 HTTP 阻塞前旁挂)
	gb28181.Start()
	// 启动服务器(阻塞直到收到退出信号)
	_ = ginhelper.StartServer(engine)
	// 优雅关闭 GB28181 SIP 服务
	gb28181.Stop()

}

func runBootstrapDB() error {
	if app.GormDbSQLite == nil {
		return errors.New("-bootstrap-db requires sqlite")
	}
	db := app.DB()
	raw, err := db.DB()
	if err != nil {
		return err
	}
	defer raw.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := sqlitebootstrap.Initialize(ctx, db)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

// runDBCheck reports runtime settings only; it does not initialize application schema.
func runDBCheck() error {
	if app.GormDbSQLite == nil {
		return errors.New("-db-check requires sqlite")
	}
	db := app.DB()
	raw, err := db.DB()
	if err != nil {
		return err
	}
	defer raw.Close()
	info, err := gormhelper.InspectSQLite(db)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(info)
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

func appliedMigrationVersions(db *gorm.DB) ([]string, error) {
	if !db.Migrator().HasTable("gb_schema_migrations") {
		return nil, nil
	}
	var versions []string
	err := db.Table("gb_schema_migrations").Order("version").Pluck("version", &versions).Error
	return versions, err
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
	if app.GormDbSQLite != nil {
		return app.GormDbSQLite, migration.DialectSQLite, nil
	}
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
