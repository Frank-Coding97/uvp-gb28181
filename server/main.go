package main

import (
	"errors"
	"log"
	"os"
	"strings"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181"
	"uvplatform.cn/uvp-gb28181/app/gb28181/migration"
	"uvplatform.cn/uvp-gb28181/app/global/app"
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
	routes.InitRoutes(engine)
	// 初始化插件路由
	ginhelper.InitPluginRoutes(engine)
	// 启动 GB28181 SIP 服务(双栈 UDP+TCP,在 HTTP 阻塞前旁挂)
	gb28181.Start()
	// 启动服务器(阻塞直到收到退出信号)
	_ = ginhelper.StartServer(engine)
	// 优雅关闭 GB28181 SIP 服务
	gb28181.Stop()

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
