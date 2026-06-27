package gb28181

import (
	"context"
	"time"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	gbroutes "uvplatform.cn/uvp-gb28181/app/gb28181/routes"
	gbsip "uvplatform.cn/uvp-gb28181/app/gb28181/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	gbzlm "uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/heartbeat"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	gbzlmrepo "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
	gbzlmsched "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
	gbzlmsvc "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
	"uvplatform.cn/uvp-gb28181/app/global/app"

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

// sipServer 持有全局 SIP 服务实例,供优雅关闭引用
var sipServer *gbsip.Server

// offlineScanner 离线扫描器
var offlineScanner *device.OfflineScanner

// zlmClient 全局 ZLM 客户端(deprecated M3 TF.2 删,M1 单节点过渡期保留)
var zlmClient *gbzlm.Client

// zlmRegistry 节点注册表(M1 新增)
var zlmRegistry *node.Registry

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

// zlmSchedulerLog 调度日志服务(T3.3 新增,可为 nil 降级)
var zlmSchedulerLog *gbzlmsched.LogService

// schedulerLogCancel 控制调度日志 worker + prune ticker 退出
var schedulerLogCancel context.CancelFunc

// PlayService 返回点播 service(可能为 nil,gb28181 未启用 / UAC 初始化失败时)
func PlayService() *play.Service { return playSvc }

// MetricsAggregator 返回全局聚合器(controllers/dashboard 用)
func MetricsAggregator() *metrics.Aggregator { return metricsAgg }

// ZLMRegistry 返回全局节点注册表(M1 新增,供 controllers / test 用)
func ZLMRegistry() *node.Registry { return zlmRegistry }

// ZLMScheduler 返回全局调度器 Manager(M2 新增,供 SIP play 改造 / test 用)
// 可能为 nil:DB 不可达或 Switch 全部失败时
func ZLMScheduler() *gbzlmsched.Manager { return zlmScheduler }

// Start 启动 GB28181 SIP 服务 + 离线扫描器(在 HTTP 服务阻塞等待信号之前调用)
// 若 gb28181.enabled=false 则跳过
func Start() {
	cfg := gbconfig.Load()
	if !cfg.Enabled {
		app.ZapLog.Info("GB28181 未启用,跳过 SIP 服务启动")
		return
	}

	// 先建聚合器,handler/UAC 拿到它做埋点
	metricsAgg = metrics.NewAggregator()
	metricsCleanupStop = make(chan struct{})
	go runMetricsCleanup(metricsAgg, metricsCleanupStop)

	// 把 provider 注入到 routes,让 dashboard controller 能拿到聚合器
	gbroutes.SetMetricsProvider(func() *metrics.Aggregator { return metricsAgg })

	srv, err := gbsip.NewServer(cfg)
	if err != nil {
		app.ZapLog.Error("GB28181 SIP 服务创建失败", zap.Error(err))
		return
	}
	srv.SetRecorder(metricsAgg)
	if err := srv.Start(); err != nil {
		app.ZapLog.Error("GB28181 SIP 服务启动失败", zap.Error(err))
		return
	}
	sipServer = srv

	// 启动离线扫描器(基于 keepalive_time 事实派生)
	offlineScanner = device.NewOfflineScanner(
		cfg.Device.OfflineScanInterval,
		cfg.Device.KeepaliveTimeoutCount,
		cfg.Device.KeepaliveGraceSeconds,
	)
	offlineScanner.Start()
	app.ZapLog.Info("GB28181 离线扫描器已启动", zap.Int("intervalSeconds", cfg.Device.OfflineScanInterval))

	// 装配 ZLM 多节点 Registry(M1 新增)
	// 启动若 meta_node 空表 → 用 yaml cfg.ZLM 自动 seed 第一节点(单节点过渡)
	setupZLMRegistry(cfg)

	// 装配 ZLM Scheduler(M2 新增)从 scheduler_setting 读 algorithm
	setupZLMScheduler()

	// 装配 ZLM 调度日志服务(M3 T3.3)异步采集 + 24h prune
	setupZLMSchedulerLog()

	// 装配 ZLM Scheduler Controller(M3 T3.3)算法切换 + 日志查询
	setupZLMSchedulerController()

	// 装配 ZLM 节点 CRUD / 配置 controller(M1 新增)
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

		// 装配心跳 Collector + Watcher(M2.1)
		// Collector 接收 on_server_keepalive Hook,Watcher 周期扫描超时节点
		collector := heartbeat.NewCollector(zlmRegistry)
		gbroutes.SetKeepaliveCollector(collector)

		watcher := heartbeat.NewWatcher(zlmRegistry, heartbeat.RealClock(),
			30*time.Second, // checkInterval
			90*time.Second, // offlineThreshold = 3 个 30s 心跳
		)
		var hbCtx context.Context
		hbCtx, heartbeatCancel = context.WithCancel(context.Background())
		watcher.Start(hbCtx)
		app.ZapLog.Info("GB28181 ZLM 心跳 Collector / Watcher 已启动",
			zap.Duration("checkInterval", 30*time.Second),
			zap.Duration("offlineThreshold", 90*time.Second))
	}

	// 向 ZLMediaKit 动态下发 Hook 配置(控制面,替代 config.ini 写死)
	// M1 单节点过渡:取 Registry 首节点;若 Registry 空(seed 失败/db 不可达)走 yaml fallback
	zlmClient = pickInitialClient(cfg)
	go func(client *gbzlm.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.ApplyConfigForNode(ctx, cfg.Media); err != nil {
			app.ZapLog.Warn("GB28181 ZLM 配置下发失败(ZLM 可能暂不可达,不影响启动)", zap.Error(err))
		} else {
			app.ZapLog.Info("GB28181 ZLM Hook 配置已下发", zap.String("hookHost", cfg.Media.HookHost))
		}
	}(zlmClient)

	// 装配点播 service(依赖 SIP UAC + ZLM 客户端 + 流就绪 Notifier)
	//
	// 多节点路径:有 zlmRegistry + zlmScheduler → NewWithScheduler,流分配给 scheduler.Pick 出的节点
	// 单节点路径(deprecated):走 play.New(cfg, zlmClient),M3 TF.2 删
	if u := srv.UAC(); u != nil {
		if zlmRegistry != nil && zlmScheduler != nil {
			zlmLocationMap = stream.NewLocationMap()
			playSvc = play.NewWithScheduler(cfg,
				schedulerPickerAdapter{m: zlmScheduler},
				zlmRegistry,
				zlmLocationMap,
				u, playSessions, gbroutes.StreamNotifier(),
				play.NewDeviceRepo(), play.NewChannelRepo())
			gbroutes.SetPlayService(playSvc)
			// hook 端点 OnStreamChanged 反向 Bind 兜底
			gbroutes.SetHookMultiNode(zlmRegistry, zlmLocationMap)
			app.ZapLog.Info("GB28181 点播 service 已装配(多节点 + scheduler)")
		} else {
			playSvc = play.New(cfg, zlmClient, u, playSessions, gbroutes.StreamNotifier(),
				play.NewDeviceRepo(), play.NewChannelRepo())
			gbroutes.SetPlayService(playSvc)
			app.ZapLog.Info("GB28181 点播 service 已装配(单节点 deprecated)")
		}
	} else {
		app.ZapLog.Warn("GB28181 UAC 不可用,点播 service 跳过装配")
	}
}

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
	app.ZapLog.Info("GB28181 ZLM Registry 已装配", zap.Int("nodes", len(reg.List())))
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

// pickInitialClient M1 单节点过渡期:优先取 Registry 首节点,失败 fallback yaml
func pickInitialClient(cfg gbconfig.Config) *gbzlm.Client {
	if zlmRegistry != nil {
		if list := zlmRegistry.List(); len(list) > 0 {
			return gbzlm.NewClientForNode(list[0])
		}
	}
	// Deprecated: yaml fallback,M3 TF.2 删
	return gbzlm.NewClient(cfg.ZLM)
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

// Stop 优雅关闭 GB28181 SIP 服务 + 离线扫描器(纳入主进程退出流程)
func Stop() {
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
	if offlineScanner != nil {
		offlineScanner.Stop()
	}
	if sipServer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sipServer.Shutdown(ctx); err != nil {
		app.ZapLog.Error("GB28181 SIP 服务关闭异常", zap.Error(err))
	}
}
