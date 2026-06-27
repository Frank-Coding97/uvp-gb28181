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
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	gbzlm "uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	gbzlmrepo "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
	gbzlmsched "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
	gbzlmsvc "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

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

// playSvc 全局点播 service(routes 用 GetPlayService 取)
var playSvc *play.Service

// playSessions 全局点播会话管理(让 hook 端点 on_stream_none_reader/on_rtp_server_timeout 也能查到)
var playSessions = uac.NewSessionManager()

// metricsAgg 全局指标聚合器(供 controllers/dashboard 暴露,供 SIP 路径埋点)
var metricsAgg *metrics.Aggregator

// metricsCleanupStop 控制 TTL 清理 goroutine 退出
var metricsCleanupStop chan struct{}

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
	if u := srv.UAC(); u != nil {
		playSvc = play.New(cfg, zlmClient, u, playSessions, gbroutes.StreamNotifier(),
			play.NewDeviceRepo(), play.NewChannelRepo())
		gbroutes.SetPlayService(playSvc)
		app.ZapLog.Info("GB28181 点播 service 已装配")
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
