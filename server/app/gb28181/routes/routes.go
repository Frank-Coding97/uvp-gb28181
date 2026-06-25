package routes

import (
	"github.com/gin-gonic/gin"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbhandler "uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	gbplay "uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
)

var deviceController = gbcontrollers.NewDeviceController()

// streamNotifier 全局流就绪事件分发器(hook 端点 publish,点播 service 订阅)
var streamNotifier = stream.NewNotifier()

// StreamNotifier 暴露给点播 service 使用
func StreamNotifier() *stream.Notifier { return streamNotifier }

var hookController = gbhandler.NewHookController(streamNotifier)

// playController 点播控制器(注入式:bootstrap 在 SIP/ZLM 初始化完成后通过 SetPlayService 设置 svc)
var playController = gbcontrollers.NewPlayController(nil)

// dashboardController SIP 监控看板控制器
// provider 由 bootstrap 注入(指向 gb28181.MetricsAggregator)
var dashboardController = gbcontrollers.NewDashboardController(nil)

// SetMetricsProvider 由 bootstrap 注入聚合器获取函数,绕开循环依赖
func SetMetricsProvider(p gbcontrollers.AggregatorProvider) {
	dashboardController = gbcontrollers.NewDashboardController(p)
}

// SetPlayService 由 bootstrap 注入 play service(routes 包先于 service 实例化,故需后置注入)
// 同时把 service 注入到 hookController(无人观看 / RTP 超时 自动断流)
func SetPlayService(svc *gbplay.Service) {
	playController = gbcontrollers.NewPlayController(svc)
	hookController.SetPlayStopper(svc)
}

// RegisterRoutes 注册 GB28181 业务路由到已带鉴权的 protected 组
// 在底座 routes.InitRoutes 的 protected 块中调用
func RegisterRoutes(protected *gin.RouterGroup) {
	gb := protected.Group("/gb28181")
	{
		dev := gb.Group("/device")
		{
			dev.GET("/list", deviceController.List)
			dev.GET("/:deviceId", deviceController.GetByDeviceID)
			dev.GET("/:deviceId/channels", deviceController.ListChannels)
		}
		// 点播:用闭包间接调用,以便后置注入的 playController 也能命中
		play := gb.Group("/play")
		{
			play.POST("/:deviceId/:channelId", func(c *gin.Context) { playController.Start(c) })
			play.DELETE("/:streamId", func(c *gin.Context) { playController.Stop(c) })
		}
		// SIP 信令看板(只读快照接口,后续 T2.1 加 SSE /stream)
		sipGroup := gb.Group("/sip/dashboard")
		{
			sipGroup.GET("/snapshot", func(c *gin.Context) { dashboardController.Snapshot(c) })
			sipGroup.GET("/stream", func(c *gin.Context) { dashboardController.Stream(c) })
		}
	}
}

// RegisterHookRoutes 注册 ZLMediaKit Hook 回调端点到 engine 根(无 /api 前缀,无鉴权)
// ZLM 以 POST JSON 回调,路径 /index/hook/*
func RegisterHookRoutes(engine *gin.Engine) {
	hook := engine.Group("/index/hook")
	{
		hook.POST("/on_server_started", hookController.OnServerStarted)
		hook.POST("/on_stream_changed", hookController.OnStreamChanged)
		hook.POST("/on_stream_none_reader", hookController.OnStreamNoneReader)
		hook.POST("/on_rtp_server_timeout", hookController.OnRtpServerTimeout)
		hook.POST("/on_publish", hookController.OnPublish)
		hook.POST("/on_play", hookController.OnPlay)
	}
}
