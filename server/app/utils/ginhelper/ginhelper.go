package ginhelper

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"

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

func accessLogger(output io.Writer) gin.HandlerFunc {
	return gin.LoggerWithConfig(gin.LoggerConfig{
		Formatter: accessLogFormatter,
		Output:    output,
	})
}

func accessLogFormatter(param gin.LogFormatterParams) string {
	path := redactAccessLogPath(param.Path)
	method := param.Method
	errorMessage := param.ErrorMessage
	if isOpenAPIAccessLogPath(param.Path) {
		// OpenAPI authentication material is not safe to copy into the general
		// access log, including through Gin's error string.
		errorMessage = ""
		if !isStandardAccessLogMethod(method) {
			method = "UNKNOWN"
		}
	}
	return fmt.Sprintf("[GIN] %v | %3d | %13v | %15s | %-7s %#v\n%s",
		param.TimeStamp.Format("2006/01/02 - 15:04:05"),
		param.StatusCode,
		param.Latency,
		param.ClientIP,
		method,
		path,
		errorMessage,
	)
}

func isStandardAccessLogMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions,
		http.MethodTrace:
		return true
	default:
		return false
	}
}

func redactAccessLogPath(path string) string {
	if isOpenAPIAccessLogPath(path) {
		return "/openapi"
	}

	parsed, err := url.ParseRequestURI(path)
	if err != nil {
		return redactAccessLogPathFallback(path)
	}
	query := parsed.Query()
	redacted := false
	for _, key := range []string{"play_token", "media_access_token", "cap"} {
		if _, exists := query[key]; exists {
			query.Set(key, "REDACTED")
			redacted = true
		}
	}
	if !redacted {
		return path
	}
	parsed.RawQuery = query.Encode()
	return parsed.RequestURI()
}

func redactAccessLogPathFallback(path string) string {
	lower := strings.ToLower(path)
	if strings.Contains(lower, "play_token=") || strings.Contains(lower, "media_access_token=") || strings.Contains(lower, "cap=") {
		if index := strings.IndexByte(path, '?'); index >= 0 {
			return path[:index] + "?REDACTED"
		}
	}
	return path
}

func isOpenAPIAccessLogPath(path string) bool {
	pathEnd := len(path)
	if index := strings.IndexAny(path, "?#"); index >= 0 {
		pathEnd = index
	}
	rawPath := path[:pathEnd]
	if rawPath == "" || rawPath[0] != '/' {
		return false
	}

	if decodedPath, err := url.PathUnescape(rawPath); err == nil {
		return isOpenAPIAccessLogPathValue(decodedPath)
	}

	// A malformed suffix must not bypass the namespace redaction. Decode only
	// the first segment, which is enough to identify the fixed namespace.
	firstSegment := rawPath[1:]
	if separator := strings.IndexByte(firstSegment, '/'); separator >= 0 {
		firstSegment = firstSegment[:separator]
	}
	if separator := strings.Index(strings.ToLower(firstSegment), "%2f"); separator >= 0 {
		firstSegment = firstSegment[:separator]
	}
	if decodedSegment, err := url.PathUnescape(firstSegment); err == nil {
		return strings.EqualFold(decodedSegment, "openapi") || strings.HasPrefix(strings.ToLower(decodedSegment), "openapi/")
	}
	return strings.EqualFold(firstSegment, "openapi") || strings.HasPrefix(strings.ToLower(firstSegment), "openapi%")
}

func isOpenAPIAccessLogPathValue(path string) bool {
	return strings.EqualFold(path, "/openapi") || strings.HasPrefix(strings.ToLower(path), "/openapi/")
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

// StartServer binds HTTP before announcing readiness. Application shutdown is
// supplied by main so SIP remains available to in-flight HTTP and job work.
func StartServer(engine *gin.Engine, stop func(context.Context) error) error {
	logRoutes(engine)
	server := &http.Server{
		Addr:         app.ConfigYml.GetString("httpserver.port"),
		Handler:      engine,
		ReadTimeout:  time.Duration(app.ConfigYml.GetInt("httpserver.read_timeout")) * time.Second,
		WriteTimeout: time.Duration(app.ConfigYml.GetInt("httpserver.write_timeout")) * time.Second,
		IdleTimeout:  time.Duration(app.ConfigYml.GetInt("httpserver.idle_timeout")) * time.Second,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer cancel()
	return serveUntilCanceled(ctx, server, app.Log(context.Background()), stop)
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
