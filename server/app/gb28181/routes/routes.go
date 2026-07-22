package routes

import (
	"sync/atomic"

	"github.com/gin-gonic/gin"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbhandler "uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	gbplay "uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/streammonitor"
	"uvplatform.cn/uvp-gb28181/app/gb28181/streamprobe"
)

var deviceController = gbcontrollers.NewDeviceController()
var catalogTreeController = gbcontrollers.NewCatalogTreeController()
var deviceMgmtController = gbcontrollers.NewDeviceMgmtController()
var mapController = gbcontrollers.NewMapController()
var anomalyController = gbcontrollers.NewAnomalyController()

// streamNotifier 全局流就绪事件分发器(hook 端点 publish,点播 service 订阅)
var streamNotifier = stream.NewNotifier()

// StreamNotifier 暴露给点播 service 使用
func StreamNotifier() *stream.Notifier { return streamNotifier }

var hookController = gbhandler.NewHookController(streamNotifier)

// playController 点播控制器(注入式:bootstrap 在 SIP/ZLM 初始化完成后通过 SetPlayService 设置 svc)
var playController = gbcontrollers.NewPlayController(nil)
var streamMonitorController = gbcontrollers.NewStreamMonitorController(nil)
var streamProbeController = gbcontrollers.NewStreamProbeController(nil)
var playService *gbplay.Service
var recordingService *gbrecording.Service
var cloudRecordingController = gbcontrollers.NewCloudRecordingController(nil)

// dashboardController SIP 监控看板控制器
// provider 由 bootstrap 注入(指向 gb28181.MetricsAggregator)
var dashboardController = gbcontrollers.NewDashboardController(nil)

// platformController 本级 SIP 平台接入信息(只读配置)
var platformController = gbcontrollers.NewPlatformController()

var setupController *gbcontrollers.SetupController

// traceController 由 bootstrap 按 Trace 开关后置注入。
var traceController atomic.Pointer[gbcontrollers.TraceController]

func init() {
	traceController.Store(gbcontrollers.NewTraceController(nil, nil, nil))
}

// zlmNodeController ZLM 节点 CRUD(注入式:bootstrap M1.6 装配 NodeService 后通过 SetZLMNodeController 注入)
var zlmNodeController *gbcontrollers.ZLMNodeController

// zlmConfigController ZLM 节点配置(注入式:同 ZLMNodeController)
var zlmConfigController *gbcontrollers.ZLMConfigController

// zlmSchedulerController ZLM 调度算法切换 + 日志查询(M3 T3.3,后置注入)
var zlmSchedulerController *gbcontrollers.ZLMSchedulerController

// SetMetricsProvider 由 bootstrap 注入聚合器获取函数,绕开循环依赖
func SetMetricsProvider(p gbcontrollers.AggregatorProvider) {
	dashboardController = gbcontrollers.NewDashboardController(p)
}

func SetSetupController(controller *gbcontrollers.SetupController) { setupController = controller }

func SetPlatformController(controller *gbcontrollers.PlatformController) {
	platformController = controller
}

func SetTraceController(ctrl *gbcontrollers.TraceController) {
	if ctrl == nil {
		traceController.Store(gbcontrollers.NewTraceController(nil, nil, nil))
		return
	}
	traceController.Store(ctrl)
}

func currentTraceController() *gbcontrollers.TraceController {
	ctrl := traceController.Load()
	if ctrl == nil {
		ctrl = gbcontrollers.NewTraceController(nil, nil, nil)
		traceController.CompareAndSwap(nil, ctrl)
	}
	return ctrl
}

// SetPlayService 由 bootstrap 注入 play service(routes 包先于 service 实例化,故需后置注入)
// 同时把 service 注入到 hookController(无人观看 / RTP 超时 自动断流)
func SetPlayService(svc *gbplay.Service) {
	playService = svc
	rebuildPlayController()
	if svc == nil {
		hookController.SetPlayStopper(nil)
		hookController.SetNoneReaderPolicy(nil)
		return
	}
	hookController.SetPlayStopper(svc)
	hookController.SetNoneReaderPolicy(svc)
}

func SetStreamMonitorService(service *streammonitor.Service) {
	streamMonitorController = gbcontrollers.NewStreamMonitorController(service)
}

func SetStreamProbeService(service *streamprobe.Service) {
	streamProbeController = gbcontrollers.NewStreamProbeController(service)
}

func rebuildPlayController() {
	if recordingService != nil {
		playController = gbcontrollers.NewPlayController(playService,
			gbcontrollers.WithStreamRetentionPolicy(recordingService))
		return
	}
	playController = gbcontrollers.NewPlayController(playService)
}

// SetZLMNodeController 由 bootstrap M1.6 注入(同 SetPlayService 模式)
func SetZLMNodeController(ctrl *gbcontrollers.ZLMNodeController) {
	zlmNodeController = ctrl
}

// SetZLMConfigController 由 bootstrap M1.6 注入
func SetZLMConfigController(ctrl *gbcontrollers.ZLMConfigController) {
	zlmConfigController = ctrl
}

// SetZLMSchedulerController 由 bootstrap M3 T3.3 注入(算法切换 + 日志)
func SetZLMSchedulerController(ctrl *gbcontrollers.ZLMSchedulerController) {
	zlmSchedulerController = ctrl
}

// SetKeepaliveCollector 由 bootstrap M2.1 注入(同 SetPlayService 模式)
// 把 heartbeat.Collector 通过 KeepaliveCollector 接口挂到 hookController
func SetKeepaliveCollector(c gbhandler.KeepaliveCollector) {
	hookController.SetKeepaliveCollector(c)
}

// SetDeviceMgmtCatalogTrigger 由 bootstrap 在 SIP UAC 就绪后注入
// handler.CatalogTrigger 满足 controllers.CatalogTrigger 接口(duck typing)
func SetDeviceMgmtCatalogTrigger(t gbhandler.CatalogTrigger) {
	deviceMgmtController.SetCatalogTrigger(t)
}

func SetDeviceMgmtSubscriptionManager(manager gbcontrollers.SubscriptionManager) {
	deviceMgmtController.SetSubscriptionManager(manager)
}

func SetDeviceMgmtPTZSender(sender gbcontrollers.DeviceControlSender) {
	deviceMgmtController.SetPTZSender(sender)
}

func SetDeviceMgmtPTZService(service *ptz.Service) {
	deviceMgmtController.SetPTZService(service)
}

// SetHookMultiNode 由 bootstrap M2.4 注入多节点反向 Bind 能力
// 让 OnStreamChanged 收到 payload.mediaServerId 后,反查 nodeID 给 LocationMap.Bind 兜底
func SetHookMultiNode(resolver gbhandler.NodeUUIDResolver, binder gbhandler.StreamLocationBinder) {
	hookController.SetMultiNode(resolver, binder)
}

func SetRecordingService(service *gbrecording.Service, resolver gbhandler.NodeUUIDResolver, indexer gbhandler.RecordMP4Indexer) {
	recordingService = service
	if service == nil {
		cloudRecordingController.SetManager(nil)
	} else {
		cloudRecordingController.SetManager(service)
	}
	rebuildPlayController()
	hookController.SetRecordMP4Indexer(resolver, indexer)
	if service == nil {
		hookController.SetStreamObserver(nil)
		return
	}
	hookController.SetStreamObserver(service)
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
			dev.PATCH("/:deviceId", deviceController.Update)
			dev.GET("/:deviceId/channels", deviceController.ListChannels)
		}
		// 点播:用闭包间接调用,以便后置注入的 playController 也能命中
		play := gb.Group("/play")
		{
			play.POST("/:deviceId/:channelId", func(c *gin.Context) { playController.Start(c) })
			play.DELETE("/:streamId", func(c *gin.Context) { playController.Stop(c) })
			play.GET("/:streamId/monitor", func(c *gin.Context) { streamMonitorController.Get(c) })
			// Gin requires wildcard names at the same path depth to match the start-play route.
			play.POST("/:deviceId/probe", func(c *gin.Context) { streamProbeController.Run(c) })
		}
		// SIP 信令看板(只读快照接口,后续 T2.1 加 SSE /stream)
		sipGroup := gb.Group("/sip/dashboard")
		{
			sipGroup.GET("/snapshot", func(c *gin.Context) { dashboardController.Snapshot(c) })
			sipGroup.GET("/stream", func(c *gin.Context) { dashboardController.Stream(c) })
		}
		gb.GET("/sip/platform", func(c *gin.Context) { platformController.Info(c) })
		setup := gb.Group("/sip/setup")
		{
			setup.GET("/status", setupRoute(func(controller *gbcontrollers.SetupController, c *gin.Context) { controller.Status(c) }))
			setup.GET("/network-interfaces", setupRoute(func(controller *gbcontrollers.SetupController, c *gin.Context) { controller.NetworkInterfaces(c) }))
			setup.PUT("/config", setupRoute(func(controller *gbcontrollers.SetupController, c *gin.Context) { controller.SaveConfig(c) }))
			setup.POST("/skip", setupRoute(func(controller *gbcontrollers.SetupController, c *gin.Context) { controller.Skip(c) }))
		}
		traceGroup := gb.Group("/sip-traces")
		{
			traceGroup.GET("/health", func(c *gin.Context) { currentTraceController().Health(c) })
			traceGroup.GET("/messages", func(c *gin.Context) { currentTraceController().ListMessages(c) })
			traceGroup.GET("/messages/:id", func(c *gin.Context) { currentTraceController().GetMessage(c) })
			traceGroup.GET("/sessions", func(c *gin.Context) { currentTraceController().ListSessions(c) })
			traceGroup.GET("/sessions/stats", func(c *gin.Context) { currentTraceController().SessionStats(c) })
			traceGroup.GET("/stream", func(c *gin.Context) { currentTraceController().Stream(c) })
			traceGroup.GET("/sessions/:callId/messages", func(c *gin.Context) { currentTraceController().ListSessionMessages(c) })
			traceGroup.POST("/captures/:id/stop", func(c *gin.Context) { currentTraceController().StopCapture(c) })
		}
		// ZLM 集群管理(M1+,后置注入 zlmNodeController)
		zlm := gb.Group("/zlm")
		{
			zlm.GET("/nodes", zlmNodeRoute(func(ctrl *gbcontrollers.ZLMNodeController, c *gin.Context) { ctrl.List(c) }))
			zlm.POST("/nodes", zlmNodeRoute(func(ctrl *gbcontrollers.ZLMNodeController, c *gin.Context) { ctrl.Create(c) }))
			zlm.GET("/nodes/:id", zlmNodeRoute(func(ctrl *gbcontrollers.ZLMNodeController, c *gin.Context) { ctrl.Get(c) }))
			zlm.PUT("/nodes/:id", zlmNodeRoute(func(ctrl *gbcontrollers.ZLMNodeController, c *gin.Context) { ctrl.Update(c) }))
			zlm.DELETE("/nodes/:id", zlmNodeRoute(func(ctrl *gbcontrollers.ZLMNodeController, c *gin.Context) { ctrl.Delete(c) }))
			zlm.POST("/nodes/:id/maintenance", zlmNodeRoute(func(ctrl *gbcontrollers.ZLMNodeController, c *gin.Context) { ctrl.SetMaintenance(c) }))
			zlm.POST("/nodes/:id/activate", zlmNodeRoute(func(ctrl *gbcontrollers.ZLMNodeController, c *gin.Context) { ctrl.Activate(c) }))
			zlm.POST("/nodes/:id/kick", zlmNodeRoute(func(ctrl *gbcontrollers.ZLMNodeController, c *gin.Context) { ctrl.KickSessions(c) }))
			zlm.POST("/nodes/:id/restart", zlmNodeRoute(func(ctrl *gbcontrollers.ZLMNodeController, c *gin.Context) { ctrl.Restart(c) }))
			// 配置子路由(共享同一 group,挂在 /nodes/:id/config)
			zlm.GET("/nodes/:id/config", zlmConfigRoute(func(ctrl *gbcontrollers.ZLMConfigController, c *gin.Context) { ctrl.Get(c) }))
			zlm.PUT("/nodes/:id/config", zlmConfigRoute(func(ctrl *gbcontrollers.ZLMConfigController, c *gin.Context) { ctrl.Update(c) }))
			zlm.POST("/nodes/:id/config/test-connection", zlmConfigRoute(func(ctrl *gbcontrollers.ZLMConfigController, c *gin.Context) { ctrl.TestConnection(c) }))
			// 调度算法 + 调度日志(M3 T3.3)
			zlm.GET("/scheduler", zlmSchedulerRoute(func(ctrl *gbcontrollers.ZLMSchedulerController, c *gin.Context) { ctrl.GetScheduler(c) }))
			zlm.PUT("/scheduler", zlmSchedulerRoute(func(ctrl *gbcontrollers.ZLMSchedulerController, c *gin.Context) { ctrl.SwitchScheduler(c) }))
			zlm.GET("/scheduler/logs", zlmSchedulerRoute(func(ctrl *gbcontrollers.ZLMSchedulerController, c *gin.Context) { ctrl.ListSchedulerLogs(c) }))
		}
		// 设备管理页(B1-B4 新建,plan §4.2)
		dmgmt := gb.Group("/device-mgmt")
		{
			// B1 catalogtree:目录树
			dmgmt.GET("/catalog/tree", catalogTreeController.Tree)
			dmgmt.GET("/catalog/tree/:id", catalogTreeController.Node)
			dmgmt.GET("/catalog/tree/:id/children", catalogTreeController.Children)
			dmgmt.GET("/catalog/tree/:id/subtree", catalogTreeController.Subtree)
			dmgmt.GET("/catalog/anomaly/count", catalogTreeController.AnomalyCount)
			// B2 devicemgmt:设备列表 + 通道列表 + 详情 + 多挂载 + timeline
			dmgmt.GET("/devices", deviceMgmtController.ListDevices)
			dmgmt.POST("/device", deviceMgmtController.CreateDevice)
			dmgmt.GET("/device/:id", deviceMgmtController.GetDevice)
			dmgmt.GET("/device/:id/status-events", deviceMgmtController.ListDeviceStatusEvents)
			dmgmt.GET("/device/:id/subscriptions", deviceMgmtController.ListSubscriptions)
			dmgmt.PATCH("/device/:id/subscriptions/:kind", deviceMgmtController.UpdateSubscription)
			dmgmt.POST("/device/:id/subscriptions/:kind/renew", deviceMgmtController.RenewSubscription)
			dmgmt.GET("/device/:id/alarms", deviceMgmtController.ListAlarms)
			dmgmt.GET("/channels", deviceMgmtController.ListChannels)
			dmgmt.GET("/channel/:id", deviceMgmtController.GetChannel)
			dmgmt.PATCH("/channel/:id", deviceMgmtController.UpdateChannel)
			dmgmt.PATCH("/channel/:id/cloud-recording", cloudRecordingController.Update)
			dmgmt.GET("/channel/:id/mounts", deviceMgmtController.ListChannelMounts)
			dmgmt.GET("/channel/:id/timeline", deviceMgmtController.ChannelTimeline)
			dmgmt.POST("/channel/:id/ptz", deviceMgmtController.ControlPTZ)
			dmgmt.POST("/channel/:id/ptz/precise", deviceMgmtController.ControlPTZPrecise)
			dmgmt.POST("/channel/:id/ptz/extended", deviceMgmtController.ControlPTZExtended)
			dmgmt.POST("/channel/:id/ptz/presets", deviceMgmtController.ControlPTZExtended)
			dmgmt.POST("/channel/:id/ptz/presets/:presetId/call", deviceMgmtController.ControlPTZExtended)
			dmgmt.GET("/channel/:id/ptz/presets", deviceMgmtController.ListPTZPresets)
			dmgmt.GET("/channel/:id/ptz/precise-status", deviceMgmtController.GetPTZState)
			dmgmt.GET("/channel/:id/ptz/cruise-tracks", deviceMgmtController.ListCruiseTracks)
			dmgmt.GET("/channel/:id/ptz/cruise-tracks/:trackId", deviceMgmtController.GetCruiseTrack)
			dmgmt.GET("/channel/:id/ptz/operations/:operationId", deviceMgmtController.GetPTZOperation)
			dmgmt.PATCH("/channel/:id/stream-transport", deviceMgmtController.UpdateChannelStreamTransport)
			// 删除(单/批,硬 cascade — 用户主动删)
			dmgmt.DELETE("/device/:id", deviceMgmtController.DeleteDevice)
			dmgmt.POST("/device/batch-delete", deviceMgmtController.BatchDeleteDevices)
			dmgmt.DELETE("/channel/:id", deviceMgmtController.DeleteChannel)
			dmgmt.POST("/channel/batch-delete", deviceMgmtController.BatchDeleteChannels)
			// 手动 Catalog 刷新(bootstrap 未装配 CatalogTrigger 时,handler 内部返 503)
			dmgmt.POST("/device/:id/catalog/refresh", deviceMgmtController.RefreshDeviceCatalog)
			dmgmt.GET("/device/:id/sip-trace-capture", func(c *gin.Context) { currentTraceController().ActiveCapture(c) })
			dmgmt.POST("/device/:id/sip-trace-captures", func(c *gin.Context) { currentTraceController().StartCapture(c) })
			// B3 map:地图视图
			dmgmt.GET("/map/markers", mapController.Markers)
			dmgmt.GET("/map/clusters", mapController.Clusters)
			dmgmt.GET("/map/no-coord-count", mapController.NoCoordCount)
			// B4 anomaly:异常治理
			dmgmt.GET("/anomaly", anomalyController.List)
			dmgmt.POST("/anomaly/:id/resolve", anomalyController.Resolve)
			dmgmt.POST("/anomaly/batch-resolve", anomalyController.BatchResolve)
		}
	}
}

func setupRoute(fn func(*gbcontrollers.SetupController, *gin.Context)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if setupController == nil {
			c.JSON(503, gin.H{"code": 503, "msg": "SIP 配置服务尚未装配"})
			return
		}
		fn(setupController, c)
	}
}

// zlmNodeRoute 包裹 handler,未注入时返 503(单节点过渡期 / 配置禁用容错)
func zlmNodeRoute(fn func(*gbcontrollers.ZLMNodeController, *gin.Context)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if zlmNodeController == nil {
			c.JSON(503, gin.H{"code": 503, "msg": "ZLM 节点服务尚未装配"})
			return
		}
		fn(zlmNodeController, c)
	}
}

// zlmConfigRoute 同上
func zlmConfigRoute(fn func(*gbcontrollers.ZLMConfigController, *gin.Context)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if zlmConfigController == nil {
			c.JSON(503, gin.H{"code": 503, "msg": "ZLM 配置服务尚未装配"})
			return
		}
		fn(zlmConfigController, c)
	}
}

// zlmSchedulerRoute 同上(M3 T3.3)
func zlmSchedulerRoute(fn func(*gbcontrollers.ZLMSchedulerController, *gin.Context)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if zlmSchedulerController == nil {
			c.JSON(503, gin.H{"code": 503, "msg": "ZLM Scheduler controller 尚未装配"})
			return
		}
		fn(zlmSchedulerController, c)
	}
}

// RegisterHookRoutes 注册 ZLMediaKit Hook 回调端点到 engine 根(无 /api 前缀,无鉴权)
// ZLM 以 POST JSON 回调,路径 /index/hook/*
func RegisterHookRoutes(engine *gin.Engine) {
	hook := engine.Group("/index/hook")
	{
		hook.POST("/on_server_started", hookController.OnServerStarted)
		hook.POST("/on_server_keepalive", hookController.OnServerKeepalive)
		hook.POST("/on_stream_changed", hookController.OnStreamChanged)
		hook.POST("/on_stream_none_reader", hookController.OnStreamNoneReader)
		hook.POST("/on_rtp_server_timeout", hookController.OnRtpServerTimeout)
		hook.POST("/on_publish", hookController.OnPublish)
		hook.POST("/on_play", hookController.OnPlay)
		hook.POST("/on_record_mp4", hookController.OnRecordMP4)
	}
}
