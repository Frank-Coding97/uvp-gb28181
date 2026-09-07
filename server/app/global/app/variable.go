package app

import (
	"uvplatform.cn/uvp-gb28181/app/global/consts"

	"log"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	BasePath         string                    // 定义项目的根目录（兼容旧部署）
	ConfigPath       string                    // 显式配置目录
	ResourcePath     string                    // 显式资源目录
	WebPath          string                    // 显式前端资源目录
	DataPath         string                    // 显式实例数据目录
	UploadPath       string                    // 显式上传目录
	RecordingsPath   string                    // 显式录像目录
	LogsPath         string                    // 显式日志目录
	SchedulerLogPath string                    // 显式调度器日志目录
	ConfigYml        YmlConfigInterf           // 全局配置文件指针
	GormDbMysql      *gorm.DB                  // mysql数据库连接
	GormDbSqlserver  *gorm.DB                  // sqlserver数据库连接
	GormDbPostgreSql *gorm.DB                  // postgresql数据库连接
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
 * @param sqlType 数据库类型 mysql sqlserver postgresql
 * @return *gorm.DB
 */
func DB(sqlType ...string) *gorm.DB {
	var dbType string
	if len(sqlType) > 0 {
		dbType = sqlType[0]
	} else {
		dbType = ConfigYml.GetString("gormv2.usedbtype")
	}
	var db *gorm.DB
	switch dbType {
	case consts.DbTypeMySql:
		db = GormDbMysql
	case consts.DbTypeSqlServer:
		db = GormDbSqlserver
	case consts.DbTypePostgreSql:
		db = GormDbPostgreSql
	default:
		db = GormDbMysql
	}
	if db == nil {
		log.Fatal("数据库连接失败")
	}

	return db
}
