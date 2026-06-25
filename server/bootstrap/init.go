package bootstrap

import (
	"context"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/global/myerrors"
	"uvplatform.cn/uvp-gb28181/app/scheduler"
	"uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/cachehelper"
	"uvplatform.cn/uvp-gb28181/app/utils/casbinhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/tokenhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/uploadhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/ymlconfig"
	"log"
	"os"
	"strings"
	"time"

	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func init() {
	// 检查必要的文件夹是否存在
	checkRequiredFolders()
	// 加载版本信息
	if err := app.LoadVersionInfo(); err != nil {
		log.Println("警告: 加载版本信息失败:", err)
	}
	// 配置文件
	app.ConfigYml = ymlconfig.CreateYamlFactory(app.BasePath + "/config")
	app.ConfigYml.ConfigFileChangeListen(func() {
		//配置文件发生变化
	})
	// 日志
	app.ZapLog = createZapFactory(service.ZapLogHandler)
	// 初始化数据库
	initDB()

	// 初始化casbin
	app.CasbinV2 = casbinhelper.NewCasbinHelper()
	err := app.CasbinV2.InitCasbin(app.DB(), app.ConfigYml.GetString("casbin.modelconfig"))
	if err != nil {
		log.Fatal("CasbinV2.InitCasbin err :" + err.Error())
	}

	// 初始化缓存管理
	app.Cache = newCache()

	// 初始化token管理
	app.TokenService = newTokenService(app.Cache)

	// 初始化文件上传服务
	app.UploadService = newUploadService()

	// 初始化任务调度器
	app.JobScheduler = newScheduler()

	// 注册所有执行器
	scheduler.RegisterExecutors()

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
			log.Fatal(myerrors.ErrorsGormInitFail + err.Error())
		} else {
			app.GormDbMysql = dbMysql
		}
	}
	//sqlserver
	if app.ConfigYml.GetInt("gormv2.sqlserver.isinitglobalgormsqlserver") == 1 {
		if dbSqlserver, err := gormhelper.GetOneSqlserverClient(); err != nil {
			log.Fatal(myerrors.ErrorsGormInitFail + err.Error())
		} else {
			app.GormDbSqlserver = dbSqlserver
		}
	}
	//postgresql
	if app.ConfigYml.GetInt("gormv2.postgresql.isinitglobalgormpostgresql") == 1 {
		if dbPostgresql, err := gormhelper.GetOnePostgreSqlClient(); err != nil {
			log.Fatal(myerrors.ErrorsGormInitFail + err.Error())
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
		log.Println("当前项目根目录:", app.BasePath)
	} else {
		log.Fatal("获取当前目录失败")
	}
	//检查配置文件是否存在
	if _, err := os.Stat(app.BasePath + consts.ConfigFilePath); err != nil {
		log.Fatal(consts.ConfigFilePath + " not exists: " + err.Error())
	}
}

// createZapFactory 创建zap日志工厂
// 不再区分 debug/生产，统一走"文件 Core + 控制台彩色 Core(可关)"双 Tee
// 调试粒度由 logs.level 控制（debug/info/warn/error/fatal/panic）
func createZapFactory(entry func(zapcore.Entry) error) *zap.Logger {
	encoderConfig := zap.NewProductionEncoderConfig()

	timePrecision := app.ConfigYml.GetString("logs.timeprecision")
	var recordTimeFormat string
	switch timePrecision {
	case "second":
		recordTimeFormat = "2006-01-02 15:04:05"
	case "millisecond":
		recordTimeFormat = "2006-01-02 15:04:05.000"
	default:
		recordTimeFormat = "2006-01-02 15:04:05"

	}
	encoderConfig.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.Format(recordTimeFormat))
	}
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	encoderConfig.TimeKey = "created_at" // 生成json格式日志的时间键字段，默认为 ts,修改以后方便日志导入到 ELK 服务器

	var fileEncoder zapcore.Encoder
	switch app.ConfigYml.GetString("logs.textformat") {
	case "console":
		fileEncoder = zapcore.NewConsoleEncoder(encoderConfig) // 普通模式
	case "json":
		fileEncoder = zapcore.NewJSONEncoder(encoderConfig) // json格式
	default:
		fileEncoder = zapcore.NewConsoleEncoder(encoderConfig) // 普通模式
	}
	// 写入器
	fileName := app.BasePath + app.ConfigYml.GetString("logs.zaplogname")
	lumberJackLogger := &lumberjack.Logger{
		Filename:   fileName,                                //日志文件的位置
		MaxSize:    app.ConfigYml.GetInt("logs.maxsize"),    //在进行切割之前，日志文件的最大大小（以MB为单位）
		MaxBackups: app.ConfigYml.GetInt("logs.maxbackups"), //保留旧文件的最大个数
		MaxAge:     app.ConfigYml.GetInt("logs.maxage"),     //保留旧文件的最大天数
		Compress:   app.ConfigYml.GetBool("logs.compress"),  //是否压缩/归档旧文件
	}
	fileWriter := zapcore.AddSync(lumberJackLogger)

	// 从配置文件读取日志等级
	logLevelStr := app.ConfigYml.GetString("logs.level")
	var logLevel zapcore.Level
	switch logLevelStr {
	case "debug":
		logLevel = zap.DebugLevel
	case "info":
		logLevel = zap.InfoLevel
	case "warn":
		logLevel = zap.WarnLevel
	case "error":
		logLevel = zap.ErrorLevel
	case "fatal":
		logLevel = zap.FatalLevel
	case "panic":
		logLevel = zap.PanicLevel
	default:
		logLevel = zap.InfoLevel // 默认使用 info 级别
	}

	// 文件 Core：无 ANSI 颜色码，干净写入 lumberjack
	cores := []zapcore.Core{
		zapcore.NewCore(fileEncoder, fileWriter, logLevel),
	}

	// 控制台 Core：带 ANSI 多字段层次染色（time 灰/level 彩/caller 青/message 白）
	// 对标 Java Spring Boot 默认 CONSOLE_LOG_PATTERN。logs.console=false 可关闭仅写文件。
	if app.ConfigYml.GetString("logs.console") != "false" {
		consoleCfg := encoderConfig
		consoleCfg.ConsoleSeparator = " | "
		consoleCfg.EncodeLevel = paddedColorLevelEncoder
		consoleCfg.EncodeCaller = cyanCallerEncoder
		consoleCfg.EncodeTime = dimTimeEncoder(recordTimeFormat)
		consoleEncoder := zapcore.NewConsoleEncoder(consoleCfg)
		cores = append(cores, zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), logLevel))
	}

	zapCore := zapcore.NewTee(cores...)
	return zap.New(zapCore, zap.AddCaller(), zap.Hooks(entry), zap.AddStacktrace(zap.WarnLevel))
}

// ANSI 颜色辅助
const (
	ansiReset = "\x1b[0m"
	ansiDim   = "\x1b[2m"  // 灰/暗淡（time）
	ansiCyan  = "\x1b[36m" // 青色（caller）
)

// 级别名 → ANSI 颜色码（参考 zap 内部 _levelToColor，DEBUG 紫/INFO 蓝/WARN 黄/ERROR & 以上 红）
var levelColorCode = map[zapcore.Level]string{
	zapcore.DebugLevel:  "\x1b[35m",
	zapcore.InfoLevel:   "\x1b[34m",
	zapcore.WarnLevel:   "\x1b[33m",
	zapcore.ErrorLevel:  "\x1b[31m",
	zapcore.DPanicLevel: "\x1b[31m",
	zapcore.PanicLevel:  "\x1b[31m",
	zapcore.FatalLevel:  "\x1b[31m",
}

// paddedColorLevelEncoder 把级别填充到 5 字符宽再上色，让 INFO/WARN/ERROR/PANIC 视觉对齐。
func paddedColorLevelEncoder(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	name := l.CapitalString()
	if len(name) < 5 {
		name = name + strings.Repeat(" ", 5-len(name))
	}
	if c, ok := levelColorCode[l]; ok {
		enc.AppendString(c + name + ansiReset)
		return
	}
	enc.AppendString(name)
}

// dimTimeEncoder 时间字段染暗淡灰，避免抢眼。
func dimTimeEncoder(layout string) zapcore.TimeEncoder {
	return func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(ansiDim + t.Format(layout) + ansiReset)
	}
}

// cyanCallerEncoder caller 字段染青色，pkg/file.go:line 格式。
func cyanCallerEncoder(c zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(ansiCyan + c.TrimmedPath() + ansiReset)
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
			panic(err)
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
		log.Fatal("初始化文件上传服务失败: " + err.Error())
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
