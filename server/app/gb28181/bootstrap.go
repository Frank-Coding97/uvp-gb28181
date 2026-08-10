package gb28181

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	"uvplatform.cn/uvp-gb28181/app/gb28181/civilcode"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
	gbhandler "uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play/reconciler"
	gbplayback "uvplatform.cn/uvp-gb28181/app/gb28181/playback"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordquery"
	gbroutes "uvplatform.cn/uvp-gb28181/app/gb28181/routes"
	gbsecurity "uvplatform.cn/uvp-gb28181/app/gb28181/security"
	gbsetup "uvplatform.cn/uvp-gb28181/app/gb28181/setup"
	gbsip "uvplatform.cn/uvp-gb28181/app/gb28181/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/snapshot"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/streammonitor"
	"uvplatform.cn/uvp-gb28181/app/gb28181/streamprobe"
	"uvplatform.cn/uvp-gb28181/app/gb28181/subscribe"
	gbtalk "uvplatform.cn/uvp-gb28181/app/gb28181/talk"
	gbtrace "uvplatform.cn/uvp-gb28181/app/gb28181/trace"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	gbzlm "uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/heartbeat"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	gbzlmprobe "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/probe"
	gbzlmrepo "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
	gbzlmsched "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
	gbzlmsvc "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// schedulerPickerAdapter 把 scheduler.Manager 适配为 play.NodePicker(避免反向依赖 play 包)
type schedulerPickerAdapter struct {
	m *gbzlmsched.Manager
}

func (a schedulerPickerAdapter) Pick(ctx context.Context, inv play.PickContext) (*node.Node, error) {
	return a.m.Pick(ctx, gbzlmsched.InviteContext{
		DeviceID:  inv.DeviceID,
		ChannelID: inv.ChannelID,
		StreamID:  inv.StreamID,
	})
}

// schedulerLogRepoAdapter 把 repo.GormSchedulerLogRepo 适配为 scheduler.SchedulerLogRepo
//
// 两边各自定义同构 struct 避免 repo → scheduler 反向依赖,这里手工字段映射。
type schedulerLogRepoAdapter struct {
	inner *gbzlmrepo.GormSchedulerLogRepo
}

func (a schedulerLogRepoAdapter) Insert(ctx context.Context, l gbzlmsched.SchedulerLog) error {
	return a.inner.Insert(ctx, gbzlmrepo.SchedulerLogRow{
		ID:           l.ID,
		HappenedAt:   l.HappenedAt,
		Algorithm:    l.Algorithm,
		NodeID:       l.NodeID,
		NodeName:     l.NodeName,
		StreamID:     l.StreamID,
		DeviceID:     l.DeviceID,
		ChannelID:    l.ChannelID,
		ErrorMessage: l.ErrorMessage,
	})
}

func (a schedulerLogRepoAdapter) List(ctx context.Context, limit int) ([]gbzlmsched.SchedulerLog, error) {
	rows, err := a.inner.List(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]gbzlmsched.SchedulerLog, 0, len(rows))
	for _, r := range rows {
		out = append(out, gbzlmsched.SchedulerLog{
			ID:           r.ID,
			HappenedAt:   r.HappenedAt,
			Algorithm:    r.Algorithm,
			NodeID:       r.NodeID,
			NodeName:     r.NodeName,
			StreamID:     r.StreamID,
			DeviceID:     r.DeviceID,
			ChannelID:    r.ChannelID,
			ErrorMessage: r.ErrorMessage,
		})
	}
	return out, nil
}

func (a schedulerLogRepoAdapter) PruneOlderThan(ctx context.Context, t time.Time) (int64, error) {
	return a.inner.PruneOlderThan(ctx, t)
}

type sipRuntimeServer interface {
	SetRecorder(metrics.Recorder)
	SetErrorHandler(func(error))
	SetPTZMessageProcessor(gbhandler.PTZMessageProcessor)
	SetRecordInfoSink(gbhandler.RecordInfoSink)
	SetPlaybackEndSink(gbhandler.PlaybackEndSink)
	SetPTZNotifyProcessor(gbhandler.PTZNotifyProcessor)
	SetSubscriptionWaker(gbhandler.SubscriptionWaker)
	SetSubscriptionNotifier(gbhandler.SubscriptionNotifier)
	SetAlarmMessageProcessor(gbhandler.AlarmMessageProcessor)
	Start() error
	UAC() *uac.UAC
	Shutdown(context.Context) error
}

var playbackService *gbplayback.Service

func SetPlaybackService(service *gbplayback.Service, snapshots gbcontrollers.PlaybackSnapshotResolver) {
	playbackService = service
	if service == nil {
		gbroutes.SetDeviceMgmtPlaybackRuntime(nil, nil)
		gbroutes.SetPlaybackMediaSink(nil)
		if sipServer != nil {
			sipServer.SetPlaybackEndSink(nil)
			if u := sipServer.UAC(); u != nil {
				u.SetPlaybackEndHook(nil)
			}
		}
		return
	}
	gbroutes.SetDeviceMgmtPlaybackRuntime(service, snapshots)
	gbroutes.SetPlaybackMediaSink(service)
	if sipServer != nil {
		sipServer.SetPlaybackEndSink(service)
		if u := sipServer.UAC(); u != nil {
			u.SetPlaybackEndHook(func(ctx context.Context, metadata uac.PlaybackDialogMetadata, reason string) error {
				return service.OnPlaybackMediaEnded(ctx, metadata.CallID, metadata.DeviceID, reason)
			})
		}
	}
}

type sipRuntimeFactory func(gbconfig.Config) (sipRuntimeServer, error)

// sipServer 持有全局 SIP 服务实例,供优雅关闭引用
var sipServer sipRuntimeServer
var sipLifecycleMu sync.Mutex

var securityRuntime *gbsecurity.Runtime

// SecurityRuntime exposes the live SIP security composition for diagnostics.
func SecurityRuntime() *gbsecurity.Runtime { return securityRuntime }

var sipRuntimeStatus = gbsetup.NewRuntimeStatus()

// SIPRuntimeStatus exposes the current SIP runtime state to setup controllers.
func SIPRuntimeStatus() *gbsetup.RuntimeStatus { return sipRuntimeStatus }

// subscriptionService and subscriptionScheduler share the SIP UAC lifecycle.
var subscriptionService *subscribe.Service
var subscriptionScheduler *subscribe.Scheduler
var ptzService *ptz.Service
var ptzScheduler ptzSchedulerLifecycle
var recordQueryService *recordquery.Service
var recordQueryMetrics *recordquery.Metrics
var playbackMetrics *gbplayback.Metrics
var positionHistoryPruneCancel context.CancelFunc

type ptzSchedulerLifecycle interface {
	Start(context.Context)
	Stop()
}

// offlineScanner 离线扫描器
var offlineScanner *device.OfflineScanner

// zlmClient 全局 ZLM 客户端(deprecated M3 TF.2 删,M1 单节点过渡期保留)
var zlmClient *gbzlm.Client

// zlmRegistry 节点注册表(M1 新增)
var zlmRegistry *node.Registry
var zlmServerConfigCache *gbzlm.ServerConfigCache

// zlmScheduler M2 新增,持有当前激活的调度算法(roundrobin / M3 weighted / leastload)
// SIP play 改造(T2.4)从这里取节点。装配失败则为 nil,调用方自己降级。
var zlmScheduler *gbzlmsched.Manager

// zlmLocationMap M2.4 新增,streamID → nodeID 索引(给多节点 play/service + hook 端点用)
var zlmLocationMap *stream.LocationMap

// playSvc 全局点播 service(routes 用 GetPlayService 取)
var playSvc *play.Service

// playSessions 全局点播会话管理(让 hook 端点 on_stream_none_reader/on_rtp_server_timeout 也能查到)
var playSessions = uac.NewSessionManager()

// metricsAgg 全局指标聚合器(供 controllers/dashboard 暴露,供 SIP 路径埋点)
var metricsAgg *metrics.Aggregator

// metricsCleanupStop 控制 TTL 清理 goroutine 退出
var metricsCleanupStop chan struct{}

// heartbeatCancel 控制 Watcher goroutine 退出(M2 新增)
var heartbeatCancel context.CancelFunc

// playReconciler 兜底对账 goroutine(通道播放状态显示 T7 新增)
var playReconciler *reconciler.Reconciler

var recordingSvc *gbrecording.Service
var recordingReconciler *gbrecording.Reconciler
var recordingCatalogScheduler *gbrecording.CatalogReconcileScheduler
var talkSvc *gbtalk.Service
var talkCleanupWorker *gbtalk.CleanupWorker

// zlmSchedulerLog 调度日志服务(T3.3 新增,可为 nil 降级)
var zlmSchedulerLog *gbzlmsched.LogService

// schedulerLogCancel 控制调度日志 worker + prune ticker 退出
var schedulerLogCancel context.CancelFunc

// PlayService 返回点播 service(可能为 nil,gb28181 未启用 / UAC 初始化失败时)
func PlayService() *play.Service { return playSvc }

// SIPTraceRuntimeEnabled reports whether the current SIP transport has trace
// hooks attached. It intentionally does not treat a degraded ClickHouse store
// as disabled; storage health is exposed by the trace health API.
func SIPTraceRuntimeEnabled() bool {
	sipLifecycleMu.Lock()
	defer sipLifecycleMu.Unlock()
	traceServer, ok := sipServer.(interface{ TraceRuntime() gbtrace.Runtime })
	return ok && traceServer.TraceRuntime() != nil
}

// MetricsAggregator 返回全局聚合器(controllers/dashboard 用)
func MetricsAggregator() *metrics.Aggregator { return metricsAgg }

// ZLMRegistry 返回全局节点注册表(M1 新增,供 controllers / test 用)
func ZLMRegistry() *node.Registry { return zlmRegistry }

// ZLMScheduler 返回全局调度器 Manager(M2 新增,供 SIP play 改造 / test 用)
// 可能为 nil:DB 不可达或 Switch 全部失败时
func ZLMScheduler() *gbzlmsched.Manager { return zlmScheduler }

func startSIPRuntime(cfg gbconfig.Config, recorder metrics.Recorder, status *gbsetup.RuntimeStatus, factory sipRuntimeFactory) (sipRuntimeServer, error) {
	status.MarkStarting()
	server, err := factory(cfg)
	if err != nil {
		status.MarkFailed(err.Error())
		return nil, err
	}
	server.SetRecorder(recorder)
	server.SetErrorHandler(func(err error) {
		status.MarkFailed(err.Error())
	})
	if err := server.Start(); err != nil {
		status.MarkFailed(err.Error())
		return nil, err
	}
	status.MarkRunning()
	return server, nil
}

func startControlPlane(cfg gbconfig.Config) {
	setupCivilCodeService()
	gbroutes.SetSetupController(gbcontrollers.NewSetupController(app.DB(), sipRuntimeStatus, nil, ReloadSIP))
	gbroutes.SetServiceConfigSIPTraceReloader(ReloadSIP)
	gbroutes.SetServiceConfigSIPTraceRuntimeProvider(SIPTraceRuntimeEnabled)
	gbroutes.SetPlatformController(gbcontrollers.NewConfiguredPlatformController(
		app.DB(), sipRuntimeStatus, cfg.Enabled, cfg.SIP.Transport,
	))
	gbroutes.SetQRController(gbcontrollers.NewConfiguredQRController(
		app.DB(), app.Cache, cfg.SIP.Transport,
	))

	metricsAgg = metrics.NewAggregator()
	metricsCleanupStop = make(chan struct{})
	go runMetricsCleanup(metricsAgg, metricsCleanupStop)
	gbroutes.SetMetricsProvider(func() *metrics.Aggregator { return metricsAgg })
	setupTraceController(cfg, nil)
	setupZLMRegistry(cfg)
	setupZLMScheduler()
	setupZLMSchedulerLog()
	setupZLMSchedulerController()

	if zlmRegistry != nil {
		adapter := gbzlm.NewServiceAdapter(cfg.Media)
		tuning := gbzlmsvc.MediaTuning{
			HookHost:                cfg.Media.HookHost,
			HookPort:                cfg.Media.HookPort,
			StreamNoneReaderTimeout: cfg.Media.StreamNoneReaderTimeout,
			RTPServerTimeout:        cfg.Media.RTPServerTimeout,
		}
		nodeSvc := gbzlmsvc.NewNodeService(zlmRegistry, adapter, tuning)
		cfgSvc := gbzlmsvc.NewConfigService(zlmRegistry, adapter)
		gbroutes.SetZLMNodeController(gbcontrollers.NewZLMNodeController(nodeSvc))
		gbroutes.SetZLMConfigController(gbcontrollers.NewZLMConfigController(cfgSvc))
		app.ZapLog.Info("GB28181 ZLM 节点/配置 controller 已装配")

		collector := heartbeat.NewCollector(zlmRegistry)
		gbroutes.SetKeepaliveCollector(collector)
		watcher := heartbeat.NewWatcher(zlmRegistry, heartbeat.RealClock(), 30*time.Second, 90*time.Second)
		var hbCtx context.Context
		hbCtx, heartbeatCancel = context.WithCancel(context.Background())
		watcher.Start(hbCtx)
		app.ZapLog.Info("GB28181 ZLM 心跳 Collector / Watcher 已启动",
			zap.Duration("checkInterval", 30*time.Second),
			zap.Duration("offlineThreshold", 90*time.Second))

		threadPoller := heartbeat.NewThreadLoadPoller(zlmRegistry, adapter, 30*time.Second)
		threadPoller.Start(hbCtx)
		app.ZapLog.Info("GB28181 ZLM 线程负载 Poller 已启动", zap.Duration("interval", 30*time.Second))
	}

	zlmClient = pickInitialClient(cfg)
	if zlmClient == nil {
		app.ZapLog.Warn("GB28181 ZLM 初始 Client 未构造(Registry 空),跳过 Hook 配置下发")
		return
	}
	go func(client *gbzlm.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.ApplyConfigForNode(ctx, cfg.Media); err != nil {
			app.ZapLog.Warn("GB28181 ZLM 配置下发失败(ZLM 可能暂不可达,不影响启动)", zap.Error(err))
		} else {
			app.ZapLog.Info("GB28181 ZLM Hook 配置已下发", zap.String("hookHost", cfg.Media.HookHost))
		}
	}(zlmClient)
}

// Start 启动 GB28181 SIP 服务 + 离线扫描器(在 HTTP 服务阻塞等待信号之前调用)
// 若 gb28181.enabled=false 则跳过。SIP 未配置时不启 SIP 依赖,由前端引导页录入并触发热启动。
func Start() {
	sipLifecycleMu.Lock()
	defer sipLifecycleMu.Unlock()

	cfg := gbconfig.Load()
	if !cfg.Enabled {
		sipRuntimeStatus.MarkDisabled()
		app.ZapLog.Info("GB28181 未启用,跳过 SIP 服务启动")
		return
	}
	startControlPlane(cfg)

	// 老 stack 升级迁移:如果 DB 空 + YAML 有 SIP 段 + gb_device 有历史数据 → 一次性 seed.
	// 幂等,首启后 DB 有数据下次调用直接 skip.
	if migrated, err := gbsetup.MigrateYAMLToDB(context.Background(), app.DB(), app.ConfigYml); err != nil {
		app.ZapLog.Warn("GB28181 SIP YAML 一次性迁移失败,忽略继续", zap.Error(err))
	} else if migrated {
		app.ZapLog.Info("GB28181 SIP 老 stack YAML 配置已迁移到 gb_sip_config,可在 config.yml 移除 gb28181.sip.* 段")
	}

	sipCfg, ok, err := loadSIPConfigFromDB(cfg)
	if err != nil {
		sipRuntimeStatus.MarkFailed(err.Error())
		app.ZapLog.Error("GB28181 SIP 配置加载失败,后台继续启动", zap.Error(err))
		return
	}
	if !ok {
		sipRuntimeStatus.MarkUnconfigured()
		app.ZapLog.Warn("GB28181 SIP 尚未配置,跳过 SIP 依赖并继续启动后台(等待引导页录入)")
		return
	}

	if err := startSIPDependencies(sipCfg); err != nil {
		app.ZapLog.Error("GB28181 SIP 服务启动失败", zap.Error(err))
		return
	}
}

// loadSIPConfigFromDB 是唯一的 SIP 配置来源:数据库 gb_sip_config.
// 无记录 → (base, false, nil):走引导流程
// 有记录且合法 → (fullCfg, true, nil)
// 有记录但字段非法 → (base, false, err):记录到 runtime,不启 SIP
func loadSIPConfigFromDB(base gbconfig.Config) (gbconfig.Config, bool, error) {
	row, err := gbsetup.NewSIPConfigRepository(app.DB()).Get(context.Background())
	if err != nil {
		return base, false, err
	}
	if row == nil {
		return base, false, nil
	}
	base.SIP = gbconfig.SIPConfig{
		ListenIP:         row.ListenIP,
		AdvertiseIP:      row.AdvertiseIP,
		DynamicAdvertise: row.DeploymentMode == gbsetup.DeploymentLAN && row.ListenIP == "0.0.0.0",
		Port:             row.Port,
		Transport:        base.SIP.Transport,
		Domain:           row.Domain,
		ServerID:         row.ServerID,
		Password:         row.Password,
		XGBVersion:       base.SIP.XGBVersion,
	}
	if len(base.SIP.Transport) == 0 {
		base.SIP.Transport = []string{"udp", "tcp"}
	}
	return base, true, nil
}

func setupSecurityRuntime() *gbsecurity.Runtime {
	clock := gbsecurity.RealClock()
	socketPath := os.Getenv("UVP_GB28181_FIREWALL_SOCKET")
	if socketPath == "" {
		socketPath = "/run/uvp/firewall-agent.sock"
	}
	agent := gbsecurity.NewUnixFirewallClient(socketPath, 2*time.Second)
	secret := []byte(os.Getenv("UVP_GB28181_NONCE_SECRET"))
	if app.DB() != nil {
		store := gbsecurity.NewGormStore(app.DB())
		runtime, err := gbsecurity.NewPersistentRuntime(context.Background(), store, clock, agent, secret)
		if err == nil {
			gbroutes.SetSecurityRuntime(runtime)
			return runtime
		}
		app.ZapLog.Warn("GB28181 安全持久化运行时装配失败,降级为内存 protect", zap.Error(err))
	}
	runtime := gbsecurity.NewRuntime(gbsecurity.DefaultPolicy(), clock, agent, secret)
	gbroutes.SetSecurityRuntime(runtime)
	return runtime
}

// startSIPDependencies 启动 SIP server + 所有依赖 UAC 的服务(点播/订阅/离线扫描等).
// 幂等:reload 时可先 stopSIPDependencies 再调这里.
func startSIPDependencies(cfg gbconfig.Config) error {
	runtime := setupSecurityRuntime()
	srv, err := startSIPRuntime(cfg, metricsAgg, sipRuntimeStatus, func(cfg gbconfig.Config) (sipRuntimeServer, error) {
		return gbsip.NewServer(cfg, gbsip.WithSecurityRuntime(runtime))
	})
	if err != nil {
		_ = runtime.Close(context.Background())
		gbroutes.SetSecurityRuntime(nil)
		return err
	}
	securityRuntime = runtime
	var newPTZService *ptz.Service
	var newPTZScheduler ptzSchedulerLifecycle
	if u := srv.UAC(); u != nil {
		newRecordQueryService, queryErr := recordquery.NewService(u, recordquery.Options{
			Timeout:            cfg.RecordQuery.Timeout(),
			MaxActiveQueries:   cfg.RecordQuery.MaxActiveQueries,
			MaxRecordsPerQuery: cfg.RecordQuery.MaxRecordsPerQuery,
			ResultTTL:          cfg.RecordQuery.ResultTTL(),
			Location:           cfg.RecordQuery.Location,
		})
		if queryErr != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = srv.Shutdown(shutdownCtx)
			cancel()
			sipRuntimeStatus.MarkFailed(queryErr.Error())
			return fmt.Errorf("装配设备录像查询 service 失败: %w", queryErr)
		}
		recordQueryService = newRecordQueryService
		recordQueryMetrics = recordquery.NewMetrics()
		srv.SetRecordInfoSink(newRecordQueryService)
		gbroutes.SetDeviceMgmtRecordQueryRuntime(newRecordQueryService, cfg.RecordQuery, recordQueryMetrics)
		newPTZService, err = ptz.NewService(app.DB(), u, time.Now)
		if err != nil {
			newRecordQueryService.Close()
			recordQueryService = nil
			recordQueryMetrics = nil
			gbroutes.SetDeviceMgmtRecordQueryRuntime(nil, gbconfig.RecordQueryConfig{}, nil)
			srv.SetRecordInfoSink(nil)
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = srv.Shutdown(shutdownCtx)
			cancel()
			sipRuntimeStatus.MarkFailed(err.Error())
			return fmt.Errorf("装配 PTZ service 失败: %w", err)
		}
		newPTZScheduler = ptz.NewScheduler(newPTZService)
		srv.SetPTZMessageProcessor(newPTZService)
		srv.SetPTZNotifyProcessor(newPTZService)
	}
	sipServer = srv
	if traceServer, ok := srv.(interface{ TraceRuntime() gbtrace.Runtime }); ok {
		setupTraceController(cfg, traceServer.TraceRuntime())
	}

	// 装配依赖 UAC 的 processor(手动 catalog 刷新按钮、PTZ、订阅、告警)
	if u := srv.UAC(); u != nil {
		gbroutes.SetDeviceMgmtCatalogTrigger(gbhandler.NewUACCatalogTrigger(u))
		ptzService = newPTZService
		ptzScheduler = newPTZScheduler
		gbroutes.SetDeviceMgmtPTZRuntime(u, ptzService)
		ptzScheduler.Start(context.Background())
		subscriptionService = subscribe.NewService(app.DB(), u, time.Now)
		gbroutes.SetDeviceMgmtSubscriptionManager(subscriptionService)
		subscriptionService.SetProcessor(gbmodels.SubscriptionKindCatalog, subscribe.NewCatalogProcessor(catalog.New(app.DB())))
		subscriptionService.SetProcessor(gbmodels.SubscriptionKindMobilePosition, subscribe.NewPositionProcessor(app.DB(), time.Now))
		subscriptionService.SetProcessor(gbmodels.SubscriptionKindAlarm, subscribe.NewAlarmProcessor(app.DB(), time.Now))
		subscriptionScheduler = subscribe.NewScheduler(subscriptionService, 30*time.Second)
		subscriptionScheduler.Start(context.Background())
		srv.SetSubscriptionWaker(subscriptionService)
		srv.SetSubscriptionNotifier(subscriptionService)
		srv.SetAlarmMessageProcessor(subscriptionService)
	}
	startPositionHistoryPruner()

	offlineScanner = device.NewOfflineScanner(
		cfg.Device.OfflineScanInterval,
		cfg.Device.KeepaliveTimeoutCount,
		cfg.Device.KeepaliveGraceSeconds,
	)
	offlineScanner.Start()
	app.ZapLog.Info("GB28181 离线扫描器已启动", zap.Int("intervalSeconds", cfg.Device.OfflineScanInterval))

	// 装配点播 service(依赖 SIP UAC + ZLM 客户端 + 流就绪 Notifier)
	if u := srv.UAC(); u != nil {
		if zlmRegistry != nil && zlmScheduler != nil {
			zlmLocationMap = stream.NewLocationMap()
			// 通道快照 service(播放触发)—— 优先尝试装配,失败/nil 都不影响主链路
			snapshotSvc := buildSnapshotService()
			opts := []play.Option{
				play.WithURLResolver(play.NewURLResolver(zlmServerConfigCache)),
			}
			if snapshotSvc != nil {
				opts = append(opts, play.WithSnapshotService(snapshotSvc))
				app.ZapLog.Info("GB28181 通道快照 service 已装配")
			}
			playSvc = play.NewWithScheduler(cfg,
				schedulerPickerAdapter{m: zlmScheduler},
				zlmRegistry,
				zlmLocationMap,
				u, playSessions, gbroutes.StreamNotifier(),
				play.NewDeviceRepo(), play.NewChannelRepo(), opts...)
			gbroutes.SetPlayService(playSvc)
			gbroutes.SetStreamMonitorService(streammonitor.NewService(zlmRegistry, zlmLocationMap, nil, time.Now))
			gbroutes.SetStreamProbeService(streamprobe.NewService(zlmRegistry, zlmLocationMap, nil, 5*time.Second, time.Now))
			gbroutes.SetHookMultiNode(zlmRegistry, zlmLocationMap)
			app.ZapLog.Info("GB28181 点播 service 已装配(多节点 + scheduler)")
		} else {
			playSvc = play.New(cfg, zlmClient, u, playSessions, gbroutes.StreamNotifier(),
				play.NewDeviceRepo(), play.NewChannelRepo())
			gbroutes.SetPlayService(playSvc)
			app.ZapLog.Info("GB28181 点播 service 已装配(单节点 deprecated;通道快照仅多节点路径启用)")
		}
	} else {
		app.ZapLog.Warn("GB28181 UAC 不可用,点播 service 跳过装配")
	}
	setupPlaybackRuntime(cfg, srv.UAC())
	setupTalkRuntime(cfg, srv)
	setupRecordingRuntime(cfg)

	// 装配兜底对账 reconciler(通道播放状态显示 T7 新增)
	// spec AC10-AC16: 5min 定期扫描 gb_channel.stream_id 跟 ZLM 真实流状态对齐,
	// 消除 hook 丢包 / 进程重启导致的假阳性.
	// 只在 playSvc 装配成功 + 配置未禁用 + 多节点路径下启用.
	if playSvc != nil && cfg.Play.ReconcileIntervalSec > 0 {
		if zlmRegistry != nil && zlmLocationMap != nil {
			interval := time.Duration(cfg.Play.ReconcileIntervalSec) * time.Second
			playReconciler = reconciler.New(interval, playSvc,
				reconciler.WithRegistry(zlmRegistry),
				reconciler.WithLocationMap(zlmLocationMap),
			)
			playReconciler.Start(context.Background())
			app.ZapLog.Info("GB28181 点播对账 reconciler 已启动",
				zap.Duration("interval", interval))
		} else {
			app.ZapLog.Info("GB28181 点播对账 reconciler 跳过装配(单节点路径 deprecated,无 registry/locationMap)")
		}
	} else if playSvc != nil {
		app.ZapLog.Info("GB28181 点播对账 reconciler 未启用(reconcile_interval_sec=0)")
	}
	return nil
}

// stopSIPDependencies 反向拆解 startSIPDependencies 建立的运行时状态.
// 用于配置热重启 —— 供 Reload 调用,不涉及 control plane 组件.
func stopSIPDependencies(ctx context.Context) {
	stopPlaybackRuntime(ctx)
	stopRecordQueryRuntime()
	stopPTZRuntime()
	stopTalkRuntime(ctx)
	stopRecordingRuntime()
	if positionHistoryPruneCancel != nil {
		positionHistoryPruneCancel()
		positionHistoryPruneCancel = nil
	}
	if subscriptionScheduler != nil {
		subscriptionScheduler.Stop()
		subscriptionScheduler = nil
	}
	subscriptionService = nil
	if offlineScanner != nil {
		offlineScanner.Stop()
		offlineScanner = nil
	}
	if playReconciler != nil {
		playReconciler.Stop()
		playReconciler = nil
	}
	playSvc = nil
	gbroutes.SetPlayService(nil)
	gbroutes.SetDeviceMgmtCatalogTrigger(nil)
	gbroutes.SetDeviceMgmtSubscriptionManager(nil)
	if sipServer != nil {
		if err := sipServer.Shutdown(ctx); err != nil {
			app.ZapLog.Warn("GB28181 SIP 服务优雅关闭失败,忽略继续", zap.Error(err))
		}
		sipServer = nil
	}
	if securityRuntime != nil {
		if err := securityRuntime.Close(ctx); err != nil {
			app.ZapLog.Warn("GB28181 安全事件持久化停止失败,忽略继续", zap.Error(err))
		}
		securityRuntime = nil
	}
	gbroutes.SetSecurityRuntime(nil)
}

func stopPlaybackRuntime(ctx context.Context) {
	if playbackService != nil {
		_ = playbackService.Close(ctx)
		playbackService = nil
	}
	playbackMetrics = nil
	gbroutes.SetDeviceMgmtPlaybackRuntime(nil, nil)
	gbroutes.SetPlaybackMediaSink(nil)
	if sipServer != nil {
		sipServer.SetPlaybackEndSink(nil)
		if u := sipServer.UAC(); u != nil {
			u.SetPlaybackEndHook(nil)
		}
	}
}

func setupPlaybackRuntime(cfg gbconfig.Config, inviter *uac.UAC) {
	if inviter == nil || recordQueryService == nil || zlmRegistry == nil || zlmScheduler == nil ||
		zlmLocationMap == nil || zlmServerConfigCache == nil {
		SetPlaybackService(nil, nil)
		playbackMetrics = nil
		app.ZapLog.Info("GB28181 设备录像回放 service 跳过装配(UAC/RecordInfo/ZLM 依赖未就绪)")
		return
	}
	registry := gbplayback.NewRegistry(gbplayback.RegistryConfig{
		IdleTimeout: cfg.Playback.IdleTimeout(),
		MaxSession:  cfg.Playback.MaxSession(),
	})
	playbackMetrics = &gbplayback.Metrics{}
	service := gbplayback.NewService(
		registry,
		gbplayback.NewZLMNodePicker(zlmScheduler, cfg.SIP.ServerID),
		gbplayback.NewZLMRTPOpener(zlmRegistry, zlmLocationMap, nil),
		uac.NewPlaybackAdapter(inviter),
		gbplayback.NewZLMMediaWaiter(zlmRegistry, zlmLocationMap, gbroutes.StreamNotifier(), zlmServerConfigCache, nil),
		gbplayback.ServiceConfig{ServerID: cfg.SIP.ServerID, MediaWait: cfg.Playback.MediaWait(), Metrics: playbackMetrics},
	)
	service.StartSweeper(context.Background(), time.Second)
	SetPlaybackService(service, recordQueryService.Snapshots())
	app.ZapLog.Info("GB28181 设备录像回放 service 已装配(ZLM scheduler + Playback UAC)")
}

func stopRecordQueryRuntime() {
	if recordQueryService != nil {
		recordQueryService.Close()
	}
	gbroutes.SetDeviceMgmtRecordQueryRuntime(nil, gbconfig.RecordQueryConfig{}, nil)
	if sipServer != nil {
		sipServer.SetRecordInfoSink(nil)
	}
	recordQueryService = nil
	recordQueryMetrics = nil
}

func stopPTZRuntime() {
	if ptzScheduler != nil {
		ptzScheduler.Stop()
		ptzScheduler = nil
	}
	if ptzService != nil {
		ptzService.Retire()
	}
	ptzService = nil
	gbroutes.SetDeviceMgmtPTZRuntime(nil, nil)
}

func setupRecordingRuntime(cfg gbconfig.Config) {
	if playSvc == nil || zlmRegistry == nil || zlmLocationMap == nil {
		gbroutes.SetRecordingService(nil, nil, nil)
		app.ZapLog.Info("GB28181 云端录像 service 跳过装配(play/registry/locationMap 未就绪)")
		return
	}
	repo := gbrecording.NewGormRepo(app.DB())
	recordingSvc = gbrecording.NewService(repo, playSvc, playSvc, zlmLocationMap, zlmRegistry,
		func(n *node.Node) gbrecording.RecorderClient { return gbzlm.NewClientForNode(n) })
	indexer := gbrecording.NewFileIndexer(repo, zlmLocationMap)
	gbroutes.SetRecordingService(recordingSvc, zlmRegistry, indexer)
	app.ZapLog.Info("GB28181 云端录像 service / Hook 已装配")

	if cfg.Recording.ReconcileIntervalSec <= 0 {
		app.ZapLog.Info("GB28181 云端录像周期对账未启用(reconcile_interval_sec=0)")
	} else {
		interval := time.Duration(cfg.Recording.ReconcileIntervalSec) * time.Second
		recordingReconciler = gbrecording.NewReconciler(repo, recordingSvc, interval, 10*time.Second)
		recordingReconciler.Start(context.Background())
		app.ZapLog.Info("GB28181 云端录像 reconciler 已启动", zap.Duration("interval", interval))
	}

	catalogReconciler := gbrecording.NewCatalogReconciler(repo, zlmLocationMap, zlmRegistry,
		func(n *node.Node) gbrecording.CatalogRecordClient { return gbzlm.NewClientForNode(n) })
	catalogReconciler.ConfigureLookbackDays(cfg.Recording.CatalogPeriodicLookbackDays, cfg.Recording.CatalogManualLookbackDays)
	catalogInterval := time.Duration(cfg.Recording.CatalogReconcileIntervalSec) * time.Second
	recordingCatalogScheduler = gbrecording.NewCatalogReconcileScheduler(catalogReconciler, zlmRegistry, catalogInterval, 10*time.Second)
	recordingCatalogScheduler.Start(context.Background())

	var capabilitySigner *gbrecording.CapabilitySigner
	capabilityKey := os.Getenv(strings.TrimSpace(cfg.Recording.CapabilityKeyEnv))
	keyReused := capabilityKey != "" && (capabilityKey == app.ConfigYml.GetString("token.jwttokensignkey") || capabilityKey == cfg.ZLM.Secret)
	if !keyReused {
		for _, n := range zlmRegistry.List() {
			if capabilityKey != "" && capabilityKey == n.APISecret {
				keyReused = true
				break
			}
		}
	}
	if keyReused {
		app.ZapLog.Warn("GB28181 云端录像 capability 密钥拒绝装配(禁止复用 JWT/ZLM secret)")
	} else if signer, err := gbrecording.NewCapabilitySigner([]byte(capabilityKey), "recording-v1"); err != nil {
		app.ZapLog.Warn("GB28181 云端录像 capability 密钥未配置或长度不足")
	} else {
		capabilitySigner = signer
	}
	catalogService := gbrecording.NewCatalogService(gbrecording.CatalogServiceConfig{
		Repo: repo, Nodes: zlmRegistry, Scheduler: recordingCatalogScheduler, Signer: capabilitySigner,
		ResolveAccess: func(ctx context.Context, userID uint) (gbrecording.CatalogAccess, error) {
			access, err := datascope.ResolveOwnerDeptAccessByUserID(ctx, app.DB(), userID)
			if errors.Is(err, datascope.ErrOwnerDeptAccessDenied) {
				return gbrecording.CatalogAccess{}, gbrecording.ErrCatalogAccessRevoked
			}
			return gbrecording.CatalogAccess{FullAccess: access.FullAccess, DeptIDs: access.DeptIDs}, err
		},
		CheckPermission: func(_ context.Context, userID uint, path, method string) (bool, error) {
			if app.CasbinV2 == nil {
				return false, errors.New("casbin unavailable")
			}
			return app.CasbinV2.Enforce(fmt.Sprintf("user_%d", userID), path, method, "")
		},
		NewDownloader: func(n *node.Node) gbrecording.ContentDownloader { return gbzlm.NewClientForNode(n) },
	})
	gbroutes.SetCloudRecordingCatalogService(catalogService)
	app.ZapLog.Info("GB28181 云端录像目录对账已装配", zap.Duration("interval", catalogInterval))
}

func stopRecordingRuntime() {
	if recordingCatalogScheduler != nil {
		if err := recordingCatalogScheduler.Stop(); err != nil {
			app.ZapLog.Warn("GB28181 云端录像目录对账停止超时", zap.Error(err))
		}
		recordingCatalogScheduler = nil
	}
	if recordingReconciler != nil {
		recordingReconciler.Stop()
		recordingReconciler = nil
	}
	recordingSvc = nil
	gbroutes.SetCloudRecordingCatalogService(nil)
	gbroutes.SetRecordingService(nil, nil, nil)
}

type broadcastSIPAdapter struct{ service *gbtalk.Service }

func (a broadcastSIPAdapter) PrepareBroadcastInvite(ctx context.Context, request gbhandler.BroadcastInviteRequest) (gbhandler.BroadcastInviteResponse, error) {
	prepared, err := a.service.PrepareBroadcastInvite(ctx, gbtalk.BroadcastInvite{
		PeerID: request.PeerID, TargetID: request.TargetID, CallID: request.CallID, CSeq: request.CSeq, SDP: request.SDP,
	})
	return gbhandler.BroadcastInviteResponse{SessionID: prepared.SessionID, AnswerSDP: prepared.AnswerSDP}, err
}

func (a broadcastSIPAdapter) OnBroadcastAck(ctx context.Context, callID string) error {
	return a.service.OnBroadcastAck(ctx, callID)
}

func (a broadcastSIPAdapter) OnBroadcastBye(ctx context.Context, callID string) error {
	return a.service.OnBroadcastBye(ctx, callID)
}

type broadcastRuntimeServer interface {
	SetBroadcastMessageProcessor(gbhandler.BroadcastMessageProcessor)
	SetBroadcastInviteProcessor(gbhandler.BroadcastInviteProcessor)
	ByeBroadcast(context.Context, string) error
}

func setupTalkRuntime(cfg gbconfig.Config, server sipRuntimeServer) {
	var inviter *uac.UAC
	var broadcastRuntime broadcastRuntimeServer
	if server != nil {
		inviter = server.UAC()
		broadcastRuntime, _ = server.(broadcastRuntimeServer)
	}
	if talkSvc != nil || talkCleanupWorker != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		stopTalkRuntime(ctx)
		cancel()
	}
	if inviter == nil || app.DB() == nil || zlmRegistry == nil || zlmScheduler == nil || zlmLocationMap == nil || zlmServerConfigCache == nil {
		gbroutes.SetTalkService(nil, nil)
		app.ZapLog.Info("GB28181 语音对讲 service 跳过装配(依赖未就绪)")
		return
	}
	if !app.DB().Migrator().HasTable(&gbmodels.GbTalkSession{}) {
		gbroutes.SetTalkService(nil, nil)
		app.ZapLog.Warn("GB28181 语音对讲表未迁移,service 跳过装配")
		return
	}
	service := gbtalk.NewService(
		gbtalk.NewGormRepo(app.DB()), zlmRegistry, zlmLocationMap,
		schedulerPickerAdapter{m: zlmScheduler}, zlmServerConfigCache, time.Now,
	)
	service.ConfigureActivation(gbtalk.ActivationDependencies{
		Inviter: inviter, Targets: gbtalk.NewGormTargetLoader(app.DB()),
		Platform:        gbtalk.ActivationPlatform{ServerID: cfg.SIP.ServerID},
		BroadcastSender: inviter, BroadcastDialogs: broadcastRuntime,
	})
	recoveryCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	if err := service.Recover(recoveryCtx); err != nil {
		app.ZapLog.Warn("GB28181 语音对讲启动恢复存在清理失败", zap.Error(err))
	}
	cancel()
	talkSvc = service
	gbroutes.SetTalkService(service, zlmRegistry)
	if broadcastRuntime != nil {
		broadcastRuntime.SetBroadcastMessageProcessor(service)
		broadcastRuntime.SetBroadcastInviteProcessor(broadcastSIPAdapter{service: service})
	}
	inviter.SetTalkByeHandler(func(metadata uac.TalkDialogMetadata) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := service.OnRemoteBye(ctx, metadata.CallID); err != nil {
				app.ZapLog.Warn("设备 TALK BYE 清理失败", zap.String("callId", metadata.CallID), zap.Error(err))
			}
		}()
	})
	talkCleanupWorker = service.StartCleanupWorker(time.Second)
	app.ZapLog.Info("GB28181 语音对讲 service / Hook / 租约扫描已装配")
}

func stopTalkRuntime(ctx context.Context) {
	if talkCleanupWorker != nil {
		talkCleanupWorker.Stop()
		talkCleanupWorker = nil
	}
	service := talkSvc
	if broadcastRuntime, ok := sipServer.(broadcastRuntimeServer); ok {
		broadcastRuntime.SetBroadcastMessageProcessor(nil)
		broadcastRuntime.SetBroadcastInviteProcessor(nil)
	}
	if sipServer != nil {
		if sipServer.UAC() != nil {
			sipServer.UAC().SetTalkByeHandler(nil)
		}
	}
	if service != nil {
		if err := service.Shutdown(ctx); err != nil {
			app.ZapLog.Warn("GB28181 语音对讲关闭清理存在失败", zap.Error(err))
		}
	}
	talkSvc = nil
	gbroutes.SetTalkService(nil, nil)
}

// ReloadSIP 触发一次热重启:关旧 SIP 依赖 → 从 DB 重新读配置 → 启新的.
// 由 SetupController.SaveConfig 保存后调用,让用户不需要重启进程.
// 失败时 runtime state 会被 MarkFailed,不 panic.
func ReloadSIP() error {
	sipLifecycleMu.Lock()
	defer sipLifecycleMu.Unlock()

	cfg := gbconfig.Load()
	if !cfg.Enabled {
		return errors.New("gb28181 未在 config.yml 启用,无法热启动")
	}
	sipCfg, ok, err := loadSIPConfigFromDB(cfg)
	if err != nil {
		sipRuntimeStatus.MarkFailed(err.Error())
		return err
	}
	if !ok {
		sipRuntimeStatus.MarkUnconfigured()
		return errors.New("DB 里没有 SIP 配置,无法热启动")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stopSIPDependencies(ctx)
	return startSIPDependencies(sipCfg)
}

func setupTraceController(cfg gbconfig.Config, runtime gbtrace.Runtime) {
	access := gbcontrollers.NewGormTraceAdminAccess(app.DB())
	var query gbcontrollers.TraceQueryService
	var capture gbcontrollers.TraceCaptureService
	if cfg.Trace.Enabled {
		query = gbtrace.QueryServiceFromRuntime(runtime)
		if app.DB() != nil {
			capture = gbtrace.NewCaptureService(app.DB(), access, time.Now)
		}
	}
	traceCtrl := gbcontrollers.NewTraceController(query, capture, access)
	// SSE stream hub 从 module 拿,给 TraceController.Stream 用
	if streamer, ok := runtime.(interface{ StreamHub() *gbtrace.StreamHub }); ok && cfg.Trace.Enabled {
		traceCtrl.SetStreamProvider(traceStreamProvider{hub: streamer.StreamHub()})
	}
	gbroutes.SetTraceController(traceCtrl)
}

// traceStreamProvider 适配 TraceStreamProvider 接口
type traceStreamProvider struct {
	hub *gbtrace.StreamHub
}

func (p traceStreamProvider) Hub() *gbtrace.StreamHub { return p.hub }

// setupZLMRegistry 启动时从 DB 加载所有节点;若空表,用 yaml cfg.ZLM seed 第一节点
// DB 不可达则 registry 为 nil(继续走 deprecated 单节点路径,降级容错)
func setupZLMRegistry(cfg gbconfig.Config) {
	if app.DB() == nil {
		app.ZapLog.Warn("GB28181 DB 不可用,跳过 ZLM Registry 装配(走 deprecated 单节点路径)")
		return
	}
	repo := gbzlmrepo.NewMetaNodeRepo(app.DB())
	reg := node.NewRegistry(repo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := reg.LoadAll(ctx); err != nil {
		app.ZapLog.Warn("GB28181 ZLM Registry LoadAll 失败,可能 meta_node 表未建", zap.Error(err))
		return
	}
	if len(reg.List()) == 0 && cfg.ZLM.Host != "" {
		// 空表 + yaml 有配置 → seed 第一节点(单节点过渡)
		uuidStr := uuid.NewString()
		_, err := reg.Add(ctx, node.Node{
			Name:            "zlm-default",
			Host:            cfg.ZLM.Host,
			ReceiveHost:     cfg.ZLM.ReceiveHost,
			PlaybackHost:    cfg.ZLM.PlaybackHost,
			APIPort:         cfg.ZLM.HTTPPort,
			APISecret:       cfg.ZLM.Secret,
			MediaServerUUID: uuidStr,
			Weight:          50,
			State:           node.StateActive,
			RTPPortStart:    30000,
			RTPPortEnd:      35000,
		})
		if err != nil {
			app.ZapLog.Warn("GB28181 ZLM 默认节点 seed 失败", zap.Error(err))
		} else {
			app.ZapLog.Info("GB28181 ZLM 已 seed 默认节点", zap.String("uuid", uuidStr), zap.String("host", cfg.ZLM.Host))
		}
	}
	zlmRegistry = reg
	zlmServerConfigCache = gbzlm.NewServerConfigCache(gbzlm.FetchViaRegistry(reg))
	app.ZapLog.Info("GB28181 ZLM Registry 已装配", zap.Int("nodes", len(reg.List())))

	// 启动主动探活:治"重启后 30 秒点播黑洞"(spec: zlm-startup-probe)
	// 独立 goroutine 不阻塞主流程;失败降级 = 当前行为(等 ZLM 下次心跳翻转)
	runStartupProbe(reg)
}

// runStartupProbe 对 Registry 中所有节点跑一次主动探活。
//
// 独立 goroutine 内执行,3 秒超时,不阻塞 bootstrap。
// 探活成功 → registry.MarkActive(节点 State 从 offline 翻回 active,内存+DB)
// 探活失败 → 保持 State 不动(下次 ZLM 心跳到达时 Collector 会自愈)
func runStartupProbe(reg *node.Registry) {
	factory := func(n *node.Node) gbzlmprobe.Client { return gbzlm.NewClientForNode(n) }
	prober := gbzlmprobe.New(reg, factory, 3*time.Second, app.ZapLog)
	go func() {
		// 独立 background ctx:探活是一次性任务,不跟 heartbeat 生命周期绑定
		// (heartbeatCancel 此时还没建;单节点探活最多 3s 就退,不会泄漏)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		prober.Run(ctx)
	}()
}

// setupZLMScheduler 装配调度器 Manager(M2 新增)
//
// 流程:
//  1. zlmRegistry 为 nil → 跳过,Manager 留空(SIP play 改造侧自降级到首节点)
//  2. 从 scheduler_setting 表读 algorithm 名(单行 id=1)
//  3. Manager.Switch(algorithm);失败 fallback "roundrobin";再失败留 nil
func setupZLMScheduler() {
	if zlmRegistry == nil {
		app.ZapLog.Warn("GB28181 ZLM Registry 未装配,跳过 Scheduler 装配")
		return
	}
	factory := gbzlmsched.NewFactory(zlmRegistry)
	manager := gbzlmsched.NewManager(factory)

	algorithm := "roundrobin"
	if app.DB() != nil {
		settingRepo := gbzlmrepo.NewSchedulerSettingRepo(app.DB())
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := settingRepo.GetCurrent(ctx)
		switch {
		case err != nil:
			app.ZapLog.Warn("GB28181 scheduler_setting 读取失败,fallback roundrobin", zap.Error(err))
		case s == nil:
			app.ZapLog.Info("GB28181 scheduler_setting 表空,fallback roundrobin")
		default:
			algorithm = s.Algorithm
		}
	}

	if err := manager.Switch(algorithm); err != nil {
		app.ZapLog.Warn("GB28181 Scheduler.Switch 失败,fallback roundrobin",
			zap.String("requested", algorithm), zap.Error(err))
		if err2 := manager.Switch("roundrobin"); err2 != nil {
			app.ZapLog.Error("GB28181 Scheduler 装配失败(roundrobin fallback 也失败,Manager 留空)",
				zap.Error(err2))
			return
		}
		algorithm = "roundrobin"
	}
	zlmScheduler = manager
	app.ZapLog.Info("GB28181 ZLM Scheduler 已装配", zap.String("algorithm", algorithm))
}

// setupZLMSchedulerLog 装配调度日志服务 + 启动 24h prune ticker(M3 T3.3)
//
// 流程:
//  1. zlmScheduler 为 nil → 跳过(没 Manager 就没 Pick,没日志可写)
//  2. app.DB() 为 nil → 跳过(无 DB 持久化能力)
//  3. 起 LogService(buffer 1000)+ Manager.SetLogService 注入
//  4. 起 24h ticker,跑 PruneOlderThan(now-7d),失败 zap.Warn
//
// 整套通过 schedulerLogCancel 控制退出。
func setupZLMSchedulerLog() {
	if zlmScheduler == nil {
		app.ZapLog.Warn("GB28181 Scheduler 未装配,跳过调度日志服务")
		return
	}
	if app.DB() == nil {
		app.ZapLog.Warn("GB28181 DB 不可用,跳过调度日志服务")
		return
	}
	inner := gbzlmrepo.NewGormSchedulerLogRepo(app.DB())
	adapter := schedulerLogRepoAdapter{inner: inner}
	svc := gbzlmsched.NewLogService(adapter, 1000)
	ctx, cancel := context.WithCancel(context.Background())
	svc.Start(ctx)

	zlmScheduler.SetLogService(svc)
	zlmSchedulerLog = svc
	schedulerLogCancel = cancel

	go pruneSchedulerLogDaily(ctx, svc)

	app.ZapLog.Info("GB28181 ZLM 调度日志服务已启动(buffer=1000, retention=7d)")
}

// pruneSchedulerLogDaily 每 24h 跑一次 PruneOlderThan(now-7d)
//
// 启动时立即跑一次(冷启清遗留),之后每 24h 一次。
// 失败 zap.Warn 不中断 ticker。
func pruneSchedulerLogDaily(ctx context.Context, svc *gbzlmsched.LogService) {
	prune := func() {
		pCtx, pCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer pCancel()
		n, err := svc.PruneOlderThan(pCtx, time.Now().Add(-7*24*time.Hour))
		if err != nil {
			app.ZapLog.Warn("GB28181 调度日志 prune 失败", zap.Error(err))
			return
		}
		if n > 0 {
			app.ZapLog.Info("GB28181 调度日志 prune 完成", zap.Int64("removed", n))
		}
	}
	prune() // 启动时立即清一轮
	tk := time.NewTicker(24 * time.Hour)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			prune()
		}
	}
}

// setupZLMSchedulerController 装配算法切换 + 日志查询 controller(M3 T3.3)
//
// 依赖 zlmScheduler(必须)+ zlmSchedulerLog(可空)+ scheduler_setting repo(可空)。
// 没 Manager 则跳过(没意义);DB 不可用则 SettingWriter 传 nil(切换仅内存)。
func setupZLMSchedulerController() {
	if zlmScheduler == nil {
		app.ZapLog.Warn("GB28181 Scheduler 未装配,跳过 Scheduler Controller")
		return
	}
	var settingWriter gbcontrollers.SchedulerSettingWriter
	if app.DB() != nil {
		settingWriter = gbzlmrepo.NewSchedulerSettingRepo(app.DB())
	}
	ctrl := gbcontrollers.NewZLMSchedulerController(zlmScheduler, zlmSchedulerLog, settingWriter)
	gbroutes.SetZLMSchedulerController(ctrl)
	app.ZapLog.Info("GB28181 ZLM Scheduler Controller 已装配")
}

// pickInitialClient 取 Registry 首节点构造 Client(Apply Hook 配置用)
// Registry 在本函数之前已通过 setupZLMRegistry 加载并按需 yaml→DB seed,
// 因此正常路径下 List() 必然非空;若 DB 不可达 / seed 失败则返回 nil,上层降级跳过下发。
func pickInitialClient(_ gbconfig.Config) *gbzlm.Client {
	if zlmRegistry == nil {
		return nil
	}
	list := zlmRegistry.List()
	if len(list) == 0 {
		return nil
	}
	return gbzlm.NewClientForNode(list[0])
}

// runMetricsCleanup 周期清理过期配对(防内存泄漏);30s TTL
func runMetricsCleanup(a *metrics.Aggregator, stop <-chan struct{}) {
	tk := time.NewTicker(30 * time.Second)
	defer tk.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tk.C:
			if n := a.CleanupExpiredPairs(30 * time.Second); n > 0 {
				app.ZapLog.Debug("GB28181 metrics 配对清理", zap.Int("cleaned", n))
			}
		}
	}
}

// civilCodeServiceAdapter 适配 civilcode.Service 到 catalog.CivilCodeLookup 接口
type civilCodeServiceAdapter struct {
	svc *civilcode.Service
}

func (a *civilCodeServiceAdapter) Lookup(code string) interface{} {
	return a.svc.Lookup(code) // *SysCivilCode → interface{}
}

// setupCivilCodeService 初始化行政区划字典服务 + warm cache + 注入到 catalog
//
// 流程:
//  1. app.DB() 为 nil → 跳过(无 DB,catalog 4 层兜底降级到 L4:000000)
//  2. 构造 civilcode.Service + WarmCache(~3500 行,~400KB)
//  3. 注入到 catalog.SetCivilCodeLookup(L1/L2 校验字典存在性用)
//
// WarmCache 失败(表不存在 / 迁移未跑)→ zap.Warn + 跳过注入,catalog 降级 L4
func setupCivilCodeService() {
	if app.DB() == nil {
		app.ZapLog.Warn("GB28181 DB 不可用,跳过 CivilCode 字典服务(catalog 4 层兜底降级 L4:000000)")
		return
	}
	if seeded, err := civilcode.SeedIfEmpty(app.DB()); err != nil {
		app.ZapLog.Warn("GB28181 CivilCode 字典 seed 失败,catalog 4 层兜底可能降级 L4",
			zap.Error(err))
	} else if seeded > 0 {
		app.ZapLog.Info("GB28181 CivilCode 字典 seed 完成", zap.Int("count", seeded))
	}
	svc := civilcode.NewService(app.DB())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := svc.WarmCache(ctx); err != nil {
		app.ZapLog.Warn("GB28181 CivilCode WarmCache 失败(可能 sys_civil_code 表未建),catalog 4 层兜底降级 L4",
			zap.Error(err))
		return
	}
	catalog.SetCivilCodeLookup(&civilCodeServiceAdapter{svc: svc})
	app.ZapLog.Info("GB28181 CivilCode 字典服务已装配", zap.Int("count", svc.AllCount()))
}

// Stop 优雅关闭 GB28181 SIP 服务 + 离线扫描器(纳入主进程退出流程)
func Stop() {
	sipLifecycleMu.Lock()
	defer sipLifecycleMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stopSIPDependencies(ctx)
	if heartbeatCancel != nil {
		heartbeatCancel()
		heartbeatCancel = nil
	}
	if schedulerLogCancel != nil {
		schedulerLogCancel() // 关 prune ticker 的 ctx
		schedulerLogCancel = nil
	}
	if zlmSchedulerLog != nil {
		zlmSchedulerLog.Stop() // drain pending,等 worker 退出
		zlmSchedulerLog = nil
	}
	if metricsCleanupStop != nil {
		close(metricsCleanupStop)
		metricsCleanupStop = nil
	}
}

func startPositionHistoryPruner() {
	if positionHistoryPruneCancel != nil || app.DB() == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	positionHistoryPruneCancel = cancel
	go func() {
		prune := func() {
			deleted, err := subscribe.PrunePositionHistory(ctx, app.DB(), time.Now(), subscribe.PositionHistoryRetentionDays())
			if err != nil {
				app.ZapLog.Warn("清理位置历史失败", zap.Error(err))
				return
			}
			if deleted > 0 {
				app.ZapLog.Info("清理过期位置历史", zap.Int64("deleted", deleted))
			}
		}
		prune()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				prune()
			}
		}
	}()
}

// buildSnapshotService 通道快照 service 装配。
//
// 依赖:
//   - zlmRegistry:按 nodeID 查节点,构造 zlm.Client 走 getSnap
//   - httpserver.serverroot / serverrootpath:落盘 & URL 前缀
//
// 装配失败返 nil,调用侧 opt-out(主链路不受影响)。
func buildSnapshotService() *snapshot.Service {
	if zlmRegistry == nil {
		app.ZapLog.Info("GB28181 通道快照 skip:zlmRegistry 未初始化(单节点部署?)")
		return nil
	}

	serverroot := app.ConfigYml.GetString("httpserver.serverroot")
	serverrootpath := app.ConfigYml.GetString("httpserver.serverrootpath")
	if serverroot == "" {
		serverroot = "./resource/public"
	}
	if serverrootpath == "" {
		serverrootpath = "/public"
	}

	// resolveNode 从 nodeID(字符串)解析出对应 Node;单节点场景空串取首个 active
	resolveNode := func(nodeID string) (*node.Node, error) {
		if nodeID == "" {
			nodes := zlmRegistry.ListActive()
			if len(nodes) == 0 {
				return nil, errors.New("无可用 ZLM 节点")
			}
			return nodes[0], nil
		}
		id, err := strconv.ParseInt(nodeID, 10, 64)
		if err != nil {
			return nil, err
		}
		n, ok := zlmRegistry.Get(id)
		if !ok {
			return nil, errors.New("node 不存在")
		}
		return n, nil
	}

	getClient := func(nodeID string) (snapshot.ZLMClient, error) {
		n, err := resolveNode(nodeID)
		if err != nil {
			return nil, err
		}
		return gbzlm.NewClientForNode(n), nil
	}

	if zlmServerConfigCache == nil {
		return nil
	}

	// BuildStreamURL:构造 ZLM 内部 rtsp 拉流 URL(比 http-flv 稳,让 FFmpeg 拉自己更可靠)
	buildStreamURL := func(ctx context.Context, nodeID, streamID string) (string, error) {
		n, err := resolveNode(nodeID)
		if err != nil {
			return "", err
		}
		cfg, err := zlmServerConfigCache.Get(ctx, n.ID)
		if err != nil {
			return "", fmt.Errorf("拉 ZLM 端口配置失败: %w", err)
		}
		if cfg.RTSPPort == 0 {
			return "", fmt.Errorf("ZLM node %d 未暴露 rtsp.port", n.ID)
		}
		// ZLM 单端口收流后 stream 落在 rtp app 下(见 play.Service zlmApp 常量)
		return fmt.Sprintf("rtsp://%s:%d/rtp/%s", n.Host, cfg.RTSPPort, streamID), nil
	}

	svc := snapshot.New(snapshot.Config{
		UploadRoot:     serverroot,
		URLPrefix:      serverrootpath,
		DedupTTL:       30 * time.Second,
		DelayBefore:    2 * time.Second,
		ZLMTimeout:     5,
		ZLMExpire:      30,
		GetClient:      getClient,
		BuildStreamURL: buildStreamURL,
		Repo:           snapshot.NewGormRepo(app.DB()),
		Logger:         app.ZapLog.Named("gb.snapshot"),
	})
	return svc
}
