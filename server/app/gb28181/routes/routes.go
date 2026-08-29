package routes

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"gorm.io/gorm"
	gbcascadecontroller "uvplatform.cn/uvp-gb28181/app/gb28181/cascade/controller"
	gbcascadeservice "uvplatform.cn/uvp-gb28181/app/gb28181/cascade/service"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbhandler "uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	gbplay "uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordquery"
	gbsecurity "uvplatform.cn/uvp-gb28181/app/gb28181/security"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/streammonitor"
	"uvplatform.cn/uvp-gb28181/app/gb28181/streamprobe"
	"uvplatform.cn/uvp-gb28181/app/gb28181/talk"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

var deviceController = gbcontrollers.NewDeviceController()
var catalogTreeController = gbcontrollers.NewCatalogTreeController()
var directoryController = gbcontrollers.NewDirectoryController()
var customGroupController = gbcontrollers.NewCustomGroupController()
var playbackSchemeController = gbcontrollers.NewPlaybackSchemeController()
var deviceMgmtController = gbcontrollers.NewDeviceMgmtController()
var mapController = gbcontrollers.NewMapController()
var anomalyController = gbcontrollers.NewAnomalyController()
var alarmController = gbcontrollers.NewAlarmController()
var channelFavoriteController = gbcontrollers.NewChannelFavoriteController()

// streamNotifier 全局流就绪事件分发器(hook 端点 publish,点播 service 订阅)
var streamNotifier = stream.NewNotifier()

// StreamNotifier 暴露给点播 service 使用
func StreamNotifier() *stream.Notifier { return streamNotifier }

var hookController = gbhandler.NewHookController(streamNotifier)

// playController 点播控制器(注入式:bootstrap 在 SIP/ZLM 初始化完成后通过 SetPlayService 设置 svc)
var playController = gbcontrollers.NewPlayController(nil)
var streamMonitorController = gbcontrollers.NewStreamMonitorController(nil)
var streamProbeController = gbcontrollers.NewStreamProbeController(nil)
var talkController atomic.Pointer[gbcontrollers.TalkController]
var playService *gbplay.Service
var autoOnDemandMu sync.Mutex
var autoOnDemandPlayService *gbplay.Service
var autoOnDemandRegistry autoOnDemandNodeRegistry
var autoOnDemandDispatcher *gbplay.AutoStartDispatcher
var recordingService *gbrecording.Service
var cloudRecordingController = gbcontrollers.NewCloudRecordingController(nil)
var cloudRecordingCatalogController atomic.Pointer[gbcontrollers.CloudRecordingCatalogController]
var deviceTrafficController atomic.Pointer[gbcontrollers.DeviceTrafficController]

// dashboardController SIP 监控看板控制器
// provider 由 bootstrap 注入(指向 gb28181.MetricsAggregator)
var dashboardController = gbcontrollers.NewDashboardController(nil)

// platformController 本级 SIP 平台接入信息(只读配置)
var platformController = gbcontrollers.NewPlatformController()

// serviceConfigController 国标服务配置页面的动态配置控制器。
var serviceConfigController = gbcontrollers.NewServiceConfigController()
var securityController = gbcontrollers.NewSecurityController(nil)
var cascadeManagementController *gbcascadecontroller.ManagementController

var setupController *gbcontrollers.SetupController

// qrController 扫码接入二维码(token 生成 + 免鉴权兑换),由 bootstrap 后置注入
var qrController = gbcontrollers.NewQRController()

// traceController 由 bootstrap 按 Trace 开关后置注入。
var traceController atomic.Pointer[gbcontrollers.TraceController]

func init() {
	traceController.Store(gbcontrollers.NewTraceController(nil, nil, nil))
	talkController.Store(gbcontrollers.NewTalkController(nil))
	cloudRecordingCatalogController.Store(gbcontrollers.NewCloudRecordingCatalogController(nil))
	deviceTrafficController.Store(gbcontrollers.NewDeviceTrafficController(nil, nil, nil, nil))
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

func SetServiceConfigSIPTraceReloader(reload gbcontrollers.SIPTraceReloader) {
	serviceConfigController.SetSIPTraceReloader(reload)
}

func SetServiceConfigSIPTraceRuntimeProvider(provider gbcontrollers.SIPTraceRuntimeProvider) {
	serviceConfigController.SetSIPTraceRuntimeProvider(provider)
}

func SetServiceConfigPlayAuthTTLUpdater(updater gbcontrollers.PlayAuthTTLUpdater) {
	serviceConfigController.SetPlayAuthTTLUpdater(updater)
}

func SetPlatformController(controller *gbcontrollers.PlatformController) {
	platformController = controller
}

func SetQRController(controller *gbcontrollers.QRController) { qrController = controller }

func SetTraceController(ctrl *gbcontrollers.TraceController) {
	if ctrl == nil {
		traceController.Store(gbcontrollers.NewTraceController(nil, nil, nil))
		return
	}
	traceController.Store(ctrl)
}

// SetSecurityProvider 由 bootstrap 注入安全聚合/封禁/agent provider。
func SetSecurityProvider(provider gbcontrollers.SecurityProvider) {
	securityController.SetProvider(provider)
}

// SetCascadeManagementController injects the cascade service after database/runtime bootstrap.
// Passing nil deliberately makes the protected endpoints return 503 until the runtime is ready.
func SetCascadeManagementController(controller *gbcascadecontroller.ManagementController) {
	cascadeManagementController = controller
}

// SetCascadeManagementService is a convenience for bootstrap code that already owns the DB handle.
func SetCascadeManagementService(service *gbcascadeservice.ManagementService, db *gorm.DB) {
	if service == nil {
		cascadeManagementController = nil
		return
	}
	cascadeManagementController = gbcascadecontroller.NewManagementController(service, db)
}

type securityRuntimeProvider struct{ runtime *gbsecurity.Runtime }

func (p securityRuntimeProvider) Snapshot() gbcontrollers.SecuritySnapshot {
	s := p.runtime.Snapshot()
	return gbcontrollers.SecuritySnapshot{Mode: s.Mode, Dropped: s.Dropped, Sampled: s.Sampled, Events: s.Events, Bans: s.Bans, Agent: s.Agent, AsOf: s.AsOf}
}
func (p securityRuntimeProvider) Events() []gbsecurity.EventAggregate { return p.runtime.Events() }
func (p securityRuntimeProvider) Bans() []gbsecurity.FirewallBan      { return p.runtime.Bans() }
func (p securityRuntimeProvider) Policy() gbsecurity.SecurityPolicy   { return p.runtime.Policy() }
func (p securityRuntimeProvider) UpdatePolicy(policy gbsecurity.SecurityPolicy, actor string) error {
	return p.runtime.UpdatePolicy(policy, actor)
}
func (p securityRuntimeProvider) Unban(id, actor string) error { return p.runtime.Unban(id, actor) }
func (p securityRuntimeProvider) AccessRules(listType gbsecurity.AccessListType) []gbsecurity.AccessRule {
	return p.runtime.AccessRules(listType)
}
func (p securityRuntimeProvider) CreateAccessRule(rule *gbsecurity.AccessRule, actor string) error {
	return p.runtime.CreateAccessRule(rule, actor)
}
func (p securityRuntimeProvider) UpdateAccessRule(rule gbsecurity.AccessRule, actor string) error {
	return p.runtime.UpdateAccessRule(rule, actor)
}
func (p securityRuntimeProvider) DeleteAccessRule(id uint64, actor string) error {
	return p.runtime.DeleteAccessRule(id, actor)
}
func (p securityRuntimeProvider) AgentStatus() gbsecurity.AgentStatus { return p.runtime.AgentStatus() }
func (p securityRuntimeProvider) Stream() (<-chan gbcontrollers.SecuritySnapshot, func()) {
	in, cancel := p.runtime.Stream()
	out := make(chan gbcontrollers.SecuritySnapshot, 8)
	go func() {
		defer close(out)
		for range in {
			out <- securityRuntimeProvider{runtime: p.runtime}.Snapshot()
		}
	}()
	return out, cancel
}

// SetSecurityRuntime wires the application runtime into the protected API.
func SetSecurityRuntime(runtime *gbsecurity.Runtime) {
	if runtime == nil {
		securityController.SetProvider(nil)
		return
	}
	securityController.SetProvider(securityRuntimeProvider{runtime: runtime})
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
		hookController.SetPlaybackMediaContextResolver(nil)
		configureAutoOnDemandService(nil)
		return
	}
	if recordingService == nil {
		svc.SetPlaybackRecordingLifecycle(nil)
	} else {
		svc.SetPlaybackRecordingLifecycle(recordingService)
	}
	hookController.SetPlayStopper(svc)
	hookController.SetNoneReaderPolicy(svc)
	hookController.SetPlaybackMediaContextResolver(svc)
	configureAutoOnDemandService(svc)
}

func SetPlayAuthorizer(authorizer gbhandler.PlayAuthorizer) {
	hookController.SetPlayAuthorizer(authorizer)
	gbcontrollers.SetPlayAuthRuntimeReady(authorizer != nil)
	if updater, ok := authorizer.(interface{ SetTTL(time.Duration) error }); ok {
		SetServiceConfigPlayAuthTTLUpdater(updater.SetTTL)
	} else {
		SetServiceConfigPlayAuthTTLUpdater(nil)
	}
}

func SetStreamMonitorService(service *streammonitor.Service) {
	streamMonitorController = gbcontrollers.NewStreamMonitorController(service)
}

func SetStreamProbeService(service *streamprobe.Service) {
	streamProbeController = gbcontrollers.NewStreamProbeController(service)
}

type talkHookAdapter struct{ service *talk.Service }

func (a talkHookAdapter) AuthorizeTalkPublish(ctx context.Context, request gbhandler.TalkPublishRequest) (bool, error) {
	return a.service.AuthorizePublish(ctx, talk.PublishAuthorization{
		NodeID: request.NodeID, App: request.App, SourceStream: request.SourceStream,
		PublishToken: request.PublishToken, PublishID: request.PublishID,
	})
}

func (a talkHookAdapter) ObserveTalkStream(ctx context.Context, nodeID int64, appName, sourceStream string, registered bool) error {
	if registered {
		return a.service.OnPublished(ctx, nodeID, appName, sourceStream)
	}
	return a.service.OnUnpublished(ctx, nodeID, appName, sourceStream)
}

func SetTalkService(service *talk.Service, resolver gbhandler.NodeUUIDResolver) {
	if service == nil || resolver == nil {
		talkController.Store(gbcontrollers.NewTalkController(nil))
		hookController.SetTalk(nil, nil, nil)
		return
	}
	talkController.Store(gbcontrollers.NewTalkController(service))
	adapter := talkHookAdapter{service: service}
	hookController.SetTalk(resolver, adapter, adapter)
}

func rebuildPlayController() {
	if recordingService != nil {
		playController = gbcontrollers.NewPlayController(playService,
			gbcontrollers.WithPlaybackRecordingStarter(recordingService))
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

// SetCascadeSourceLeaseChecker keeps cascade-owned sources alive while ZLM
// reports no browser readers; the existing none-reader policy remains the
// final browser/reconciler cleanup authority.
func SetCascadeSourceLeaseChecker(checker gbhandler.SourceLeaseChecker) {
	hookController.SetSourceLeaseChecker(checker)
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

func SetDeviceMgmtPTZRuntime(sender gbcontrollers.DeviceControlSender, service *ptz.Service) {
	deviceMgmtController.SetPTZRuntime(sender, service)
}

func SetDeviceMgmtRecordQueryRuntime(service gbcontrollers.RecordQueryService, cfg gbconfig.RecordQueryConfig, metrics *recordquery.Metrics) {
	deviceMgmtController.SetRecordQueryRuntime(service, cfg, metrics)
}

func SetDeviceMgmtPlaybackRuntime(service gbcontrollers.PlaybackSessionService, snapshots gbcontrollers.PlaybackSnapshotResolver) {
	deviceMgmtController.SetPlaybackRuntime(service, snapshots)
}

// SetHookMultiNode 由 bootstrap M2.4 注入多节点反向 Bind 能力
// 让 OnStreamChanged 收到 payload.mediaServerId 后,反查 nodeID 给 LocationMap.Bind 兜底
func SetHookMultiNode(resolver gbhandler.NodeUUIDResolver, binder gbhandler.StreamLocationBinder) {
	hookController.SetMultiNode(resolver, binder)
	registry, _ := resolver.(autoOnDemandNodeRegistry)
	configureAutoOnDemandRegistry(registry)
}

type autoOnDemandNodeRegistry interface {
	gbhandler.AutoOnDemandNodeResolver
	Get(int64) (*node.Node, bool)
	IsAutoOnDemandReady(int64) bool
	List() []*node.Node
}

type autoOnDemandTargetValidator struct {
	channels gbplay.ChannelRepo
}

func (v autoOnDemandTargetValidator) ValidateAutoOnDemandTarget(ctx context.Context, deviceID, channelID string) error {
	channel, err := v.channels.FindChannel(ctx, deviceID, channelID)
	if err != nil {
		return err
	}
	if channel == nil {
		return gbplay.ErrChannelNotFound
	}
	return nil
}

func configureAutoOnDemandService(service *gbplay.Service) {
	autoOnDemandMu.Lock()
	defer autoOnDemandMu.Unlock()
	registry := autoOnDemandRegistry
	if service == nil {
		registry = nil
	}
	configureAutoOnDemandRuntimeLocked(service, registry)
}

func configureAutoOnDemandRegistry(registry autoOnDemandNodeRegistry) {
	autoOnDemandMu.Lock()
	defer autoOnDemandMu.Unlock()
	configureAutoOnDemandRuntimeLocked(autoOnDemandPlayService, registry)
}

func configureAutoOnDemandRuntimeLocked(service *gbplay.Service, registry autoOnDemandNodeRegistry) {
	hookController.SetAutoOnDemandRuntime(nil, nil, nil)
	if autoOnDemandDispatcher != nil {
		_ = autoOnDemandDispatcher.Stop()
		autoOnDemandDispatcher = nil
	}
	autoOnDemandPlayService = service
	autoOnDemandRegistry = registry
	if service == nil || registry == nil {
		return
	}

	knownNodeIDs := make([]int64, 0)
	for _, mediaNode := range registry.List() {
		if autoOnDemandNodeAllowed(registry, mediaNode.ID) {
			knownNodeIDs = append(knownNodeIDs, mediaNode.ID)
		}
	}
	dispatcher := gbplay.NewAutoStartDispatcher(service, gbplay.AutoStartDispatcherOptions{
		KnownNodeIDs: knownNodeIDs,
		NodeAllowed: func(nodeID int64) bool {
			return autoOnDemandNodeAllowed(registry, nodeID)
		},
	})
	autoOnDemandDispatcher = dispatcher
	hookController.SetAutoOnDemandRuntime(
		registry,
		autoOnDemandTargetValidator{channels: gbplay.NewChannelRepo()},
		dispatcher,
	)
}

func autoOnDemandNodeAllowed(registry autoOnDemandNodeRegistry, nodeID int64) bool {
	mediaNode, ok := registry.Get(nodeID)
	return ok && mediaNode != nil && mediaNode.IsActive() && !mediaNode.IsNearCapacity() &&
		registry.IsAutoOnDemandReady(nodeID)
}

func SetPlaybackMediaSink(sink gbhandler.PlaybackMediaSink) {
	hookController.SetPlaybackMediaSink(sink)
}

func SetFlowRuntime(resolver gbhandler.FlowReportNodeResolver, collector gbhandler.FlowCollector) {
	hookController.SetFlowRuntime(resolver, collector)
}

func SetDeviceTrafficController(controller *gbcontrollers.DeviceTrafficController) {
	if controller == nil {
		controller = gbcontrollers.NewDeviceTrafficController(nil, nil, nil, nil)
	}
	deviceTrafficController.Store(controller)
}

func currentDeviceTrafficController() *gbcontrollers.DeviceTrafficController {
	controller := deviceTrafficController.Load()
	if controller == nil {
		controller = gbcontrollers.NewDeviceTrafficController(nil, nil, nil, nil)
		deviceTrafficController.CompareAndSwap(nil, controller)
	}
	return controller
}

func SetRecordingService(service *gbrecording.Service, resolver gbhandler.NodeUUIDResolver, indexer gbhandler.RecordMP4Indexer) {
	recordingService = service
	if playService != nil {
		if service == nil {
			playService.SetPlaybackRecordingLifecycle(nil)
		} else {
			playService.SetPlaybackRecordingLifecycle(service)
		}
	}
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

func SetCloudRecordingCatalogService(service gbcontrollers.CloudRecordingCatalogAPI) {
	cloudRecordingCatalogController.Store(gbcontrollers.NewCloudRecordingCatalogController(service))
}

func currentCloudRecordingCatalogController() *gbcontrollers.CloudRecordingCatalogController {
	controller := cloudRecordingCatalogController.Load()
	if controller == nil {
		controller = gbcontrollers.NewCloudRecordingCatalogController(nil)
		cloudRecordingCatalogController.CompareAndSwap(nil, controller)
	}
	return controller
}

// RegisterRoutes 注册 GB28181 业务路由到已带鉴权的 protected 组
// 在底座 routes.InitRoutes 的 protected 块中调用
func RegisterRoutes(protected *gin.RouterGroup) {
	gb := protected.Group("/gb28181")
	{
		favorites := gb.Group("/channel-favorite-groups")
		{
			favorites.GET("", channelFavoriteController.List)
			favorites.POST("", channelFavoriteController.Create)
			favorites.POST("/:id/channels", channelFavoriteController.Append)
			favorites.DELETE("/:id/channels", channelFavoriteController.Remove)
			favorites.DELETE("/:id", channelFavoriteController.Delete)
		}
		cloudRecordings := gb.Group("/cloud-recordings")
		{
			cloudRecordings.GET("/files", func(c *gin.Context) { currentCloudRecordingCatalogController().ListFiles(c) })
			cloudRecordings.GET("/files/options", func(c *gin.Context) { currentCloudRecordingCatalogController().FileOptions(c) })
			cloudRecordings.POST("/files/batch-delete", func(c *gin.Context) { currentCloudRecordingCatalogController().DeleteFiles(c) })
			cloudRecordings.GET("/files/:id", func(c *gin.Context) { currentCloudRecordingCatalogController().FileDetail(c) })
			cloudRecordings.DELETE("/files/:id", func(c *gin.Context) { currentCloudRecordingCatalogController().DeleteFile(c) })
			cloudRecordings.POST("/files/:id/access", func(c *gin.Context) { currentCloudRecordingCatalogController().IssueAccess(c) })
			cloudRecordings.POST("/files/:id/downloads", func(c *gin.Context) { currentCloudRecordingCatalogController().CreateDownload(c) })
			cloudRecordings.GET("/downloads/:taskId", func(c *gin.Context) { currentCloudRecordingCatalogController().DownloadStatus(c) })
			cloudRecordings.DELETE("/downloads/:taskId", func(c *gin.Context) { currentCloudRecordingCatalogController().CancelDownload(c) })
			cloudRecordings.GET("/active", func(c *gin.Context) { currentCloudRecordingCatalogController().ActiveSessions(c) })
			cloudRecordings.POST("/active/:id/stop", func(c *gin.Context) { currentCloudRecordingCatalogController().StopActiveSession(c) })
			cloudRecordings.GET("/reconciliations", func(c *gin.Context) { currentCloudRecordingCatalogController().Reconciliations(c) })
			cloudRecordings.POST("/reconciliations", func(c *gin.Context) { currentCloudRecordingCatalogController().TriggerReconciliation(c) })
		}
		alarms := gb.Group("/alarms")
		{
			alarms.GET("", alarmController.List)
			alarms.POST("/batch-delete", alarmController.BatchDelete)
			alarms.POST("/clear-all", alarmController.ClearAll)
			alarms.GET("/:id", alarmController.Detail)
			alarms.DELETE("/:id", alarmController.Delete)
		}
		deviceTraffic := gb.Group("/device-traffic")
		{
			deviceTraffic.GET("/summary", func(c *gin.Context) { currentDeviceTrafficController().Summary(c) })
			deviceTraffic.GET("/trend", func(c *gin.Context) { currentDeviceTrafficController().Trend(c) })
			deviceTraffic.GET("/realtime", func(c *gin.Context) { currentDeviceTrafficController().Realtime(c) })
			deviceTraffic.GET("/sessions", func(c *gin.Context) { currentDeviceTrafficController().Sessions(c) })
			deviceTraffic.GET("/coverage", func(c *gin.Context) { currentDeviceTrafficController().Coverage(c) })
			deviceTraffic.GET("/viewers", func(c *gin.Context) { currentDeviceTrafficController().Viewers(c) })
			deviceTraffic.POST("/viewers/kick", func(c *gin.Context) { currentDeviceTrafficController().KickViewer(c) })
		}
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
			play.POST("/:deviceId/:channelId/authorization", func(c *gin.Context) { playController.Authorize(c) })
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
		serviceConfig := gb.Group("/sip/service-config")
		{
			serviceConfig.GET("/position-history", serviceConfigController.GetPositionHistory)
			serviceConfig.PUT("/position-history", serviceConfigController.UpdatePositionHistory)
			serviceConfig.GET("/sdp-extension", serviceConfigController.GetSDPExtension)
			serviceConfig.PUT("/sdp-extension", serviceConfigController.UpdateSDPExtension)
			serviceConfig.GET("/sync-channels-on-online", serviceConfigController.GetSyncChannelsOnOnline)
			serviceConfig.PUT("/sync-channels-on-online", serviceConfigController.UpdateSyncChannelsOnOnline)
			serviceConfig.GET("/online-on-heartbeat", serviceConfigController.GetOnlineOnHeartbeat)
			serviceConfig.PUT("/online-on-heartbeat", serviceConfigController.UpdateOnlineOnHeartbeat)
			serviceConfig.GET("/save-alarm-messages", serviceConfigController.GetSaveAlarmMessages)
			serviceConfig.PUT("/save-alarm-messages", serviceConfigController.UpdateSaveAlarmMessages)
			serviceConfig.GET("/sip-command-timeout", serviceConfigController.GetSIPCommandTimeout)
			serviceConfig.PUT("/sip-command-timeout", serviceConfigController.UpdateSIPCommandTimeout)
			serviceConfig.GET("/preallocation-mode", serviceConfigController.GetPreallocationMode)
			serviceConfig.PUT("/preallocation-mode", serviceConfigController.UpdatePreallocationMode)
			serviceConfig.GET("/ignore-channel-offline-status-notify", serviceConfigController.GetIgnoreChannelOfflineStatusNotify)
			serviceConfig.PUT("/ignore-channel-offline-status-notify", serviceConfigController.UpdateIgnoreChannelOfflineStatusNotify)
			serviceConfig.GET("/ptz-default-speed", serviceConfigController.GetPTZDefaultSpeed)
			serviceConfig.PUT("/ptz-default-speed", serviceConfigController.UpdatePTZDefaultSpeed)
			serviceConfig.GET("/default-channel-stream-transport", serviceConfigController.GetDefaultChannelStreamTransport)
			serviceConfig.PUT("/default-channel-stream-transport", serviceConfigController.UpdateDefaultChannelStreamTransport)
			serviceConfig.GET("/default-playback-protocol", serviceConfigController.GetDefaultPlaybackProtocol)
			serviceConfig.PUT("/default-playback-protocol", serviceConfigController.UpdateDefaultPlaybackProtocol)
			serviceConfig.GET("/global-subscriptions", serviceConfigController.GetGlobalSubscriptions)
			serviceConfig.PUT("/global-subscriptions", serviceConfigController.UpdateGlobalSubscriptions)
			serviceConfig.GET("/default-channel-audio", serviceConfigController.GetDefaultChannelAudio)
			serviceConfig.PUT("/default-channel-audio", serviceConfigController.UpdateDefaultChannelAudio)
			serviceConfig.GET("/playback-settings", serviceConfigController.GetPlaybackSettings)
			serviceConfig.PUT("/playback-settings", serviceConfigController.UpdatePlaybackSettings)
			serviceConfig.GET("/fixed-address-playback", serviceConfigController.GetFixedAddressPlayback)
			serviceConfig.PUT("/fixed-address-playback", serviceConfigController.UpdateFixedAddressPlayback)
			serviceConfig.GET("/play-auth", serviceConfigController.GetPlayAuth)
			serviceConfig.PUT("/play-auth", serviceConfigController.UpdatePlayAuth)
			serviceConfig.GET("/sip-log", serviceConfigController.GetSIPLog)
			serviceConfig.PUT("/sip-log", serviceConfigController.UpdateSIPLog)
		}
		setup := gb.Group("/sip/setup")
		{
			setup.GET("/status", setupRoute(func(controller *gbcontrollers.SetupController, c *gin.Context) { controller.Status(c) }))
			setup.GET("/network-interfaces", setupRoute(func(controller *gbcontrollers.SetupController, c *gin.Context) { controller.NetworkInterfaces(c) }))
			setup.PUT("/config", setupRoute(func(controller *gbcontrollers.SetupController, c *gin.Context) { controller.SaveConfig(c) }))
			setup.POST("/skip", setupRoute(func(controller *gbcontrollers.SetupController, c *gin.Context) { controller.Skip(c) }))
		}
		// 扫码接入:生成一次性 token(兑换端点在 RegisterPublicRoutes,免鉴权)
		gb.POST("/sip/qr/token", func(c *gin.Context) { qrController.GenerateToken(c) })
		playbackSchemes := gb.Group("/playback-schemes")
		{
			playbackSchemes.GET("", playbackSchemeController.List)
			playbackSchemes.GET("/:id", playbackSchemeController.Detail)
			playbackSchemes.POST("", playbackSchemeController.Create)
			playbackSchemes.PATCH("/:id", playbackSchemeController.Rename)
			playbackSchemes.PUT("/:id/layout", playbackSchemeController.ReplaceLayout)
			playbackSchemes.DELETE("/:id", playbackSchemeController.Delete)
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
		securityGroup := gb.Group("/security")
		{
			securityGroup.GET("/snapshot", securityController.Snapshot)
			securityGroup.GET("/events", securityController.Events)
			securityGroup.GET("/bans", securityController.Bans)
			securityGroup.POST("/bans/:id/unban", securityController.Unban)
			securityGroup.GET("/access-rules", securityController.AccessRules)
			securityGroup.POST("/access-rules", securityController.CreateAccessRule)
			securityGroup.PUT("/access-rules/:id", securityController.UpdateAccessRule)
			securityGroup.DELETE("/access-rules/:id", securityController.DeleteAccessRule)
			securityGroup.GET("/policy", securityController.Policy)
			securityGroup.PUT("/policy", securityController.UpdatePolicy)
			securityGroup.GET("/agent/health", securityController.AgentHealth)
			securityGroup.GET("/stream", securityController.Stream)
		}
		cascade := gb.Group("/cascade")
		{
			cascade.GET("/platforms", cascadeRoute(func(ctrl *gbcascadecontroller.ManagementController, c *gin.Context) { ctrl.List(c) }))
			cascade.POST("/platforms", cascadeRoute(func(ctrl *gbcascadecontroller.ManagementController, c *gin.Context) { ctrl.Create(c) }))
			cascade.GET("/platforms/:id", cascadeRoute(func(ctrl *gbcascadecontroller.ManagementController, c *gin.Context) { ctrl.Get(c) }))
			cascade.PUT("/platforms/:id", cascadeRoute(func(ctrl *gbcascadecontroller.ManagementController, c *gin.Context) { ctrl.Update(c) }))
			cascade.DELETE("/platforms/:id", cascadeRoute(func(ctrl *gbcascadecontroller.ManagementController, c *gin.Context) { ctrl.Delete(c) }))
			cascade.POST("/platforms/:id/enable", cascadeRoute(func(ctrl *gbcascadecontroller.ManagementController, c *gin.Context) { ctrl.SetEnabled(c) }))
			cascade.POST("/platforms/:id/disable", cascadeRoute(func(ctrl *gbcascadecontroller.ManagementController, c *gin.Context) { ctrl.SetEnabled(c) }))
			cascade.PUT("/platforms/:id/enabled", cascadeRoute(func(ctrl *gbcascadecontroller.ManagementController, c *gin.Context) { ctrl.SetEnabled(c) }))
			cascade.POST("/platforms/:id/reconnect", cascadeRoute(func(ctrl *gbcascadecontroller.ManagementController, c *gin.Context) { ctrl.Reconnect(c) }))
			cascade.GET("/platforms/:id/shares", cascadeRoute(func(ctrl *gbcascadecontroller.ManagementController, c *gin.Context) { ctrl.GetShares(c) }))
			cascade.PUT("/platforms/:id/shares", cascadeRoute(func(ctrl *gbcascadecontroller.ManagementController, c *gin.Context) { ctrl.ReplaceShares(c) }))
			// Plan-compatible aliases keep channel terminology available to existing clients.
			cascade.GET("/platforms/:id/channels", cascadeRoute(func(ctrl *gbcascadecontroller.ManagementController, c *gin.Context) { ctrl.GetShares(c) }))
			cascade.POST("/platforms/:id/channels/share", cascadeRoute(func(ctrl *gbcascadecontroller.ManagementController, c *gin.Context) { ctrl.ReplaceShares(c) }))
			cascade.POST("/platforms/:id/channels/unshare", cascadeRoute(func(ctrl *gbcascadecontroller.ManagementController, c *gin.Context) { ctrl.ReplaceShares(c) }))
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
			dmgmt.GET("/directory/tree", directoryController.Tree)
			dmgmt.POST("/custom-groups", customGroupController.Create)
			dmgmt.PATCH("/custom-groups/:id", customGroupController.Rename)
			dmgmt.POST("/custom-groups/:id/move", customGroupController.Move)
			dmgmt.DELETE("/custom-groups/:id", customGroupController.Delete)
			dmgmt.POST("/custom-groups/:id/devices", customGroupController.AddDevices)
			dmgmt.POST("/custom-groups/:id/devices/remove", customGroupController.RemoveDevices)
			dmgmt.GET("/catalog/tree/:id", catalogTreeController.Node)
			dmgmt.GET("/catalog/tree/:id/children", catalogTreeController.Children)
			dmgmt.GET("/catalog/tree/:id/subtree", catalogTreeController.Subtree)
			dmgmt.GET("/catalog/anomaly/count", catalogTreeController.AnomalyCount)
			// B2 devicemgmt:设备列表 + 通道列表 + 详情 + 多挂载 + timeline
			dmgmt.GET("/devices", deviceMgmtController.ListDevices)
			dmgmt.GET("/permission-workbench/summary", deviceMgmtController.PermissionWorkbenchSummary)
			dmgmt.POST("/permission-workbench/devices/resolve", deviceMgmtController.ResolvePermissionWorkbenchDevices)
			dmgmt.POST("/permission-workbench/grants/query", deviceMgmtController.QueryPermissionWorkbenchGrants)
			dmgmt.POST("/permission-workbench/grants/apply", deviceMgmtController.ApplyPermissionWorkbenchGrants)
			dmgmt.GET("/permission-workbench/grant-targets", deviceMgmtController.SearchPermissionWorkbenchGrantTargets)
			dmgmt.POST("/permission-workbench/assignments", deviceMgmtController.ApplyPermissionWorkbenchAssignments)
			dmgmt.POST("/permission-workbench/assignments/departments", deviceMgmtController.ApplyPermissionWorkbenchDepartmentAssignment)
			dmgmt.POST("/device", deviceMgmtController.CreateDevice)
			dmgmt.GET("/device/:id", deviceMgmtController.GetDevice)
			dmgmt.GET("/device/:id/status-events", deviceMgmtController.ListDeviceStatusEvents)
			dmgmt.GET("/device/:id/subscriptions", deviceMgmtController.ListSubscriptions)
			dmgmt.PATCH("/device/:id/subscriptions/:kind", deviceMgmtController.UpdateSubscription)
			dmgmt.POST("/device/:id/subscriptions/:kind/renew", deviceMgmtController.RenewSubscription)
			dmgmt.GET("/device/:id/alarms", deviceMgmtController.ListAlarms)
			dmgmt.GET("/channels", deviceMgmtController.ListChannels)
			dmgmt.GET("/channel/:id", deviceMgmtController.GetChannel)
			dmgmt.GET("/channel/:id/record-query/options", deviceMgmtController.GetRecordQueryOptions)
			dmgmt.POST("/channel/:id/record-query", deviceMgmtController.QueryDeviceRecords)
			dmgmt.POST("/channel/:id/playback-sessions", deviceMgmtController.CreatePlaybackSession)
			dmgmt.GET("/channel/:id/playback-sessions/:sessionId", deviceMgmtController.GetPlaybackSession)
			dmgmt.POST("/channel/:id/playback-sessions/:sessionId/actions", deviceMgmtController.ActionPlaybackSession)
			dmgmt.DELETE("/channel/:id/playback-sessions/:sessionId", deviceMgmtController.DeletePlaybackSession)
			dmgmt.PATCH("/channel/:id", deviceMgmtController.UpdateChannel)
			dmgmt.PATCH("/channel/:id/cloud-recording", cloudRecordingController.Update)
			dmgmt.GET("/channel/:id/mounts", deviceMgmtController.ListChannelMounts)
			dmgmt.GET("/channel/:id/timeline", deviceMgmtController.ChannelTimeline)
			dmgmt.GET("/channel/:id/control-capabilities", deviceMgmtController.GetControlCapabilities)
			dmgmt.GET("/channel/:id/device-status", deviceMgmtController.GetDeviceStatus)
			dmgmt.POST("/channel/:id/device-control", deviceMgmtController.ControlDevice)
			dmgmt.POST("/channel/:id/talk-sessions", func(c *gin.Context) { talkController.Load().Create(c) })
			dmgmt.GET("/channel/:id/talk-sessions/:sessionId", func(c *gin.Context) { talkController.Load().Get(c) })
			dmgmt.DELETE("/channel/:id/talk-sessions/:sessionId", func(c *gin.Context) { talkController.Load().Delete(c) })
			dmgmt.POST("/channel/:id/ptz", deviceMgmtController.ControlPTZ)
			dmgmt.POST("/channel/:id/ptz/precise", deviceMgmtController.ControlPTZPrecise)
			dmgmt.POST("/channel/:id/ptz/extended", deviceMgmtController.ControlPTZExtended)
			dmgmt.POST("/channel/:id/ptz/presets", deviceMgmtController.CreatePTZPreset)
			dmgmt.POST("/channel/:id/ptz/presets/:presetId/call", deviceMgmtController.CallPTZPreset)
			dmgmt.DELETE("/channel/:id/ptz/presets/:presetId", deviceMgmtController.DeletePTZPreset)
			dmgmt.POST("/channel/:id/ptz/cruise", deviceMgmtController.ControlPTZCruise)
			dmgmt.POST("/channel/:id/ptz/cruise/tracks", deviceMgmtController.CreateCruiseTrack)
			dmgmt.GET("/channel/:id/ptz/home-position", deviceMgmtController.GetPTZHomePosition)
			dmgmt.PATCH("/channel/:id/ptz/home-position", deviceMgmtController.UpdatePTZHomePosition)
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

// RegisterContentRoutes must be called before the global request-timeout and
// operation-log middleware. Capability verification is performed by the
// controller on every request; JWT and query-logging middleware are omitted.
func RegisterContentRoutes(engine *gin.Engine) {
	engine.GET("/api/gb28181/cloud-recordings/downloads/:taskId/content", func(c *gin.Context) {
		currentCloudRecordingCatalogController().DownloadContent(c)
	})
	engine.GET("/api/gb28181/cloud-recordings/content/:id", func(c *gin.Context) {
		currentCloudRecordingCatalogController().Content(c)
	})
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

func cascadeRoute(fn func(*gbcascadecontroller.ManagementController, *gin.Context)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cascadeManagementController == nil {
			c.JSON(503, gin.H{"code": 503, "msg": "国标级联服务尚未装配"})
			return
		}
		fn(cascadeManagementController, c)
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
		hook.POST("/on_flow_report", hookController.OnFlowReport)
		hook.POST("/on_stream_not_found", hookController.OnStreamNotFound)
		hook.POST("/on_record_mp4", hookController.OnRecordMP4)
	}
}

// RegisterPublicRoutes 注册免鉴权的 GB28181 端点到 /api 下的 public 组.
//
// Gin 的中间件按**组**挂载而非路径前缀匹配 —— public 与 protected 同为
// api.Group("") 的子组,所以这里既能保留 /api/gb28181/... 路径,又不经过
// JWT / Casbin.设备端没有登录态,一次性 token 是唯一凭据.
func RegisterPublicRoutes(public *gin.RouterGroup) {
	public.POST("/gb28181/sip/qr/exchange", func(c *gin.Context) { qrController.Exchange(c) })
}

// RegisterQRLandingRoute 注册扫码引导页到 engine 根(无 /api 前缀,无鉴权).
//
// 二维码把 token 放在 fragment(#t=...),fragment 不会发给服务端,所以通用扫码
// App 打开这个 URL 只会看到一句引导文案,既不会消费 token 也拿不到密码.
func RegisterQRLandingRoute(engine *gin.Engine) {
	engine.GET("/gb28181/qr", func(c *gin.Context) { qrController.Landing(c) })
}
