package ginhelper

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/scheduler"

	"io"

	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func GetEngine() *gin.Engine {
	debug := app.ConfigYml.GetBool("server.appdebug")
	if debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	root := app.ZapLog
	if root == nil {
		root = zap.NewNop()
	}
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard
	gin.DebugPrintFunc = func(string, ...interface{}) {
		root.Named("gin").Debug("Gin framework diagnostic", zap.String("event", "gin.diagnostic"))
	}
	gin.DebugPrintRouteFunc = func(string, string, string, int) {}
	engine := gin.New()
	engine.Use(RequestLogging(root), CustomRecovery())
	if debug {
		pprof.Register(engine)
	}
	return engine
}

// PluginRouteFunc 插件路由函数类型
// 该函数类型定义了插件路由的回调函数，插件模块通过该函数注册自己的路由
type PluginRouteFunc func(*gin.Engine)

// pluginRouteFuncs 存储已注册的插件路由函数
var pluginRouteFuncs []PluginRouteFunc

// RegisterPluginRoutes 注册插件路由
// 该函数用于注册插件的路由函数，由插件模块主动调用
// 参数 routeFunc 是插件的路由注册函数，该函数会在初始化时被调用
func RegisterPluginRoutes(routeFunc PluginRouteFunc) {
	if routeFunc != nil {
		pluginRouteFuncs = append(pluginRouteFuncs, routeFunc)
		app.ZapLog.Info("插件路由函数注册成功")
	} else {
		app.ZapLog.Warn("插件路由函数为空，注册失败")
	}
}

// InitPluginRoutes 初始化插件路由
// 该函数在main.go中调用，将*gin.Engine传递给已注册的插件路由函数
// 参数 engine 是Gin引擎实例，会传递给每个已注册的插件路由函数
func InitPluginRoutes(engine *gin.Engine) {
	if len(pluginRouteFuncs) == 0 {
		app.ZapLog.Info("没有注册的插件路由函数")
		return
	}

	// 遍历所有已注册的插件路由函数，并调用它们
	for i, routeFunc := range pluginRouteFuncs {
		if routeFunc != nil {
			routeFunc(engine)
			app.ZapLog.Info("插件路由初始化成功", zap.Int("pluginIndex", i))
		} else {
			app.ZapLog.Warn("插件路由函数为空，跳过初始化", zap.Int("pluginIndex", i))
		}
	}

	app.ZapLog.Info("所有插件路由初始化完成", zap.Int("pluginCount", len(pluginRouteFuncs)))
}

// GetPluginRouteFuncs 获取已注册的插件路由函数列表
// 该函数主要用于调试和测试
func GetPluginRouteFuncs() []PluginRouteFunc {
	return pluginRouteFuncs
}

// PrintStartupBanner 打印启动横幅信息
func PrintStartupBanner() {
	// 从配置获取信息
	port := app.ConfigYml.GetString("httpserver.port")
	serverRoot := app.ConfigYml.GetString("httpserver.serverroot")
	dbType := app.ConfigYml.GetString("gormv2.usedbtype")

	// 获取数据库名称
	dbName := app.ConfigYml.GetString("gormv2." + dbType + ".write.database")
	if dbName == "" {
		dbName = "unknown"
	}

	// 版本信息
	version := app.AppVersion.Version
	if version == "" {
		version = "unknown"
	}

	// 打印启动信息（简洁纯文本格式）
	fmt.Println()
	fmt.Println("===========================================")
	fmt.Println("  GinFast Framework")
	fmt.Println("===========================================")
	fmt.Printf("  Version    : %s\n", version)
	fmt.Printf("  Port       : %s\n", port)
	fmt.Printf("  ServerRoot : %s\n", serverRoot)
	fmt.Printf("  DbType     : %s\n", dbType)
	fmt.Printf("  Database   : %s\n", dbName)
	fmt.Println("===========================================")
	fmt.Println()
}

// StartServer 配置并启动HTTP服务器，支持优雅关闭
func StartServer(engine *gin.Engine) error {
	logRoutes(engine)
	// 打印启动横幅
	PrintStartupBanner()

	// 配置HTTP服务器超时设置
	server := &http.Server{
		Addr:         app.ConfigYml.GetString("httpserver.port"),
		Handler:      engine,
		ReadTimeout:  time.Duration(app.ConfigYml.GetInt("httpserver.read_timeout")) * time.Second,
		WriteTimeout: time.Duration(app.ConfigYml.GetInt("httpserver.write_timeout")) * time.Second,
		IdleTimeout:  time.Duration(app.ConfigYml.GetInt("httpserver.idle_timeout")) * time.Second,
	}

	// 在 goroutine 中启动服务器
	go func() {
		app.ZapLog.Info("服务器启动", zap.String("addr", server.Addr))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			app.ZapLog.Fatal("服务器启动失败", zap.Error(err))
		}
	}()

	// 监听系统信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit,
		syscall.SIGINT,  // Ctrl+C
		syscall.SIGTERM, // kill 命令
		syscall.SIGQUIT, // Ctrl+\
	)

	sig := <-quit
	app.ZapLog.Info("收到退出信号，开始优雅关闭...", zap.String("signal", sig.String()))

	// 设置超时上下文，防止关闭时间过长
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 优雅关闭服务器
	if err := server.Shutdown(ctx); err != nil {
		app.ZapLog.Error("服务器关闭失败", zap.Error(err))
		return err
	}

	// 停止任务结果处理器
	// 注意：必须在停止调度器之前停止，确保所有结果都被保存
	app.ZapLog.Info("正在停止任务结果处理器...")
	scheduler.StopResultHandler()
	app.ZapLog.Info("任务结果处理器已停止")

	// 停止任务调度器
	if app.JobScheduler != nil {
		app.ZapLog.Info("正在停止任务调度器...")
		app.JobScheduler.Stop()
		app.ZapLog.Info("任务调度器已停止")
	}

	app.ZapLog.Info("服务器已优雅关闭")
	return nil
}

// Emit registered routes after registration, including release mode. This does
// not change which routes are registered or their access control.
func logRoutes(engine *gin.Engine) {
	if !app.ConfigYml.GetBool("logs.routes") {
		return
	}
	for _, route := range engine.Routes() {
		app.Log(context.Background()).Named("routes").Info("HTTP route registered", zap.String("event", "http.route_registered"), zap.String("method", route.Method), zap.String("route", route.Path))
	}
}
