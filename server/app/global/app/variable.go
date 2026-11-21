package app

import (
	"uvplatform.com/uvp-gb28181/app/global/consts"
	"uvplatform.com/uvp-gb28181/app/utils/logging"

	"log"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	LogRuntime       *logging.Runtime          // startup-owned logging runtime
	RealtimeLogHub   *logging.EventHub         // bounded realtime console log stream
	BasePath         string                    // 定义项目的根目录
	ConfigYml        YmlConfigInterf           // 全局配置文件指针
	GormDbMysql      *gorm.DB                  // mysql数据库连接
	GormDbSqlserver  *gorm.DB                  // sqlserver数据库连接
	GormDbPostgreSql *gorm.DB                  // postgresql数据库连接
	GormDbSqlite     *gorm.DB                  // sqlite数据库连接（绿色安装包默认库，单文件、零外部依赖）
	GormDbKingbase   *gorm.DB                  // 人大金仓 KingbaseES（PG 协议，复用 postgres 驱动）
	GormDbPolarDB    *gorm.DB                  // 阿里 PolarDB（MySQL 协议，复用 mysql 驱动）
	GormDbDM         *gorm.DB                  // 达梦 DM8（自有协议，独立方言）
	ZapLog           *zap.Logger               // 全局日志指针
	CasbinV2         CasbinInterf              // casbin指针
	Cache            CacheInterf               // 缓存指针
	TokenService     TokenServiceInterface     // token管理
	SessionValidator SessionValidatorInterface // persistent login-session validation
	LoginLogRecorder LoginLogRecorderInterface // login audit recorder
	Response         ResponseHandler           // 全局响应指针
	UploadService    FileUploadService         // 文件上传服务
	JobScheduler     JobSchedulerInterf        // 全局任务调度器
)

/*
 * @Description: 获取数据库连接
 * @param sqlType 数据库类型 mysql sqlserver postgresql sqlite kingbase polardb dm
 *	不传则读配置 gormv2.usedbtype
 * @return *gorm.DB
 */
func DB(sqlType ...string) *gorm.DB {
	var raw string
	if len(sqlType) > 0 {
		raw = sqlType[0]
	} else {
		raw = ConfigYml.GetString("gormv2.usedbtype")
	}
	// ⛔ 走统一的别名收敛函数，不要在本地维护第二份 switch —— 这里历史上只认
	// "postgresql"，配成 "postgres" 会静默回落 MySQL，而 handler 那边却认，
	// 结果是同一个进程读写两个库。
	dbType := consts.NormalizeDbType(raw)
	var db *gorm.DB
	switch dbType {
	case consts.DbTypeSqlServer:
		db = GormDbSqlserver
	case consts.DbTypePostgreSql:
		db = GormDbPostgreSql
	case consts.DbTypeSqlite:
		db = GormDbSqlite
	case consts.DbTypeKingbase:
		db = GormDbKingbase
	case consts.DbTypePolarDB:
		db = GormDbPolarDB
	case consts.DbTypeDM:
		db = GormDbDM
	default:
		// 未配置（空串）或未知取值：保持历史行为回落 MySQL。
		db = GormDbMysql
	}
	if db == nil {
		log.Fatal("数据库连接失败")
	}

	return db
}
