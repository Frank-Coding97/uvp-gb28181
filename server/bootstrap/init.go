package bootstrap

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/migration"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/scheduler"
	"uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/cachehelper"
	"uvplatform.cn/uvp-gb28181/app/utils/casbinhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/tokenhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/uploadhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/ymlconfig"
)

func init() {
	// 检查必要的文件夹是否存在
	checkRequiredFolders()
	versionErr := app.LoadVersionInfo()
	var err error
	app.ConfigYml, err = ymlconfig.LoadYamlFactory(app.BasePath + "/config")
	if err != nil {
		startupFail("config", err)
	}
	initLogging()
	if versionErr != nil {
		logging.ReportStartupFailure(nil, app.ZapLog, "version", versionErr)
	}
	// 初始化数据库
	initDB()

	// -migrate-up / -migrate-down=<文件名> 是纯运维入口:只完成配置+DB 初始化,
	// 不执行 Up/业务初始化(main 解析参数后直接走 Down)。
	// 否则迁移失败时回滚命令会先重试同一失败的 Up 并退出,永远到不了 Down
	if migrationCommandRequested() {
		return
	}

	// 数据库迁移自动执行(schema 变更随部署生效,先迁移后启动业务初始化)
	if err := migration.RunMigrations(map[string]*gorm.DB{
		"mysql":      app.GormDbMysql,
		"sqlserver":  app.GormDbSqlserver,
		"postgresql": app.GormDbPostgreSql,
	}); err != nil {
		startupFail("migration", err)
	}

	// 初始化casbin
	app.CasbinV2 = casbinhelper.NewCasbinHelper()
	err = app.CasbinV2.InitCasbin(app.DB(), app.ConfigYml.GetString("casbin.modelconfig"))
	if err != nil {
		startupFail("casbin", err)
	}

	// 初始化缓存管理
	app.Cache = newCache()

	// 初始化token管理
	app.TokenService = newTokenService(app.Cache)

	// 初始化持久化登录会话校验;JWT 中间件在 Casbin 前使用,数据库故障 fail-closed。
	app.SessionValidator = service.NewAuthSessionService(app.DB())
	app.LoginLogRecorder = service.NewLoginLogService(app.DB())

	// 初始化文件上传服务
	app.UploadService = newUploadService()

	// 初始化任务调度器
	app.JobScheduler = newScheduler()

	// 注册所有执行器
	scheduler.RegisterExecutors()
	if err := scheduler.RegisterSystemJobs(app.DB()); err != nil {
		startupFail("scheduler", err)
	}

	// 从数据库加载启用的任务到调度器及任务结果处理器
	scheduler.LoadJobsFromDB()

	// 初始化Response
	app.Response = response.NewResponseHandler()
}

// 初始化数据库
func initDB() {
	// mysql
	if app.ConfigYml.GetInt("gormv2.mysql.isinitglobalgormmysql") == 1 {
		if dbMysql, err := gormhelper.GetOneMysqlClient(); err != nil {
			startupFail("database", err)
		} else {
			app.GormDbMysql = dbMysql
		}
	}
	//sqlserver
	if app.ConfigYml.GetInt("gormv2.sqlserver.isinitglobalgormsqlserver") == 1 {
		if dbSqlserver, err := gormhelper.GetOneSqlserverClient(); err != nil {
			startupFail("database", err)
		} else {
			app.GormDbSqlserver = dbSqlserver
		}
	}
	//postgresql
	if app.ConfigYml.GetInt("gormv2.postgresql.isinitglobalgormpostgresql") == 1 {
		if dbPostgresql, err := gormhelper.GetOnePostgreSqlClient(); err != nil {
			startupFail("database", err)
		} else {
			app.GormDbPostgreSql = dbPostgresql
		}
	}
}

// 检查必要的文件夹是否存在
func checkRequiredFolders() {
	// 初始化程序根目录
	if path, err := os.Getwd(); err == nil {
		// 路径进行处理，兼容单元测试程序程序启动时的奇怪路径
		if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "-test") {
			app.BasePath = strings.Replace(strings.Replace(path, `\test`, "", 1), `/test`, "", 1)
		} else {
			app.BasePath = path
		}
		// BasePath is used for resolution, not emitted as free-form text.
	} else {
		startupFail("filesystem", errors.New("working directory unavailable"))
	}
	//检查配置文件是否存在
	if _, err := os.Stat(app.BasePath + consts.ConfigFilePath); err != nil {
		startupFail("config", err)
	}
}

// initLogging constructs one immutable runtime before dependencies start.
func initLogging() {
	cfg, err := logging.ParseConfig(app.ConfigYml, app.BasePath)
	if err != nil {
		startupFail("logging", err)
	}
	runtime, err := logging.OpenRuntime(logging.Options{Config: cfg, Version: app.AppVersion.Version, Instance: uuid.NewString()})
	if err != nil {
		startupFail("logging", err)
	}
	app.LogRuntime, app.ZapLog = runtime, runtime.Root
	logging.InstallStandardBridge(app.ZapLog.Named("stdlib"))
	for _, notice := range cfg.Notices {
		app.ZapLog.Named("startup").Warn("legacy logging configuration", zap.String("event", "logging.config_compatibility"), zap.String("notice", notice))
	}
	app.ConfigYml.ConfigFileChangeListen(func() {
		next, err := logging.ParseConfig(app.ConfigYml, app.BasePath)
		if err != nil {
			logging.ReportStartupFailure(nil, app.ZapLog, "config", err)
			return
		}
		app.LogRuntime.NoticeReload(next)
	})
}

func startupFail(phase string, err error) {
	logging.ReportStartupFailure(nil, app.ZapLog, phase, err)
	if app.LogRuntime != nil {
		_ = app.LogRuntime.Close()
	}
	os.Exit(1)
}

// newCache 初始化缓存
func newCache() app.CacheInterf {
	cacheType := app.ConfigYml.GetString("server.cachetype")
	if cacheType == "redis" {
		redisHelper, err := cachehelper.NewRedisHelper(
			app.ConfigYml.GetString("redis.host")+":"+app.ConfigYml.GetString("redis.port"),
			app.ConfigYml.GetString("redis.password"),
			app.ConfigYml.GetInt("redis.indexdb"),
		)
		if err != nil {
			startupFail("cache", err)
		}

		return redisHelper
	}
	return cachehelper.NewMemoryHelper()
}

func newTokenService(cache app.CacheInterf) app.TokenServiceInterface {
	tokenExpire := app.ConfigYml.GetDuration("token.jwttokenexpire")
	refreshExpire := app.ConfigYml.GetDuration("token.jwttokenrefreshexpire")

	return &tokenhelper.TokenService{
		RedisHelper:    cache,
		JWTSecret:      app.ConfigYml.GetString("token.jwttokensignkey"),
		Ctx:            context.Background(),
		TokenExpire:    tokenExpire,
		RefreshExpire:  refreshExpire,
		CacheKeyPrefix: app.ConfigYml.GetString("token.cachekeyprefix"),
		IsCache:        app.ConfigYml.GetBool("token.iscache"),
	}
}

// newUploadService 初始化文件上传服务
func newUploadService() app.FileUploadService {
	uploadService, err := uploadhelper.CreateUploadService()
	if err != nil {
		startupFail("upload", err)
	}
	return uploadService
}

// newScheduler 初始化任务调度器
func newScheduler() app.JobSchedulerInterf {
	logDir := app.BasePath + app.ConfigYml.GetString("scheduler.log.dir")

	// 解析日志级别
	levelStr := app.ConfigYml.GetString("scheduler.log.level")
	var level schedulerhelper.LogLevel
	switch levelStr {
	case "debug":
		level = schedulerhelper.LevelDebug
	case "info":
		level = schedulerhelper.LevelInfo
	case "warn":
		level = schedulerhelper.LevelWarn
	case "error":
		level = schedulerhelper.LevelError
	case "fatal":
		level = schedulerhelper.LevelFatal
	default:
		level = schedulerhelper.LevelInfo
	}

	// 获取结果通道缓冲大小
	bufferSize := app.ConfigYml.GetInt("scheduler.job_results_buffer_size")
	if bufferSize <= 0 {
		bufferSize = 1000 // 默认值
	}

	scheduler := schedulerhelper.NewJobScheduler(
		schedulerhelper.WithLoggerConfig(logDir, level),
		schedulerhelper.WithJobResultsBufferSize(bufferSize),
	)

	// 启动调度器
	scheduler.Start()

	return scheduler
}

// migrationCommandRequested 判断启动参数是否请求纯迁移运维入口。
// 空 down 参数不算请求:否则 bootstrap 跳过迁移但 main 正常启动业务。
func migrationCommandRequested() bool {
	for _, arg := range os.Args {
		if arg == "-migrate-up" {
			return true
		}
		if strings.HasPrefix(arg, "-migrate-down=") && strings.TrimPrefix(arg, "-migrate-down=") != "" {
			return true
		}
	}
	return false
}
