package gb28181

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	"uvplatform.cn/uvp-gb28181/app/gb28181/civilcode"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbdashboard "uvplatform.cn/uvp-gb28181/app/gb28181/dashboard"
	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
	"uvplatform.cn/uvp-gb28181/app/gb28181/devicecapture"
	gbhandler "uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play/reconciler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	gbplayback "uvplatform.cn/uvp-gb28181/app/gb28181/playback"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordingplan"
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
	"uvplatform.cn/uvp-gb28181/app/gb28181/trace/diagnosis"
	"uvplatform.cn/uvp-gb28181/app/gb28181/traffic"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/upgrade"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
	gbzlm "uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/heartbeat"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	gbzlmprobe "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/probe"
	gbzlmrepo "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
	gbzlmsched "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
	gbzlmsvc "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/scheduler/executors"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// schedulerPickerAdapter 把 scheduler.Manager 适配为 play.NodePicker(避免反向依赖 play 包)
type schedulerPickerAdapter struct {
	m *gbzlmsched.Manager
}

func (a schedulerPickerAdapter) Pick(ctx context.Context, inv play.PickContext) (*node.Node, error) {
	return a.m.Pick(ctx, gbzlmsched.InviteContext{
		DeviceID:        inv.DeviceID,
		ChannelID:       inv.ChannelID,
		StreamID:        inv.StreamID,
		PreferredNodeID: inv.PreferredNodeID,
	})
}

// schedulerLogRepoAdapter 把 repo.GormSchedulerLogRepo 适配为 scheduler.SchedulerLogRepo
//
// 两边各自定义同构 struct 避免 repo → scheduler 反向依赖,这里手工字段映射。
type schedulerLogRepoAdapter struct {
	inner *gbzlmrepo.GormSchedulerLogRepo
}

var _ gbzlmsched.SchedulerLogRepo = schedulerLogRepoAdapter{}
var _ gbzlmsched.SchedulerLogFilteredRepo = schedulerLogRepoAdapter{}

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
	return schedulerLogRowsToDomain(rows), nil
}

// ListFiltered keeps the production adapter on the repository's typed,
// parameterized query path. Without this optional method LogService falls
// back to List(1000), which can silently miss older matching rows.
func (a schedulerLogRepoAdapter) ListFiltered(ctx context.Context, filter gbzlmsched.SchedulerLogFilter) ([]gbzlmsched.SchedulerLog, error) {
	rows, err := a.inner.ListFiltered(ctx, schedulerLogFilterToRepo(filter))
	if err != nil {
		return nil, err
	}
	return schedulerLogRowsToDomain(rows), nil
}

func schedulerLogRowsToDomain(rows []gbzlmrepo.SchedulerLogRow) []gbzlmsched.SchedulerLog {
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
	return out
}

func schedulerLogFilterToRepo(filter gbzlmsched.SchedulerLogFilter) gbzlmrepo.SchedulerLogFilter {
	algorithm := filter.Algorithm
	if algorithm == "" {
		algorithm = filter.Policy
	}
	return gbzlmrepo.SchedulerLogFilter{
		From:      filter.From,
		To:        filter.To,
		NodeID:    filter.NodeID,
		Algorithm: algorithm,
		Policy:    filter.Policy,
		Result:    string(filter.Result),
		StreamID:  filter.StreamID,
		Limit:     filter.Limit,
	}
}

func (a schedulerLogRepoAdapter) PruneOlderThan(ctx context.Context, t time.Time) (int64, error) {
	return a.inner.PruneOlderThan(ctx, t)
}

type sipRuntimeServer interface {
	SetRecorder(metrics.Recorder)
	SetErrorHandler(func(error))
	SetPTZMessageProcessor(gbhandler.PTZMessageProcessor)
	SetRecordInfoSink(gbhandler.RecordInfoSink)
	SetSnapshotSink(gbhandler.SnapshotSink)
	SetPlaybackEndSink(gbhandler.PlaybackEndSink)
	SetSubscriptionWaker(gbhandler.SubscriptionWaker)
	SetSubscriptionNotifier(gbhandler.SubscriptionNotifier)
	SetAlarmMessageProcessor(gbhandler.AlarmMessageProcessor)
	Start() error
	UAC() *uac.UAC
	Shutdown(context.Context) error
}

func diagnosisSinkForServer(server sipRuntimeServer) diagnosis.DiagnosticSink {
	provider, ok := server.(interface{ TraceRuntime() gbtrace.Runtime })
	if !ok {
		return diagnosis.NoopSink{}
	}
	return gbtrace.DiagnosisSinkFromRuntime(provider.TraceRuntime())
}

var playbackService *gbplayback.Service
var playbackRegistry *gbplayback.Registry
var deviceCaptureRegistry *devicecapture.Registry

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
var firmwareUpgradeService *upgrade.Service
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

// playAuthMetrics records fixed-outcome authorization counters in memory.
var playAuthMetrics *playauth.Metrics

// playSessions 全局点播会话管理(让 hook 端点 on_stream_none_reader/on_rtp_server_timeout 也能查到)
var playSessions = uac.NewSessionManager()

// metricsAgg 全局指标聚合器(供 controllers/dashboard 暴露,供 SIP 路径埋点)
var metricsAgg *metrics.Aggregator
var metricsRecorder metrics.Recorder
var metricsPersistCancel context.CancelFunc
var dashboardRetentionCancel context.CancelFunc

// metricsCleanupStop 控制 TTL 清理 goroutine 退出
var metricsCleanupStop chan struct{}

// heartbeatCancel 控制 Watcher goroutine 退出(M2 新增)
var heartbeatCancel context.CancelFunc
var trafficCancel context.CancelFunc
var trafficResolver *traffic.AttributionResolver
var trafficRealtime *traffic.RealtimeStore

// playReconciler 兜底对账 goroutine(通道播放状态显示 T7 新增)
var playReconciler *reconciler.Reconciler

var recordingSvc *gbrecording.Service
var recordingRepo *gbrecording.GormRepo
var recordingReconciler *gbrecording.Reconciler
var recordingCatalogScheduler *gbrecording.CatalogReconcileScheduler
var recordingCatalogService *gbrecording.CatalogService
var recordingPlanEngine *recordingplan.Engine
var recordingPlanLeases *play.SourceLeaseRegistry

// workClaimReconciler 回收被中断的启动留下的通道占用：占用停在 starting 时，
// 引擎自身的读取路径(Observe/Get)无法推进它，只有 Stop 能释放。没有这个兜底，
// 一次失败的启动会让该通道的云录像/作业录像永久返回"资源已被其他任务占用"。
var workClaimReconciler *workrecording.AbandonedStartReconciler

var talkSvc *gbtalk.Service
var talkRepo *gbtalk.GormRepo
var talkCleanupWorker *gbtalk.CleanupWorker

// zlmSchedulerLog 调度日志服务(T3.3 新增,可为 nil 降级)
var zlmSchedulerLog *gbzlmsched.LogService

// schedulerLogCancel 控制调度日志 worker + prune ticker 退出
var schedulerLogCancel context.CancelFunc

// PlayService 返回点播 service(可能为 nil,gb28181 未启用 / UAC 初始化失败时)
func PlayService() *play.Service { return playSvc }

func PlayAuthorizationMetrics() *playauth.Metrics { return playAuthMetrics }

// SIPTraceRuntimeEnabled reports whether the current SIP transport has trace
// hooks attached. It intentionally does not treat a degraded trace store
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
	cascadeCipher, err := loadCascadeCredentialCipher()
	if err != nil {
		app.ZapLog.Warn("国标级联凭据密钥未配置,列表可用但密码写入和启用受限",
			zap.String("env", cascadeCredentialKeyEnv))
	}
	setupCascadeManagement(nil, cascadeCipher)
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
	metricsRecorder = metricsAgg
	var persistentDashboard *metrics.PersistentDashboardSnapshot
	if db := app.DB(); db != nil && db.Migrator().HasTable(&gbmodels.GbSipMetricMinute{}) && db.Migrator().HasTable(&gbmodels.GbSipMetricFlush{}) && db.Migrator().HasTable(&gbmodels.GbSipMetricGap{}) {
		persistent := metrics.NewPersistentRecorder(db, metricsAgg)
		persistCtx, cancel := context.WithCancel(context.Background())
		metricsPersistCancel = cancel
		metricsRecorder = persistent
		go persistent.Run(persistCtx, time.Second)
		persistentDashboard = metrics.NewPersistentDashboardSnapshot(db, metricsAgg, time.Local)
	}
	metricsCleanupStop = make(chan struct{})
	go runMetricsCleanup(metricsAgg, metricsCleanupStop)
	if persistentDashboard != nil {
		gbroutes.SetMetricsProvider(func() *metrics.Aggregator { return metricsAgg }, persistentDashboard)
	} else {
		gbroutes.SetMetricsProvider(func() *metrics.Aggregator { return metricsAgg })
	}
	if db := app.DB(); db != nil && db.Migrator().HasTable(&gbmodels.GbPlayAttempt{}) {
		gbroutes.SetPlayAttemptStore(gbdashboard.NewPlayAttemptStore(db))
		dashboardRetentionCancel = startDashboardRetentionRuntime(db, 24*time.Hour, func(result gbdashboard.RetentionResult, err error) {
			if err != nil {
				app.ZapLog.Warn("清理仪表盘历史事实失败", zap.Error(err))
				return
			}
			if result.Total() > 0 {
				app.ZapLog.Info("清理过期仪表盘历史事实", zap.Int64("deleted", result.Total()))
			}
		})
	} else {
		gbroutes.SetPlayAttemptStore(nil)
	}
	setupTraceController(cfg, nil)
	setupZLMRegistry(cfg)
	setupTrafficRuntime()
	setupZLMScheduler()
	setupZLMSchedulerLog()
	setupZLMSchedulerController()

	if zlmRegistry != nil {
		adapter := gbzlm.NewServiceAdapter(cfg.Media)
		tuning := gbzlmsvc.MediaTuning{
			HookBaseURL:             cfg.Media.HookBaseURL,
			HookRequireTLS:          cfg.Media.HookRequireTLS,
			HookHost:                cfg.Media.HookHost,
			HookPort:                cfg.Media.HookPort,
			StreamNoneReaderTimeout: cfg.Media.StreamNoneReaderTimeout,
			RTPServerTimeout:        cfg.Media.RTPServerTimeout,
		}
		nodeSvc := gbzlmsvc.NewNodeService(zlmRegistry, adapter, tuning)
		restartCoordinator := gbzlmsvc.NewRestartCoordinator(zlmRegistry)
		nodeSvc.SetRestartCoordinator(restartCoordinator)
		nodeSvc.SetLogger(app.ZapLog)
		cfgSvc := gbzlmsvc.NewConfigService(zlmRegistry, adapter)
		gbroutes.SetZLMNodeController(gbcontrollers.NewZLMNodeController(nodeSvc))
		gbroutes.SetZLMConfigController(gbcontrollers.NewZLMConfigController(cfgSvc))
		setupZLMManagementCore(nodeSvc, restartCoordinator)
		gbroutes.SetRestartStartedNotifier(restartCoordinator)
		app.ZapLog.Info("GB28181 ZLM 节点/配置 controller 已装配")

		collector := heartbeat.NewCollectorWithNotifier(zlmRegistry, nodeSvc, restartCoordinator)
		gbroutes.SetKeepaliveCollector(collector)
		watcher := heartbeat.NewWatcherWithNotifier(zlmRegistry, heartbeat.RealClock(), 30*time.Second, 90*time.Second, restartCoordinator)
		var hbCtx context.Context
		hbCtx, heartbeatCancel = context.WithCancel(context.Background())
		watcher.Start(hbCtx)
		app.ZapLog.Info("GB28181 ZLM 心跳 Collector / Watcher 已启动",
			zap.Duration("checkInterval", 30*time.Second),
			zap.Duration("offlineThreshold", 90*time.Second))

		threadPoller := heartbeat.NewThreadLoadPoller(zlmRegistry, adapter, 30*time.Second)
		threadPoller.Start(hbCtx)
		app.ZapLog.Info("GB28181 ZLM 线程负载 Poller 已启动", zap.Duration("interval", 30*time.Second))

		go func() {
			initialCtx, cancel := context.WithTimeout(hbCtx, 30*time.Second)
			for _, result := range nodeSvc.ApplyActiveConfigs(initialCtx) {
				if result.Err != nil {
					app.ZapLog.Warn("GB28181 ZLM 节点配置启动收敛失败",
						zap.Int64("nodeId", result.NodeID), zap.String("name", result.Name), zap.Error(result.Err))
					continue
				}
				app.ZapLog.Info("GB28181 ZLM 节点配置启动收敛完成",
					zap.Int64("nodeId", result.NodeID), zap.String("name", result.Name))
			}
			cancel()

			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-hbCtx.Done():
					return
				case <-ticker.C:
					for _, mediaNode := range zlmRegistry.ListActive() {
						if !zlmRegistry.IsAutoOnDemandReady(mediaNode.ID) {
							nodeSvc.ScheduleConfigConvergence(mediaNode.ID)
						}
					}
				}
			}
		}()
	}

	zlmClient = pickInitialClient(cfg)
	if zlmClient == nil {
		app.ZapLog.Warn("GB28181 ZLM 初始 Client 未构造(Registry 空),跳过 Hook 配置下发")
		return
	}
}

func startDashboardRetentionRuntime(db *gorm.DB, interval time.Duration, report func(gbdashboard.RetentionResult, error)) context.CancelFunc {
	if db == nil || !db.Migrator().HasTable(&gbmodels.GbSipMetricMinute{}) || !db.Migrator().HasTable(&gbmodels.GbSipMetricFlush{}) ||
		!db.Migrator().HasTable(&gbmodels.GbSipMetricGap{}) || !db.Migrator().HasTable(&gbmodels.GbPlayAttempt{}) {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	go gbdashboard.NewDashboardRetention(db, 500).Run(ctx, interval, report)
	return cancel
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
	if err := gbconfig.EnsurePlayAuthActiveKey(app.ConfigYml); err != nil {
		if gbconfig.CurrentPlayAuthSettings().Enabled {
			sipRuntimeStatus.MarkFailed(err.Error())
			app.ZapLog.Error("GB28181 播放鉴权密钥初始化失败", zap.Error(err))
			return
		}
		app.ZapLog.Warn("GB28181 播放鉴权密钥初始化失败,鉴权保持关闭", zap.Error(err))
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
	agent := gbsecurity.NewFirewallAgentClient(socketPath, 2*time.Second)
	secret := []byte(os.Getenv("UVP_GB28181_NONCE_SECRET"))
	if app.DB() != nil {
		store := gbsecurity.NewGormStore(app.DB())
		runtime, err := gbsecurity.NewPersistentRuntime(context.Background(), store, clock, agent, secret)
		if err == nil {
			gbroutes.SetSecurityRuntime(runtime)
			return runtime
		}
		app.ZapLog.Warn("GB28181 安全持久化运行时装配失败,降级为内存 protect 并关闭新增IP自动封禁", zap.Error(err))
	}
	runtime := gbsecurity.NewRuntime(gbsecurity.DefaultPolicy(), clock, agent, secret)
	// Without authenticated-device history an automatic source ban could lock
	// out a legitimate shared egress. Admission still rejects unsafe packets.
	runtime.SetAutoBanEnabled(false)
	gbroutes.SetSecurityRuntime(runtime)
	return runtime
}

// startSIPDependencies 启动 SIP server + 所有依赖 UAC 的服务(点播/订阅/离线扫描等).
// 幂等:reload 时可先 stopSIPDependencies 再调这里.
func startSIPDependencies(cfg gbconfig.Config) error {
	protectionCtx, protectionCancel := context.WithTimeout(context.Background(), 30*time.Second)
	protectionErr := recoverRecordingProtection(protectionCtx)
	protectionCancel()
	if protectionErr != nil {
		return protectionErr
	}
	runtime := setupSecurityRuntime()
	srv, err := startSIPRuntime(cfg, metricsRecorder, sipRuntimeStatus, func(cfg gbconfig.Config) (sipRuntimeServer, error) {
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
	var newFirmwareUpgradeService *upgrade.Service
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
		if app.DB() != nil {
			newFirmwareUpgradeService, err = upgrade.NewService(app.DB(), u, newPTZService, time.Now)
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
				return fmt.Errorf("装配设备固件升级 service 失败: %w", err)
			}
			setter, ok := srv.(interface {
				SetUpgradeProcessor(gbhandler.UpgradeMessageProcessor)
			})
			if !ok {
				newRecordQueryService.Close()
				recordQueryService = nil
				recordQueryMetrics = nil
				gbroutes.SetDeviceMgmtRecordQueryRuntime(nil, gbconfig.RecordQueryConfig{}, nil)
				srv.SetRecordInfoSink(nil)
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = srv.Shutdown(shutdownCtx)
				cancel()
				sipRuntimeStatus.MarkFailed("SIP server 未提供设备升级消息路由")
				return errors.New("SIP server 未提供设备升级消息路由")
			}
			setter.SetUpgradeProcessor(newFirmwareUpgradeService)
		}
	}
	sipServer = srv
	if err := startCascadeRuntime(cfg, srv); err != nil {
		app.ZapLog.Error("国标级联运行时装配失败,设备侧 SIP 继续运行", zap.Error(err))
	} else {
		app.ZapLog.Info("国标级联运行时已装配")
	}
	if traceServer, ok := srv.(interface{ TraceRuntime() gbtrace.Runtime }); ok {
		setupTraceController(cfg, traceServer.TraceRuntime())
	}

	// 装配依赖 UAC 的 processor(手动 catalog 刷新按钮、PTZ、订阅、告警)
	if u := srv.UAC(); u != nil {
		gbroutes.SetDeviceMgmtCatalogTrigger(gbhandler.NewUACCatalogTrigger(u))
		ptzService = newPTZService
		ptzScheduler = newPTZScheduler
		firmwareUpgradeService = newFirmwareUpgradeService
		gbroutes.SetDeviceMgmtPTZRuntime(u, ptzService)
		gbroutes.SetDeviceMgmtFirmwareUpgradeService(firmwareUpgradeService)
		captureRoot := app.ConfigYml.GetString("httpserver.serverroot")
		if captureRoot == "" {
			captureRoot = "./resource/public"
		}
		deviceCaptureRegistry = devicecapture.NewRegistry(captureRoot)
		gbroutes.SetDeviceMgmtCaptureRuntime(deviceCaptureRegistry)
		srv.SetSnapshotSink(deviceCaptureRegistry)
		ptzScheduler.Start(context.Background())
		subscriptionService = subscribe.NewService(app.DB(), u, time.Now)
		gbroutes.SetDeviceMgmtSubscriptionManager(subscriptionService)
		subscriptionService.SetProcessor(gbmodels.SubscriptionKindCatalog, subscribe.NewCatalogProcessor(catalog.New(app.DB())))
		subscriptionService.SetProcessor(gbmodels.SubscriptionKindMobilePosition, subscribe.NewPositionProcessor(app.DB(), time.Now))
		subscriptionService.SetProcessor(gbmodels.SubscriptionKindAlarm, subscribe.NewAlarmProcessor(app.DB(), time.Now))
		subscriptionService.SetProcessor(gbmodels.SubscriptionKindPTZPrecisePosition, subscribe.NewPTZProcessor(newPTZService))
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

	playAuthSettings := gbconfig.CurrentPlayAuthSettings()
	activePlayKey, previousPlayKey := gbconfig.PlayAuthKeyMaterialFrom(app.ConfigYml)
	playSigner, signerErr := buildPlaySigner(
		playAuthSettings,
		activePlayKey,
		previousPlayKey,
		app.ConfigYml.GetString("token.jwttokensignkey"),
		cfg.ZLM.Secret,
	)
	var playAuthorization *playauth.AuthorizationService
	playAuthMetrics = nil
	if signerErr != nil {
		gbroutes.SetPlayAuthorizer(nil)
		if playAuthSettings.Enabled {
			return fmt.Errorf("装配 GB28181 播放鉴权失败: %w", signerErr)
		}
		app.ZapLog.Warn("GB28181 播放鉴权 signer 未装配，播放鉴权保持关闭")
	} else if playSigner != nil {
		playAuthMetrics = playauth.NewMetrics()
		playAuthorization = playauth.NewAuthorizationService(
			playSigner,
			playauth.NewAuthorizationRegistry(),
			playauth.WithAuthorizationMetrics(playAuthMetrics),
		)
		gbroutes.SetPlayAuthorizer(playAuthorization)
		app.ZapLog.Info("GB28181 播放鉴权服务已装配")
	} else {
		gbroutes.SetPlayAuthorizer(nil)
	}

	// 装配点播 service(依赖 SIP UAC + ZLM 客户端 + 流就绪 Notifier)
	if u := srv.UAC(); u != nil {
		if zlmRegistry != nil && zlmScheduler != nil {
			zlmLocationMap = stream.NewLocationMap()
			if trafficRealtime != nil {
				gbroutes.SetDeviceTrafficController(gbcontrollers.NewDeviceTrafficController(app.DB(), trafficRealtime, zlmRegistry, zlmLocationMap))
			}
			// 通道快照 service(播放触发)—— 优先尝试装配,失败/nil 都不影响主链路
			snapshotSvc := buildSnapshotService()
			opts := []play.Option{
				play.WithURLResolver(play.NewURLResolver(zlmServerConfigCache)),
				play.WithDiagnosticSink(diagnosisSinkForServer(srv)),
			}
			if trafficResolver != nil {
				opts = append(opts, play.WithLiveReadyObserver(func(session play.LiveSession) {
					trafficResolver.RegisterLive(traffic.LiveBinding{
						NodeID: session.NodeID, StreamID: session.StreamID, Generation: session.Generation,
						DeviceCode: session.DeviceID, ChannelCode: session.ChannelID,
					})
				}))
			}
			if snapshotSvc != nil {
				opts = append(opts, play.WithSnapshotService(snapshotSvc))
				app.ZapLog.Info("GB28181 通道快照 service 已装配")
			}
			if playAuthorization != nil {
				opts = append(opts, play.WithPlayTokenIssuer(playAuthorization))
			}
			playSvc = play.NewWithScheduler(cfg,
				schedulerPickerAdapter{m: zlmScheduler},
				zlmRegistry,
				zlmLocationMap,
				u, playSessions, gbroutes.StreamNotifier(),
				play.NewDeviceRepo(), play.NewChannelRepo(), opts...)
			if err := restoreWorkGenerationFloor(context.Background(), playSvc); err != nil {
				return fmt.Errorf("恢复录像代际失败: %w", err)
			}
			playSvc.SetSourceCloseGuard(guardCascadeAndRecordingSourceClose)
			playSvc.BeginRecovery()
			recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), 30*time.Second)
			recoveryStats, recoveryErr := playSvc.RecoverLiveSessions(recoveryCtx)
			recoveryCancel()
			if recoveryErr != nil {
				return fmt.Errorf("恢复 GB28181 实时点播会话失败: %w", recoveryErr)
			}
			playSvc.FinishRecovery()
			gbroutes.SetPlayService(playSvc)
			gbroutes.SetStreamMonitorService(streammonitor.NewService(zlmRegistry, zlmLocationMap, nil, time.Now))
			gbroutes.SetStreamProbeService(streamprobe.NewService(zlmRegistry, zlmLocationMap, nil, 5*time.Second, time.Now))
			gbroutes.SetHookMultiNode(zlmRegistry, zlmLocationMap)
			app.ZapLog.Info("GB28181 点播 service 已装配(多节点 + scheduler)",
				zap.Int("recoveryScanned", recoveryStats.Scanned),
				zap.Int("recoveryRestored", recoveryStats.Restored),
				zap.Int("recoveryCleaned", recoveryStats.Cleaned),
				zap.Int("recoverySkipped", recoveryStats.Skipped),
				zap.Int("recoveryFailed", recoveryStats.Failed))
		} else {
			opts := []play.Option{play.WithDiagnosticSink(diagnosisSinkForServer(srv))}
			if playAuthorization != nil {
				opts = append(opts, play.WithPlayTokenIssuer(playAuthorization))
			}
			playSvc = play.New(cfg, zlmClient, u, playSessions, gbroutes.StreamNotifier(),
				play.NewDeviceRepo(), play.NewChannelRepo(), opts...)
			if err := restoreWorkGenerationFloor(context.Background(), playSvc); err != nil {
				return fmt.Errorf("恢复录像代际失败: %w", err)
			}
			playSvc.SetSourceCloseGuard(guardCascadeAndRecordingSourceClose)
			gbroutes.SetPlayService(playSvc)
			app.ZapLog.Info("GB28181 点播 service 已装配(单节点 deprecated;通道快照仅多节点路径启用)")
		}
	} else {
		app.ZapLog.Warn("GB28181 UAC 不可用,点播 service 跳过装配")
	}
	if err := setupCascadeVideoRuntime(srv); err != nil {
		return fmt.Errorf("装配级联点播失败: %w", err)
	}
	setupPlaybackRuntime(cfg, srv.UAC())
	setupTalkRuntime(cfg, srv)
	setupRecordingRuntime(cfg)
	installZLMManagementController()

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

func buildPlaySigner(settings gbconfig.PlayAuthSettings, active, previous, jwtSecret, zlmSecret string) (*playauth.Signer, error) {
	active = strings.TrimSpace(active)
	previous = strings.TrimSpace(previous)
	if active == "" && !settings.Enabled {
		return nil, nil
	}
	if active == "" {
		return nil, playauth.ErrKeyInvalid
	}
	for _, reused := range []string{strings.TrimSpace(jwtSecret), strings.TrimSpace(zlmSecret)} {
		if reused != "" && active == reused {
			return nil, fmt.Errorf("%w: active key must not reuse another application secret", playauth.ErrKeyInvalid)
		}
		if previous != "" && reused != "" && previous == reused {
			return nil, fmt.Errorf("%w: previous key must not reuse another application secret", playauth.ErrKeyInvalid)
		}
	}
	activeKey := playauth.KeyMaterial{Secret: []byte(active)}
	var signer *playauth.Signer
	var err error
	if previous == "" {
		signer, err = playauth.NewKeyring(activeKey, nil)
	} else {
		previousKey := playauth.KeyMaterial{Secret: []byte(previous)}
		signer, err = playauth.NewKeyring(activeKey, &previousKey)
	}
	if err != nil {
		return nil, err
	}
	ttlSeconds := settings.TTLSeconds
	if ttlSeconds == 0 {
		ttlSeconds = gbconfig.DefaultPlayAuthTTLSeconds
	}
	if err := signer.SetTTL(time.Duration(ttlSeconds) * time.Second); err != nil {
		return nil, err
	}
	return signer, nil
}

// stopSIPDependencies 反向拆解 startSIPDependencies 建立的运行时状态.
// 用于配置热重启 —— 供 Reload 调用,不涉及 control plane 组件.
func stopSIPDependencies(ctx context.Context) {
	// Remove the facade before stopping any dependency it can call. Reload
	// installs a fresh bundle only after all new business runtimes are ready.
	clearZLMManagementController()
	stopPlaybackRuntime(ctx)
	stopRecordQueryRuntime()
	stopFirmwareUpgradeRuntime()
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
	playAuthMetrics = nil
	gbroutes.SetPlayService(nil)
	gbroutes.SetPlayAuthorizer(nil)
	gbroutes.SetDeviceMgmtCatalogTrigger(nil)
	gbroutes.SetDeviceMgmtSubscriptionManager(nil)
	gbroutes.SetDeviceMgmtCaptureRuntime(nil)
	deviceCaptureRegistry = nil
	if sipServer != nil {
		sipServer.SetSnapshotSink(nil)
	}
	stopCascadeRuntime(ctx)
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
	playbackRegistry = nil
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
		playbackRegistry = nil
		playbackMetrics = nil
		app.ZapLog.Info("GB28181 设备录像回放 service 跳过装配(UAC/RecordInfo/ZLM 依赖未就绪)")
		return
	}
	registry := gbplayback.NewRegistry(gbplayback.RegistryConfig{
		IdleTimeout: cfg.Playback.IdleTimeout(),
		MaxSession:  cfg.Playback.MaxSession(),
	})
	playbackRegistry = registry
	playbackMetrics = &gbplayback.Metrics{}
	service := gbplayback.NewService(
		registry,
		gbplayback.NewZLMNodePicker(zlmScheduler, cfg.SIP.ServerID),
		gbplayback.NewZLMRTPOpener(zlmRegistry, zlmLocationMap, nil),
		uac.NewPlaybackAdapter(inviter),
		gbplayback.NewZLMMediaWaiter(zlmRegistry, zlmLocationMap, gbroutes.StreamNotifier(), zlmServerConfigCache, nil),
		gbplayback.ServiceConfig{ServerID: cfg.SIP.ServerID, MediaWait: cfg.Playback.MediaWait(), Metrics: playbackMetrics},
	)
	if trafficResolver != nil {
		service.SetMediaReadyObserver(func(session gbplayback.Session) {
			nodeID, _ := strconv.ParseInt(session.NodeID, 10, 64)
			trafficResolver.RegisterPlayback(traffic.PlaybackBinding{
				NodeID: nodeID, StreamID: session.StreamID, SessionID: session.ID,
				DeviceCode: session.DeviceID, ChannelCode: session.ChannelID, MediaKind: traffic.MediaKindPlayback,
			})
		})
	}
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

func stopFirmwareUpgradeRuntime() {
	oldService := firmwareUpgradeService
	// Detach inbound routing and the HTTP facade first. Once detached, Retire
	// can safely drain the old generation without admitting new work.
	if sipServer != nil {
		if setter, ok := sipServer.(interface {
			SetUpgradeProcessor(gbhandler.UpgradeMessageProcessor)
		}); ok {
			setter.SetUpgradeProcessor(nil)
		}
	}
	gbroutes.SetDeviceMgmtFirmwareUpgradeService(nil)
	if oldService != nil {
		oldService.Retire()
	}
	firmwareUpgradeService = nil
}

func setupRecordingRuntime(cfg gbconfig.Config) {
	if playSvc == nil || zlmRegistry == nil || zlmLocationMap == nil {
		recordingRepo = nil
		recordingSvc = nil
		gbroutes.SetRecordingService(nil, nil, nil)
		gbroutes.SetWorkRecordingController(nil)
		app.ZapLog.Info("GB28181 云端录像 service 跳过装配(play/registry/locationMap 未就绪)")
		return
	}
	repo := gbrecording.NewGormRepo(app.DB())
	recordingRepo = repo
	workRecorder := newWorkRecorder(playSvc)
	workLeases := play.NewSourceLeaseRegistry()
	workService := workrecording.NewService(app.DB(), workRecorder, prepareWorkRecording(playSvc, workLeases))
	gbroutes.SetWorkRecordingSourceLeaseChecker(gbhandler.CombinedSourceLeaseChecker{workLeases, persistedWorkLeases{}})
	workController := gbcontrollers.NewWorkRecordingController(workService)
	workLedger := workrecording.NewBatchService(app.DB(), workService)
	// The ledger row is the single source of truth for a work order's state, so
	// the engine notifies it on every child transition instead of letting each
	// reader re-derive the aggregate.
	workService.SetStateObserver(workLedger.ObserveJobState)
	workController.SetBatchService(workLedger)
	// Work recordings are written on the ZLM node that produced them, so every
	// download has to be served by that node: reading the stored path from this
	// process only works when the record directory happens to be shared, which a
	// remote node never is.
	workController.SetFileSourceFactory(func(nodeID int64) (gbcontrollers.WorkRecordingFileSource, bool) {
		if zlmRegistry == nil {
			return nil, false
		}
		n, ok := zlmRegistry.Get(nodeID)
		if !ok || n == nil {
			return nil, false
		}
		return gbzlm.NewClientForNode(n), true
	})
	gbroutes.SetWorkRecordingController(workController)
	// A start that fails after reserving the channel but before binding media
	// leaves the claim in starting, which every reader treats as live. Sweep
	// those once they are provably past the start deadline.
	stopWorkClaimReconciler()
	workClaimReconciler = workrecording.NewAbandonedStartReconciler(workService, workrecording.AbandonedStartInterval, workrecording.AbandonedStartGrace)
	if workClaimReconciler != nil {
		workClaimReconciler.Start(context.Background())
	}
	recordingSvc = gbrecording.NewService(repo, zlmLocationMap, zlmRegistry,
		func(n *node.Node) gbrecording.RecorderClient {
			return gbrecording.GuardRecorderClient(gbzlm.NewClientForNode(n), recordingMutationGate.Load(), n.ID, repo.FindChannelByStream)
		})
	recordingSvc.SetOperationGuard(guardLegacyRecordingOperation)
	indexer := gbrecording.NewFileIndexer(repo, zlmLocationMap)
	gbroutes.SetRecordingService(recordingSvc, zlmRegistry, gbrecording.NewWorkFileIndexer(app.DB(), indexer))
	recordingPlanLeases = play.NewSourceLeaseRegistry()
	recordingPlanOrchestrator := recordingplan.NewOrchestrator(playSvc, recordingSvc, recordingPlanLeases, nil)
	planEnabled := cfg.Recording.PlanEnabled
	recordingPlanEngine = recordingplan.NewEngine(app.DB(), recordingPlanOrchestrator, recordingplan.EngineOptions{
		InstanceID: uuid.NewString(), BatchSize: 200, Workers: 8, DeviceConcurrency: 2,
		BoundaryJitter: 5 * time.Second, LeaseTTL: 15 * time.Second, Enabled: &planEnabled,
	})
	recordingPlanEngine.SetLiveCurrentProvider(playSvc)
	executors.SetRecordingPlanRuntime(recordingPlanEngine)
	device.SetStatusObserver(recordingPlanEngine)
	gbroutes.SetRecordingPlanSourceLeaseChecker(recordingPlanLeases)
	gbroutes.SetRecordingPlanStreamObserver(recordingPlanEngine)
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
	jwtRootKey := strings.TrimSpace(app.ConfigYml.GetString("token.jwttokensignkey"))
	capabilityKey := strings.TrimSpace(os.Getenv(strings.TrimSpace(cfg.Recording.CapabilityKeyEnv)))
	explicitCapabilityKey := capabilityKey != ""
	keySource := "应用根密钥派生"
	if !explicitCapabilityKey {
		derivedKey, err := gbrecording.DeriveCapabilityKey([]byte(jwtRootKey))
		if err != nil {
			app.ZapLog.Warn("GB28181 云端录像 capability 密钥无法派生(JWT 根密钥为空)")
		} else {
			capabilityKey = string(derivedKey)
		}
	} else {
		keySource = "环境变量"
	}
	keyReused := explicitCapabilityKey && (capabilityKey == jwtRootKey || capabilityKey == cfg.ZLM.Secret)
	if !keyReused && explicitCapabilityKey {
		for _, n := range zlmRegistry.List() {
			if capabilityKey == n.APISecret {
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
		app.ZapLog.Info("GB28181 云端录像 capability signer 已装配", zap.String("keySource", keySource))
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
		NewDownloader:    func(n *node.Node) gbrecording.ContentDownloader { return gbzlm.NewClientForNode(n) },
		NewFileClient:    func(n *node.Node) gbrecording.CatalogFileClient { return gbzlm.NewClientForNode(n) },
		RecordingStopper: recordingSvc,
		Downloads:        gbrecording.NewDownloadRegistry(gbrecording.DownloadRegistryConfig{}),
	})
	recordingCatalogService = catalogService
	gbroutes.SetCloudRecordingCatalogService(catalogService)
	app.ZapLog.Info("GB28181 云端录像目录对账已装配", zap.Duration("interval", catalogInterval))
}

// stopWorkClaimReconciler stops the abandoned-start sweeper if it is running.
// Safe to call when it was never started (disabled) and on hot reload.
func stopWorkClaimReconciler() {
	if workClaimReconciler == nil {
		return
	}
	workClaimReconciler.Stop()
	workClaimReconciler = nil
}

func stopRecordingRuntime() {
	stopWorkClaimReconciler()
	device.SetStatusObserver(nil)
	gbroutes.SetRecordingPlanStreamObserver(nil)
	executors.SetRecordingPlanRuntime(nil)
	gbroutes.SetRecordingPlanSourceLeaseChecker(nil)
	recordingPlanEngine = nil
	recordingPlanLeases = nil
	if recordingCatalogService != nil {
		recordingCatalogService.CloseDownloads()
		recordingCatalogService = nil
	}
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
	recordingRepo = nil
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
		talkRepo = nil
		gbroutes.SetTalkService(nil, nil)
		app.ZapLog.Info("GB28181 语音对讲 service 跳过装配(依赖未就绪)")
		return
	}
	if !app.DB().Migrator().HasTable(&gbmodels.GbTalkSession{}) {
		talkRepo = nil
		gbroutes.SetTalkService(nil, nil)
		app.ZapLog.Warn("GB28181 语音对讲表未迁移,service 跳过装配")
		return
	}
	repo := gbtalk.NewGormRepo(app.DB())
	service := gbtalk.NewService(
		repo, zlmRegistry, zlmLocationMap,
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
	talkRepo = repo
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
	talkRepo = nil
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
	if err := gbconfig.EnsurePlayAuthActiveKey(app.ConfigYml); err != nil {
		if gbconfig.CurrentPlayAuthSettings().Enabled {
			sipRuntimeStatus.MarkFailed(err.Error())
			return fmt.Errorf("初始化播放鉴权密钥失败: %w", err)
		}
		app.ZapLog.Warn("GB28181 播放鉴权密钥初始化失败,鉴权保持关闭", zap.Error(err))
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
	gbroutes.SetHookAuthResolver(reg)
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

func setupTrafficRuntime() {
	gbroutes.SetFlowRuntime(nil, nil)
	trafficResolver = nil
	trafficRealtime = nil
	if trafficCancel != nil {
		trafficCancel()
		trafficCancel = nil
	}
	db := app.DB()
	if db == nil || zlmRegistry == nil {
		app.ZapLog.Info("GB28181 流量统计跳过装配(DB/ZLM Registry 未就绪)")
		return
	}
	for _, model := range []interface{}{
		&gbmodels.GbDeviceTrafficSession{}, &gbmodels.GbDeviceTrafficDaily{}, &gbmodels.GbDeviceTrafficHourly{}, &gbmodels.GbDeviceTrafficGap{},
	} {
		if !db.Migrator().HasTable(model) {
			app.ZapLog.Warn("GB28181 流量统计表未迁移,运行时跳过装配")
			return
		}
	}
	repo, err := traffic.NewGormRepository(db)
	if err != nil {
		app.ZapLog.Warn("GB28181 流量统计仓储装配失败", zap.Error(err))
		return
	}
	resolver := traffic.NewAttributionResolver()
	realtime := traffic.NewRealtimeStore(2 * time.Minute)
	flow := traffic.NewFlowService(repo, resolver, zlmRegistry, time.Now)
	sam := traffic.NewSampler(zlmRegistry, nil, repo, resolver, realtime, time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	trafficCancel = cancel
	trafficResolver = resolver
	trafficRealtime = realtime
	gbroutes.SetFlowRuntime(zlmRegistry, flow)
	gbroutes.SetDeviceTrafficController(gbcontrollers.NewDeviceTrafficController(db, realtime, zlmRegistry, zlmLocationMap))
	sam.Start(ctx, time.Minute)
	traffic.StartSessionPruner(ctx, repo, time.Now, func(err error) {
		app.ZapLog.Warn("GB28181 终态流量会话清理失败", zap.Error(err))
	})
	app.ZapLog.Info("GB28181 流量统计 Hook / 采样器已装配", zap.Duration("interval", time.Minute))
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
	teardownZLMManagementCore()
	if trafficCancel != nil {
		trafficCancel()
		trafficCancel = nil
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
	if metricsPersistCancel != nil {
		metricsPersistCancel()
		metricsPersistCancel = nil
	}
	if dashboardRetentionCancel != nil {
		dashboardRetentionCancel()
		dashboardRetentionCancel = nil
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
	buildStreamURL := func(ctx context.Context, nodeID, streamID, playToken string) (string, error) {
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
		return snapshotStreamURL(cfg.RTSPPort, streamID, playToken), nil
	}

	svc := snapshot.New(snapshot.Config{
		UploadRoot:     serverroot,
		URLPrefix:      serverrootpath,
		DelayBefore:    2 * time.Second,
		ZLMTimeout:     5,
		ZLMExpire:      1,
		GetClient:      getClient,
		BuildStreamURL: buildStreamURL,
		Repo:           snapshot.NewGormRepo(app.DB()),
		Logger:         app.ZapLog.Named("gb.snapshot"),
	})
	return svc
}

func snapshotStreamURL(rtspPort int, streamID, playToken string) string {
	streamURL := url.URL{
		Scheme: "rtsp",
		Host:   fmt.Sprintf("127.0.0.1:%d", rtspPort),
		Path:   "/rtp/" + streamID,
	}
	if playToken != "" {
		query := streamURL.Query()
		query.Set(playauth.QueryParameter, playToken)
		streamURL.RawQuery = query.Encode()
	}
	return streamURL.String()
}
