package gormhelper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/global/consts"
	"uvplatform.com/uvp-gb28181/app/global/myerrors"

	dameng "github.com/godoes/gorm-dameng"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	gormLog "gorm.io/gorm/logger"
	"gorm.io/plugin/dbresolver"
)

// 获取一个 mysql 客户端
func GetOneMysqlClient() (*gorm.DB, error) {
	sqlType := "mysql"
	readDbIsOpen := app.ConfigYml.GetInt("gormv2." + sqlType + ".isopenreaddb")
	return GetSqlDriver(sqlType, readDbIsOpen)
}

// 获取一个 sqlserver 客户端
func GetOneSqlserverClient() (*gorm.DB, error) {
	sqlType := "sqlserver"
	readDbIsOpen := app.ConfigYml.GetInt("gormv2." + sqlType + ".isopenreaddb")
	return GetSqlDriver(sqlType, readDbIsOpen)
}

// 获取一个 postgresql 客户端
func GetOnePostgreSqlClient() (*gorm.DB, error) {
	sqlType := "postgresql"
	readDbIsOpen := app.ConfigYml.GetInt("gormv2." + sqlType + ".isopenreaddb")
	return GetSqlDriver(sqlType, readDbIsOpen)
}

// 获取一个人大金仓 KingbaseES 客户端。
//
// 金仓在通信协议层兼容 PostgreSQL（官方文档给的 Go 示例就是
// gorm.io/driver/postgres + 默认端口 54321），所以这里走同一个 dialector：
// 好处是 migration.DialectOf 会把它判成 postgres，整批 -postgresql.sql
// 迁移集直接复用，不需要第二套 SQL。
func GetOneKingbaseClient() (*gorm.DB, error) {
	sqlType := consts.DbTypeKingbase
	readDbIsOpen := app.ConfigYml.GetInt("gormv2." + sqlType + ".isopenreaddb")
	return GetSqlDriver(sqlType, readDbIsOpen)
}

// 获取一个阿里 PolarDB 客户端（MySQL 版）。
//
// PolarDB 的 MySQL 版走 MySQL wire protocol，官方驱动可直连，故复用
// mysql dialector ⇒ DialectOf 判成 mysql ⇒ 复用 mysql 迁移集。
// ⚠️ 若现场是 PolarDB for PostgreSQL 版本，走 GetOnePostgreSqlClient 那一路，
// 不能配成 polardb，否则会用 MySQL 协议去连 PG 端口。
func GetOnePolarDBClient() (*gorm.DB, error) {
	sqlType := consts.DbTypePolarDB
	readDbIsOpen := app.ConfigYml.GetInt("gormv2." + sqlType + ".isopenreaddb")
	return GetSqlDriver(sqlType, readDbIsOpen)
}

// 获取一个达梦 DM8 客户端。
//
// ⛔ 达梦是三者中唯一协议不兼容现有驱动的，用社区维护的 gorm dialector
// （基于达梦官方 Go 驱动源码二次开发，驱动已内嵌在 dm8 子包里，无 CGO）。
// 换回达梦官方安装目录下的 gorm_v2_dialect.zip 时，只需改这一处 import
// 与 getDbDialector 里的一个 case —— 其余代码不认具体驱动包。
func GetOneDMClient() (*gorm.DB, error) {
	sqlType := consts.DbTypeDM
	readDbIsOpen := app.ConfigYml.GetInt("gormv2." + sqlType + ".isopenreaddb")
	return GetSqlDriver(sqlType, readDbIsOpen)
}

// Client 按数据库类型取出连接，是**唯一**的类型分发入口。
//
// ⛔ 调用方不要再自己写 switch。历史状况：codegenservice 与
// pluginsmanagerservice 各抄了四份完全相同的四分支 switch（合计 8 处），
// 加一种库要改 8 个地方，漏掉一处就是「代码生成器连 MySQL、业务连达梦」
// 这种串库故障，而且症状看起来像数据丢了。
//
// sqlType 为空时读 gormv2.usedbtype；未配置或未知取值回落 MySQL，
// 保持与 app.DB() 一致的历史语义。
func Client(sqlType string) (*gorm.DB, error) {
	if strings.TrimSpace(sqlType) == "" {
		sqlType = app.ConfigYml.GetString("gormv2.usedbtype")
	}
	switch consts.NormalizeDbType(sqlType) {
	case consts.DbTypeSqlServer:
		return GetOneSqlserverClient()
	case consts.DbTypePostgreSql:
		return GetOnePostgreSqlClient()
	case consts.DbTypeSqlite:
		return GetOneSqliteClient()
	case consts.DbTypeKingbase:
		return GetOneKingbaseClient()
	case consts.DbTypePolarDB:
		return GetOnePolarDBClient()
	case consts.DbTypeDM:
		return GetOneDMClient()
	default:
		return GetOneMysqlClient()
	}
}

// 获取数据库驱动, 可以通过options 动态参数连接任意多个数据库
func GetSqlDriver(sqlType string, readDbIsOpen int, dbConf ...ConfigParams) (*gorm.DB, error) {

	var dbDialector gorm.Dialector
	if val, err := getDbDialector(sqlType, "Write", dbConf...); err != nil {
		app.Log(context.Background()).Named("db").Error("数据库驱动初始化失败",
			zap.String("event", "db.dialector.init_failed"),
			zap.String("dialect", sqlType), zap.String("role", "write"), zap.Error(err))
	} else {
		dbDialector = val
	}
	gormDb, err := gorm.Open(dbDialector, &gorm.Config{
		SkipDefaultTransaction: true,
		PrepareStmt:            true,
		Logger:                 redefineLog(sqlType), //拦截、接管 gorm v2 自带日志
	})
	if err != nil {
		//gorm 数据库驱动初始化失败
		return nil, err
	}

	// 如果开启了读写分离，配置读数据库（resource、read、replicas）
	// 读写分离配置只
	if readDbIsOpen == 1 {
		if val, err := getDbDialector(sqlType, "Read", dbConf...); err != nil {
			app.Log(context.Background()).Named("db").Error("数据库驱动初始化失败",
				zap.String("event", "db.dialector.init_failed"),
				zap.String("dialect", sqlType), zap.String("role", "read"), zap.Error(err))
		} else {
			dbDialector = val
		}
		resolverConf := dbresolver.Config{
			Replicas: []gorm.Dialector{dbDialector}, //  读 操作库，查询类
			Policy:   dbresolver.RandomPolicy{},     // sources/replicas 负载均衡策略适用于
		}
		err = gormDb.Use(dbresolver.Register(resolverConf).
			SetConnMaxIdleTime(time.Duration(app.ConfigYml.GetInt("gormv2."+sqlType+".read.setconnmaxidletime")) * time.Second).
			SetConnMaxLifetime(time.Duration(app.ConfigYml.GetInt("gormv2."+sqlType+".read.setconnmaxlifetime")) * time.Second).
			SetMaxIdleConns(app.ConfigYml.GetInt("gormv2." + sqlType + ".read.setmaxidleconns")).
			SetMaxOpenConns(app.ConfigYml.GetInt("gormv2." + sqlType + ".read.setmaxopenconns")))
		if err != nil {
			return nil, err
		}
	}

	if err := installLogContext(gormDb); err != nil {
		return nil, err
	}

	// 查询没有数据，屏蔽 gorm v2 包中会爆出的错误
	// https://github.com/go-gorm/gorm/issues/3789  此 issue 所反映的问题就是我们本次解决掉的
	_ = gormDb.Callback().Query().Before("gorm:query").Register("disable_raise_record_not_found", MaskNotDataError)

	// https://github.com/go-gorm/gorm/issues/4838
	_ = gormDb.Callback().Create().Before("gorm:before_create").Register("CreateBeforeHook", CreateBeforeHook)
	// 为了完美支持gorm的一系列回调函数
	_ = gormDb.Callback().Update().Before("gorm:before_update").Register("UpdateBeforeHook", UpdateBeforeHook)
	// 删除前Hook，自动设置 DeletedAt 字段
	_ = gormDb.Callback().Delete().Before("gorm:before_delete").Register("DeleteBeforeHook", DeleteBeforeHook)

	// 为主连接设置连接池(43行返回的数据库驱动指针)
	if rawDb, err := gormDb.DB(); err != nil {
		return nil, err
	} else {
		rawDb.SetConnMaxIdleTime(time.Duration(app.ConfigYml.GetInt("gormv2."+sqlType+".write.setconnmaxidletime")) * time.Second)
		rawDb.SetConnMaxLifetime(time.Duration(app.ConfigYml.GetInt("gormv2."+sqlType+".write.setconnmaxlifetime")) * time.Second)
		rawDb.SetMaxIdleConns(app.ConfigYml.GetInt("gormv2." + sqlType + ".write.setmaxidleconns"))
		rawDb.SetMaxOpenConns(app.ConfigYml.GetInt("gormv2." + sqlType + ".write.setmaxopenconns"))
		return gormDb, nil
	}
}

// 获取一个数据库方言(Dialector),通俗的说就是根据不同的连接参数，获取具体的一类数据库的连接指针
func getDbDialector(sqlType, readWrite string, dbConf ...ConfigParams) (gorm.Dialector, error) {
	var dbDialector gorm.Dialector
	dsn := getDsn(sqlType, readWrite, dbConf...)
	switch strings.ToLower(sqlType) {
	case "mysql", "polardb":
		// PolarDB（MySQL 版）走 MySQL wire protocol，官方驱动直连。
		dbDialector = mysql.Open(dsn)
	case "sqlserver", "mssql":
		dbDialector = sqlserver.Open(dsn)
	case "postgres", "postgresql", "postgre", "kingbase":
		// 金仓在协议层兼容 PostgreSQL，复用同一 dialector ⇒ DialectOf 判成
		// postgres ⇒ 复用整批 -postgresql.sql 迁移集。
		dbDialector = postgres.Open(dsn)
	case "dm", "dameng":
		dbDialector = dameng.Open(dsn)
	default:
		return nil, errors.New(myerrors.ErrorsDbDriverNotExists + sqlType)
	}
	return dbDialector, nil
}

// 根据配置参数生成数据库驱动 dsn
func getDsn(sqlType, readWrite string, dbConf ...ConfigParams) string {
	Host := app.ConfigYml.GetString("gormv2." + sqlType + "." + readWrite + ".host")
	DataBase := app.ConfigYml.GetString("gormv2." + sqlType + "." + readWrite + ".database")
	Port := app.ConfigYml.GetInt("gormv2." + sqlType + "." + readWrite + ".port")
	User := app.ConfigYml.GetString("gormv2." + sqlType + "." + readWrite + ".user")
	Pass := app.ConfigYml.GetString("gormv2." + sqlType + "." + readWrite + ".pass")
	Charset := app.ConfigYml.GetString("gormv2." + sqlType + "." + readWrite + ".charset")
	TimeZone := app.ConfigYml.GetString("gormv2." + sqlType + ".timezone")

	if len(dbConf) > 0 {
		if strings.ToLower(readWrite) == "write" {
			if len(dbConf[0].Write.Host) > 0 {
				Host = dbConf[0].Write.Host
			}
			if len(dbConf[0].Write.DataBase) > 0 {
				DataBase = dbConf[0].Write.DataBase
			}
			if dbConf[0].Write.Port > 0 {
				Port = dbConf[0].Write.Port
			}
			if len(dbConf[0].Write.User) > 0 {
				User = dbConf[0].Write.User
			}
			if len(dbConf[0].Write.Pass) > 0 {
				Pass = dbConf[0].Write.Pass
			}
			if len(dbConf[0].Write.Charset) > 0 {
				Charset = dbConf[0].Write.Charset
			}
		} else {
			if len(dbConf[0].Read.Host) > 0 {
				Host = dbConf[0].Read.Host
			}
			if len(dbConf[0].Read.DataBase) > 0 {
				DataBase = dbConf[0].Read.DataBase
			}
			if dbConf[0].Read.Port > 0 {
				Port = dbConf[0].Read.Port
			}
			if len(dbConf[0].Read.User) > 0 {
				User = dbConf[0].Read.User
			}
			if len(dbConf[0].Read.Pass) > 0 {
				Pass = dbConf[0].Read.Pass
			}
			if len(dbConf[0].Read.Charset) > 0 {
				Charset = dbConf[0].Read.Charset
			}
		}
	}

	switch strings.ToLower(sqlType) {
	case "mysql", "polardb":
		// PolarDB（MySQL 版）与 MySQL 同一套 DSN 语法。
		if TimeZone == "" {
			TimeZone = "Local"
		}
		// multiStatements=true:迁移 runner 一次 Exec 整个 .sql 文件(多语句),驱动必须放行
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=true&loc=%s&multiStatements=true", User, Pass, Host, Port, DataBase, Charset, TimeZone)
	case "sqlserver", "mssql":
		// UseRowNumberForPaging=true 这个参数会告诉GORM的驱动，在为分页生成SQL时，使用兼容旧版本（SQL Server 2008）的ROW_NUMBER()语法，而不是OFFSET...FETCH
		return fmt.Sprintf("server=%s;port=%d;database=%s;user id=%s;password=%s;encrypt=disable;UseRowNumberForPaging=true", Host, Port, DataBase, User, Pass)
	case "postgresql", "postgre", "postgres", "kingbase":
		// 金仓与 PostgreSQL 同一套 libpq 连接串（端口默认 54321，由配置给）。
		// 如果未配置时区，默认使用 Asia/Shanghai
		if TimeZone == "" {
			TimeZone = "Asia/Shanghai"
		}
		return fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable TimeZone=%s", Host, Port, DataBase, User, Pass, TimeZone)
	case "dm", "dameng":
		// 达梦的 DSN 与前三者完全不同：dm://user:pass@host:port?schema=X
		//
		// ⛔ schema 不是可选的锦上添花 —— 达梦一套实例下按模式(schema)隔离表，
		//    登录后看到的表集合取决于当前模式。不指定时驱动会用用户的默认模式，
		//    而「用户名即模式名」，一旦建库时的模式与登录用户不一致，
		//    迁移会把表建到 A 模式、业务查询去 B 模式找，表现为「建表成功但查不到表」。
		//    所以这里读不到就退回用户名，绝不留给驱动自己猜。
		schema := app.ConfigYml.GetString("gormv2." + sqlType + "." + readWrite + ".schema")
		if schema == "" {
			schema = app.ConfigYml.GetString("gormv2." + sqlType + ".schema")
		}
		if schema == "" {
			schema = User
		}
		return dameng.BuildUrl(User, Pass, Host, Port, map[string]string{"schema": schema})
	}
	return ""
}

// 创建自定义日志模块，对 gorm 日志进行拦截、
func redefineLog(sqlType string) gormLog.Interface { return createCustomGormLog(sqlType) }
